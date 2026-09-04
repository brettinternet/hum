package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"hum/internal/app"
	"hum/internal/output"
	"hum/internal/protocol"
)

const (
	wireVersion              = protocol.Version
	defaultWireMaxLine       = protocol.DefaultMaxLineBytes
	maxWaitTimeoutMS   int64 = (1<<63 - 1) / int64(time.Millisecond)
)

// Server owns one app.Supervisor and exposes it over one private Unix socket.
// Managed processes are intentionally not children of any request handler:
// closing a client connection only cancels that request's response stream.
type Server struct {
	owner      *runtimeOwner
	paths      RuntimePaths
	listener   net.Listener
	supervisor *app.Supervisor
	version    int
	maxLine    int
	log        *boundedLog

	projectsMu sync.Mutex
	projects   map[string]struct{}

	serveMu      sync.Mutex
	serveStarted bool
	serveDone    chan struct{}
	serveErr     error

	readyOnce  sync.Once
	ready      chan struct{}
	readyErrMu sync.Mutex
	readyErr   error

	shutdownMu        sync.Mutex // lifecycle admission gate
	shutdownStarted   bool
	shutdownDone      chan struct{}
	shutdownErr       error
	shutdownResponses sync.WaitGroup
	closing           chan struct{}
}

// NewServer creates a listener, claims runtime ownership, and constructs the
// supervisor when Config.Supervisor is nil. It does not begin accepting until
// Serve is called, allowing callers to install a readiness wait first.
func NewServer(cfg Config) (*Server, error) {
	paths := NewRuntimePaths(cfg.RuntimeDir)
	owner, err := acquireRuntime(paths)
	if err != nil {
		return nil, err
	}
	paths = owner.paths
	version := cfgVersion(cfg.Version)
	maxLine := cfg.MaxLineBytes
	if maxLine <= 0 {
		maxLine = 64 * 1024
	}
	var supervisor *app.Supervisor
	if cfg.Supervisor != nil {
		supervisor = cfg.Supervisor
	} else {
		stopGrace := cfg.StopGrace
		if stopGrace == 0 {
			stopGrace = 10 * time.Second
		}
		supervisor, err = app.New(app.Options{
			CompletedLimit: cfg.CompletedLimit,
			StopGrace:      stopGrace,
			OutputLimits:   cfg.OutputLimits,
			MaxLineBytes:   maxLine,
		})
		if err != nil {
			owner.release()
			return nil, fmt.Errorf("create supervisor: %w", err)
		}
	}
	log, err := openBoundedLog(paths.Log, cfg.LogBytes)
	if err != nil {
		owner.release()
		if cfg.Supervisor == nil {
			_ = supervisor.Shutdown(context.Background())
		}
		return nil, fmt.Errorf("open daemon log: %w", err)
	}
	listener, err := owner.bind()
	if err != nil {
		_ = log.Close()
		owner.release()
		if cfg.Supervisor == nil {
			_ = supervisor.Shutdown(context.Background())
		}
		return nil, err
	}
	// The lock serializes only setup. It remains as a stable inode and is
	// reacquired by cleanup so startup and teardown cannot race.
	owner.unlockStartup()
	return &Server{
		owner:        owner,
		paths:        paths,
		listener:     listener,
		supervisor:   supervisor,
		version:      version,
		maxLine:      maxLine,
		log:          log,
		projects:     make(map[string]struct{}),
		serveDone:    make(chan struct{}),
		ready:        make(chan struct{}),
		shutdownDone: make(chan struct{}),
		closing:      make(chan struct{}),
	}, nil
}

func cfgVersion(version string) int {
	if version == "" {
		return wireVersion
	}
	value, err := strconv.Atoi(version)
	if err != nil || value <= 0 {
		return wireVersion
	}
	return value
}

// Paths returns the runtime artifacts owned by this server.
func (s *Server) Paths() RuntimePaths { return s.paths }

// Supervisor returns the application supervisor owned by this daemon.
func (s *Server) Supervisor() *app.Supervisor { return s.supervisor }

// RuntimePaths is a descriptive alias for Paths at the server boundary.
func (s *Server) RuntimePaths() RuntimePaths { return s.paths }

func (s *Server) SocketPath() string { return s.paths.Socket }
func (s *Server) PID() int           { return s.owner.pid }

func (s *Server) RuntimeDir() string { return s.paths.Dir }
func (s *Server) ReadyPath() string  { return s.paths.Ready }

// Logf writes a bounded diagnostic record to the daemon runtime log.
// Detached command paths use this instead of a caller-owned stream.
func (s *Server) Logf(format string, args ...any) {
	if s == nil || s.log == nil {
		return
	}
	s.log.Printf(format, args...)
}

// Serve accepts independent request connections until shutdown or ctx
// cancellation. It is safe for request handlers to outlive the accept loop.
func (s *Server) Serve(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.serveMu.Lock()
	if s.serveStarted {
		s.serveMu.Unlock()
		return errors.New("daemon server Serve called more than once")
	}
	s.serveStarted = true
	s.serveMu.Unlock()

	signalCtx, stopSignals := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	defer close(s.serveDone)
	go func() {
		<-signalCtx.Done()
		// Both an explicit daemon signal and Serve context cancellation are
		// terminal for a foreground server. Cleanup is forced so no process
		// group is orphaned when the daemon leaves.
		_ = s.shutdown(true)
	}()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if signalCtx.Err() != nil || s.isShutdown() {
				<-s.shutdownDone
				s.shutdownResponses.Wait()
				s.shutdownMu.Lock()
				shutdownErr := s.shutdownErr
				s.shutdownMu.Unlock()
				s.setServeErr(shutdownErr)
				return shutdownErr
			}
			if errors.Is(err, net.ErrClosed) {
				s.setServeErr(nil)
				return nil
			}
			s.setServeErr(err)
			return err
		}
		s.readyOnce.Do(func() {
			if err := s.owner.markReady(); err != nil {
				s.readyErrMu.Lock()
				s.readyErr = err
				s.readyErrMu.Unlock()
			}
			close(s.ready)
		})
		go s.serveConn(conn)
	}
}

func (s *Server) setServeErr(err error) {
	s.serveMu.Lock()
	s.serveErr = err
	s.serveMu.Unlock()
}

func (s *Server) isShutdown() bool {
	s.shutdownMu.Lock()
	started := s.shutdownStarted
	s.shutdownMu.Unlock()
	return started
}

func (s *Server) registerShutdownResponse() bool {
	s.shutdownMu.Lock()
	defer s.shutdownMu.Unlock()
	select {
	case <-s.shutdownDone:
		return false
	default:
		s.shutdownResponses.Add(1)
		return true
	}
}

// WaitReady waits until the listening socket has accepted at least one
// connection and the readiness artifact has been written.
func (s *Server) WaitReady(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	// The probe is deliberately a real Unix connection. Binding a socket is
	// not readiness: the accept loop must take this connection before the
	// readiness file/channel become visible.
	probe, err := (&net.Dialer{}).DialContext(ctx, "unix", s.paths.Socket)
	if err != nil {
		return err
	}
	_ = probe.Close()
	select {
	case <-s.ready:
		s.readyErrMu.Lock()
		err := s.readyErr
		s.readyErrMu.Unlock()
		return err
	case <-s.shutdownDone:
		s.readyErrMu.Lock()
		err := s.readyErr
		s.readyErrMu.Unlock()
		if err != nil {
			return err
		}
		return errors.New("daemon stopped before readiness")
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Wait waits for Serve to finish. If Serve has not been called, it returns
// immediately because construction alone does not start a server loop.
func (s *Server) Wait() error {
	s.serveMu.Lock()
	started := s.serveStarted
	done := s.serveDone
	s.serveMu.Unlock()
	if !started {
		return nil
	}
	<-done
	s.serveMu.Lock()
	err := s.serveErr
	s.serveMu.Unlock()
	return err
}

// Close performs forced daemon shutdown, waiting for every process tree before
// removing runtime artifacts. It is idempotent.
func (s *Server) Close() error { return s.shutdown(true) }

// Shutdown performs the daemon-level shutdown operation. A non-forced
// shutdown refuses while active process names remain; forced shutdown invokes
// Supervisor.Shutdown and removes artifacts only after all groups terminate.
func (s *Server) Shutdown(ctx context.Context, force bool) error {
	return s.shutdown(force)
}

func (s *Server) shutdown(force bool) error {
	s.shutdownMu.Lock()
	if s.shutdownStarted {
		done := s.shutdownDone
		s.shutdownMu.Unlock()
		<-done
		s.shutdownMu.Lock()
		err := s.shutdownErr
		s.shutdownMu.Unlock()
		return err
	}
	if !force {
		if names := s.activeProcessNames(); len(names) != 0 {
			s.shutdownMu.Unlock()
			return &ActiveProcessesError{Names: names}
		}
	}
	s.shutdownStarted = true
	close(s.closing)
	s.shutdownMu.Unlock()

	var shutdownErr error
	// Supervisor.Shutdown deliberately ignores caller cancellation while it
	// performs TERM/KILL and waits for process-tree termination. The daemon
	// must not remove its socket while a managed group is still alive.
	if err := s.supervisor.Shutdown(context.Background()); err != nil {
		shutdownErr = errors.Join(shutdownErr, err)
	}
	_ = s.listener.Close()
	if err := s.owner.cleanup(); err != nil {
		shutdownErr = errors.Join(shutdownErr, err)
	}
	if err := s.log.Close(); err != nil {
		shutdownErr = errors.Join(shutdownErr, err)
	}
	s.shutdownMu.Lock()
	s.shutdownErr = shutdownErr
	close(s.shutdownDone)
	s.shutdownMu.Unlock()
	return shutdownErr
}

func (s *Server) activeProcessNames() []string {
	s.projectsMu.Lock()
	projects := make([]string, 0, len(s.projects))
	for root := range s.projects {
		projects = append(projects, root)
	}
	s.projectsMu.Unlock()
	sort.Strings(projects)
	var names []string
	for _, root := range projects {
		items, err := s.supervisor.List(root, false)
		if err != nil {
			continue
		}
		for _, item := range items {
			if item.State == app.StateRunning {
				names = append(names, item.Root+": "+item.Name)
			}
		}
	}
	sort.Strings(names)
	return names
}

func (s *Server) trackProcess(p app.Process) {
	s.projectsMu.Lock()
	s.projects[p.Root] = struct{}{}
	s.projectsMu.Unlock()
}

func (s *Server) listProcesses(cwd string, all, includeCompleted bool) ([]app.Process, error) {
	first, err := s.supervisor.List(cwd, includeCompleted)
	if err != nil {
		return nil, err
	}
	if !all {
		return first, nil
	}
	for _, item := range first {
		s.trackProcess(item)
	}
	s.projectsMu.Lock()
	roots := make([]string, 0, len(s.projects))
	for root := range s.projects {
		roots = append(roots, root)
	}
	s.projectsMu.Unlock()
	sort.Strings(roots)
	seen := make(map[string]struct{}, len(roots))
	items := make([]app.Process, 0, len(first))
	for _, item := range first {
		seen[item.Root] = struct{}{}
		items = append(items, item)
	}
	for _, root := range roots {
		if _, ok := seen[root]; ok {
			continue
		}
		other, err := s.supervisor.List(root, includeCompleted)
		if err != nil {
			continue
		}
		items = append(items, other...)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Root != items[j].Root {
			return items[i].Root < items[j].Root
		}
		if items[i].Name != items[j].Name {
			return items[i].Name < items[j].Name
		}
		return items[i].Start.Before(items[j].Start)
	})
	return items, nil
}

func (s *Server) serveConn(conn net.Conn) {
	defer conn.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	decoder := protocol.NewDecoder(conn, s.maxLine)
	encoder := protocol.NewEncoder(conn, s.maxLine)
	first, err := decoder.DecodeRequest()
	if err != nil {
		_ = writeProtocolError(encoder, protocol.OpHello, err)
		return
	}
	if first.Op != protocol.OpHello || first.Hello == nil {
		_ = writeProtocolError(encoder, protocol.OpHello, errors.New("first request must be hello"))
		return
	}
	versionErr := error(nil)
	if first.Hello.Version != s.version {
		versionErr = &VersionMismatchError{ClientVersion: first.Hello.Version, DaemonVersion: s.version}
		_ = writeProtocolError(encoder, protocol.OpHello, versionErr)
	} else if err := encoder.EncodeResponse(protocol.Hello{Op: protocol.OpHello, Version: s.version}); err != nil {
		return
	}

	for {
		protocolReq, err := decoder.DecodeRequest()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			_ = writeProtocolError(encoder, protocol.Operation(""), err)
			return
		}
		if versionErr != nil && protocolReq.Op != protocol.OpShutdown {
			_ = writeProtocolError(encoder, protocolReq.Op, versionErr)
			continue
		}
		req, err := wireRequestFromProtocol(protocolReq)
		if err != nil {
			_ = writeProtocolError(encoder, protocolReq.Op, err)
			continue
		}
		shutdownResponseRegistered := false
		if req.Op == "shutdown" {
			shutdownResponseRegistered = s.registerShutdownResponse()
		}
		if req.Op == "follow" {
			s.handleFollow(ctx, conn, encoder, req)
			return
		}
		if req.Op == "wait" {
			s.handleWait(ctx, conn, encoder, req)
			return
		}
		resp, terminal := s.dispatch(req)
		writeErr := writeProtocolResponse(encoder, resp)
		if shutdownResponseRegistered {
			s.shutdownResponses.Done()
		}
		if writeErr != nil {
			return
		}
		if terminal {
			return
		}
	}
}

func dispatchError(op string, err error) wireResponse {
	return wireResponse{Op: op, Error: protocolWireError(err)}
}

func (s *Server) dispatch(req wireRequest) (wireResponse, bool) {
	switch req.Op {
	case "start":
		s.shutdownMu.Lock()
		if s.shutdownStarted {
			s.shutdownMu.Unlock()
			return dispatchError(req.Op, app.ErrSupervisorClosed), false
		}
		if req.Cwd == "" {
			req.Cwd = "."
		}
		p, err := s.supervisor.Start(app.StartRequest{Name: req.Name, Source: req.Source, Root: req.Root, Argv: req.Argv, Cwd: req.Cwd, Env: append([]string(nil), req.Env...), Ready: appReadinessConfigFromWire(req.Ready)})
		if err != nil {
			s.shutdownMu.Unlock()
			return dispatchError(req.Op, err), false
		}
		s.trackProcess(p)
		s.shutdownMu.Unlock()
		process := wireProcessFromApp(p)
		return wireResponse{Op: req.Op, OK: true, Process: &process}, false
	case "list":
		if req.Cwd == "" {
			req.Cwd = "."
		}
		items, err := s.listProcesses(req.Cwd, req.All, req.IncludeCompleted)
		if err != nil {
			return dispatchError(req.Op, err), false
		}
		for _, p := range items {
			s.trackProcess(p)
		}
		return wireResponse{Op: req.Op, OK: true, Processes: wireProcessesFromApp(items)}, false
	case "get":
		p, err := s.supervisor.Get(req.Cwd, req.Name)
		if err != nil {
			return dispatchError(req.Op, err), false
		}
		s.trackProcess(p)
		process := wireProcessFromApp(p)
		nextCursor := uint64(p.NextCursor)
		process.NextCursor = &nextCursor
		return wireResponse{Op: req.Op, OK: true, Process: &process}, false
	case "output":
		store, err := s.supervisor.Output(req.Cwd, req.Name)
		if err != nil {
			return dispatchError(req.Op, err), false
		}
		options, err := readOptionsFromWire(req)
		if err != nil {
			return dispatchError(req.Op, err), false
		}
		result, err := store.Read(options)
		if err != nil {
			return dispatchError(req.Op, err), false
		}
		return wireResponseFromRead(req.Op, result), false
	case "wait":
		response, err := s.executeWait(context.Background(), req)
		if err != nil {
			return dispatchError(req.Op, err), false
		}
		return response, false
	case "signal":
		sig, err := parseSignal(req.Signal)
		if err != nil {
			return dispatchError(req.Op, err), false
		}
		if err := s.supervisor.Signal(req.Cwd, req.Name, sig); err != nil {
			return dispatchError(req.Op, err), false
		}
		return wireResponse{Op: req.Op, OK: true}, false
	case "stop":
		if err := s.supervisor.Stop(context.Background(), req.Cwd, req.Name); err != nil {
			return dispatchError(req.Op, err), false
		}
		var process *wireProcess
		if item, err := s.supervisor.Get(req.Cwd, req.Name); err == nil {
			s.trackProcess(item)
			value := wireProcessFromApp(item)
			process = &value
		}
		return wireResponse{Op: req.Op, OK: true, Process: process}, false
	case "remove":
		if err := s.supervisor.Remove(context.Background(), req.Cwd, req.Name); err != nil {
			return dispatchError(req.Op, err), false
		}
		return wireResponse{Op: req.Op, OK: true}, false
	case "restart":
		s.shutdownMu.Lock()
		if s.shutdownStarted {
			s.shutdownMu.Unlock()
			return dispatchError(req.Op, app.ErrSupervisorClosed), false
		}
		options := app.RestartOptions{
			Update: req.Update, Source: req.Source, Root: req.Root, Cwd: req.Cwd,
			Argv: append([]string(nil), req.Argv...), Env: append([]string(nil), req.Env...),
			Ready: appReadinessConfigFromWire(req.Ready),
		}
		process, err := s.supervisor.Restart(context.Background(), req.Cwd, req.Name, options)
		if err != nil {
			s.shutdownMu.Unlock()
			return dispatchError(req.Op, err), false
		}
		s.trackProcess(process)
		s.shutdownMu.Unlock()
		value := wireProcessFromApp(process)
		return wireResponse{Op: req.Op, OK: true, Process: &value}, false
	case "shutdown":
		err := s.shutdown(req.Force)
		if err != nil {
			return dispatchError(req.Op, err), false
		}
		return wireResponse{Op: req.Op, OK: true}, true
	default:
		return wireResponse{Error: &wireError{Code: "unknown_operation", Message: fmt.Sprintf("unknown operation %q", req.Op)}}, false
	}
}

func (s *Server) executeWait(ctx context.Context, req wireRequest) (wireResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	options, timeout, err := waitOptionsFromWire(req)
	if err != nil {
		return wireResponse{}, err
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	result, err := s.supervisor.Wait(waitCtx, req.Cwd, req.Name, options)
	if err != nil {
		return wireResponse{}, err
	}
	return wireResponseFromWait(result), nil
}

func (s *Server) handleWait(ctx context.Context, conn net.Conn, encoder *protocol.Encoder, req wireRequest) {
	waitCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	disconnected := make(chan struct{})
	go func() {
		var one [1]byte
		_, _ = conn.Read(one[:])
		close(disconnected)
		cancel()
	}()

	response, err := s.executeWait(waitCtx, req)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			select {
			case <-disconnected:
				return
			default:
			}
		}
		_ = writeProtocolError(encoder, protocol.OpWait, err)
		return
	}
	_ = writeProtocolResponse(encoder, response)
}

func waitOptionsFromWire(req wireRequest) (app.WaitOptions, time.Duration, error) {
	if req.Name == "" {
		return app.WaitOptions{}, 0, fmt.Errorf("%w: wait name is required", app.ErrInvalidRequest)
	}
	if req.TimeoutMS <= 0 {
		return app.WaitOptions{}, 0, fmt.Errorf("%w: wait timeout must be positive", app.ErrInvalidRequest)
	}
	if req.TimeoutMS > maxWaitTimeoutMS {
		return app.WaitOptions{}, 0, fmt.Errorf("%w: wait timeout exceeds server maximum", app.ErrInvalidRequest)
	}
	options := app.WaitOptions{}
	if req.After != nil {
		cursor := output.Cursor(*req.After)
		options.After = &cursor
	}
	if req.Match != "" {
		match, err := regexp.Compile(req.Match)
		if err != nil {
			return app.WaitOptions{}, 0, fmt.Errorf("%w: invalid match expression: %v", app.ErrInvalidRequest, err)
		}
		options.Match = match
	}
	return options, time.Duration(req.TimeoutMS) * time.Millisecond, nil
}

func (s *Server) handleFollow(ctx context.Context, conn net.Conn, encoder *protocol.Encoder, req wireRequest) {
	options, err := readOptionsFromWire(req)
	if err != nil {
		_ = writeProtocolError(encoder, protocol.OpFollow, err)
		return
	}
	sub, err := s.supervisor.Subscribe(req.Cwd, req.Name, options)
	if err != nil {
		_ = writeProtocolError(encoder, protocol.OpFollow, err)
		return
	}
	defer sub.Close()
	if err := encoder.EncodeResponse(protocol.StreamEvent{Op: protocol.OpEvent, Type: protocol.EventReady, Name: req.Name, Ready: true}); err != nil {
		return
	}
	followCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		var one [1]byte
		_, _ = conn.Read(one[:])
		cancel()
	}()
	for {
		event, err := sub.Next(followCtx)
		if err != nil {
			if errors.Is(err, output.ErrStoreClosed) {
				return
			}
			if !errors.Is(err, context.Canceled) && !errors.Is(err, io.EOF) {
				_ = encoder.EncodeResponse(protocol.StreamEvent{Op: protocol.OpEvent, Type: protocol.EventError, Name: req.Name, Error: wireErrorToProtocol(protocolWireError(err))})
			}
			return
		}
		if event.Exit != nil {
			exitText := fmt.Sprintf("%s exited with code %d\n", req.Name, event.Exit.Code)
			if event.Exit.Code < 0 {
				exitText = fmt.Sprintf("%s exited by signal\n", req.Name)
			}
			for _, text := range []string{exitText, fmt.Sprintf("%s waiting for next launch\n", req.Name)} {
				message := protocol.StreamEvent{Op: protocol.OpEvent, Type: protocol.EventOutput, Name: req.Name, Entries: []protocol.OutputEntry{{Stream: protocol.StreamSystem, Time: event.Exit.Time, Text: text}}}
				if err := encoder.EncodeResponse(message); err != nil {
					return
				}
			}
			continue
		}
		if err := encoder.EncodeResponse(protocolStreamEventFromOutput(req.Name, event)); err != nil {
			return
		}
	}
}

func readOptionsFromWire(req wireRequest) (output.ReadOptions, error) {
	options := output.ReadOptions{Tail: req.Tail, MaxEntries: req.MaxEntries, MaxBytes: req.MaxBytes}
	if req.After != nil {
		cursor := output.Cursor(*req.After)
		options.After = &cursor
	}
	if req.Match != "" {
		match, err := regexp.Compile(req.Match)
		if err != nil {
			return output.ReadOptions{}, fmt.Errorf("%w: invalid match expression: %v", app.ErrInvalidRequest, err)
		}
		options.Match = match
	}
	options.Streams = streamMask(req.Stream)
	return options, nil
}

func streamMask(stream string) output.StreamMask {
	switch strings.ToLower(stream) {
	case "stdout":
		return output.StdoutMask
	case "stderr":
		return output.StderrMask
	case "system":
		return output.SystemMask
	case "both", "stdout+stderr", "stdout,stderr":
		return output.AllStreams
	default:
		return output.AllStreams
	}
}

func parseSignal(name string) (os.Signal, error) {
	switch strings.ToUpper(strings.TrimPrefix(name, "SIG")) {
	case "INT":
		return syscall.SIGINT, nil
	case "TERM":
		return syscall.SIGTERM, nil
	case "HUP":
		return syscall.SIGHUP, nil
	case "QUIT":
		return syscall.SIGQUIT, nil
	case "KILL":
		return syscall.SIGKILL, nil
	default:
		return nil, fmt.Errorf("%w: %q", app.ErrInvalidSignal, name)
	}
}

// wire DTOs intentionally contain no environment field on responses.
type wireRequest struct {
	Op               string               `json:"op"`
	Version          int                  `json:"version,omitempty"`
	Name             string               `json:"name,omitempty"`
	Argv             []string             `json:"argv,omitempty"`
	Cwd              string               `json:"cwd,omitempty"`
	Root             string               `json:"root,omitempty"`
	Env              []string             `json:"env,omitempty"`
	Source           string               `json:"source,omitempty"`
	Ready            *wireReadinessConfig `json:"ready,omitempty"`
	Update           bool                 `json:"update,omitempty"`
	All              bool                 `json:"all,omitempty"`
	IncludeCompleted bool                 `json:"include_completed,omitempty"`
	After            *uint64              `json:"after,omitempty"`
	Tail             int                  `json:"tail,omitempty"`
	Stream           string               `json:"stream,omitempty"`
	Match            string               `json:"match,omitempty"`
	TimeoutMS        int64                `json:"timeout_ms,omitempty"`
	MaxEntries       int                  `json:"max_entries,omitempty"`
	MaxBytes         int                  `json:"max_bytes,omitempty"`
	Signal           string               `json:"signal,omitempty"`
	Force            bool                 `json:"force,omitempty"`
}
type wireReadinessConfig struct {
	Match   string        `json:"match"`
	Timeout time.Duration `json:"timeout"`
}

type wireResponse struct {
	Op             string        `json:"op,omitempty"`
	Name           string        `json:"name,omitempty"`
	OK             bool          `json:"ok,omitempty"`
	Version        int           `json:"version,omitempty"`
	Process        *wireProcess  `json:"process,omitempty"`
	Processes      []wireProcess `json:"processes,omitempty"`
	Entries        []wireEntry   `json:"entries,omitempty"`
	Next           *uint64       `json:"next,omitempty"`
	Oldest         *uint64       `json:"oldest,omitempty"`
	Latest         *uint64       `json:"latest,omitempty"`
	EvictedThrough *uint64       `json:"evicted_through,omitempty"`
	Truncated      bool          `json:"truncated,omitempty"`
	More           bool          `json:"more,omitempty"`
	Type           string        `json:"type,omitempty"`
	Outcome        string        `json:"outcome,omitempty"`
	Cursor         *uint64       `json:"cursor,omitempty"`
	Ready          bool          `json:"ready,omitempty"`
	Exit           *wireExit     `json:"exit,omitempty"`
	Error          *wireError    `json:"error,omitempty"`
}

type wireError struct {
	Code          string   `json:"code"`
	Message       string   `json:"message"`
	Details       any      `json:"details,omitempty"`
	DaemonVersion int      `json:"daemon_version,omitempty"`
	Processes     []string `json:"processes,omitempty"`
}

type wireProcess struct {
	Name         string           `json:"name"`
	Source       string           `json:"source,omitempty"`
	Root         string           `json:"root"`
	PID          int              `json:"pid"`
	PGID         int              `json:"pgid"`
	Cwd          string           `json:"cwd"`
	Argv         []string         `json:"argv"`
	Start        time.Time        `json:"start"`
	LaunchCursor uint64           `json:"launch_cursor"`
	NextCursor   *uint64          `json:"next_cursor,omitempty"`
	State        string           `json:"state"`
	Exit         *wireProcessExit `json:"exit,omitempty"`
	ExitCode     int              `json:"exit_code,omitempty"`
	ExitedAt     time.Time        `json:"exited_at,omitempty"`
	RestartCount int              `json:"restart_count,omitempty"`
	Readiness    *wireReadiness   `json:"readiness,omitempty"`
}

type wireReadiness struct {
	State  string    `json:"state"`
	Cursor *uint64   `json:"cursor,omitempty"`
	Time   time.Time `json:"time,omitempty"`
	Match  string    `json:"match,omitempty"`
}

type wireProcessExit struct {
	Code     int       `json:"code,omitempty"`
	ExitCode int       `json:"exit_code,omitempty"`
	Error    string    `json:"error,omitempty"`
	Time     time.Time `json:"time,omitempty"`
}

type wireReadResult struct {
	Entries        []wireEntry `json:"entries,omitempty"`
	Next           *uint64     `json:"next,omitempty"`
	Oldest         *uint64     `json:"oldest,omitempty"`
	Latest         *uint64     `json:"latest,omitempty"`
	EvictedThrough *uint64     `json:"evicted_through,omitempty"`
	Truncated      bool        `json:"truncated,omitempty"`
	More           bool        `json:"more,omitempty"`
}
type wireEntry struct {
	Cursor uint64    `json:"cursor"`
	Stream string    `json:"stream"`
	Time   time.Time `json:"time"`
	Text   string    `json:"text"`
}

type wireExit struct {
	Code  int       `json:"code"`
	Error string    `json:"error,omitempty"`
	Time  time.Time `json:"time"`
}

func protocolWireError(err error) *wireError {
	if err == nil {
		return nil
	}
	var version *VersionMismatchError
	if errors.As(err, &version) {
		return &wireError{Code: string(protocol.ErrorVersionMismatch), Message: version.Error(), Details: protocol.VersionMismatchDetails{Client: version.ClientVersion, Daemon: version.DaemonVersion}, DaemonVersion: version.DaemonVersion}
	}
	var active *ActiveProcessesError
	if errors.As(err, &active) {
		return &wireError{Code: string(protocol.ErrorActiveProcesses), Message: active.Error(), Details: append([]string(nil), active.Names...), Processes: append([]string(nil), active.Names...)}
	}
	var malformed *MalformedRequestError
	if errors.As(err, &malformed) {
		return &wireError{Code: string(protocol.ErrorMalformed), Message: malformed.Error()}
	}
	var oversized *RequestTooLargeError
	if errors.As(err, &oversized) {
		return &wireError{Code: string(protocol.ErrorOversized), Message: oversized.Error()}
	}
	return &wireError{Code: errorCode(err), Message: err.Error()}
}

func errorCode(err error) string {
	switch {
	case errors.Is(err, app.ErrProcessNotFound):
		return string(protocol.ErrorNotFound)
	case errors.Is(err, app.ErrNameInUse):
		return string(protocol.ErrorNameInUse)
	case errors.Is(err, app.ErrInvalidName), errors.Is(err, app.ErrInvalidRequest):
		return string(protocol.ErrorInvalidRequest)
	case errors.Is(err, app.ErrInvalidSignal):
		return string(protocol.ErrorInvalidSignal)
	case errors.Is(err, app.ErrSupervisorClosed):
		return string(protocol.ErrorSupervisorClosed)
	case errors.Is(err, output.ErrFutureCursor), errors.Is(err, output.ErrEntryTooLarge), errors.Is(err, output.ErrReadLimit):
		return string(protocol.ErrorOutput)
	default:
		return string(protocol.ErrorInternal)
	}
}

func wireProcessesFromApp(items []app.Process) []wireProcess {
	result := make([]wireProcess, 0, len(items))
	for _, item := range items {
		result = append(result, wireProcessFromApp(item))
	}
	return result
}

func wireProcessFromApp(item app.Process) wireProcess {
	result := wireProcess{
		Name: item.Name, Source: item.Source, Root: item.Root, PID: item.PID, PGID: item.PGID,
		Cwd: item.Cwd, Argv: append([]string(nil), item.Argv...), Start: item.Start,
		LaunchCursor: uint64(item.LaunchCursor), State: string(item.State),
		ExitCode: item.ExitCode, ExitedAt: item.ExitedAt, RestartCount: item.RestartCount,
	}
	if item.Readiness != nil {
		result.Readiness = &wireReadiness{
			State: item.Readiness.State, Cursor: cursorUint64(item.Readiness.Cursor),
			Time: item.Readiness.Time, Match: item.Readiness.Match,
		}
	}
	if item.Exit != nil {
		result.Exit = &wireProcessExit{Code: item.Exit.ExitCode, Error: errorString(item.Exit.Err), Time: item.Exit.ExitedAt}
	}
	return result
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func wireReadResultFromOutput(result output.ReadResult) *wireReadResult {
	wire := &wireReadResult{
		Entries:   make([]wireEntry, 0, len(result.Entries)),
		Truncated: result.Truncated, More: result.More,
	}
	for _, item := range result.Entries {
		wire.Entries = append(wire.Entries, wireEntry{Cursor: uint64(item.Cursor), Stream: streamName(item.Stream), Time: item.Time, Text: item.Text})
	}
	wire.Next = cursorUint64(result.Next)
	wire.Oldest = cursorUint64(result.Oldest)
	wire.Latest = cursorUint64(result.Latest)
	wire.EvictedThrough = cursorUint64(result.EvictedThrough)
	return wire
}

func eventTypeForRead(result output.ReadResult) string {
	if result.EvictedThrough != nil {
		return "eviction"
	}
	if result.Next != nil && len(result.Entries) == 0 {
		return "cursor"
	}
	return "output"
}

func cursorUint64(cursor *output.Cursor) *uint64 {
	if cursor == nil {
		return nil
	}
	value := uint64(*cursor)
	return &value
}
