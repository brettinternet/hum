// Package process starts and supervises one direct child process.
package process

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"hum/internal/output"
)

const (
	processGroupPollInterval = 10 * time.Millisecond
	captureDrainTimeout      = 100 * time.Millisecond
	captureHardTimeout       = time.Second
	defaultIdleFlush         = 100 * time.Millisecond
)

// Spec describes one child process launch.
//
// Argv is passed directly to exec without a shell. Env is copied exactly;
// nil and an empty slice both start the child with no environment variables.
// IdleFlush controls how long an unterminated output fragment may remain
// buffered; zero selects a short default.
type Spec struct {
	Dir          string
	Argv         []string
	Env          []string
	Output       *output.Store
	MaxLineBytes int
	IdleFlush    time.Duration
	Now          func() time.Time
	// Started runs after the child is spawned but before output capture begins.
	Started func() error
}

// Result is the immutable terminal status of a child.
//
// ExitCode is the process exit status. A process terminated by a signal has an
// exit code of -1, as reported by os.ProcessState.ExitCode. Err contains
// unexpected wait failures and output capture failures; an ordinary non-zero
// process exit is represented by ExitCode and does not populate Err.
type Result struct {
	ExitCode int
	Err      error
	ExitedAt time.Time
}

// Child is one started process and its process group.
type Child struct {
	pid  int
	pgid int

	output       *output.Store
	maxLineBytes int
	idleFlush    time.Duration
	now          func() time.Time

	done         chan struct{}
	leaderDone   chan struct{}
	groupGone    chan struct{}
	mu           sync.Mutex
	groupEnded   bool
	groupEndedAt time.Time
	res          Result
}

// Start launches the command described by spec in a fresh process group.
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

	argv := cloneStrings(spec.Argv)
	env := cloneStrings(spec.Env)
	now := spec.Now
	if env == nil {
		env = []string{}
	}
	if now == nil {
		now = time.Now
	}
	idleFlush := spec.IdleFlush
	if idleFlush == 0 {
		idleFlush = defaultIdleFlush
	}

	resolvedPath, err := resolveExecutable(argv[0], env, spec.Dir)
	if err != nil {
		return nil, fmt.Errorf("process: resolve %q: %w", argv[0], err)
	}

	stdin, err := os.Open(os.DevNull)
	if err != nil {
		return nil, fmt.Errorf("process: open stdin: %w", err)
	}
	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("process: create stdout pipe: %w", err)
	}
	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		_ = stdin.Close()
		_ = stdoutReader.Close()
		_ = stdoutWriter.Close()
		return nil, fmt.Errorf("process: create stderr pipe: %w", err)
	}

	cmd := exec.Command(resolvedPath, argv[1:]...)
	// Keep the caller's argv[0] while using the environment-resolved path for
	// executable lookup.
	cmd.Args = argv
	cmd.Dir = spec.Dir
	cmd.Env = env
	cmd.Stdin = stdin
	cmd.Stdout = stdoutWriter
	cmd.Stderr = stderrWriter
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		// Cmd.Start closes its child descriptors on both success and failure;
		// close every local descriptor as well so setup failures cannot leak
		// resources.
		_ = stdin.Close()
		_ = stdoutReader.Close()
		_ = stdoutWriter.Close()
		_ = stderrReader.Close()
		_ = stderrWriter.Close()
		return nil, fmt.Errorf("process: start %q: %w", argv[0], err)
	}
	if spec.Started != nil {
		if err := spec.Started(); err != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			_ = cmd.Wait()
			_ = stdin.Close()
			_ = stdoutReader.Close()
			_ = stdoutWriter.Close()
			_ = stderrReader.Close()
			_ = stderrWriter.Close()
			return nil, fmt.Errorf("process: started callback: %w", err)
		}
	}

	// Stdio files assigned directly to Cmd are not owned by os/exec and remain
	// open in the parent after Start. Close the parent's copies immediately so
	// capture observes EOF once the child (and any descendants) close theirs.
	_ = stdin.Close()
	_ = stdoutWriter.Close()
	_ = stderrWriter.Close()
	child := &Child{
		pid:          cmd.Process.Pid,
		pgid:         cmd.Process.Pid,
		output:       spec.Output,
		maxLineBytes: spec.MaxLineBytes,
		idleFlush:    idleFlush,
		now:          now,
		done:         make(chan struct{}),
		leaderDone:   make(chan struct{}),
		groupGone:    make(chan struct{}),
		res:          Result{},
	}
	go child.observeGroupExit()
	go child.run(cmd, stdoutReader, stderrReader)
	return child, nil
}

// PID reports the operating-system process identifier.
func (c *Child) PID() int {
	return c.pid
}

// PGID reports the process-group identifier assigned at launch.
func (c *Child) PGID() int {
	return c.pgid
}

// Done is closed after the process has been waited for, the original process
// group has disappeared, both output streams have reached EOF or been
// canceled, and the output store has been notified.
func (c *Child) Done() <-chan struct{} {
	return c.done
}

// Wait blocks until the child reaches its terminal transition and returns its
// result. It is safe to call Wait repeatedly and concurrently.
func (c *Child) Wait() Result {
	<-c.done
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.res
}

// Signal sends sig to every member of the child's process group.
func (c *Child) Signal(sig os.Signal) error {
	if c == nil {
		return errors.New("process: nil child")
	}
	if c.pgid <= 0 {
		return errors.New("process: invalid process group")
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.groupEnded {
		return os.ErrProcessDone
	}

	signal, ok := sig.(syscall.Signal)
	if !ok {
		return fmt.Errorf("process: unsupported signal %v", sig)
	}
	if err := syscall.Kill(-c.pgid, signal); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			c.markGroupEndedLocked()
			return os.ErrProcessDone
		}
		return err
	}
	return nil
}

func (c *Child) run(cmd *exec.Cmd, stdoutReader, stderrReader *os.File) {
	stdoutWriter, stdoutSetupErr := output.NewLineWriter(
		output.Stdout,
		c.maxLineBytes,
		c.idleFlush,
		c.now,
		c.output.Append,
	)
	stderrWriter, stderrSetupErr := output.NewLineWriter(
		output.Stderr,
		c.maxLineBytes,
		c.idleFlush,
		c.now,
		c.output.Append,
	)

	stdoutDone := make(chan error, 1)
	stderrDone := make(chan error, 1)
	stdoutFinished := make(chan struct{})
	stderrFinished := make(chan struct{})
	stdoutProgress := make(chan struct{}, 1)
	stderrProgress := make(chan struct{}, 1)
	stdoutControl := &captureControl{}
	stderrControl := &captureControl{}
	go func() {
		defer close(stdoutFinished)
		stdoutDone <- capture(stdoutReader, stdoutWriter, stdoutSetupErr, c.groupGone, stdoutProgress, stdoutControl)
	}()
	go func() {
		defer close(stderrFinished)
		stderrDone <- capture(stderrReader, stderrWriter, stderrSetupErr, c.groupGone, stderrProgress, stderrControl)
	}()

	waitErr := cmd.Wait()
	exitCode := -1
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	} else {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) && exitErr.ProcessState != nil {
			exitCode = exitErr.ProcessState.ExitCode()
		}
	}
	// Publish the leader's wait status before waiting for redirected
	// descendants. Tests and callers that need to coordinate a group signal can
	// distinguish a reaped leader from a still-running process group.
	close(c.leaderDone)

	// The original process group is the lifecycle barrier. Escaped
	// descendants retaining inherited pipe descriptors cannot pin completion.
	<-c.groupGone
	c.mu.Lock()
	at := c.groupEndedAt
	c.mu.Unlock()
	hardDeadline := time.Now().Add(captureHardTimeout)
	stdoutControl.setHardDeadline(hardDeadline)
	stderrControl.setHardDeadline(hardDeadline)
	stdoutCancelDone := make(chan struct{})
	stderrCancelDone := make(chan struct{})
	go func() {
		cancelCapture(stdoutReader, stdoutFinished, stdoutProgress, stdoutControl)
		close(stdoutCancelDone)
	}()
	go func() {
		cancelCapture(stderrReader, stderrFinished, stderrProgress, stderrControl)
		close(stderrCancelDone)
	}()
	<-stdoutCancelDone
	<-stderrCancelDone
	stdoutErr := <-stdoutDone
	stderrErr := <-stderrDone
	result := Result{
		ExitCode: exitCode,
		Err:      errors.Join(normalizeWaitError(waitErr), stdoutErr, stderrErr),
		ExitedAt: at,
	}

	// capture waits for EOF or cancellation and closes both LineWriters before
	// this point, so NotifyExit's watermark includes every retained output
	// record.
	c.output.NotifyExit(output.Exit{Code: exitCode, Time: at})

	c.mu.Lock()
	c.res = result
	c.mu.Unlock()
	close(c.done)
}

func (c *Child) observeGroupExit() {
	waitForProcessGroupExit(c.pgid, c.groupGone)

	c.mu.Lock()
	c.markGroupEndedLocked()
	c.mu.Unlock()
}

func (c *Child) markGroupEndedLocked() {
	if c.groupEnded {
		return
	}
	c.groupEnded = true
	close(c.groupGone)
	c.groupEndedAt = c.now()
}

type captureControl struct {
	mu           sync.Mutex
	active       bool
	hardDeadline time.Time
}

func (c *captureControl) setHardDeadline(deadline time.Time) {
	c.mu.Lock()
	c.active = true
	c.hardDeadline = deadline
	c.mu.Unlock()
}

func (c *captureControl) isActive() bool {
	c.mu.Lock()
	active := c.active
	c.mu.Unlock()
	return active
}

func (c *captureControl) nextDeadline() time.Time {
	deadline := time.Now().Add(captureDrainTimeout)
	c.mu.Lock()
	hardDeadline := c.hardDeadline
	c.mu.Unlock()
	if !hardDeadline.IsZero() && hardDeadline.Before(deadline) {
		return hardDeadline
	}
	return deadline
}

func (c *captureControl) hardDeadlineAt() time.Time {
	c.mu.Lock()
	hardDeadline := c.hardDeadline
	c.mu.Unlock()
	if hardDeadline.IsZero() {
		return time.Now().Add(captureHardTimeout)
	}
	return hardDeadline
}

type captureReader struct {
	file     *os.File
	canceled <-chan struct{}
	progress chan<- struct{}
	control  *captureControl
}

func (r captureReader) Read(p []byte) (int, error) {
	return r.file.Read(p)
}

func (r captureReader) noteProgress() {
	select {
	case <-r.canceled:
		if !r.control.isActive() {
			return
		}
		_ = r.file.SetReadDeadline(r.control.nextDeadline())
		select {
		case r.progress <- struct{}{}:
		default:
		}
	default:
	}
}

func cancelCapture(
	reader *os.File,
	finished <-chan struct{},
	progress <-chan struct{},
	control *captureControl,
) {
	select {
	case <-finished:
		return
	default:
	}

	hardDeadline := control.hardDeadlineAt()
	hardTimer := time.NewTimer(time.Until(hardDeadline))
	defer hardTimer.Stop()

	if err := reader.SetReadDeadline(hardDeadline); err == nil {
		select {
		case <-finished:
		case <-hardTimer.C:
			select {
			case <-finished:
			default:
				_ = reader.Close()
			}
		}
		return
	}

	idleTimer := time.NewTimer(time.Until(hardDeadline))
	defer idleTimer.Stop()
	for {
		select {
		case <-finished:
			return
		case <-progress:
			resetCaptureTimer(idleTimer, time.Until(control.nextDeadline()))
		case <-idleTimer.C:
			select {
			case <-finished:
				return
			default:
				_ = reader.Close()
				return
			}
		case <-hardTimer.C:
			select {
			case <-finished:
				return
			default:
				_ = reader.Close()
				return
			}
		}
	}
}

func resetCaptureTimer(timer *time.Timer, duration time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(duration)
}

func drainCapture(source captureReader, buffer []byte) error {
	for {
		n, err := source.Read(buffer)
		if n > 0 {
			source.noteProgress()
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

func capture(
	reader *os.File,
	writer *output.LineWriter,
	setupErr error,
	canceled <-chan struct{},
	progress chan<- struct{},
	control *captureControl,
) error {
	source := captureReader{
		file:     reader,
		canceled: canceled,
		progress: progress,
		control:  control,
	}
	buffer := make([]byte, 32*1024)
	var errs []error
	isCanceled := func() bool {
		select {
		case <-canceled:
			return true
		default:
			return false
		}
	}
	isCanceledError := func(err error) bool {
		return isCanceled() &&
			(errors.Is(err, os.ErrClosed) || errors.Is(err, os.ErrDeadlineExceeded))
	}
	if setupErr != nil {
		errs = append(errs, setupErr)
		if err := drainCapture(source, buffer); err != nil && !isCanceledError(err) {
			errs = append(errs, err)
		}
	} else {
		for {
			n, readErr := source.Read(buffer)
			if n > 0 {
				written, writeErr := writer.Write(buffer[:n])
				source.noteProgress()
				if writeErr == nil && written != n {
					writeErr = io.ErrShortWrite
				}
				if writeErr != nil {
					if !isCanceledError(writeErr) {
						errs = append(errs, writeErr)
					}
					if !isCanceled() {
						if drainErr := drainCapture(source, buffer); drainErr != nil && !isCanceledError(drainErr) {
							errs = append(errs, drainErr)
						}
					}
					break
				}
			}
			if readErr != nil {
				if readErr != io.EOF && !isCanceledError(readErr) {
					errs = append(errs, readErr)
				}
				break
			}
		}
		if err := writer.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if err := reader.Close(); err != nil {
		if !isCanceledError(err) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func resolveExecutable(name string, env []string, dir string) (string, error) {
	if filepath.Base(name) != name {
		return name, nil
	}

	pathValue := ""
	hasPath := false
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if ok && key == "PATH" {
			pathValue = value
			hasPath = true
		}
	}
	if !hasPath {
		return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
	}

	targetDir := dir
	if targetDir == "" {
		var err error
		targetDir, err = os.Getwd()
		if err != nil {
			return "", &exec.Error{Name: name, Err: err}
		}
	} else if !filepath.IsAbs(targetDir) {
		var err error
		targetDir, err = filepath.Abs(targetDir)
		if err != nil {
			return "", &exec.Error{Name: name, Err: err}
		}
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
		candidate := filepath.Join(directory, name)
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() || info.Mode().Perm()&0111 == 0 {
			continue
		}
		resolved, err := filepath.Abs(candidate)
		if err != nil {
			return "", &exec.Error{Name: name, Err: err}
		}
		return resolved, nil
	}

	return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
}

func waitForProcessGroupExit(pgid int, done <-chan struct{}) {
	ticker := time.NewTicker(processGroupPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		default:
		}
		if err := syscall.Kill(-pgid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		select {
		case <-done:
			return
		case <-ticker.C:
		}
	}
}

func normalizeWaitError(err error) error {
	if err == nil {
		return nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return nil
	}
	return err
}

func cloneStrings(values []string) []string {
	if values == nil {
		return nil
	}
	copyOfValues := make([]string, len(values))
	copy(copyOfValues, values)
	return copyOfValues
}
