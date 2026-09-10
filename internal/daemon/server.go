package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
	"time"

	"hum/internal/app"
	"hum/internal/output"
	"hum/internal/project"
	"hum/internal/protocol"
	sharedsignals "hum/internal/signals"
)

const (
	wireVersion              = protocol.Version
	defaultWireMaxLine       = protocol.DefaultMaxLineBytes
	maxWaitTimeoutMS   int64 = (1<<63 - 1) / int64(time.Millisecond)
	// maxBoundedReadBytes caps one bounded output response so its JSON encoding
	// fits within a single wire message.
	maxBoundedReadBytes = defaultWireMaxLine / 2
	// followerDrainGrace bounds how long a follower may take to accept the
	// final shutdown events before its transport is abandoned.
	followerDrainGrace = 2 * time.Second
)

// Server owns one app.Supervisor and exposes it over one private Unix socket.
// Managed processes are intentionally not children of any request handler:
// closing a client connection only cancels that request's response stream.
type Server struct {
	owner          *runtimeOwner
	paths          RuntimePaths
	listener       net.Listener
	supervisor     *app.Supervisor
	version        int
	maxLine        int
	log            *boundedLog
	warnings       []protocol.StartupWarning
	unresolvedDone chan struct{}

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
	followers         sync.WaitGroup
	closing           chan struct{}
	unresolvedOnce    sync.Once
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
	version := cfg.WireVersion
	if version <= 0 {
		version = wireVersion
	}
	maxLine := cfg.MaxLineBytes
	if maxLine <= 0 {
		maxLine = 64 * 1024
	}
	stopGrace := cfg.StopGrace
	var supervisor *app.Supervisor
	if cfg.Supervisor != nil {
		supervisor = cfg.Supervisor
	} else {
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
	supervisor.SetPersistenceHooks(owner.persistProcess, owner.removeProcess)
	warnings, err := owner.reconcileStartup(supervisor, stopGrace)
	if err != nil {
		if cfg.Supervisor == nil {
			_ = supervisor.Shutdown(context.Background())
		}
		owner.release()
		return nil, fmt.Errorf("startup reconciliation: %w", err)
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
	projects := make(map[string]struct{})
	for _, item := range supervisor.UnresolvedProcesses() {
		projects[item.Root] = struct{}{}
	}
	server := &Server{
		owner:          owner,
		paths:          paths,
		listener:       listener,
		supervisor:     supervisor,
		version:        version,
		maxLine:        maxLine,
		log:            log,
		projects:       projects,
		serveDone:      make(chan struct{}),
		ready:          make(chan struct{}),
		shutdownDone:   make(chan struct{}),
		closing:        make(chan struct{}),
		warnings:       append([]protocol.StartupWarning(nil), warnings...),
		unresolvedDone: make(chan struct{}),
	}
	go server.monitorUnresolved()
	return server, nil
}

// Paths returns the runtime artifacts owned by this server.
func (s *Server) Paths() RuntimePaths { return s.paths }

// Supervisor returns the application supervisor owned by this daemon.
func (s *Server) Supervisor() *app.Supervisor { return s.supervisor }

// StartupWarnings returns the reconciliation summary retained for this daemon
// lifetime. The returned slice is independent of server storage.
func (s *Server) StartupWarnings() []protocol.StartupWarning {
	if s == nil {
		return nil
	}
	return append([]protocol.StartupWarning(nil), s.warnings...)
}

func (s *Server) monitorUnresolved() {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			for _, item := range s.supervisor.UnresolvedProcesses() {
				group := RuntimeGroup{Scope: item.Scope, ProjectRoot: item.Root, Name: item.Name, LeaderPID: item.PID, PGID: item.PGID, StartIdentity: item.StartIdentity}
				if !runtimeGroupAlive(group.PGID) {
					// Keep blocking duplicate launches until durable state no longer
					// claims the unresolved group.
					if s.owner.removeProcess(item) == nil {
						_ = s.supervisor.ClearUnresolvedScoped(item.Scope, item.Root, item.Name)
					}
				}
			}
		case <-s.unresolvedDone:
			return
		}
	}
}

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
	s.unresolvedOnce.Do(func() { close(s.unresolvedDone) })
	s.shutdownMu.Unlock()

	var shutdownErr error
	// Supervisor.Shutdown deliberately ignores caller cancellation while it
	// performs TERM/KILL and waits for process-tree termination. The daemon
	// must not remove its socket while a managed group is still alive.
	if err := s.supervisor.Shutdown(context.Background()); err != nil {
		shutdownErr = errors.Join(shutdownErr, err)
	}
	// Follow handlers must flush the supervisor shutdown error before the
	// daemon exits and tears down their connections. Closing s.closing starts
	// the bounded drain so a client that stopped reading cannot block exit.
	close(s.closing)
	s.followers.Wait()
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
		scope := app.ScopeProject
		if root == "" {
			scope = app.ScopeGlobal
		}
		items, err := s.supervisor.ListScoped(scope, root, false)
		if err != nil {
			continue
		}
		for _, item := range items {
			if app.IsActiveState(item.State) {
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
	return s.listProcessesScoped(cwd, app.ScopeProject, all, includeCompleted)
}

func (s *Server) listProcessesScoped(cwd, scope string, all, includeCompleted bool) ([]app.Process, error) {
	first, err := s.supervisor.ListScoped(scope, cwd, includeCompleted)
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
		// An empty tracked root records the global scope, which is listed
		// separately below. Listing it as a project would rediscover the
		// caller's own root and duplicate every record in it.
		if root == "" {
			continue
		}
		if _, ok := seen[root]; ok {
			continue
		}
		other, err := s.supervisor.ListScoped(app.ScopeProject, root, includeCompleted)
		if err != nil {
			continue
		}
		items = append(items, other...)
	}
	if scope != app.ScopeGlobal {
		global, globalErr := s.supervisor.ListScoped(app.ScopeGlobal, "", includeCompleted)
		if globalErr == nil {
			items = append(items, global...)
		}
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
	decoder := protocol.NewDecoder(conn, defaultWireMaxLine)
	encoder := protocol.NewEncoder(conn, defaultWireMaxLine)
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
	} else if err := encoder.EncodeResponse(protocol.HelloResponse{Op: protocol.OpHello, Version: s.version, Warnings: s.StartupWarnings()}); err != nil {
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
		if err := normalizeProtocolScope(&protocolReq); err != nil {
			_ = writeProtocolError(encoder, protocolReq.Op, err)
			continue
		}
		shutdownResponseRegistered := protocolReq.Op == protocol.OpShutdown && s.registerShutdownResponse()
		if protocolReq.Op == protocol.OpFollow {
			s.handleFollow(ctx, conn, encoder, *protocolReq.Follow)
			return
		}
		if protocolReq.Op == protocol.OpInputAttach {
			s.handleInput(ctx, conn, decoder, encoder, *protocolReq.InputAttach)
			return
		}
		if protocolReq.Op == protocol.OpWait {
			s.handleWait(ctx, conn, encoder, *protocolReq.Wait)
			return
		}
		resp, terminal := s.dispatch(&protocolReq)
		writeErr := encoder.EncodeResponse(resp)

		var oversized *protocol.OversizedError
		if errors.As(writeErr, &oversized) {
			message := fmt.Sprintf("response of %d bytes exceeds the %d byte message limit; request fewer entries or bytes", oversized.Size, oversized.Limit)
			writeErr = writeProtocolError(encoder, protocolReq.Op, protocol.NewWireError(protocol.ErrorOversized, message, nil))
		}
		if shutdownResponseRegistered {
			s.shutdownResponses.Done()
		}
		if writeErr != nil || terminal {
			return
		}
	}
}

func manifestStopGrace(root, name string) *time.Duration {
	if root == "" || name == "" {
		return nil
	}
	definitions, err := project.LoadDefinitions(root)
	if err != nil {
		return nil
	}
	for _, definition := range definitions {
		if definition.Name == name {
			if definition.StopGrace == nil {
				return nil
			}
			grace := *definition.StopGrace
			return &grace
		}
	}
	return nil
}

func dispatchError(op protocol.Operation, err error) protocol.ErrorResponse {
	return protocol.ErrorResponse{Op: op, OK: false, Error: protocolWireError(err)}
}

func (s *Server) dispatch(req *protocol.Request) (any, bool) {
	switch req.Op {
	case protocol.OpStart:
		value := req.Start
		if value.Root != "" {
			canonical, err := canonicalRequestRoot(value.Root)
			if err != nil {
				return dispatchError(req.Op, err), false
			}
			value.Root = canonical
		}
		s.shutdownMu.Lock()
		if s.shutdownStarted {
			s.shutdownMu.Unlock()
			return dispatchError(req.Op, app.ErrSupervisorClosed), false
		}
		if value.Cwd == "" {
			value.Cwd = "."
		}
		if value.StopGrace == nil && project.IsManifestSource(value.Source) {
			value.StopGrace = manifestStopGrace(value.Root, value.Name)
		}
		var size *app.TTYSize
		if value.TTYSize != nil {
			size = &app.TTYSize{Columns: value.TTYSize.Columns, Rows: value.TTYSize.Rows}
		}
		launched, err := s.supervisor.Start(app.StartRequest{Name: value.Name, Scope: value.Scope, Source: value.Source, Root: value.Root, Argv: value.Argv, Cwd: value.Cwd, Env: append([]string(nil), value.Env...), Ready: appReadinessConfigFromProtocol(value.Ready), TTY: value.TTY, TTYSize: size, Restart: app.RestartPolicy(value.Restart), StopGrace: value.StopGrace, Attached: value.Attached})
		if err != nil {
			s.shutdownMu.Unlock()
			return dispatchError(req.Op, err), false
		}
		s.trackProcess(launched)
		s.shutdownMu.Unlock()
		item := protocolProcessFromApp(launched)
		return protocol.StartResponse{Op: req.Op, OK: true, Process: &item, Warnings: s.StartupWarnings()}, false
	case protocol.OpList:
		value := req.List
		if value.Cwd == "" {
			value.Cwd = "."
		}
		items, err := s.listProcessesScoped(value.Cwd, value.Scope, value.All, value.IncludeCompleted)
		if err != nil {
			return dispatchError(req.Op, err), false
		}
		for _, item := range items {
			s.trackProcess(item)
		}
		return protocol.ListResponse{Op: req.Op, OK: true, Processes: protocolProcessesFromApp(items), Warnings: s.StartupWarnings()}, false
	case protocol.OpGet:
		value := req.Get
		item, err := s.supervisor.GetScoped(value.Scope, value.Cwd, value.Name)
		if err != nil {
			return dispatchError(req.Op, err), false
		}
		s.trackProcess(item)
		snapshot := protocolProcessFromApp(item)
		cursor := protocol.Cursor(item.NextCursor)
		snapshot.NextCursor = &cursor
		return protocol.GetResponse{Op: req.Op, OK: true, Process: &snapshot, Warnings: s.StartupWarnings()}, false
	case protocol.OpOutput:
		value := req.Output
		store, err := s.supervisor.OutputScoped(value.Scope, value.Cwd, value.Name)
		if err != nil {
			return dispatchError(req.Op, err), false
		}
		options, err := readOptionsFromProtocol(*value)
		if err != nil {
			return dispatchError(req.Op, err), false
		}
		result, err := store.Read(options)
		if err != nil {
			return dispatchError(req.Op, err), false
		}
		return protocolOutputResponse(req.Op, stripBoundedChildText(result)), false
	case protocol.OpSignal:
		value := req.Signal
		parsed, err := sharedsignals.Parse(value.Signal)
		if err != nil {
			return dispatchError(req.Op, err), false
		}
		var signalErr error
		if value.Control {
			signalErr = s.supervisor.SignalControlScoped(value.Scope, value.Cwd, value.Name, parsed.Signal)
		} else {
			signalErr = s.supervisor.SignalObservationalScoped(value.Scope, value.Cwd, value.Name, parsed.Signal)
		}
		if signalErr != nil {
			return dispatchError(req.Op, signalErr), false
		}
		return protocol.SignalResponse{Op: req.Op, OK: true, Name: value.Name, Signal: &protocol.SignalInfo{Name: parsed.Name, Number: parsed.Number}, Status: "sent"}, false
	case protocol.OpStop:
		value := req.Stop
		if err := s.supervisor.StopScoped(context.Background(), value.Scope, value.Cwd, value.Name); err != nil {
			return dispatchError(req.Op, err), false
		}
		var snapshot *protocol.Process
		if item, err := s.supervisor.GetScoped(value.Scope, value.Cwd, value.Name); err == nil {
			s.trackProcess(item)
			v := protocolProcessFromApp(item)
			snapshot = &v
		}
		return protocol.StopResponse{Op: req.Op, OK: true, Process: snapshot}, false
	case protocol.OpRemove:
		value := req.Remove
		if err := s.supervisor.RemoveScoped(context.Background(), value.Scope, value.Cwd, value.Name); err != nil {
			return dispatchError(req.Op, err), false
		}
		return protocol.RemoveResponse{Op: req.Op, OK: true}, false
	case protocol.OpRestart:
		value := req.Restart
		if value.Root != "" {
			canonical, err := canonicalRequestRoot(value.Root)
			if err != nil {
				return dispatchError(req.Op, err), false
			}
			value.Root = canonical
		}
		s.shutdownMu.Lock()
		if s.shutdownStarted {
			s.shutdownMu.Unlock()
			return dispatchError(req.Op, app.ErrSupervisorClosed), false
		}
		if value.StopGrace == nil && value.Update && project.IsManifestSource(value.Source) {
			value.StopGrace = manifestStopGrace(value.Root, value.Name)
		}
		var size *app.TTYSize
		if value.TTYSize != nil {
			size = &app.TTYSize{Columns: value.TTYSize.Columns, Rows: value.TTYSize.Rows}
		}
		options := app.RestartOptions{Update: value.Update, Source: value.Source, Root: value.Root, Cwd: value.Cwd, Argv: append([]string(nil), value.Argv...), Env: append([]string(nil), value.Env...), Ready: appReadinessConfigFromProtocol(value.Ready), TTY: value.TTY, TTYSize: size, Restart: app.RestartPolicy(value.Restart), StopGrace: value.StopGrace, Scope: value.Scope}
		launched, err := s.supervisor.RestartScoped(context.Background(), value.Scope, value.Cwd, value.Name, options)
		if err != nil {
			s.shutdownMu.Unlock()
			return dispatchError(req.Op, err), false
		}
		s.trackProcess(launched)
		s.shutdownMu.Unlock()
		snapshot := protocolProcessFromApp(launched)
		return protocol.RestartResponse{Op: req.Op, OK: true, Process: &snapshot}, false
	case protocol.OpShutdown:
		if err := s.shutdown(req.Shutdown.Force); err != nil {
			return dispatchError(req.Op, err), false
		}
		return protocol.ShutdownResponse{Op: req.Op, OK: true}, true
	default:
		// A known op that this connection cannot dispatch (a repeated hello or
		// an input op outside an attach) is a protocol misuse, not a daemon
		// fault; keep the pre-refactor unknown_operation code and blank op.
		return protocol.ErrorResponse{OK: false, Error: protocol.NewWireError(protocol.ErrorUnknownOperation, fmt.Sprintf("unknown operation %q", req.Op), nil)}, false
	}
}

func (s *Server) executeWait(ctx context.Context, req protocol.WaitRequest) (protocol.WaitResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	options, timeout, err := waitOptionsFromProtocol(req)
	if err != nil {
		return protocol.WaitResponse{}, err
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	result, err := s.supervisor.WaitScoped(waitCtx, req.Scope, req.Cwd, req.Name, options)
	if err != nil {
		return protocol.WaitResponse{}, err
	}
	return protocolWaitResponse(result), nil
}

func (s *Server) handleWait(ctx context.Context, conn net.Conn, encoder *protocol.Encoder, req protocol.WaitRequest) {
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
	_ = encoder.EncodeResponse(response)
}

func (s *Server) handleFollow(ctx context.Context, conn net.Conn, encoder *protocol.Encoder, req protocol.FollowRequest) {
	s.shutdownMu.Lock()
	if s.shutdownStarted {
		s.shutdownMu.Unlock()
		_ = writeProtocolError(encoder, protocol.OpFollow, app.ErrSupervisorClosed)
		return
	}
	s.followers.Add(1)
	s.shutdownMu.Unlock()
	defer s.followers.Done()

	options, err := readOptionsFromFollow(req)
	if err != nil {
		_ = writeProtocolError(encoder, protocol.OpFollow, err)
		return
	}
	sub, err := s.supervisor.SubscribeScoped(req.Scope, req.Cwd, req.Name, options)
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
	go func() {
		// After the supervisor has shut down, a follower that no longer reads
		// its socket must not hold the daemon open: bound any blocked write,
		// then cancel the stream once the drain window has elapsed.
		select {
		case <-s.closing:
			_ = conn.SetWriteDeadline(time.Now().Add(followerDrainGrace))
			timer := time.NewTimer(followerDrainGrace)
			defer timer.Stop()
			select {
			case <-timer.C:
				cancel()
			case <-followCtx.Done():
			}
		case <-followCtx.Done():
		}
	}()
	for {
		event, err := sub.Next(followCtx)
		if err != nil {
			if errors.Is(err, output.ErrStoreClosed) {
				return
			}
			if !errors.Is(err, context.Canceled) && !errors.Is(err, io.EOF) {
				_ = encoder.EncodeResponse(protocol.StreamEvent{Op: protocol.OpEvent, Type: protocol.EventError, Name: req.Name, Error: protocolWireError(err)})
			}
			return
		}
		if event.Exit != nil {
			if err := encoder.EncodeResponse(protocolStreamEventFromOutput(req.Name, event)); err != nil {
				return
			}
			if req.UntilExit {
				return
			}
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

func canonicalRequestRoot(root string) (string, error) {
	if root == "" || !filepath.IsAbs(root) {
		return "", fmt.Errorf("%w: project root must be an absolute existing directory", app.ErrInvalidRequest)
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("%w: project root must be an absolute existing directory", app.ErrInvalidRequest)
	}
	canonical, err := project.CanonicalPath(root)
	if err != nil {
		return "", fmt.Errorf("%w: project root: %v", app.ErrInvalidRequest, err)
	}
	return canonical, nil
}

func (s *Server) handleInput(ctx context.Context, conn net.Conn, decoder *protocol.Decoder, encoder *protocol.Encoder, req protocol.InputAttachRequest) {
	if req.Root != "" {
		canonical, err := canonicalRequestRoot(req.Root)
		if err != nil {
			_ = writeProtocolError(encoder, protocol.OpInputAttach, err)
			return
		}
		req.Root = canonical
	}
	if !req.TTY {
		_ = writeProtocolError(encoder, protocol.OpInputAttach, app.ErrInputNotTTY)
		return
	}
	var size *app.TTYSize
	if req.Columns != 0 || req.Rows != 0 {
		size = &app.TTYSize{Columns: req.Columns, Rows: req.Rows}
	}
	if len(req.Argv) != 0 {
		if err := s.supervisor.PrepareTTYScoped(req.Scope, app.StartRequest{Name: req.Name, Scope: req.Scope, Root: req.Root, Cwd: req.Cwd, Argv: req.Argv, Source: req.Source, Ready: appReadinessConfigFromProtocol(req.Ready), TTY: true, TTYSize: size}); err != nil {
			_ = writeProtocolError(encoder, protocol.OpInputAttach, err)
			return
		}
	}
	lease, err := s.supervisor.AcquireInputAtScoped(req.Scope, req.Root, req.Cwd, req.Name, true, size)
	if err != nil {
		_ = writeProtocolError(encoder, protocol.OpInputAttach, err)
		return
	}

	// inputCtx is canceled by lease closure as well as transport loss. Closing
	// conn from the watcher is essential: the main goroutine may be blocked in
	// DecodeRequest while Remove, detach, or daemon shutdown closes only the
	// application lease.
	inputCtx, inputCancel := context.WithCancel(ctx)
	defer inputCancel()
	leaseDone := lease.Done()
	// An explicit input_release keeps this connection alive long enough for the
	// post-release acknowledgement. Other lease closures still cancel and
	// close the transport so a blocked decoder is released promptly.
	releaseRequested := make(chan struct{})
	transportDone := make(chan struct{})
	go func() {
		defer close(transportDone)
		select {
		case <-leaseDone:
			select {
			case <-releaseRequested:
				return
			default:
				inputCancel()
				_ = conn.Close()
			}
		case <-inputCtx.Done():
		}
	}()
	defer func() {
		inputCancel()
		_ = conn.Close()
		lease.Release()
		<-transportDone
	}()
	if err := encoder.EncodeResponse(protocol.InputAttachResponse{Op: protocol.OpInputAttach, OK: true}); err != nil {
		return
	}
	initial, err := lease.Next(inputCtx)
	if err != nil {
		return
	}
	writeMu := sync.Mutex{}
	writeState := func(event app.InputEvent) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return encoder.EncodeResponse(protocol.InputStateEvent{Op: protocol.OpInputState, State: string(event.State), LaunchCursor: protocol.Cursor(event.LaunchCursor), TTY: event.TTY})
	}
	if err := writeState(initial); err != nil {
		return
	}
	eventDone := make(chan struct{})
	go func() {
		defer close(eventDone)
		for {
			event, eventErr := lease.Next(inputCtx)
			if eventErr != nil {
				return
			}
			if writeState(event) != nil {
				inputCancel()
				_ = conn.Close()
				return
			}
		}
	}()
	defer func() { inputCancel(); _ = conn.Close(); <-eventDone }()
	for {
		request, decodeErr := decoder.DecodeRequest()
		if decodeErr != nil {
			if errors.Is(decodeErr, io.EOF) {
				return
			}
			_ = writeProtocolError(encoder, protocol.Operation(""), decodeErr)
			return
		}
		var response error
		switch request.Op {
		case protocol.OpInputWrite:
			inputRequest := *request.InputWrite
			data, err := inputRequest.InputBytes()
			if err != nil {
				response = err
			} else {
				response = runInputOperation(inputCtx, conn, func(operationCtx context.Context) error {
					return lease.Write(operationCtx, output.Cursor(inputRequest.LaunchCursor), data)
				})
			}
			ack := protocol.InputAckResponse{Op: protocol.OpInputWrite, OK: response == nil, Error: protocolWireError(response)}
			if err == nil {
				ack.LaunchCursor, ack.Written = inputRequest.LaunchCursor, len(data)
			}
			writeMu.Lock()
			writeErr := encoder.EncodeResponse(ack)
			writeMu.Unlock()
			if writeErr != nil {
				return
			}
		case protocol.OpInputResize:
			inputRequest := *request.InputResize
			response = runInputOperation(inputCtx, conn, func(operationCtx context.Context) error {
				return lease.Resize(operationCtx, output.Cursor(inputRequest.LaunchCursor), inputRequest.Columns, inputRequest.Rows)
			})
			ack := protocol.InputAckResponse{Op: protocol.OpInputResize, OK: response == nil, LaunchCursor: inputRequest.LaunchCursor, Error: protocolWireError(response)}
			writeMu.Lock()
			writeErr := encoder.EncodeResponse(ack)
			writeMu.Unlock()
			if writeErr != nil {
				return
			}
		case protocol.OpInputRelease:
			close(releaseRequested)
			lease.Release()
			ack := protocol.InputAckResponse{Op: protocol.OpInputRelease, OK: true}
			writeMu.Lock()
			writeErr := encoder.EncodeResponse(ack)
			writeMu.Unlock()
			if writeErr != nil {
				return
			}
			return
		default:
			response = fmt.Errorf("%w: input connection does not accept %q", app.ErrInvalidRequest, request.Op)
			ack := protocol.InputAckResponse{Op: request.Op, OK: false, Error: protocolWireError(response)}
			writeMu.Lock()
			writeErr := encoder.EncodeResponse(ack)
			writeMu.Unlock()
			if writeErr != nil {
				return
			}
		}
	}
}

// runInputOperation watches the dedicated connection while a write or resize
// may block in the child. The client sends requests serially and waits for an
// acknowledgement, so peeking one byte here cannot consume a valid next
// request; it lets a client close cancel a stalled operation without affecting
// unrelated connections.
func runInputOperation(ctx context.Context, conn net.Conn, operation func(context.Context) error) error {
	operationCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		var one [1]byte
		_, err := conn.Read(one[:])
		if err != nil {
			cancel()
		}
	}()
	err := operation(operationCtx)
	cancel()
	_ = conn.SetReadDeadline(time.Now())
	<-watchDone
	_ = conn.SetReadDeadline(time.Time{})
	return err
}

func stripBoundedChildText(result output.ReadResult) output.ReadResult {
	var entries []output.Entry
	for index, entry := range result.Entries {
		if entry.Stream != output.Stdout && entry.Stream != output.Stderr {
			continue
		}
		stripped := output.StripTerminalControl(entry.Text)
		if stripped == entry.Text {
			continue
		}
		if entries == nil {
			entries = append([]output.Entry(nil), result.Entries...)
		}
		entries[index].Text = stripped
	}
	if entries != nil {
		result.Entries = entries
	}
	return result
}

const maxSinceMilliseconds int64 = (1<<63 - 1) / int64(time.Millisecond)

func errorCode(err error) string {
	switch {
	case errors.Is(err, app.ErrProcessNotFound):
		return string(protocol.ErrorNotFound)
	case errors.Is(err, app.ErrNotRunning):
		return string(protocol.ErrorNotRunning)
	case errors.Is(err, app.ErrNameInUse):
		return string(protocol.ErrorNameInUse)
	case errors.Is(err, app.ErrInvalidName), errors.Is(err, app.ErrInvalidRequest):
		return string(protocol.ErrorInvalidRequest)
	case errors.Is(err, app.ErrInvalidSignal), errors.Is(err, sharedsignals.ErrInvalidSignal):
		return string(protocol.ErrorInvalidSignal)
	case errors.Is(err, app.ErrSupervisorClosed):
		return string(protocol.ErrorSupervisorClosed)
	case errors.Is(err, output.ErrFutureCursor), errors.Is(err, output.ErrEntryTooLarge), errors.Is(err, output.ErrReadLimit):
		return string(protocol.ErrorOutput)
	case errors.Is(err, app.ErrInputConflict):
		return string(protocol.ErrorInputConflict)
	case errors.Is(err, app.ErrInputTooLarge):
		return string(protocol.ErrorInputTooLarge)
	case errors.Is(err, app.ErrInputClosed):
		return string(protocol.ErrorInputClosed)
	case errors.Is(err, app.ErrInputStale):
		return string(protocol.ErrorInputStale)
	case errors.Is(err, app.ErrInputNotTTY):
		return string(protocol.ErrorInputNotTTY)
	case errors.Is(err, app.ErrUnresolved):
		return string(protocol.ErrorUnresolved)
	default:
		return string(protocol.ErrorInternal)
	}
}
