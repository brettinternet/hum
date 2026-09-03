package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"hum/internal/app"
	"hum/internal/daemon"
	"hum/internal/output"
	"hum/internal/protocol"

	urfavecli "github.com/urfave/cli/v3"
)

func newCLICommands(version, buildTime string, writer, errWriter io.Writer) []*urfavecli.Command {
	runStopOnNthArg := 1
	return []*urfavecli.Command{
		{
			Name:      "serve",
			Usage:     "run the hum daemon in the foreground",
			ArgsUsage: "",
			Flags: []urfavecli.Flag{
				&urfavecli.BoolFlag{Name: "daemon", Usage: "run the hum daemon detached"},
			},
			Action: func(ctx context.Context, cmd *urfavecli.Command) error {
				return serveCommand(ctx, cmd, version, buildTime, errWriter)
			},
		},
		{
			Name:         "run",
			Usage:        "start a named process",
			ArgsUsage:    "NAME -- COMMAND [ARGS...]",
			StopOnNthArg: &runStopOnNthArg,
			Flags: []urfavecli.Flag{
				&urfavecli.BoolFlag{Name: "detach", Usage: "start the process without attaching"},
				&urfavecli.BoolFlag{Name: "json", Usage: "write stable JSON"},
			},
			Action: func(ctx context.Context, cmd *urfavecli.Command) error {
				return runCommand(ctx, cmd, version, buildTime, writer, errWriter)
			},
		},
		{
			Name:      "list",
			Usage:     "list supervised processes",
			ArgsUsage: "",
			Flags: []urfavecli.Flag{
				&urfavecli.BoolFlag{Name: "all", Usage: "list processes from every project"},
				&urfavecli.BoolFlag{Name: "json", Usage: "write stable JSON"},
			},
			Action: func(ctx context.Context, cmd *urfavecli.Command) error {
				return listCommand(ctx, cmd, version, buildTime, writer)
			},
		},
		{
			Name:      "status",
			Usage:     "show one supervised process",
			ArgsUsage: "NAME",
			Flags: []urfavecli.Flag{
				&urfavecli.BoolFlag{Name: "json", Usage: "write stable JSON"},
			},
			Action: func(ctx context.Context, cmd *urfavecli.Command) error {
				return statusCommand(ctx, cmd, version, buildTime, writer)
			},
		},
		{
			Name:      "logs",
			Usage:     "read retained process output",
			ArgsUsage: "NAME",
			Flags: []urfavecli.Flag{
				&urfavecli.StringFlag{Name: "stream", Value: "both", Usage: "select stdout, stderr, or both"},
				&urfavecli.IntFlag{Name: "tail", Usage: "select the final N entries"},
				&urfavecli.Uint64Flag{Name: "after-cursor", Usage: "read entries after this cursor"},
				&urfavecli.IntFlag{Name: "limit-bytes", Usage: "limit returned output bytes"},
				&urfavecli.StringFlag{Name: "match", Usage: "filter entries by regular expression"},
				&urfavecli.BoolFlag{Name: "follow", Usage: "follow output until process exit"},
				&urfavecli.BoolFlag{Name: "json", Usage: "write stable JSON"},
			},
			Action: func(ctx context.Context, cmd *urfavecli.Command) error {
				return logsCommand(ctx, cmd, version, buildTime, writer, errWriter)
			},
		},
		{
			Name:        "wait",
			Usage:       "wait for matching output or process exit",
			ArgsUsage:   "NAME",
			Description: "Wait without --match returns when the process exits; waiting for declared readiness uses hum start <name>.",
			Flags: []urfavecli.Flag{
				&urfavecli.Uint64Flag{Name: "after-cursor", Usage: "search entries after this cursor"},
				&urfavecli.StringFlag{Name: "match", Usage: "wait for output matching this regular expression"},
				&urfavecli.StringFlag{Name: "timeout", Usage: "maximum wait duration (default 30s)"},
				&urfavecli.BoolFlag{Name: "json", Usage: "write stable JSON"},
			},
			Action: func(ctx context.Context, cmd *urfavecli.Command) error {
				return waitCommand(ctx, cmd, version, buildTime, writer)
			},
		},
		{
			Name:      "stop",
			Usage:     "stop one or more supervised processes",
			ArgsUsage: "NAME...",
			Flags: []urfavecli.Flag{
				&urfavecli.BoolFlag{Name: "json", Usage: "write stable JSON"},
			},
			Action: func(ctx context.Context, cmd *urfavecli.Command) error {
				return stopCommand(ctx, cmd, version, buildTime, writer)
			},
		},
		{
			Name:      "shutdown",
			Usage:     "shut down the hum daemon",
			ArgsUsage: "",
			Flags: []urfavecli.Flag{
				&urfavecli.BoolFlag{Name: "stop-processes", Usage: "stop managed processes before shutdown"},
				&urfavecli.BoolFlag{Name: "json", Usage: "write stable JSON"},
			},
			Action: func(ctx context.Context, cmd *urfavecli.Command) error {
				return shutdownCommand(ctx, cmd, version, buildTime, writer)
			},
		},
	}
}

func serveCommand(ctx context.Context, cmd *urfavecli.Command, version, buildTime string, errWriter io.Writer) error {
	if err := requireNoArgs(cmd, "serve"); err != nil {
		return err
	}
	ctx = nonNilContext(ctx)
	cfg, err := cliConfig(cmd, version, buildTime)
	if err != nil {
		return err
	}
	dcfg, err := daemonConfig(cfg)
	if err != nil {
		return err
	}
	if cmd.Bool("daemon") {
		pid, err := ensureDaemon(ctx, cfg)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(errWriter, "hum serve: listening on %s (PID %d)\n", daemon.NewRuntimePaths(cfg.RuntimeDir).Socket, pid)
		return err
	}
	server, err := daemon.NewServer(dcfg)
	if err != nil {
		return err
	}
	serveCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	defer signal.Stop(signals)
	go func() {
		select {
		case <-signals:
			cancel()
		case <-serveCtx.Done():
		}
	}()
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- server.Serve(serveCtx)
	}()
	if err := server.WaitReady(serveCtx); err != nil {
		if isDaemonChild() {
			server.Logf("hum serve: readiness failed: %v\n", err)
		}
		_ = server.Close()
		<-serveDone
		return err
	}
	if isDaemonChild() {
		server.Logf("hum serve: listening on %s (PID %d)\n", server.SocketPath(), server.PID())
		return <-serveDone
	}
	if _, err := fmt.Fprintf(errWriter, "hum serve: listening on %s (PID %d)\n", server.SocketPath(), server.PID()); err != nil {
		_ = server.Close()
		<-serveDone
		return err
	}
	return <-serveDone
}

func parseRunArgs(cmd *urfavecli.Command) (string, []string, error) {
	args := cmd.Args().Slice()
	if len(args) == 0 {
		return "", nil, errors.New("run requires a process name")
	}
	separator := -1
	for i, arg := range args {
		if arg == "--" {
			separator = i
			break
		}
	}
	if separator < 0 && len(args) >= 2 && !strings.HasPrefix(args[1], "-") {
		argv := append([]string(nil), args[1:]...)
		if argv[0] == "" {
			return "", nil, errors.New("run requires a non-empty command after --")
		}
		return args[0], argv, nil
	}
	if separator < 0 {
		return "", nil, errors.New("run requires a command after --")
	}
	if separator < 2 {
		return "", nil, errors.New("run accepts exactly one process name before --")
	}
	for i := 1; i < separator; i++ {
		flag := args[i]
		name, value, hasValue := strings.Cut(strings.TrimPrefix(flag, "--"), "=")
		switch name {
		case "detach", "json":
			if hasValue {
				return "", nil, fmt.Errorf("--%s does not take a value", name)
			}
			if err := cmd.Set(name, "true"); err != nil {
				return "", nil, err
			}
		case "runtime-dir", "stop-grace", "output-bytes", "completed-records":
			if !hasValue {
				i++
				if i >= separator {
					return "", nil, fmt.Errorf("--%s requires a value", name)
				}
				value = args[i]
			}
			if err := cmd.Set(name, value); err != nil {
				return "", nil, err
			}
		default:
			return "", nil, fmt.Errorf("unknown run option %q", flag)
		}
	}
	argv := append([]string(nil), args[separator+1:]...)
	if len(argv) == 0 || argv[0] == "" {
		return "", nil, errors.New("run requires a non-empty command after --")
	}
	return args[0], argv, nil
}

func runCommand(ctx context.Context, cmd *urfavecli.Command, version, buildTime string, writer, errWriter io.Writer) error {
	name, argv, err := parseRunArgs(cmd)
	if err != nil {
		return err
	}
	ctx = nonNilContext(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	cfg, err := cliConfig(cmd, version, buildTime)
	if err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("current directory: %w", err)
	}
	client, err := runDaemonClient(ctx, cfg)
	if err != nil {
		return err
	}
	defer client.Close()
	signals := notifyFollowSignals()
	defer signal.Stop(signals)
	process, err := client.Start(ctx, daemon.StartRequest{Name: name, Argv: argv, Cwd: cwd, Env: os.Environ()})
	if err != nil {
		if isNameInUse(err) || errors.Is(err, app.ErrNameInUse) {
			return fmt.Errorf("%w; watch it with hum logs %s --follow", err, name)
		}
		return err
	}
	if cmd.Bool("detach") {
		if cmd.Bool("json") {
			return encodeJSON(writer, runResult{
				Name:   process.Name,
				PID:    process.PID,
				Cursor: protocol.Cursor(process.LaunchCursor),
			})
		}
		_, err := fmt.Fprintf(writer, "started %s (PID %d, cursor %d)\n", process.Name, process.PID, process.LaunchCursor)
		return err
	}

	followRequest := daemon.FollowRequest{
		Name:       name,
		Cwd:        cwd,
		Stream:     protocol.StreamBoth,
		MaxEntries: cfg.ReadEntries,
		MaxBytes:   int(cfg.ReadBytes),
	}
	follower, err := client.Follow(context.Background(), followRequest)
	if err != nil {
		return err
	}
	defer follower.Close()
	interrupts := 0
	exitCode, exited, err := followLoop(ctx, follower, signals, func(event output.Event) error {
		if event.Read == nil {
			return nil
		}
		for _, entry := range event.Read.Entries {
			if err := writeAttachedEntry(writer, errWriter, entry); err != nil {
				return err
			}
		}
		return nil
	}, func(sig os.Signal) (bool, error) {
		if sig == os.Interrupt {
			interrupts++
			if interrupts == 1 {
				err := client.Signal(context.Background(), daemon.SignalRequest{Name: name, Cwd: cwd, Signal: "SIGINT"})
				if isNotFound(err) {
					return false, nil
				}
				return false, err
			}
			err := client.Stop(context.Background(), daemon.StopRequest{Name: name, Cwd: cwd})
			if isNotFound(err) {
				return false, nil
			}
			return false, err
		}
		if sig == syscall.SIGTERM || sig == syscall.SIGHUP {
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		return err
	}
	if !exited {
		return nil
	}
	if exitCode == 0 {
		return nil
	}
	return urfavecli.Exit("", exitCode)
}

func listCommand(ctx context.Context, cmd *urfavecli.Command, version, buildTime string, writer io.Writer) error {
	if err := requireNoArgs(cmd, "list"); err != nil {
		return err
	}
	ctx = nonNilContext(ctx)
	cfg, err := cliConfig(cmd, version, buildTime)
	if err != nil {
		return err
	}
	client, err := daemonClient(ctx, cfg)
	if err != nil {
		if daemonUnavailable(err) {
			if cmd.Bool("json") {
				return encodeJSON(writer, listJSON{Processes: []protocol.Process{}})
			}
			_, writeErr := fmt.Fprintln(writer, stopUnavailableMessage)
			return writeErr
		}
		return err
	}
	defer client.Close()
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("current directory: %w", err)
	}
	processes, err := client.List(ctx, daemon.ListRequest{Cwd: cwd, All: cmd.Bool("all")})
	if err != nil {
		return err
	}
	if cmd.Bool("json") {
		items := make([]protocol.Process, 0, len(processes))
		for _, process := range processes {
			items = append(items, processJSON(process))
		}
		return encodeJSON(writer, listJSON{Processes: items})
	}
	return renderListHuman(writer, processes, cmd.Bool("all"))
}

func statusCommand(ctx context.Context, cmd *urfavecli.Command, version, buildTime string, writer io.Writer) error {
	args := cmd.Args().Slice()
	if len(args) == 0 {
		return errors.New("status requires a process name")
	}
	if len(args) != 1 {
		return errors.New("status accepts exactly one process name")
	}
	name := args[0]
	ctx = nonNilContext(ctx)
	cfg, err := cliConfig(cmd, version, buildTime)
	if err != nil {
		return err
	}
	client, err := daemonClient(ctx, cfg)
	if err != nil {
		if client != nil {
			_ = client.Close()
		}
		if daemonUnavailable(err) {
			return newUserFacingError(logsUnavailableMessage)
		}
		return err
	}
	defer client.Close()
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("current directory: %w", err)
	}
	process, err := client.Get(ctx, daemon.GetRequest{Name: name, Cwd: cwd})
	if err != nil {
		return err
	}
	if cmd.Bool("json") {
		return encodeJSON(writer, statusJSONFor(process))
	}
	return renderStatusHuman(writer, process)
}

func logsCommand(ctx context.Context, cmd *urfavecli.Command, version, buildTime string, writer, errWriter io.Writer) error {
	args := cmd.Args().Slice()
	if len(args) == 0 {
		return errors.New("logs requires a process name")
	}
	if len(args) != 1 {
		return errors.New("logs accepts exactly one process name")
	}
	stream := cmd.String("stream")
	if stream != "stdout" && stream != "stderr" && stream != "both" {
		return fmt.Errorf("stream must be one of stdout, stderr, or both: %q", stream)
	}
	tail := cmd.Int("tail")
	if tail < 0 {
		return errors.New("tail must not be negative")
	}
	limitBytes := cmd.Int("limit-bytes")
	if limitBytes < 0 {
		return errors.New("limit-bytes must not be negative")
	}
	name := args[0]
	ctx = nonNilContext(ctx)
	cfg, err := cliConfig(cmd, version, buildTime)
	if err != nil {
		return err
	}
	client, err := daemonClient(ctx, cfg)
	if err != nil {
		if daemonUnavailable(err) {
			return newUserFacingError(logsUnavailableMessage)
		}
		return err
	}
	defer client.Close()
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("current directory: %w", err)
	}
	maxBytes := int(cfg.ReadBytes)
	if limitBytes != 0 {
		maxBytes = limitBytes
	}
	var after *protocol.Cursor
	if cmd.IsSet("after-cursor") {
		cursor := protocol.Cursor(cmd.Uint64("after-cursor"))
		after = &cursor
	}
	request := daemon.OutputRequest{
		Name: name, Cwd: cwd, After: after, Tail: tail, Stream: protocol.Stream(stream), Match: cmd.String("match"),
		MaxEntries: cfg.ReadEntries, MaxBytes: maxBytes,
	}
	if cmd.Bool("follow") {
		signals := notifyFollowSignals()
		defer signal.Stop(signals)
		follower, err := client.Follow(context.Background(), daemon.FollowRequest{
			Name: request.Name, Cwd: request.Cwd, After: request.After, Tail: request.Tail, Stream: request.Stream,
			Match: request.Match, MaxEntries: request.MaxEntries, MaxBytes: request.MaxBytes,
		})
		if err != nil {
			return err
		}
		defer follower.Close()
		_, _, err = followLoop(ctx, follower, signals, func(event output.Event) error {
			if cmd.Bool("json") {
				return encodeJSON(writer, eventJSON(name, event))
			}
			if event.Read == nil {
				return nil
			}
			if err := writeLogEntries(writer, event.Read.Entries); err != nil {
				return err
			}
			return writeCursorTrailer(errWriter, *event.Read)
		}, func(sig os.Signal) (bool, error) {
			if sig == os.Interrupt || sig == syscall.SIGTERM || sig == syscall.SIGHUP {
				return true, nil
			}
			return false, nil
		})
		return err
	}
	result, err := client.Output(ctx, request)
	if err != nil {
		return err
	}
	if cmd.Bool("json") {
		return encodeJSON(writer, protocol.NewOutputResponse(outputJSON(result)))
	}
	if err := writeLogEntries(writer, result.Entries); err != nil {
		return err
	}
	return writeCursorTrailer(errWriter, result)
}

const defaultWaitTimeout = 30 * time.Second

func waitCommand(ctx context.Context, cmd *urfavecli.Command, version, buildTime string, writer io.Writer) error {
	args := cmd.Args().Slice()
	if len(args) == 0 {
		return errors.New("wait requires a process name")
	}
	if len(args) != 1 {
		return errors.New("wait accepts exactly one process name")
	}
	name := args[0]

	var after *protocol.Cursor
	if cmd.IsSet("after-cursor") {
		cursor := protocol.Cursor(cmd.Uint64("after-cursor"))
		after = &cursor
	}

	match := cmd.String("match")
	if cmd.IsSet("match") {
		if _, err := regexp.Compile(match); err != nil {
			return fmt.Errorf("match must be a valid regular expression: %w", err)
		}
	}

	timeout := defaultWaitTimeout
	if cmd.IsSet("timeout") {
		parsed, err := time.ParseDuration(cmd.String("timeout"))
		if err != nil {
			return fmt.Errorf("timeout must be a valid duration: %w", err)
		}
		if parsed <= 0 {
			return errors.New("timeout must be positive")
		}
		timeout = parsed
	}
	timeoutMS := int64(timeout / time.Millisecond)
	if timeoutMS <= 0 {
		return errors.New("timeout must be at least 1ms")
	}

	ctx = nonNilContext(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	cfg, err := cliConfig(cmd, version, buildTime)
	if err != nil {
		return err
	}
	client, err := daemonClient(ctx, cfg)
	if err != nil {
		if client != nil {
			_ = client.Close()
		}
		if daemonUnavailable(err) {
			return newUserFacingError(logsUnavailableMessage)
		}
		return err
	}
	defer client.Close()
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("current directory: %w", err)
	}
	result, err := client.Wait(ctx, daemon.WaitRequest{
		Name:      name,
		Cwd:       cwd,
		After:     after,
		Match:     match,
		TimeoutMS: timeoutMS,
	})
	if err != nil {
		return err
	}
	if cmd.Bool("json") {
		if err := encodeJSON(writer, waitJSONFor(result)); err != nil {
			return err
		}
	} else if err := renderWaitHuman(writer, result); err != nil {
		return err
	}

	switch result.Outcome {
	case app.WaitMatched:
		return nil
	case app.WaitExited:
		if cmd.IsSet("match") {
			return urfavecli.Exit("", 3)
		}
		return nil
	case app.WaitTimedOut:
		return urfavecli.Exit("", 2)
	default:
		return fmt.Errorf("wait returned unknown outcome %q", result.Outcome)
	}
}

func stopCommand(ctx context.Context, cmd *urfavecli.Command, version, buildTime string, writer io.Writer) error {
	names := cmd.Args().Slice()
	if len(names) == 0 {
		return errors.New("stop requires at least one process name")
	}
	ctx = nonNilContext(ctx)
	cfg, err := cliConfig(cmd, version, buildTime)
	if err != nil {
		return err
	}
	client, err := daemonClient(ctx, cfg)
	if err != nil {
		if daemonUnavailable(err) {
			if cmd.Bool("json") {
				for _, name := range names {
					if err := encodeJSON(writer, stopResult{Name: name, Status: "not_running"}); err != nil {
						return err
					}
				}
				return nil
			}
			_, writeErr := fmt.Fprintln(writer, stopUnavailableMessage)
			return writeErr
		}
		return err
	}
	defer client.Close()
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("current directory: %w", err)
	}
	processes, err := client.List(ctx, daemon.ListRequest{Cwd: cwd})
	if err != nil {
		if daemonUnavailable(err) {
			if cmd.Bool("json") {
				for _, name := range names {
					if encodeErr := encodeJSON(writer, stopResult{Name: name, Status: "not_running"}); encodeErr != nil {
						return encodeErr
					}
				}
				return nil
			}
			_, writeErr := fmt.Fprintln(writer, stopUnavailableMessage)
			return writeErr
		}
		return err
	}
	running := make(map[string]bool, len(processes))
	for _, process := range processes {
		if process.State == app.StateRunning {
			running[process.Name] = true
		}
	}
	var firstErr error
	for _, name := range names {
		result := stopResult{Name: name}
		if !running[name] {
			result.Status = "not_running"
		} else {
			stopErr := client.Stop(context.Background(), daemon.StopRequest{Name: name, Cwd: cwd})
			if stopErr == nil {
				result.Status = "stopped"
				running[name] = false
			} else if isNotFound(stopErr) || daemonUnavailable(stopErr) {
				result.Status = "not_running"
				running[name] = false
			} else {
				result.Status = "error"
				result.Message = stopErr.Error()
				if firstErr == nil {
					firstErr = stopErr
				}
			}
		}
		if cmd.Bool("json") {
			if err := encodeJSON(writer, result); err != nil {
				return err
			}
		} else if err := renderStopHuman(writer, result); err != nil {
			return err
		}
	}
	return firstErr
}

func shutdownCommand(ctx context.Context, cmd *urfavecli.Command, version, buildTime string, writer io.Writer) error {
	if err := requireNoArgs(cmd, "shutdown"); err != nil {
		return err
	}
	ctx = nonNilContext(ctx)
	cfg, err := cliConfig(cmd, version, buildTime)
	if err != nil {
		return err
	}
	client, err := daemonClient(ctx, cfg)
	if err != nil {
		var versionMismatch *daemon.VersionMismatchError
		if client == nil || !errors.As(err, &versionMismatch) {
			if daemonUnavailable(err) {
				if cmd.Bool("json") {
					return encodeJSON(writer, shutdownResult{Status: "not_running"})
				}
				_, writeErr := fmt.Fprintln(writer, shutdownUnavailableMessage)
				return writeErr
			}
			return err
		}
	}
	defer client.Close()
	force := cmd.Bool("stop-processes")
	shutdownErr := client.Shutdown(context.Background(), daemon.ShutdownRequest{Force: force})
	if shutdownErr != nil {
		if daemonUnavailable(shutdownErr) {
			if cmd.Bool("json") {
				return encodeJSON(writer, shutdownResult{Status: "not_running"})
			}
			_, writeErr := fmt.Fprintln(writer, shutdownUnavailableMessage)
			return writeErr
		}
		if cmd.Bool("json") && isActiveProcesses(shutdownErr) {
			encodeErr := encodeJSON(writer, struct {
				Status    string   `json:"status"`
				Message   string   `json:"message"`
				Processes []string `json:"processes,omitempty"`
			}{Status: "error", Message: shutdownErr.Error(), Processes: activeProcessNames(shutdownErr)})
			if encodeErr != nil {
				return encodeErr
			}
		}
		return shutdownErr
	}
	if cmd.Bool("json") {
		return encodeJSON(writer, shutdownResult{Status: "stopped"})
	}
	_, err = fmt.Fprintln(writer, "hum daemon shut down")
	return err
}

func notifyFollowSignals() chan os.Signal {
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	return signals
}

type followerResult struct {
	event output.Event
	err   error
}

func followLoop(parent context.Context, follower *daemon.Follower, signals <-chan os.Signal, onEvent func(output.Event) error, onSignal func(os.Signal) (bool, error)) (exitCode int, exited bool, err error) {
	parent = nonNilContext(parent)
	nextCtx, cancelNext := context.WithCancel(context.Background())
	defer cancelNext()

	next := make(chan followerResult, 1)
	requestNext := func() {
		go func() {
			event, nextErr := follower.Next(nextCtx)
			result := followerResult{event: event, err: nextErr}
			select {
			case next <- result:
			case <-nextCtx.Done():
			}
		}()
	}
	requestNext()

	closeFollower := func() {
		cancelNext()
		_ = follower.Close()
	}
	for {
		select {
		case result := <-next:
			if parent.Err() != nil {
				closeFollower()
				return 0, false, nil
			}
			if result.err != nil {
				closeFollower()
				if errors.Is(result.err, context.Canceled) || errors.Is(result.err, context.DeadlineExceeded) {
					return 0, false, nil
				}
				return 0, false, result.err
			}
			if result.event.Exit != nil {
				if err := onEvent(result.event); err != nil {
					closeFollower()
					return 0, false, err
				}
				closeFollower()
				return result.event.Exit.Code, true, nil
			}
			if err := onEvent(result.event); err != nil {
				closeFollower()
				return 0, false, err
			}
			requestNext()
		case sig, ok := <-signals:
			if !ok {
				signals = nil
				continue
			}
			if sig == nil {
				continue
			}
			if parent.Err() != nil {
				closeFollower()
				return 0, false, nil
			}
			done, signalErr := onSignal(sig)
			if signalErr != nil {
				closeFollower()
				return 0, false, signalErr
			}
			if done {
				closeFollower()
				return 0, false, nil
			}
		case <-parent.Done():
			closeFollower()
			return 0, false, nil
		}
	}
}

func requireNoArgs(cmd *urfavecli.Command, name string) error {
	if cmd.NArg() != 0 {
		return fmt.Errorf("%s accepts no positional arguments", name)
	}
	return nil
}

func nonNilContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
