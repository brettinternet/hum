//go:build windows

// Package process starts and supervises one direct child process.
package process

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
	"unsafe"

	"hum/internal/output"

	"golang.org/x/sys/windows"
)

const (
	processGroupPollInterval = 10 * time.Millisecond
	captureDrainTimeout      = 100 * time.Millisecond
	captureHardTimeout       = time.Second
	defaultIdleFlush         = 100 * time.Millisecond
	windowsStopExitCode      = 1
	// PROC_THREAD_ATTRIBUTE_JOB_LIST is available on Windows 10 version 1709
	// and later. Assigning the job during process creation prevents a newly
	// launched process from creating descendants before it is contained.
	procThreadAttributeJobList = 0x0002000d
)

// Spec describes one child process launch.
//
// Argv is passed directly to CreateProcess without a shell. Env is copied
// exactly and nil or empty starts the child with no environment variables.
// IdleFlush controls how long an unterminated output fragment may remain
// buffered; zero selects a short default. Windows process capture is
// non-interactive; TTY options are rejected.
type Spec struct {
	Dir          string
	Argv         []string
	Env          []string
	Output       *output.Store
	MaxLineBytes int
	IdleFlush    time.Duration
	Now          func() time.Time
	TTY          bool
	TTYSize      *TTYSize
	Started      func() error
}

// TTYSize is a pseudo-terminal size in character cells.
type TTYSize struct {
	Columns uint16
	Rows    uint16
}

// SignalInfo identifies the signal that terminated a child. Windows does not
// report Unix signals, so Windows results always have a nil Signal.
type SignalInfo struct {
	Name   string
	Number int
}

// Signal is a concise alias for SignalInfo.
type Signal = SignalInfo

// Result is the immutable terminal status of a child.
type Result struct {
	ExitCode int
	Signal   *SignalInfo
	Err      error
	ExitedAt time.Time
}

// Child is one started process and the private Job Object that owns its
// descendants.
type Child struct {
	pid             int
	pgid            int
	startIdentity   string
	processHandle   windows.Handle
	jobHandle       windows.Handle
	launchJobHandle windows.Handle // original handle value; detect substitution
	ownedJobHandle  windows.Handle // retained ownership handle used for all job operations

	output       *output.Store
	maxLineBytes int
	idleFlush    time.Duration
	now          func() time.Time

	done          chan struct{}
	leaderDone    chan struct{}
	groupGone     chan struct{}
	mu            sync.Mutex
	groupEnded    bool
	groupEndedAt  time.Time
	stopRequested bool
	res           Result
}

// Start launches the command described by spec with exact arguments and
// environment. The child is assigned to its private Job Object before its
// primary thread is resumed, so descendants cannot race out of ownership.
func Start(spec Spec) (*Child, error) {
	if len(spec.Argv) == 0 {
		return nil, errors.New("process: argv must not be empty")
	}
	if spec.Output == nil {
		return nil, errors.New("process: output store is required")
	}
	if spec.MaxLineBytes <= 0 {
		return nil, errors.New("process: max line bytes must be positive")
	}
	if spec.IdleFlush < 0 {
		return nil, errors.New("process: idle flush must not be negative")
	}
	if spec.TTY || spec.TTYSize != nil {
		return nil, errors.New("process: tty mode is unsupported on Windows")
	}

	argv := cloneStrings(spec.Argv)
	env := cloneStrings(spec.Env)
	if env == nil {
		env = []string{}
	}
	resolvedPath, err := ResolveExecutable(argv[0], env, spec.Dir)
	if err != nil {
		return nil, fmt.Errorf("process: resolve %q: %w", argv[0], err)
	}
	envBlock, err := windowsEnvironmentBlock(env)
	if err != nil {
		return nil, fmt.Errorf("process: build environment: %w", err)
	}
	commandLine, err := windowsCommandLine(argv)
	if err != nil {
		return nil, fmt.Errorf("process: build command line: %w", err)
	}

	var currentDir *uint16
	if spec.Dir != "" {
		dir, absErr := filepath.Abs(spec.Dir)
		if absErr != nil {
			return nil, fmt.Errorf("process: resolve working directory: %w", absErr)
		}
		currentDir, err = windows.UTF16PtrFromString(dir)
		if err != nil {
			return nil, fmt.Errorf("process: encode working directory: %w", err)
		}
	}
	applicationName, err := windows.UTF16PtrFromString(resolvedPath)
	if err != nil {
		return nil, fmt.Errorf("process: encode executable path: %w", err)
	}
	// CreateProcess receives the resolved image path separately; the command
	// line deliberately keeps the caller's argv[0] and arguments unchanged.
	commandLinePtr, err := windows.UTF16PtrFromString(commandLine)
	if err != nil {
		return nil, fmt.Errorf("process: encode command line: %w", err)
	}

	var job, ownedJob, processHandle, threadHandle windows.Handle
	var stdinHandle, stdoutRead, stdoutWrite, stderrRead, stderrWrite windows.Handle
	var stdoutFile, stderrFile *os.File
	defer func() {
		for _, handle := range []windows.Handle{stdinHandle, stdoutRead, stdoutWrite, stderrRead, stderrWrite, threadHandle, processHandle, job, ownedJob} {
			if handle != 0 {
				_ = windows.CloseHandle(handle)
			}
		}
	}()

	job, err = windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("process: create job object: %w", err)
	}
	self := windows.CurrentProcess()
	if err := windows.DuplicateHandle(self, job, self, &ownedJob, 0, false, windows.DUPLICATE_SAME_ACCESS); err != nil {
		return nil, fmt.Errorf("process: retain job ownership proof: %w", err)
	}
	jobLimits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&jobLimits)),
		uint32(unsafe.Sizeof(jobLimits)),
	); err != nil {
		return nil, fmt.Errorf("process: configure job object: %w", err)
	}

	security := windows.SecurityAttributes{
		Length:        uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		InheritHandle: 1,
	}
	stdinHandle, err = windows.CreateFile(
		windows.StringToUTF16Ptr("NUL"),
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		&security,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return nil, fmt.Errorf("process: open null stdin: %w", err)
	}
	if err := windows.CreatePipe(&stdoutRead, &stdoutWrite, &security, 0); err != nil {
		return nil, fmt.Errorf("process: create stdout pipe: %w", err)
	}
	if err := windows.SetHandleInformation(stdoutRead, windows.HANDLE_FLAG_INHERIT, 0); err != nil {
		return nil, fmt.Errorf("process: protect stdout reader: %w", err)
	}
	if err := windows.CreatePipe(&stderrRead, &stderrWrite, &security, 0); err != nil {
		return nil, fmt.Errorf("process: create stderr pipe: %w", err)
	}
	if err := windows.SetHandleInformation(stderrRead, windows.HANDLE_FLAG_INHERIT, 0); err != nil {
		return nil, fmt.Errorf("process: protect stderr reader: %w", err)
	}

	attributes, err := windows.NewProcThreadAttributeList(2)
	if err != nil {
		return nil, fmt.Errorf("process: create process attributes: %w", err)
	}
	defer attributes.Delete()
	jobHandles := []windows.Handle{job}
	if err := attributes.Update(
		procThreadAttributeJobList,
		unsafe.Pointer(&jobHandles[0]),
		unsafe.Sizeof(jobHandles[0]),
	); err != nil {
		return nil, fmt.Errorf("process: configure child job ownership: %w", err)
	}
	inheritedHandles := []windows.Handle{stdinHandle, stdoutWrite, stderrWrite}
	if err := attributes.Update(
		windows.PROC_THREAD_ATTRIBUTE_HANDLE_LIST,
		unsafe.Pointer(&inheritedHandles[0]),
		unsafe.Sizeof(inheritedHandles[0])*uintptr(len(inheritedHandles)),
	); err != nil {
		return nil, fmt.Errorf("process: configure inherited handles: %w", err)
	}
	startup := windows.StartupInfoEx{
		StartupInfo: windows.StartupInfo{
			Flags:     windows.STARTF_USESTDHANDLES,
			StdInput:  stdinHandle,
			StdOutput: stdoutWrite,
			StdErr:    stderrWrite,
		},
		ProcThreadAttributeList: attributes.List(),
	}
	startup.StartupInfo.Cb = uint32(unsafe.Sizeof(startup))
	var processInfo windows.ProcessInformation
	flags := uint32(windows.CREATE_SUSPENDED | windows.CREATE_UNICODE_ENVIRONMENT | windows.EXTENDED_STARTUPINFO_PRESENT)
	if err := windows.CreateProcess(
		applicationName,
		commandLinePtr,
		nil,
		nil,
		true,
		flags,
		&envBlock[0],
		currentDir,
		&startup.StartupInfo,
		&processInfo,
	); err != nil {
		return nil, fmt.Errorf("process: start %q: %w", argv[0], err)
	}
	processHandle, threadHandle = processInfo.Process, processInfo.Thread
	_ = windows.CloseHandle(stdinHandle)
	stdinHandle = 0
	_ = windows.CloseHandle(stdoutWrite)
	stdoutWrite = 0
	_ = windows.CloseHandle(stderrWrite)
	stderrWrite = 0

	startIdentity, err := processHandleIdentity(processHandle)
	if err != nil {
		_ = windows.TerminateJobObject(job, windowsStopExitCode)
		_, _ = windows.WaitForSingleObject(processHandle, windows.INFINITE)
		return nil, fmt.Errorf("process: read start identity: %w", err)
	}
	if _, err := windows.ResumeThread(threadHandle); err != nil {
		_ = windows.TerminateJobObject(job, windowsStopExitCode)
		_, _ = windows.WaitForSingleObject(processHandle, windows.INFINITE)
		return nil, fmt.Errorf("process: resume child: %w", err)
	}
	_ = windows.CloseHandle(threadHandle)
	threadHandle = 0

	stdoutFile = os.NewFile(uintptr(stdoutRead), "process-stdout")
	stdoutRead = 0
	stderrFile = os.NewFile(uintptr(stderrRead), "process-stderr")
	stderrRead = 0
	if stdoutFile == nil || stderrFile == nil {
		if stdoutFile != nil {
			_ = stdoutFile.Close()
		}
		if stderrFile != nil {
			_ = stderrFile.Close()
		}
		_ = windows.TerminateJobObject(job, windowsStopExitCode)
		_, _ = windows.WaitForSingleObject(processHandle, windows.INFINITE)
		return nil, errors.New("process: wrap capture pipe")
	}

	now := spec.Now
	if now == nil {
		now = time.Now
	}
	idleFlush := spec.IdleFlush
	if idleFlush == 0 {
		idleFlush = defaultIdleFlush
	}
	child := &Child{
		pid:             int(processInfo.ProcessId),
		pgid:            int(processInfo.ProcessId),
		startIdentity:   startIdentity,
		processHandle:   processHandle,
		jobHandle:       job,
		launchJobHandle: job,
		ownedJobHandle:  ownedJob,
		output:          spec.Output,
		maxLineBytes:    spec.MaxLineBytes,
		idleFlush:       idleFlush,
		now:             now,
		done:            make(chan struct{}),
		leaderDone:      make(chan struct{}),
		groupGone:       make(chan struct{}),
	}
	if spec.Started != nil {
		if err := spec.Started(); err != nil {
			_ = windows.TerminateJobObject(job, windowsStopExitCode)
			_, _ = windows.WaitForSingleObject(processHandle, windows.INFINITE)
			_ = stdoutFile.Close()
			_ = stderrFile.Close()
			return nil, fmt.Errorf("process: started callback: %w", err)
		}
	}

	go child.observeJobExit()
	go child.run(stdoutFile, stderrFile)
	job = 0
	ownedJob = 0
	processHandle = 0
	stdoutFile = nil
	stderrFile = nil
	return child, nil
}

// PID reports the operating-system process identifier.
func (c *Child) PID() int {
	return c.pid
}

// PGID reports the root PID used as the Windows lifecycle-group identifier.
// Windows process trees are owned through the child's Job Object, not a POSIX
// process group.
func (c *Child) PGID() int {
	return c.pgid
}

// StartIdentity returns the process creation-time identity captured while the
// root was still suspended.
func (c *Child) StartIdentity() string {
	if c == nil {
		return ""
	}
	return c.startIdentity
}

// ProcessStartIdentity reads the host process-start identity for pid.
func ProcessStartIdentity(pid int) (string, error) {
	if pid <= 0 {
		return "", fmt.Errorf("process start identity: invalid pid %d", pid)
	}
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return "", os.ErrNotExist
		}
		return "", fmt.Errorf("open process %d for identity: %w", pid, err)
	}
	defer windows.CloseHandle(process)
	return processHandleIdentity(process)
}

func processHandleIdentity(process windows.Handle) (string, error) {
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(process, &creation, &exit, &kernel, &user); err != nil {
		return "", fmt.Errorf("read process creation time: %w", err)
	}
	ticks := uint64(creation.HighDateTime)<<32 | uint64(creation.LowDateTime)
	if ticks == 0 {
		return "", errors.New("read process creation time: value is zero")
	}
	return fmt.Sprintf("windows-filetime:%016x", ticks), nil
}

// ProcessGroupAlive reports whether the supplied PID still identifies a live
// process. Windows process trees are not POSIX process groups.
func ProcessGroupAlive(pgid int) bool {
	if pgid <= 0 {
		return false
	}
	alive, err := processGroupAlive(pgid)
	return err != nil || alive
}

func processGroupAlive(pid int) (bool, error) {
	process, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return false, nil
		}
		return false, fmt.Errorf("open process %d: %w", pid, err)
	}
	defer windows.CloseHandle(process)
	result, err := windows.WaitForSingleObject(process, 0)
	if err != nil {
		return false, fmt.Errorf("probe process %d: %w", pid, err)
	}
	switch result {
	case uint32(windows.WAIT_OBJECT_0):
		return false, nil
	case uint32(windows.WAIT_TIMEOUT):
		return true, nil
	default:
		return false, fmt.Errorf("probe process %d: unexpected wait result %#x", pid, result)
	}
}

// IsTTY reports whether the child owns a pseudo-terminal. Windows children are
// always non-TTY.
func (c *Child) IsTTY() bool {
	return false
}

// Write forwards bytes to the child pseudo-terminal. Windows does not support
// pseudo-terminal input through this process layer.
func (c *Child) Write(p []byte) (int, error) {
	return c.WriteContext(context.Background(), p)
}

// WriteContext is unsupported because Windows process children cannot own a
// pseudo-terminal through this process layer.
func (c *Child) WriteContext(ctx context.Context, p []byte) (int, error) {
	return 0, errors.New("process: child has no tty input")
}

// Resize reports that Windows process children do not own a pseudo-terminal.
func (c *Child) Resize(columns, rows uint16) error {
	return errors.New("process: child has no tty")
}

// ResizeContext is unsupported because Windows process children cannot own a
// pseudo-terminal through this process layer.
func (c *Child) ResizeContext(ctx context.Context, columns, rows uint16) error {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	return c.Resize(columns, rows)
}

// LeaderDone is closed as soon as the process-group leader has exited.
func (c *Child) LeaderDone() <-chan struct{} {
	return c.leaderDone
}

// HasSurvivingDescendants reports whether the leader exited while another
// process remains in the private Job Object.
func (c *Child) HasSurvivingDescendants() bool {
	if c == nil {
		return false
	}
	select {
	case <-c.leaderDone:
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.groupEnded || c.ownedJobHandle == 0 {
			return false
		}
		ids, err := jobProcessIDs(c.ownedJobHandle)
		return err != nil || len(ids) != 0
	default:
		return false
	}
}

// Done is closed after the process and every member of its Job Object have
// exited, output has been captured, and the output store has been notified.
func (c *Child) Done() <-chan struct{} {
	return c.done
}

// Wait blocks until the child reaches its terminal transition. It is safe to
// call Wait repeatedly and concurrently.
func (c *Child) Wait() Result {
	<-c.done
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.res
}

// Signal rejects Unix signals. Use Stop to terminate the Windows-owned tree.
func (c *Child) Signal(sig os.Signal) error {
	if c == nil {
		return errors.New("process: nil child")
	}
	return fmt.Errorf("process: Unix signals are unsupported on Windows (use Stop; got %v)", sig)
}

// Stop terminates the private Job Object after proving that the root process
// handle still identifies the captured PID and creation time. If the root has
// exited, the retained process handle preserves that proof while the Job
// Object handle proves ownership of surviving descendants.
func (c *Child) Stop() error {
	if c == nil {
		return errors.New("process: nil child")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.groupEnded {
		return os.ErrProcessDone
	}
	if c.stopRequested {
		return nil
	}
	if c.processHandle == 0 || c.jobHandle == 0 || c.ownedJobHandle == 0 || c.startIdentity == "" || c.pid <= 0 {
		return errors.New("process: incomplete Windows ownership proof")
	}
	if c.jobHandle != c.launchJobHandle {
		return errors.New("process: job ownership mismatch")
	}
	pid, err := windows.GetProcessId(c.processHandle)
	if err != nil {
		return fmt.Errorf("process: verify root PID: %w", err)
	}
	if int(pid) != c.pid {
		return fmt.Errorf("process: root PID mismatch: handle identifies %d, expected %d", pid, c.pid)
	}
	identity, err := processHandleIdentity(c.processHandle)
	if err != nil {
		return fmt.Errorf("process: verify root creation identity: %w", err)
	}
	if identity != c.startIdentity {
		return errors.New("process: root creation identity mismatch")
	}
	ids, err := jobProcessIDs(c.ownedJobHandle)
	if err != nil {
		return fmt.Errorf("process: verify job ownership: %w", err)
	}
	rootExited, err := processHandleExited(c.processHandle)
	if err != nil {
		return fmt.Errorf("process: verify root state: %w", err)
	}
	if len(ids) == 0 {
		if !rootExited {
			return errors.New("process: root is not a member of its owned job")
		}
		c.markGroupEndedLocked()
		return os.ErrProcessDone
	}
	if !rootExited && !containsPID(ids, pid) {
		return errors.New("process: root is not a member of its owned job")
	}
	if err := windows.TerminateJobObject(c.ownedJobHandle, windowsStopExitCode); err != nil {
		return fmt.Errorf("process: terminate owned job: %w", err)
	}
	c.stopRequested = true
	return nil
}

func processHandleExited(process windows.Handle) (bool, error) {
	result, err := windows.WaitForSingleObject(process, 0)
	if err != nil {
		return false, err
	}
	switch result {
	case uint32(windows.WAIT_OBJECT_0):
		return true, nil
	case uint32(windows.WAIT_TIMEOUT):
		return false, nil
	default:
		return false, fmt.Errorf("unexpected wait result %#x", result)
	}
}

func containsPID(ids []uint32, pid uint32) bool {
	for _, id := range ids {
		if id == pid {
			return true
		}
	}
	return false
}

func (c *Child) run(stdoutReader, stderrReader *os.File) {
	stdoutDone := make(chan error, 1)
	stderrDone := make(chan error, 1)
	go func() { stdoutDone <- captureWindows(stdoutReader, output.Stdout, c) }()
	go func() { stderrDone <- captureWindows(stderrReader, output.Stderr, c) }()

	waitErr := waitForWindowsProcess(c.processHandle)
	exitCode := 1
	if waitErr == nil {
		var code uint32
		if err := windows.GetExitCodeProcess(c.processHandle, &code); err != nil {
			waitErr = fmt.Errorf("read process exit code: %w", err)
		} else {
			exitCode = int(code)
		}
	} else {
		_ = c.Stop()
		if retryErr := waitForWindowsProcess(c.processHandle); retryErr != nil {
			waitErr = errors.Join(waitErr, retryErr)
		}
	}
	close(c.leaderDone)
	<-c.groupGone
	stdoutErr := <-stdoutDone
	stderrErr := <-stderrDone
	c.mu.Lock()
	at := c.groupEndedAt
	result := Result{
		ExitCode: exitCode,
		Err:      errors.Join(waitErr, stdoutErr, stderrErr),
		ExitedAt: at,
	}
	c.output.NotifyExit(output.Exit{Code: exitCode, Time: at})
	c.res = result
	_ = windows.CloseHandle(c.processHandle)
	_ = windows.CloseHandle(c.jobHandle)
	_ = windows.CloseHandle(c.ownedJobHandle)
	c.processHandle = 0
	c.jobHandle = 0
	c.ownedJobHandle = 0
	c.mu.Unlock()
	close(c.done)
}

func waitForWindowsProcess(process windows.Handle) error {
	result, err := windows.WaitForSingleObject(process, windows.INFINITE)
	if err != nil {
		return fmt.Errorf("wait for process: %w", err)
	}
	if result != uint32(windows.WAIT_OBJECT_0) {
		return fmt.Errorf("wait for process: unexpected wait result %#x", result)
	}
	return nil
}

func (c *Child) observeJobExit() {
	ticker := time.NewTicker(processGroupPollInterval)
	defer ticker.Stop()
	for {
		c.mu.Lock()
		job := c.ownedJobHandle
		c.mu.Unlock()
		ids, err := jobProcessIDs(job)
		if err == nil && len(ids) == 0 {
			c.mu.Lock()
			c.markGroupEndedLocked()
			c.mu.Unlock()
			return
		}
		<-ticker.C
	}
}

func (c *Child) markGroupEndedLocked() {
	if c.groupEnded {
		return
	}
	c.groupEnded = true
	c.groupEndedAt = c.now()
	close(c.groupGone)
}

var peekNamedPipeProc = windows.NewLazySystemDLL("kernel32.dll").NewProc("PeekNamedPipe")

func captureWindows(reader *os.File, stream output.Stream, c *Child) error {
	writer, setupErr := output.NewLineWriter(stream, c.maxLineBytes, c.idleFlush, c.now, c.output.Append)
	var errs []error
	if setupErr != nil {
		errs = append(errs, setupErr)
	}
	buffer := make([]byte, 32*1024)
	captureOutput := setupErr == nil
	var drainStarted, lastProgress, hardDeadline time.Time
	handle := windows.Handle(reader.Fd())
	for {
		available, peekErr := peekPipeAvailable(handle)
		if peekErr != nil {
			if !errors.Is(peekErr, windows.ERROR_BROKEN_PIPE) {
				errs = append(errs, peekErr)
			}
			break
		}
		if available != 0 {
			readSize := len(buffer)
			if uint64(available) < uint64(readSize) {
				readSize = int(available)
			}
			var n uint32
			readErr := windows.ReadFile(handle, buffer[:readSize], &n, nil)
			if readErr != nil {
				if errors.Is(readErr, windows.ERROR_NO_DATA) {
					if windowsChannelClosed(c.groupGone) {
						now := time.Now()
						if drainStarted.IsZero() {
							drainStarted, lastProgress, hardDeadline = now, now, now.Add(captureHardTimeout)
						}
						if !now.Before(hardDeadline) || now.Sub(lastProgress) >= captureDrainTimeout {
							break
						}
					}
					time.Sleep(processGroupPollInterval)
					continue
				}
				if !errors.Is(readErr, windows.ERROR_BROKEN_PIPE) {
					errs = append(errs, readErr)
				}
				break
			}
			if n > 0 {
				if captureOutput {
					written, writeErr := writer.Write(buffer[:n])
					if writeErr == nil && written != int(n) {
						writeErr = io.ErrShortWrite
					}
					if writeErr != nil {
						errs = append(errs, writeErr)
						captureOutput = false
					}
				}
				now := time.Now()
				if drainStarted.IsZero() && windowsChannelClosed(c.groupGone) {
					drainStarted, lastProgress, hardDeadline = now, now, now.Add(captureHardTimeout)
				} else if !drainStarted.IsZero() {
					lastProgress = now
				}
			}
			continue
		}

		if windowsChannelClosed(c.groupGone) {
			now := time.Now()
			if drainStarted.IsZero() {
				drainStarted, lastProgress, hardDeadline = now, now, now.Add(captureHardTimeout)
			}
			if !now.Before(hardDeadline) || now.Sub(lastProgress) >= captureDrainTimeout {
				break
			}
		}
		time.Sleep(processGroupPollInterval)
	}
	if writer != nil {
		if err := writer.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if err := reader.Close(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func peekPipeAvailable(handle windows.Handle) (uint32, error) {
	var available uint32
	result, _, callErr := peekNamedPipeProc.Call(
		uintptr(handle),
		0,
		0,
		0,
		uintptr(unsafe.Pointer(&available)),
		0,
	)
	if result == 0 {
		if callErr != nil && !errors.Is(callErr, windows.ERROR_SUCCESS) {
			return 0, callErr
		}
		return 0, errors.New("PeekNamedPipe failed")
	}
	return available, nil
}

func windowsChannelClosed(channel <-chan struct{}) bool {
	select {
	case <-channel:
		return true
	default:
		return false
	}
}

func jobProcessIDs(job windows.Handle) ([]uint32, error) {
	if job == 0 {
		return nil, errors.New("invalid job object handle")
	}
	pointerSize := unsafe.Sizeof(uintptr(0))
	bufferSize := uintptr(8) + pointerSize*16
	for attempts := 0; attempts < 8; attempts++ {
		if bufferSize > uintptr(^uint32(0)) {
			return nil, errors.New("job process list is too large")
		}
		buffer := make([]byte, int(bufferSize))
		var returned uint32
		err := windows.QueryInformationJobObject(
			job,
			windows.JobObjectBasicProcessIdList,
			uintptr(unsafe.Pointer(&buffer[0])),
			uint32(len(buffer)),
			&returned,
		)
		assigned := binary.LittleEndian.Uint32(buffer[0:4])
		listed := binary.LittleEndian.Uint32(buffer[4:8])
		if err != nil && !errors.Is(err, windows.ERROR_MORE_DATA) && !errors.Is(err, windows.ERROR_INSUFFICIENT_BUFFER) {
			return nil, err
		}
		capacity := uint32((len(buffer) - 8) / int(pointerSize))
		if err == nil && assigned <= listed && listed <= capacity {
			ids := unsafe.Slice((*uintptr)(unsafe.Pointer(&buffer[8])), int(listed))
			processIDs := make([]uint32, 0, len(ids))
			for _, id := range ids {
				processIDs = append(processIDs, uint32(id))
			}
			return processIDs, nil
		}
		required := uintptr(8) + uintptr(assigned)*pointerSize
		if required <= bufferSize {
			required = bufferSize * 2
		}
		bufferSize = required
	}
	return nil, errors.New("job process list changed while querying")
}

// ResolveExecutable resolves name using only the supplied environment and
// working directory. Windows lookup honors PATH and PATHEXT without consulting
// the daemon's ambient environment.
func ResolveExecutable(name string, env []string, dir string) (string, error) {
	if name == "" {
		return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
	}
	pathValue, hasPath := windowsEnvValue(env, "PATH")
	pathExt, hasPathExt := windowsEnvValue(env, "PATHEXT")
	if !hasPathExt {
		pathExt = ".COM;.EXE;.BAT;.CMD"
	}
	extensions := windowsPathExtensions(pathExt)
	var candidates []string
	if filepath.Ext(name) == "" {
		for _, extension := range extensions {
			if strings.EqualFold(extension, ".EXE") || strings.EqualFold(extension, ".COM") {
				candidates = append(candidates, name+extension)
			}
		}
	} else if strings.EqualFold(filepath.Ext(name), ".EXE") || strings.EqualFold(filepath.Ext(name), ".COM") {
		candidates = []string{name}
	}

	targetDir := dir
	if targetDir == "" {
		var err error
		targetDir, err = os.Getwd()
		if err != nil {
			return "", &exec.Error{Name: name, Err: err}
		}
	}
	if !filepath.IsAbs(targetDir) {
		var err error
		targetDir, err = filepath.Abs(targetDir)
		if err != nil {
			return "", &exec.Error{Name: name, Err: err}
		}
	}

	if filepath.Base(name) != name || filepath.VolumeName(name) != "" {
		for _, candidateName := range candidates {
			candidate := candidateName
			if !filepath.IsAbs(candidate) {
				candidate = filepath.Join(targetDir, candidate)
			}
			if resolved, ok := windowsExecutableFile(candidate); ok {
				return resolved, nil
			}
		}
		return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
	}
	if !hasPath {
		return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
	}
	directories := filepath.SplitList(pathValue)
	if len(directories) == 0 {
		directories = []string{""}
	}
	for _, directory := range directories {
		if directory == "" {
			directory = targetDir
		} else if !filepath.IsAbs(directory) {
			directory = filepath.Join(targetDir, directory)
		}
		for _, candidateName := range candidates {
			if resolved, ok := windowsExecutableFile(filepath.Join(directory, candidateName)); ok {
				return resolved, nil
			}
		}
	}
	return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
}

func windowsExecutableFile(candidate string) (string, bool) {
	info, err := os.Stat(candidate)
	if err != nil || info.IsDir() {
		return "", false
	}
	resolved, err := filepath.Abs(candidate)
	return resolved, err == nil
}

func windowsEnvValue(env []string, name string) (string, bool) {
	value, found := "", false
	for _, entry := range env {
		key, candidate, ok := strings.Cut(entry, "=")
		if ok && strings.EqualFold(key, name) {
			value, found = candidate, true
		}
	}
	return value, found
}

func windowsPathExtensions(value string) []string {
	var extensions []string
	for _, extension := range filepath.SplitList(value) {
		extension = strings.TrimSpace(extension)
		if extension == "" {
			continue
		}
		if !strings.HasPrefix(extension, ".") {
			extension = "." + extension
		}
		extensions = append(extensions, extension)
	}
	return extensions
}

func windowsCommandLine(argv []string) (string, error) {
	if len(argv) == 0 {
		return "", errors.New("argv must not be empty")
	}
	parts := make([]string, len(argv))
	for i, arg := range argv {
		if strings.IndexByte(arg, 0) >= 0 {
			return "", errors.New("argument contains NUL")
		}
		parts[i] = windows.EscapeArg(arg)
	}
	return strings.Join(parts, " "), nil
}

func windowsEnvironmentBlock(env []string) ([]uint16, error) {
	entries := cloneStrings(env)
	for _, entry := range entries {
		if !strings.Contains(entry, "=") {
			return nil, fmt.Errorf("environment entry %q has no '='", entry)
		}
		if _, err := windows.UTF16FromString(entry); err != nil {
			return nil, fmt.Errorf("environment entry contains NUL: %w", err)
		}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return strings.ToUpper(entries[i]) < strings.ToUpper(entries[j])
	})
	block := make([]uint16, 0)
	for _, entry := range entries {
		block = append(block, utf16.Encode([]rune(entry))...)
		block = append(block, 0)
	}
	if len(block) == 0 {
		return []uint16{0, 0}, nil
	}
	return append(block, 0), nil
}

func cloneStrings(values []string) []string {
	if values == nil {
		return nil
	}
	copyOfValues := make([]string, len(values))
	copy(copyOfValues, values)
	return copyOfValues
}
