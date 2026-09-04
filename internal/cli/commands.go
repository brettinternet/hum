package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"hum/internal/app"
	"hum/internal/daemon"
	"hum/internal/output"
	"hum/internal/protocol"
	"hum/internal/skill"

	urfavecli "github.com/urfave/cli/v3"
)

func newCLICommands(version, buildTime string, writer, errWriter io.Writer) []*urfavecli.Command {
	runStopOnNthArg := 1
	return []*urfavecli.Command{
		{
			Name:      "serve",
			Usage:     "run the hum daemon in the foreground or detached",
			ArgsUsage: "",
			Description: "By default, hum serve stays attached in the foreground and writes daemon diagnostics to stderr. " +
				"hum serve --daemon starts a detached daemon, waits for readiness, and reports its PID and socket before returning.",
			Flags: []urfavecli.Flag{
				&urfavecli.BoolFlag{Name: "daemon", Aliases: []string{"d"}, Usage: "start the daemon detached, wait for readiness, and print its PID and socket"},
			},
			Action: func(ctx context.Context, cmd *urfavecli.Command) error {
				return serveCommand(ctx, cmd, version, buildTime, errWriter)
			},
		},
		{
			Name:      "init",
			Usage:     "create a hum.yaml manifest from project discovery",
			ArgsUsage: "",
			Description: "Create hum.yaml from strict project discovery without starting a daemon. " +
				"A single candidate is written as a generated manifest; no candidate or ambiguous candidates produce a commented template. " +
				"Reports the absolute path, outcome, and next command hum up; use --json for stable JSON.",
			Flags: []urfavecli.Flag{
				&urfavecli.BoolFlag{Name: "json", Aliases: []string{"j"}, Usage: "write stable JSON"},
			},
			Action: func(ctx context.Context, cmd *urfavecli.Command) error {
				return initCommand(ctx, cmd, writer)
			},
		},
		mcpCLICommand(version, buildTime, writer),
		{
			Name:      "skill",
			Usage:     "print the shell-only fallback skill",
			ArgsUsage: "",
			Description: "Print the embedded Agent Skills file for shell-only fallback use. " +
				"MCP-capable agents should use hum mcp instead.",
			Action: func(ctx context.Context, cmd *urfavecli.Command) error {
				return skillCommand(ctx, cmd, writer)
			},
		},
		{
			Name:         "run",
			Usage:        "start a named process (attached by default)",
			ArgsUsage:    "NAME -- COMMAND [ARGS...]",
			StopOnNthArg: &runStopOnNthArg,
			Description: "hum run automatically starts a detached daemon when none is available. " +
				"Without --detach, it stays attached and streams the managed process stdout and stderr until exit. " +
				"With --detach, it starts the process, prints its name and PID (or JSON), and returns immediately; the daemon keeps owning it.",
			Flags: []urfavecli.Flag{
				&urfavecli.BoolFlag{Name: "detach", Aliases: []string{"d"}, Usage: "start the process detached and return without attaching"},
				&urfavecli.BoolFlag{Name: "json", Aliases: []string{"j"}, Usage: "write stable JSON for detached runs; attached runs stream raw child output"},
			},
			Action: func(ctx context.Context, cmd *urfavecli.Command) error {
				return runCommand(ctx, cmd, version, buildTime, writer, errWriter)
			},
		},
		{
			Name:        "start",
			Usage:       "ensure one or more manifest processes are running",
			ArgsUsage:   "NAME...",
			Description: "Start resolves names from hum.yaml, launches stopped declarations with the full client environment, and waits for readiness unless --no-wait is set.",
			Flags: []urfavecli.Flag{
				&urfavecli.BoolFlag{Name: "no-wait", Usage: "return after the process is spawned"},
				&urfavecli.StringFlag{Name: "timeout", Aliases: []string{"t"}, Usage: "maximum readiness wait duration"},
				&urfavecli.BoolFlag{Name: "json", Aliases: []string{"j"}, Usage: "write one stable JSON object per name"},
			},
			Action: func(ctx context.Context, cmd *urfavecli.Command) error {
				return startCommand(ctx, cmd, version, buildTime, writer)
			},
		},
		{
			Name:        "up",
			Usage:       "ensure every manifest process is running",
			ArgsUsage:   "",
			Description: "Up resolves every hum.yaml declaration in lexical order, continues after launch failures, and waits for readiness concurrently unless --no-wait is set.",
			Flags: []urfavecli.Flag{
				&urfavecli.BoolFlag{Name: "no-wait", Usage: "return after processes are spawned"},
				&urfavecli.StringFlag{Name: "timeout", Aliases: []string{"t"}, Usage: "maximum readiness wait duration"},
				&urfavecli.BoolFlag{Name: "json", Aliases: []string{"j"}, Usage: "write one stable JSON object per declaration"},
			},
			Action: func(ctx context.Context, cmd *urfavecli.Command) error {
				return upCommand(ctx, cmd, version, buildTime, writer)
			},
		},
		{
			Name:      "down",
			Usage:     "stop every process in the current project",
			ArgsUsage: "",
			Description: "Down lists every active process in the current project, including resolved manifest and ad-hoc processes, and applies the graceful process-group stop sequence to each concurrently. " +
				"Declared manifest names with no running record are reported as not running; down emits one stable result per name and is idempotent. " +
				"Down never starts or shuts down the daemon, and when no work exists it reports Nothing is running in this project.",
			Flags: []urfavecli.Flag{
				&urfavecli.BoolFlag{Name: "json", Aliases: []string{"j"}, Usage: "write one stable JSON object per name"},
			},
			Action: func(ctx context.Context, cmd *urfavecli.Command) error {
				return downCommand(ctx, cmd, version, buildTime, writer)
			},
		},
		{
			Name:      "list",
			Usage:     "list supervised processes (current project by default)",
			ArgsUsage: "",
			Description: "List is read-only and does not start an empty daemon. " +
				"Use --all to include processes from every project; when nothing is running, it reports that state.",
			Flags: []urfavecli.Flag{
				&urfavecli.BoolFlag{Name: "all", Aliases: []string{"a"}, Usage: "list processes from every project"},
				&urfavecli.BoolFlag{Name: "json", Aliases: []string{"j"}, Usage: "write stable JSON"},
			},
			Action: func(ctx context.Context, cmd *urfavecli.Command) error {
				return listCommand(ctx, cmd, version, buildTime, writer)
			},
		},
		{
			Name:      "status",
			Usage:     "show one supervised process (read-only)",
			ArgsUsage: "NAME",
			Description: "Status only reads one named process and never starts a daemon. " +
				"If no daemon is available, resolved manifest names point to hum start <name>; undefined names keep the hum run <name> -- <command> guidance.",
			Flags: []urfavecli.Flag{
				&urfavecli.BoolFlag{Name: "json", Aliases: []string{"j"}, Usage: "write stable JSON"},
			},
			Action: func(ctx context.Context, cmd *urfavecli.Command) error {
				return statusCommand(ctx, cmd, version, buildTime, writer)
			},
		},
		{
			Name:      "logs",
			Usage:     "read retained process output (read-only)",
			ArgsUsage: "NAME",
			Description: "Logs reads bounded retained output without changing process state. " +
				"--follow first reads retained entries and then streams new entries; following is read-only, so Ctrl+C cancels only the follower and never signals the managed process. " +
				"If no daemon is available, it reports Nothing is running and points to hum run <name> -- <command> as the launch command.",
			Flags: []urfavecli.Flag{
				&urfavecli.StringFlag{Name: "stream", Aliases: []string{"s"}, Value: "both", Usage: "select stdout, stderr, or both"},
				&urfavecli.IntFlag{Name: "tail", Aliases: []string{"n"}, Usage: "select the final N entries"},
				&urfavecli.Uint64Flag{Name: "after-cursor", Aliases: []string{"c"}, Usage: "read entries after this cursor"},
				&urfavecli.IntFlag{Name: "limit-bytes", Aliases: []string{"b"}, Usage: "limit returned output bytes"},
				&urfavecli.StringFlag{Name: "match", Aliases: []string{"m"}, Usage: "filter entries by regular expression"},
				&urfavecli.BoolFlag{Name: "follow", Aliases: []string{"f"}, Usage: "follow output until process exit"},
				&urfavecli.BoolFlag{Name: "json", Aliases: []string{"j"}, Usage: "write stable JSON"},
			},
			Action: func(ctx context.Context, cmd *urfavecli.Command) error {
				return logsCommand(ctx, cmd, version, buildTime, writer, errWriter)
			},
		},
		{
			Name:      "wait",
			Usage:     "wait for matching output or process exit (30s default)",
			ArgsUsage: "NAME",
			Description: "Without --match, wait returns when the process exits; with --match, it returns when output matches or the process exits. " +
				"It searches from the current incarnation's launch cursor by default and waits at most 30s unless --timeout is set. " +
				"Exit code is 0 for a match or an exit without --match, 3 when --match sees process exit first, and 2 on timeout. " +
				"Current processes can use wait --match for output matching; future resolved-process commands use readiness from hum start and hum up. " +
				"Without a daemon, resolved manifest names point to hum start <name>; undefined names keep the hum run <name> -- <command> guidance.",
			Flags: []urfavecli.Flag{
				&urfavecli.Uint64Flag{Name: "after-cursor", Aliases: []string{"c"}, DefaultText: "current launch cursor", Usage: "search entries after this cursor"},
				&urfavecli.StringFlag{Name: "match", Aliases: []string{"m"}, Usage: "wait for output matching this non-empty regular expression"},
				&urfavecli.StringFlag{Name: "timeout", Aliases: []string{"t"}, Usage: "maximum wait duration (default 30s)"},
				&urfavecli.BoolFlag{Name: "json", Aliases: []string{"j"}, Usage: "write stable JSON"},
			},
			Action: func(ctx context.Context, cmd *urfavecli.Command) error {
				return waitCommand(ctx, cmd, version, buildTime, writer)
			},
		},
		{
			Name:      "restart",
			Usage:     "restart one or more processes by name",
			ArgsUsage: "NAME...",
			Description: "Restart applies the graceful stop sequence and attempts to relaunch requested names with each one's recorded command, working directory, and environment. " +
				"Names are attempted in order; the first error stops the remaining restarts, so it reports only successful attempts; each result includes the new PID and launch cursor. " +
				"It restarts processes, not the daemon. " +
				"If no daemon is available, it reports Nothing is running and points to hum run <name> -- <command> as the launch command.",
			Flags: []urfavecli.Flag{
				&urfavecli.BoolFlag{Name: "json", Aliases: []string{"j"}, Usage: "write one stable JSON object per name"},
			},
			Action: func(ctx context.Context, cmd *urfavecli.Command) error {
				return restartCommand(ctx, cmd, version, buildTime, writer)
			},
		},
		{
			Name:      "stop",
			Usage:     "stop one or more named processes (idempotent)",
			ArgsUsage: "NAME...",
			Description: "Stop accepts multiple names and applies the graceful process-group stop sequence to each one, returning one result per name. " +
				"Stopping an already-stopped or unknown name succeeds as not running, so stop is idempotent. " +
				"Stop affects managed processes only and does not shut down the daemon; with no daemon, it reports Nothing is running.",
			Flags: []urfavecli.Flag{
				&urfavecli.BoolFlag{Name: "json", Aliases: []string{"j"}, Usage: "write one stable JSON object per name"},
			},
			Action: func(ctx context.Context, cmd *urfavecli.Command) error {
				return stopCommand(ctx, cmd, version, buildTime, writer)
			},
		},
		{
			Name:      "shutdown",
			Usage:     "shut down the hum daemon",
			ArgsUsage: "",
			Description: "Shutdown controls daemon lifetime rather than one named process. " +
				"By default it refuses while managed processes are active and lists their names, leaving them running. " +
				"Use --stop-processes to gracefully stop every managed process before the daemon exits; if no daemon is running, shutdown succeeds with No hum daemon is running.",
			Flags: []urfavecli.Flag{
				&urfavecli.BoolFlag{Name: "stop-processes", Usage: "stop all managed processes before shutting down; default refuses when any are active"},
				&urfavecli.BoolFlag{Name: "json", Aliases: []string{"j"}, Usage: "write stable JSON"},
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
		if len(args) == 1 {
			return args[0], nil, nil
		}
		return "", nil, errors.New("run requires a command after --")
	}
	if separator < 2 {
		return "", nil, errors.New("run accepts exactly one process name before --")
	}
	for i := 1; i < separator; i++ {
		flag := args[i]
		flagName, value, hasValue := strings.Cut(flag, "=")
		name := ""
		switch flagName {
		case "-d":
			name = "detach"
		case "-j":
			name = "json"
		default:
			if strings.HasPrefix(flagName, "--") {
				name = strings.TrimPrefix(flagName, "--")
			}
		}
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
	var manifest manifestState
	if len(argv) == 0 {
		manifest, err = loadManifest(cwd)
	} else {
		manifest, err = loadManifestOrEmpty(cwd)
	}
	if err != nil {
		return err
	}
	definition, declared := manifest.byName[name]
	launchCwd := cwd
	lookupCwd := cwd
	source := "ad_hoc"
	var ready *protocol.ReadinessConfig
	if len(argv) == 0 {
		if !declared {
			return errors.New("run requires a command after --")
		}
		argv = append([]string(nil), definition.Argv...)
		launchCwd = definition.Cwd
		lookupCwd = manifest.root
		source = definition.Source
		ready = readinessConfig(definition)
	} else if declared {
		return fmt.Errorf("process %q is declared in hum.yaml; use hum start %s", name, name)
	}
	client, err := runDaemonClient(ctx, cfg)
	if err != nil {
		return err
	}
	defer client.Close()
	signals := notifyFollowSignals()
	defer signal.Stop(signals)
	process, err := client.Start(ctx, daemon.StartRequest{Name: name, Source: source, Root: manifestRootForLaunch(source, manifest.root), Argv: argv, Cwd: launchCwd, Env: os.Environ(), Ready: ready})
	if err != nil {
		if isNameInUse(err) || errors.Is(err, app.ErrNameInUse) {
			return fmt.Errorf("%w; watch it with hum logs %s --follow", err, name)
		}
		return err
	}
	if cmd.Bool("detach") {
		result := runResult{
			Name:   process.Name,
			PID:    process.PID,
			Cursor: protocol.Cursor(process.LaunchCursor),
		}
		if process.Source != "" && process.Source != "ad_hoc" {
			result.Source = process.Source
			result.Argv = append([]string(nil), process.Argv...)
			result.Outcome = "started"
			if ready == nil {
				result.Outcome = "running_unverified"
				result.Readiness = "running_unverified"
			} else if process.Readiness != nil {
				result.Readiness = process.Readiness.State
				if process.Readiness.Cursor != nil {
					cursor := protocol.Cursor(*process.Readiness.Cursor)
					result.ReadyCursor = &cursor
				}
			}
		}
		if cmd.Bool("json") {
			return encodeJSON(writer, result)
		}
		if result.Source == "" {
			_, err := fmt.Fprintf(writer, "started %s (PID %d, cursor %d)\n", process.Name, process.PID, process.LaunchCursor)
			return err
		}
		_, err := fmt.Fprintf(writer,
			"started %s (PID %d, cursor %d, source=%s, argv=%s, outcome=%s, readiness=%s)\n",
			process.Name, process.PID, process.LaunchCursor, result.Source, shellJoin(result.Argv),
			result.Outcome, result.Readiness)
		return err
	}
	followRequest := daemon.FollowRequest{
		Name:       name,
		Cwd:        lookupCwd,
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
				err := client.Signal(context.Background(), daemon.SignalRequest{Name: name, Cwd: lookupCwd, Signal: "SIGINT"})
				if isNotFound(err) {
					return false, nil
				}
				return false, err
			}
			err := client.Stop(context.Background(), daemon.StopRequest{Name: name, Cwd: lookupCwd})
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
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("current directory: %w", err)
	}
	manifest, err := loadManifestOrEmpty(cwd)
	if err != nil {
		return err
	}
	cfg, err := cliConfig(cmd, version, buildTime)
	if err != nil {
		return err
	}
	client, err := daemonClient(ctx, cfg)
	if err != nil {
		if daemonUnavailable(err) {
			processes := make([]app.Process, 0, len(manifest.defs))
			for _, definition := range manifest.defs {
				processes = append(processes, manifestProcess(definition, manifest.root))
			}
			if cmd.Bool("json") {
				items := make([]listProcessJSON, 0, len(processes))
				for _, process := range processes {
					items = append(items, processJSON(process))
				}
				return encodeJSON(writer, listJSON{Processes: items})
			}
			return renderListHuman(writer, processes, cmd.Bool("all"))
		}
		return err
	}
	defer client.Close()
	processes, err := client.List(ctx, daemon.ListRequest{Cwd: cwd, All: cmd.Bool("all")})
	if err != nil {
		return err
	}
	processes = mergeManifestProcesses(manifest, processes)
	if cmd.Bool("json") {
		items := make([]listProcessJSON, 0, len(processes))
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
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("current directory: %w", err)
	}
	manifest, err := loadManifestOrEmpty(cwd)
	if err != nil {
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
			if definition, ok := manifest.byName[name]; ok {
				return manifestUnavailableMessage(definition)
			}
			return newUserFacingError(logsUnavailableMessage)
		}
		return err
	}
	defer client.Close()
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
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("current directory: %w", err)
	}
	manifest, err := loadManifestOrEmpty(cwd)
	if err != nil {
		return err
	}
	cfg, err := cliConfig(cmd, version, buildTime)
	if err != nil {
		return err
	}
	client, err := daemonClient(ctx, cfg)
	if err != nil {
		if daemonUnavailable(err) {
			if definition, ok := manifest.byName[name]; ok {
				return manifestUnavailableMessage(definition)
			}
			return newUserFacingError(logsUnavailableMessage)
		}
		return err
	}
	defer client.Close()
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
			return writeLogEntries(writer, event.Read.Entries)
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
		if match == "" {
			return errors.New("match must not be empty")
		}
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
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("current directory: %w", err)
	}
	manifest, err := loadManifestOrEmpty(cwd)
	if err != nil {
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
			if definition, ok := manifest.byName[name]; ok {
				return manifestUnavailableMessage(definition)
			}
			return newUserFacingError(logsUnavailableMessage)
		}
		return err
	}
	defer client.Close()
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

func downCommand(ctx context.Context, cmd *urfavecli.Command, version, buildTime string, writer io.Writer) error {
	if err := requireNoArgs(cmd, "down"); err != nil {
		return err
	}
	ctx = nonNilContext(ctx)
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("current directory: %w", err)
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
			return renderDownResults(writer, nil, cmd.Bool("json"))
		}
		return err
	}
	defer client.Close()
	processes, err := client.List(ctx, daemon.ListRequest{Cwd: cwd})
	if err != nil {
		if daemonUnavailable(err) {
			return renderDownResults(writer, nil, cmd.Bool("json"))
		}
		return err
	}
	manifest, err := loadManifestOrEmpty(cwd)
	if err != nil {
		return err
	}
	processes = mergeManifestProcesses(manifest, processes)
	byName := make(map[string]app.Process, len(processes))
	for _, process := range processes {
		existing, ok := byName[process.Name]
		if !ok || (existing.State != app.StateRunning && process.State == app.StateRunning) {
			byName[process.Name] = process
		}
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return renderDownResults(writer, nil, cmd.Bool("json"))
	}

	type workerResult struct {
		index  int
		result stopResult
		err    error
	}
	workers := make(chan workerResult, len(names))
	var waitGroup sync.WaitGroup
	for index, name := range names {
		if byName[name].State != app.StateRunning {
			continue
		}
		waitGroup.Add(1)
		go func(index int, name string) {
			defer waitGroup.Done()
			result := stopResult{Name: name}
			worker, stopErr := daemonClient(context.Background(), cfg)
			if worker != nil {
				defer worker.Close()
			}
			if stopErr == nil {
				if worker == nil {
					stopErr = errors.New("daemon connection returned nil client")
				} else {
					stopErr = worker.Stop(context.Background(), daemon.StopRequest{Name: name, Cwd: cwd})
				}
			}
			switch {
			case stopErr == nil:
				result.Status = "stopped"
			case isNotFound(stopErr):
				result.Status = "not_running"
			default:
				result.Status = "error"
				result.Message = stopErr.Error()
			}
			workers <- workerResult{index: index, result: result, err: stopErr}
		}(index, name)
	}
	waitGroup.Wait()
	close(workers)
	stopErrors := make([]error, len(names))
	results := make([]stopResult, len(names))
	for index, name := range names {
		results[index] = stopResult{Name: name, Status: "not_running"}
	}
	for worker := range workers {
		results[worker.index] = worker.result
		if worker.result.Status == "error" {
			stopErrors[worker.index] = worker.err
		}
	}
	if err := renderDownResults(writer, results, cmd.Bool("json")); err != nil {
		return err
	}
	for _, stopErr := range stopErrors {
		if stopErr != nil {
			return stopErr
		}
	}
	return nil
}

func restartCommand(ctx context.Context, cmd *urfavecli.Command, version, buildTime string, writer io.Writer) error {
	names := cmd.Args().Slice()
	if len(names) == 0 {
		return errors.New("restart requires at least one process name")
	}
	ctx = nonNilContext(ctx)
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("current directory: %w", err)
	}
	manifest, err := loadManifestOrEmpty(cwd)
	if err != nil {
		return err
	}
	cfg, err := cliConfig(cmd, version, buildTime)
	if err != nil {
		return err
	}
	client, err := daemonClient(ctx, cfg)
	if err != nil {
		if daemonUnavailable(err) {
			for _, name := range names {
				if definition, ok := manifest.byName[name]; ok {
					return manifestUnavailableMessage(definition)
				}
			}
			return newUserFacingError(logsUnavailableMessage)
		}
		return err
	}
	defer client.Close()
	for _, name := range names {
		request := daemon.RestartRequest{Name: name, Cwd: cwd}
		definition, manifestLaunch := manifest.byName[name]
		if manifestLaunch {
			request.Update = true
			request.Root = manifest.root
			request.Cwd = definition.Cwd
			request.Argv = append([]string(nil), definition.Argv...)
			request.Env = manifestProcessEnv()
			request.Source = definition.Source
			request.Ready = readinessConfig(definition)
		}
		process, err := client.Restart(ctx, request)
		if err != nil {
			return err
		}
		readiness, readyCursor := processReadinessFields(process)
		result := restartResult{
			Name:         process.Name,
			Source:       process.Source,
			Argv:         append([]string(nil), process.Argv...),
			PID:          process.PID,
			Restarts:     process.RestartCount,
			LaunchCursor: protocol.Cursor(process.LaunchCursor),
			Readiness:    readiness,
			ReadyCursor:  readyCursor,
		}
		if cmd.Bool("json") {
			if manifestLaunch {
				if err := encodeJSON(writer, result); err != nil {
					return err
				}
			} else if err := encodeJSON(writer, legacyRestartResult{
				Name:         result.Name,
				PID:          result.PID,
				Restarts:     result.Restarts,
				LaunchCursor: result.LaunchCursor,
			}); err != nil {
				return err
			}
		} else if err := renderRestartHuman(writer, result); err != nil {
			return err
		}
	}
	return nil
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

func startCommand(ctx context.Context, cmd *urfavecli.Command, version, buildTime string, writer io.Writer) error {
	names := cmd.Args().Slice()
	if len(names) == 0 {
		return errors.New("start requires at least one process name")
	}
	return manifestLaunchCommand(ctx, cmd, version, buildTime, writer, names)
}

func upCommand(ctx context.Context, cmd *urfavecli.Command, version, buildTime string, writer io.Writer) error {
	if err := requireNoArgs(cmd, "up"); err != nil {
		return err
	}
	manifest, err := loadManifestForCommand()
	if err != nil {
		return err
	}
	names := make([]string, 0, len(manifest.defs))
	for _, definition := range manifest.defs {
		names = append(names, definition.Name)
	}
	return manifestLaunchCommandWithState(ctx, cmd, version, buildTime, writer, manifest, names)
}

func manifestLaunchCommand(ctx context.Context, cmd *urfavecli.Command, version, buildTime string, writer io.Writer, names []string) error {
	manifest, err := loadManifestForCommand()
	if err != nil {
		return err
	}
	return manifestLaunchCommandWithState(ctx, cmd, version, buildTime, writer, manifest, names)
}

func loadManifestForCommand() (manifestState, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return manifestState{}, fmt.Errorf("current directory: %w", err)
	}
	return loadManifest(cwd)
}

func manifestLaunchCommandWithState(ctx context.Context, cmd *urfavecli.Command, version, buildTime string, writer io.Writer, manifest manifestState, names []string) error {
	ctx = nonNilContext(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	timeoutOverride, err := manifestTimeoutOverride(cmd)
	if err != nil {
		return err
	}
	cfg, err := cliConfig(cmd, version, buildTime)
	if err != nil {
		return err
	}
	client, err := runDaemonClient(ctx, cfg)
	if err != nil {
		return err
	}
	defer client.Close()
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("current directory: %w", err)
	}
	results := make([]manifestLaunchResult, len(names))
	waitItems := make([]manifestWaitItem, 0, len(names))
	env := manifestProcessEnv()
	for index, name := range names {
		definition, ok := manifest.byName[name]
		if !ok {
			results[index] = manifestLaunchError(undefinedManifestDefinition(name), fmt.Errorf("manifest does not declare process %q", name))
			continue
		}
		result, process, _, ensureErr := ensureManifestStart(ctx, client, cwd, manifest.root, definition, env)
		if ensureErr != nil {
			results[index] = manifestLaunchError(definition, ensureErr)
			continue
		}
		results[index] = result
		if cmd.Bool("no-wait") || result.Outcome == "error" ||
			process.State != app.StateRunning || process.Readiness == nil ||
			process.Readiness.State != app.ReadinessStarting {
			continue
		}
		timeout := timeoutOverride
		if timeout == 0 {
			timeout, err = parseManifestTimeout(cmd, definition)
			if err != nil {
				results[index] = manifestLaunchError(definition, err)
				continue
			}
		}
		waitItems = append(waitItems, manifestWaitItem{index: index, definition: definition, process: process, initial: result.Outcome, timeout: timeout})
	}
	var waits sync.WaitGroup
	for _, item := range waitItems {
		item := item
		waits.Add(1)
		go func() {
			defer waits.Done()
			waitResult, waitErr := manifestReadinessResult(client, ctx, cwd, item.definition, item.process, item.initial, item.timeout)
			if waitErr != nil {
				waitResult = manifestLaunchError(item.definition, waitErr)
			}
			results[item.index] = waitResult
		}()
	}
	waits.Wait()
	for _, result := range results {
		if cmd.Bool("json") {
			if err := encodeJSON(writer, manifestResultJSON(result)); err != nil {
				return err
			}
		} else if err := renderManifestLaunchHuman(writer, result); err != nil {
			return err
		}
	}
	return aggregateManifestExit(results)

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

func skillCommand(_ context.Context, cmd *urfavecli.Command, writer io.Writer) error {
	if err := requireNoArgs(cmd, "skill"); err != nil {
		return err
	}
	_, err := io.WriteString(writer, skill.Content())
	return err
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
