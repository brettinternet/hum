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
	"hum/internal/project"
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
				"Without --detach, it follows the named session across process exits and launches until Ctrl+C detaches the observer. " +
				"With --detach, it starts the process, prints its name and PID (or JSON), and returns immediately; the daemon keeps owning it. " +
				"TTY mode uses raw mode, forwards terminal bytes including Ctrl+C, Ctrl+D, and Ctrl+Z, and consumes Ctrl-] to detach input; non-TTY Ctrl+C remains observer detach. " +
				"TTY output merges child streams as stdout, exactly one owner forwards input and SIGWINCH resizes, and logs --follow is output-only; EOF or shutdown releases the owner.",
			Flags: []urfavecli.Flag{
				&urfavecli.BoolFlag{Name: "detach", Aliases: []string{"d"}, Usage: "start the process detached and return without attaching"},
				&urfavecli.BoolFlag{Name: "json", Aliases: []string{"j"}, Usage: "write stable JSON for detached runs; attached runs stream raw child output"},
				&urfavecli.BoolFlag{Name: "tty", Usage: "launch an ad-hoc command in a pseudo-terminal and forward attached input"},
			},
			Action: func(ctx context.Context, cmd *urfavecli.Command) error {
				return runCommand(ctx, cmd, version, buildTime, writer, errWriter)
			},
		},
		{
			Name:        "start",
			Usage:       "ensure one or more named sessions are running",
			ArgsUsage:   "NAME...",
			Description: "Start is idempotent for running sessions and relaunches retained stopped sessions. Absent names resolve from hum.yaml or conventional discovery. It launches only the explicitly named processes, never pulls in transitive after prerequisites, and waits for each resolved readiness unless --no-wait is set. Declared restart: on-failure sessions recover unexpected crashes with five bounded attempts; explicit start cancels pending backoff and uses the current definition.",
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
			Description: "Up resolves every hum.yaml declaration in lexical order, launches independent roots concurrently, and gates each after dependency on all direct prerequisites observed as ready. It waits per process from that process's launch or running observation, continues after launch failures, continues after failures, and reports skipped direct blockers plus any retained existing process state without mutating it; --no-wait is rejected before daemon contact when after is declared. During bounded recovery, up observes an exited declaration as recovery_pending or recovery_exhausted without sending a start request or waiting for an automatic successor; these outcomes make up exit 3 because the declaration is not running. Use targeted start NAME or restart NAME to cancel recovery and launch immediately. One invocation does not follow an automatic prerequisite successor; rerun hum up after recovery. A declared restart: on-failure session retries unexpected crashes with bounded 1s/2s/4s/8s/16s backoff; automatic attempts retain their effective launch spec. Human-only up without --json or --no-wait writes bounded startup progress to stderr in transition-completion order, with at most two lines per declaration; final summaries stay on stdout, and timeout or early-exit lines point to hum logs NAME without streaming child output.",
			Flags: []urfavecli.Flag{
				&urfavecli.BoolFlag{Name: "no-wait", Usage: "return after processes are spawned"},
				&urfavecli.StringFlag{Name: "timeout", Aliases: []string{"t"}, Usage: "maximum readiness wait duration"},
				&urfavecli.BoolFlag{Name: "json", Aliases: []string{"j"}, Usage: "write one stable JSON object per declaration"},
			},
			Action: func(ctx context.Context, cmd *urfavecli.Command) error {
				return upCommand(ctx, cmd, version, buildTime, writer, errWriter)
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
				"Use --all to include processes from every project; followed records show their live followers count, while unfollowed human output is unchanged. JSON includes restart, relaunches, and pending next_launch_at fields. When nothing is running, it reports that state.",
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
			Description: "Status only reads one named process and never starts a daemon. It reports followers, the live attached run and logs --follow count, as a read-only observation. JSON and human output expose restart policy, relaunches, and pending backoff. Read retained failing output before editing a recovering process. " +
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
			ArgsUsage: "[NAME...]",
			Description: "Logs reads bounded retained output without changing process state. One or more names may be supplied; names are selected in command-line order. " +
				"With no names, the current project's declarations are resolved once in lexical order, with no ad-hoc sessions included. Duplicate names are rejected, and --after-cursor is only supported for one explicit name. " +
				"Aggregate filters, tails, and byte or entry limits apply independently to each selected session; bounded JSON is named NDJSON and bounded human entries use an atomic [NAME] prefix. " +
				"Bounded child output and child-output matches use terminal-control-stripped text; system entries remain raw. " +
				"Patterns containing raw ESC bytes no longer match stripped child text; a ^ anchor now matches colourised output whose raw first byte is ESC. " +
				"Stored bytes, cursors, and limit accounting remain raw; control-only bounded child entries remain present with empty text. " +
				"--follow ensures a daemon exists, may attach before the first launch, and follows each selected named session with one follower across exit, wait, and launch boundaries. " +
				"Aggregate follow serializes writes, keeps per-session errors named and isolated, and cancels every follower on daemon loss or output failure. " +
				"With --follow --match, selection uses stripped child text but selected entries are emitted raw; attached run output is also raw. " +
				"Stripping is per entry, so split sequences and carriage-return redraw frames are not collapsed; there is no --raw flag or other raw opt-out. " +
				"Following is read-only, so Ctrl+C cancels only the follower for one name and closes all followers in an aggregate; it never signals the managed process or any other managed process. Without --follow, an unavailable daemon reports Nothing is running.",
			Flags: []urfavecli.Flag{
				&urfavecli.StringFlag{Name: "stream", Aliases: []string{"s"}, Value: "both", Usage: "select stdout, stderr, or both"},
				&urfavecli.IntFlag{Name: "tail", Aliases: []string{"n"}, Usage: "select the final N entries"},
				&urfavecli.Uint64Flag{Name: "after-cursor", Aliases: []string{"c"}, Usage: "read entries after this cursor"},
				&urfavecli.IntFlag{Name: "limit-bytes", Aliases: []string{"b"}, Usage: "limit returned output bytes"},
				&urfavecli.StringFlag{Name: "match", Aliases: []string{"m"}, Usage: "filter entries by regular expression"},
				&urfavecli.BoolFlag{Name: "follow", Aliases: []string{"f"}, Usage: "follow the named session across process launches until interrupted"},
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
			Description: "Without --match, wait returns when one process incarnation exits; with --match, it returns when output matches or that incarnation exits. " +
				"Without --after-cursor, a stopped or never-launched session waits for its next launch and evaluates from that launch cursor. " +
				"It starts a daemon when needed and waits at most 30s unless --timeout is set. Exit code is 0 for a match or an exit without --match, 3 when --match sees process exit first, and 2 on timeout.",
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
			Name:      "input",
			Usage:     "write one bounded payload to a running TTY session",
			ArgsUsage: "NAME",
			Description: "Input writes exactly one payload to the initial running TTY incarnation at its launch cursor and returns after acknowledgement. It is at-most-once: a launch race or lost acknowledgement is returned without resend. " +
				"It never starts a daemon or process, waits for a launch, queues input, retries, or retains or explicitly echoes bytes. " +
				"--text sends exact bytes without a newline; --base64 requires strict padded base64 without whitespace and decodes to 1-32768 bytes. " +
				"Observe with logs or wait --match, answer with input, then wait --match to confirm; an occupied owner fails immediately with an ownership conflict.",
			Flags: []urfavecli.Flag{
				&urfavecli.StringFlag{Name: "text", Usage: "write exact text bytes without appending a newline"},
				&urfavecli.StringFlag{Name: "base64", Usage: "write strictly padded base64 bytes without whitespace"},
				&urfavecli.BoolFlag{Name: "json", Usage: "write stable JSON"},
			},
			Action: func(ctx context.Context, cmd *urfavecli.Command) error {
				return inputCommand(ctx, cmd, version, buildTime, writer)
			},
		},
		{
			Name:      "restart",
			Usage:     "restart one or more processes by name",
			ArgsUsage: "NAME...",
			Description: "Restart applies the graceful stop sequence and attempts to relaunch requested names with each one's recorded command, working directory, and environment. " +
				"Names are attempted in order; the first error stops the remaining restarts, so it reports only successful attempts; each result includes the new PID and launch cursor. " +
				"It restarts processes, not the daemon. A manifest may set restart: on-failure (never is the default) for five bounded crash relaunches; inspect retained failing logs before editing again. " +
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
			Name:      "remove",
			Usage:     "remove one or more supervision sessions",
			ArgsUsage: "NAME...",
			Description: "Remove stops each running incarnation, closes attached followers, and discards retained output and launch state. " +
				"The observed followers count never warns, prompts, or blocks removal. It changes runtime state only and never edits hum.yaml.",
			Flags: []urfavecli.Flag{
				&urfavecli.BoolFlag{Name: "json", Aliases: []string{"j"}, Usage: "write one stable JSON object per name"},
			},
			Action: func(ctx context.Context, cmd *urfavecli.Command) error {
				return removeCommand(ctx, cmd, version, buildTime, writer)
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
		if cmd.Bool("tty") || cmd.IsSet("tty") {
			return "", nil, errors.New("--tty requires an ad-hoc command after --")
		}
		for _, arg := range args[1:] {
			if arg == "--tty" || arg == "--tty=true" {
				return "", nil, errors.New("--tty requires an ad-hoc command after --")
			}
		}
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
		case "detach", "json", "tty":
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
	manifest, err := loadManifestOrEmpty(cwd)
	if err != nil {
		return err
	}
	definition, declared := manifest.byName[name]
	if (cmd.Bool("tty") || cmd.IsSet("tty")) && len(argv) == 0 {
		return errors.New("--tty requires an ad-hoc command after --")
	}
	if len(argv) != 0 && declared {
		return fmt.Errorf("process %q is declared in hum.yaml; use hum start %s", name, name)
	}
	client, err := runDaemonClient(ctx, cfg)
	if err != nil {
		return err
	}
	defer client.Close()

	launch := func() (app.Process, error) {
		if len(argv) != 0 {
			return client.Start(ctx, daemon.StartRequest{Name: name, Source: "ad_hoc", Argv: argv, Cwd: cwd, Env: os.Environ(), TTY: cmd.Bool("tty")})
		}
		if declared {
			return client.Start(ctx, daemon.StartRequest{Name: name, Source: definition.Source, Root: manifest.root, Argv: definition.Argv, Cwd: definition.Cwd, Env: os.Environ(), Ready: readinessConfig(definition), TTY: definition.TTY, Restart: protocolRestartPolicy(definition)})
		}
		return client.Start(ctx, daemon.StartRequest{Name: name, Root: manifest.root, Cwd: manifest.root})
	}

	if cmd.Bool("detach") {
		if len(argv) == 0 && !declared {
			current, getErr := client.Get(ctx, daemon.GetRequest{Name: name, Cwd: manifest.root})
			if getErr != nil || len(current.Argv) == 0 {
				return errors.New("run requires a command after --")
			}
		}
		process, startErr := launch()
		if startErr != nil {
			if isNameInUse(startErr) || errors.Is(startErr, app.ErrNameInUse) {
				return fmt.Errorf("%w; watch it with hum logs %s --follow", startErr, name)
			}
			return startErr
		}
		result := runResult{Name: process.Name, PID: process.PID, Cursor: protocol.Cursor(process.LaunchCursor)}
		if process.Source != "" && process.Source != "ad_hoc" {
			result.Source, result.Argv, result.Outcome = process.Source, append([]string(nil), process.Argv...), "started"
			if process.Readiness == nil || process.Readiness.State == app.ReadinessRunningUnverified {
				result.Outcome, result.Readiness = "running_unverified", "running_unverified"
			} else {
				result.Readiness = process.Readiness.State
			}
		}
		if cmd.Bool("json") {
			return encodeJSON(writer, result)
		}
		if result.Source != "" {
			_, err := fmt.Fprintf(writer, "started %s (PID %d, cursor %d, source=%s, argv=%s, outcome=%s, readiness=%s)\n", process.Name, process.PID, process.LaunchCursor, result.Source, shellJoin(result.Argv), result.Outcome, result.Readiness)
			return err
		}
		_, err := fmt.Fprintf(writer, "started %s (PID %d, cursor %d)\n", process.Name, process.PID, process.LaunchCursor)
		return err
	}

	current, getErr := client.Get(ctx, daemon.GetRequest{Name: name, Cwd: manifest.root})
	// A supplied command defines a replacement incarnation, so only an
	// explicit --tty reserves input for it. Existing/declared TTY definitions
	// are attached automatically when merely observing or launching them.
	wantTTY := cmd.Bool("tty") || len(argv) == 0 && ((declared && definition.TTY) || (!declared && getErr == nil && current.TTY))
	var localInput *ttyInput
	inputConflict := false
	if wantTTY && !cmd.Bool("detach") {
		inputRequest := ttyInputRequest(name, manifest.root, definition, argv)
		if len(argv) != 0 {
			inputRequest.Cwd = cwd
			inputRequest.Root = manifest.root
			inputRequest.Argv = append([]string(nil), argv...)
			inputRequest.Source = "ad_hoc"
		}
		if declared {
			inputRequest.Cwd = definition.Cwd
			inputRequest.Root = manifest.root
		}
		if len(argv) == 0 && !declared && getErr == nil {
			// The retained record is already fully staged. Do not send its
			// argv back through PrepareTTY: input attach has no environment
			// payload and would otherwise erase the retained cwd/environment.
			inputRequest.Cwd = current.Cwd
		}
		session, attachErr := client.InputAttach(ctx, inputRequest)
		if attachErr != nil {
			if isInputConflict(attachErr) {
				inputConflict = true
				if _, writeErr := fmt.Fprintln(errWriter, "tty input is already owned; following output only"); writeErr != nil {
					return writeErr
				}
			} else {
				return attachErr
			}
		} else {
			localInput, err = newTTYInput(session, errWriter)
			if err != nil {
				_ = session.Release()
				return err
			}
			if _, writeErr := fmt.Fprintln(errWriter, "tty input attached; press Ctrl-] to detach input"); writeErr != nil {
				localInput.close()
				return writeErr
			}
			localInput.start()
			defer localInput.close()
		}
	}

	signals := notifyFollowSignals()
	defer signal.Stop(signals)
	follower, err := client.Follow(context.Background(), daemon.FollowRequest{Name: name, Cwd: manifest.root, Stream: protocol.StreamBoth, MaxEntries: cfg.ReadEntries, MaxBytes: int(cfg.ReadBytes)})
	if err != nil {
		return err
	}
	defer follower.Close()

	// Follow creates unresolved durable records. Refresh after the follower is
	// registered so the waiting message and launch decision preserve the
	// pre-existing non-TTY lifecycle race guarantees.
	current, getErr = client.Get(ctx, daemon.GetRequest{Name: name, Cwd: manifest.root})

	shouldLaunch := !inputConflict && (len(argv) != 0 || declared && (getErr != nil || current.State != app.StateRunning))
	if shouldLaunch {
		if _, err := launch(); err != nil {
			if isNameInUse(err) || errors.Is(err, app.ErrNameInUse) {
				return fmt.Errorf("%w; watch it with hum logs %s --follow", err, name)
			}
			return err
		}
	} else if getErr == nil && current.State != app.StateRunning {
		if len(current.Argv) == 0 && !declared {
			_, err = fmt.Fprintf(writer, "%s waiting for first launch (name does not resolve; hum run %s -- COMMAND may create it)\n", name, name)
		} else if len(current.Argv) == 0 {
			_, err = fmt.Fprintf(writer, "%s waiting for first launch\n", name)
		} else {
			_, err = fmt.Fprintf(writer, "%s waiting for next launch\n", name)
		}
		if err != nil {
			return err
		}
	}

	_, _, err = followLoop(ctx, follower, signals, func(event output.Event) error {
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
		return sig == os.Interrupt || sig == syscall.SIGTERM || sig == syscall.SIGHUP, nil
	})
	return err
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
	if len(args) != 1 {
		return aggregateLogsCommand(ctx, cmd, version, buildTime, writer, errWriter, args)
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
	var client *daemon.Client
	if cmd.Bool("follow") {
		client, err = runDaemonClient(ctx, cfg)
	} else {
		client, err = daemonClient(ctx, cfg)
	}
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
		if process, getErr := client.Get(ctx, daemon.GetRequest{Name: name, Cwd: cwd}); getErr == nil && process.State != app.StateRunning {
			message := logsWaitingMessage(name, process, manifest)
			if cmd.Bool("json") {
				err = encodeJSON(writer, eventJSON(name, output.Event{Read: &output.ReadResult{Entries: []output.Entry{{Stream: output.System, Time: time.Now(), Text: message}}}}))
			} else {
				_, err = io.WriteString(writer, message)
			}
			if err != nil {
				return err
			}
		}
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

type aggregateLogFollowerResult struct {
	index int
	event output.Event
	err   error
}

func aggregateLogsCommand(ctx context.Context, cmd *urfavecli.Command, version, buildTime string, writer, errWriter io.Writer, args []string) error {
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

	ctx = nonNilContext(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(args) > 0 {
		if duplicate := duplicateLogName(args); duplicate != "" {
			return fmt.Errorf("logs contains duplicate process name %q", duplicate)
		}
	}
	if cmd.IsSet("after-cursor") {
		return errors.New("logs --after-cursor is only supported for one explicit process name")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("current directory: %w", err)
	}

	var manifest manifestState
	if len(args) == 0 {
		// The no-name form is intentionally strict: it has the same definition
		// set as up, rather than falling back to an ad-hoc session.
		manifest, err = loadManifest(cwd)
	} else {
		manifest, err = loadManifestOrEmpty(cwd)
	}
	if err != nil {
		return err
	}
	names := append([]string(nil), args...)
	if len(args) == 0 {
		names = make([]string, 0, len(manifest.defs))
		for _, definition := range manifest.defs {
			names = append(names, definition.Name)
		}
	}
	if len(names) == 0 {
		return newUserFacingError("No process declarations resolve for logs. Define processes in hum.yaml or run hum init.")
	}

	cfg, err := cliConfig(cmd, version, buildTime)
	if err != nil {
		return err
	}
	maxBytes := int(cfg.ReadBytes)
	if limitBytes != 0 {
		maxBytes = limitBytes
	}
	request := daemon.OutputRequest{
		Cwd: cwd, Tail: tail, Stream: protocol.Stream(stream), Match: cmd.String("match"),
		MaxEntries: cfg.ReadEntries, MaxBytes: maxBytes,
	}
	var client *daemon.Client
	if cmd.Bool("follow") {
		client, err = runDaemonClient(ctx, cfg)
	} else {
		client, err = daemonClient(ctx, cfg)
	}
	if err != nil {
		if client != nil {
			_ = client.Close()
		}
		if cmd.Bool("follow") || !daemonUnavailable(err) {
			return err
		}
		return renderAggregateLogsUnavailable(writer, errWriter, cmd.Bool("json"), names, manifest)
	}
	defer client.Close()
	if cmd.Bool("follow") {
		return aggregateLogsFollow(ctx, cmd, client, request, names, manifest, writer, errWriter)
	}
	return aggregateLogsRead(ctx, cmd, client, request, names, writer, errWriter)
}

func aggregateLogNamedError(name string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", name, err)
}

func duplicateLogName(names []string) string {
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		if _, ok := seen[name]; ok {
			return name
		}
		seen[name] = struct{}{}
	}
	return ""
}

func renderAggregateLogsUnavailable(writer, errWriter io.Writer, jsonOutput bool, names []string, manifest manifestState) error {
	renderer := newAggregateLogRenderer(writer, errWriter, jsonOutput)
	var firstErr error
	for _, name := range names {
		nameErr := newUserFacingError(logsUnavailableMessage)
		if definition, ok := manifest.byName[name]; ok {
			nameErr = manifestUnavailableMessage(definition)
		}
		if firstErr == nil {
			firstErr = aggregateLogNamedError(name, nameErr)
		}
		if err := renderer.writeError(name, aggregateLogWireError(nameErr)); err != nil {
			return err
		}
	}
	return firstErr
}

func aggregateLogsRead(ctx context.Context, cmd *urfavecli.Command, client *daemon.Client, request daemon.OutputRequest, names []string, writer, errWriter io.Writer) error {
	renderer := newAggregateLogRenderer(writer, errWriter, cmd.Bool("json"))
	var firstErr error
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		request.Name = name
		result, err := client.Output(ctx, request)
		if err != nil {
			if aggregateLogFatalError(err) {
				return err
			}
			if firstErr == nil {
				firstErr = aggregateLogNamedError(name, err)
			}
			if writeErr := renderer.writeError(name, aggregateLogWireError(err)); writeErr != nil {
				return writeErr
			}
			continue
		}
		if err := renderer.writeEvent(name, output.Event{Read: &result}); err != nil {
			return err
		}
		if err := renderer.writeCursor(name, result); err != nil {
			return err
		}
	}
	return firstErr
}

func aggregateLogsFollow(ctx context.Context, cmd *urfavecli.Command, client *daemon.Client, request daemon.OutputRequest, names []string, manifest manifestState, writer, errWriter io.Writer) error {
	renderer := newAggregateLogRenderer(writer, errWriter, cmd.Bool("json"))
	signals := notifyFollowSignals()
	defer signal.Stop(signals)

	followCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	followers := make([]*daemon.Follower, len(names))
	var readers sync.WaitGroup
	var closeOnce sync.Once
	closeAll := func() {
		closeOnce.Do(func() {
			cancel()
			for _, follower := range followers {
				if follower != nil {
					_ = follower.Close()
				}
			}
			readers.Wait()
		})
	}
	defer closeAll()

	var firstErr error
	for index, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		follower, err := client.Follow(ctx, daemon.FollowRequest{
			Name: name, Cwd: request.Cwd, After: request.After, Tail: request.Tail, Stream: request.Stream,
			Match: request.Match, MaxEntries: request.MaxEntries, MaxBytes: request.MaxBytes,
		})
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if aggregateLogFatalError(err) {
				return err
			}
			if firstErr == nil {
				firstErr = aggregateLogNamedError(name, err)
			}
			if writeErr := renderer.writeError(name, aggregateLogWireError(err)); writeErr != nil {
				return writeErr
			}
			continue
		}
		followers[index] = follower
	}

	// Match the single-name follow lifecycle message without changing the
	// fixed membership selected before the daemon was contacted.
	for index, follower := range followers {
		if follower == nil {
			continue
		}
		name := names[index]
		process, getErr := client.Get(ctx, daemon.GetRequest{Name: name, Cwd: request.Cwd})
		if getErr != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if aggregateLogFatalError(getErr) {
				return getErr
			}
			if firstErr == nil {
				firstErr = aggregateLogNamedError(name, getErr)
			}
			if writeErr := renderer.writeError(name, aggregateLogWireError(getErr)); writeErr != nil {
				return writeErr
			}
			_ = follower.Close()
			followers[index] = nil
			continue
		}
		if process.State == app.StateRunning {
			continue
		}
		message := logsWaitingMessage(name, process, manifest)
		if err := renderer.writeEvent(name, output.Event{Read: &output.ReadResult{Entries: []output.Entry{{Stream: output.System, Time: time.Now(), Text: message}}}}); err != nil {
			return err
		}
	}

	events := make(chan aggregateLogFollowerResult, len(names))
	active := 0
	for index, follower := range followers {
		if follower == nil {
			continue
		}
		active++
		readers.Add(1)
		go func(index int, follower *daemon.Follower) {
			defer readers.Done()
			for {
				event, err := follower.Next(followCtx)
				result := aggregateLogFollowerResult{index: index, event: event, err: err}
				select {
				case events <- result:
				case <-followCtx.Done():
					return
				}
				if err != nil {
					return
				}
			}
		}(index, follower)
	}
	if active == 0 {
		return firstErr
	}

	for active > 0 {
		select {
		case <-ctx.Done():
			return nil
		case sig, ok := <-signals:
			if !ok {
				signals = nil
				continue
			}
			if sig == nil {
				continue
			}
			return nil
		case result := <-events:
			name := names[result.index]
			if result.err != nil {
				if ctx.Err() != nil || followCtx.Err() != nil {
					return nil
				}
				cleanClose := false
				fatal := aggregateLogFatalError(result.err)
				if errors.Is(result.err, io.EOF) {
					_, getErr := client.Get(ctx, daemon.GetRequest{Name: name, Cwd: request.Cwd})
					cleanClose = getErr == nil || isNotFound(getErr)
					fatal = !cleanClose && aggregateLogFatalError(getErr)
				}
				if fatal {
					return result.err
				}
				if !cleanClose {
					if firstErr == nil {
						firstErr = aggregateLogNamedError(name, result.err)
					}
					if err := renderer.writeError(name, aggregateLogWireError(result.err)); err != nil {
						return err
					}
				}
				_ = followers[result.index].Close()
				followers[result.index] = nil
				active--
				if active == 0 {
					return firstErr
				}
				continue
			}
			if err := renderer.writeEvent(name, result.event); err != nil {
				return err
			}
		}
	}
	return firstErr
}

func logsWaitingMessage(name string, process app.Process, manifest manifestState) string {
	message := fmt.Sprintf("%s waiting for next launch\n", name)
	if len(process.Argv) == 0 {
		message = fmt.Sprintf("%s waiting for first launch\n", name)
		if _, declared := manifest.byName[name]; !declared {
			message = fmt.Sprintf("%s waiting for first launch (name does not resolve; hum run %s -- COMMAND may create it)\n", name, name)
		}
	}
	return message
}

func aggregateLogFatalError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	if daemonUnavailable(err) || errors.Is(err, io.EOF) {
		return true
	}
	var wire *daemon.WireError
	if errors.As(err, &wire) && wire != nil {
		switch wire.Code {
		case protocol.ErrorSupervisorClosed, protocol.ErrorVersionMismatch:
			return true
		default:
			return false
		}
	}
	return true
}

func aggregateLogWireError(err error) *protocol.WireError {
	var wire *daemon.WireError
	if errors.As(err, &wire) && wire != nil {
		copy := *wire
		return &copy
	}
	message := "aggregate logs failed"
	if err != nil {
		message = err.Error()
	}
	return protocol.NewWireError(protocol.ErrorInternal, message, nil)
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
	if _, err := loadManifestOrEmpty(cwd); err != nil {
		return err
	}
	cfg, err := cliConfig(cmd, version, buildTime)
	if err != nil {
		return err
	}
	client, err := runDaemonClient(ctx, cfg)
	if err != nil {
		if client != nil {
			_ = client.Close()
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
	resettable := make(map[string]bool, len(processes))
	for _, process := range processes {
		if process.State == app.StateRunning {
			running[process.Name] = true
		}
		resettable[process.Name] = process.NextLaunchAt != nil || process.Restart == app.RestartOnFailure && process.Relaunches > 0
	}
	var firstErr error
	for _, name := range names {
		result := stopResult{Name: name}
		if !running[name] && !resettable[name] {
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

func removeCommand(ctx context.Context, cmd *urfavecli.Command, version, buildTime string, writer io.Writer) error {
	names := cmd.Args().Slice()
	if len(names) == 0 {
		return errors.New("remove requires at least one process name")
	}
	ctx = nonNilContext(ctx)
	cfg, err := cliConfig(cmd, version, buildTime)
	if err != nil {
		return err
	}
	client, err := daemonClient(ctx, cfg)
	if err != nil {
		return err
	}
	defer client.Close()
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("current directory: %w", err)
	}
	for _, name := range names {
		if err := client.Remove(context.Background(), daemon.RemoveRequest{Name: name, Cwd: cwd}); err != nil {
			return err
		}
		result := stopResult{Name: name, Status: "removed"}
		if cmd.Bool("json") {
			if err := encodeJSON(writer, result); err != nil {
				return err
			}
		} else if _, err := fmt.Fprintf(writer, "removed %s\n", name); err != nil {
			return err
		}
	}
	return nil
}

func processNeedsRestartControl(process app.Process) bool {
	return process.NextLaunchAt != nil || process.Restart == app.RestartOnFailure && process.Relaunches > 0
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
		if byName[name].State != app.StateRunning && !processNeedsRestartControl(byName[name]) {
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
			request.TTY = definition.TTY
			request.Restart = protocolRestartPolicy(definition)
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
			Restart:      string(effectiveProcessRestart(process)),
			Relaunches:   process.Relaunches,
			NextLaunchAt: process.NextLaunchAt,
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

func upCommand(ctx context.Context, cmd *urfavecli.Command, version, buildTime string, writer, errWriter io.Writer) error {
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
	if cmd.Bool("no-wait") && manifestHasAfter(manifest.defs) {
		return errors.New("hum up --no-wait is not allowed when hum.yaml declares after dependencies")
	}
	return manifestLaunchCommandWithStateMode(ctx, cmd, version, buildTime, writer, manifest, names, true, true, errWriter)
}

func manifestLaunchCommand(ctx context.Context, cmd *urfavecli.Command, version, buildTime string, writer io.Writer, names []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("current directory: %w", err)
	}
	manifest, err := loadManifest(cwd)
	if err != nil {
		var noCandidate *project.NoCandidateError
		if !errors.As(err, &noCandidate) {
			return err
		}
		cfg, configErr := cliConfig(cmd, version, buildTime)
		if configErr != nil {
			return configErr
		}
		client, dialErr := daemonClient(ctx, cfg)
		if client != nil {
			_ = client.Close()
		}
		if dialErr != nil {
			return err
		}
		manifest, err = loadManifestOrEmpty(cwd)
		if err != nil {
			return err
		}
	}
	return manifestLaunchCommandWithState(ctx, cmd, version, buildTime, writer, manifest, names, false)
}

func loadManifestForCommand() (manifestState, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return manifestState{}, fmt.Errorf("current directory: %w", err)
	}
	return loadManifest(cwd)
}

func manifestLaunchCommandWithState(ctx context.Context, cmd *urfavecli.Command, version, buildTime string, writer io.Writer, manifest manifestState, names []string, preserveRecovery bool) error {
	return manifestLaunchCommandWithStateMode(ctx, cmd, version, buildTime, writer, manifest, names, preserveRecovery, false, nil)
}

func manifestLaunchCommandWithStateMode(ctx context.Context, cmd *urfavecli.Command, version, buildTime string, writer io.Writer, manifest manifestState, names []string, preserveRecovery, ordered bool, progressWriter io.Writer) error {
	ctx = nonNilContext(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if ordered && cmd.Bool("no-wait") && manifestHasAfter(manifest.defs) {
		return errors.New("hum up --no-wait is not allowed when hum.yaml declares after dependencies")
	}
	timeoutOverride, err := manifestTimeoutOverride(cmd)
	if err != nil {
		return err
	}
	if ordered && len(manifest.defs) == 0 {
		if cmd.Bool("json") {
			return nil
		}
		_, err = fmt.Fprintln(writer, "No processes are declared in hum.yaml.")
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
	env := manifestProcessEnv()
	var results []manifestLaunchResult
	var progress *manifestProgressRenderer
	if ordered && !cmd.Bool("json") && !cmd.Bool("no-wait") && progressWriter != nil {
		progress = newManifestProgressRenderer(progressWriter, len(names))
	}
	if ordered {
		results, err = manifestUpSchedule(ctx, cmd, client, cwd, manifest, names, env, timeoutOverride, preserveRecovery, progress)
	} else {
		results, err = manifestStartConcurrent(ctx, cmd, client, cwd, manifest, names, env, timeoutOverride)
	}
	if err != nil {
		return err
	}
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

func manifestHasAfter(definitions []project.Definition) bool {
	for _, definition := range definitions {
		if len(definition.After) != 0 {
			return true
		}
	}
	return false
}

type manifestLaunchState struct {
	definition project.Definition
	process    app.Process
	result     manifestLaunchResult
	observedAt time.Time
}

func ensureNamedManifestStart(ctx context.Context, client *daemon.Client, cwd string, manifest manifestState, name string, env []string, preserveRecovery bool) (project.Definition, manifestLaunchResult, app.Process) {
	definition, ok := manifest.byName[name]
	if !ok {
		definition = undefinedManifestDefinition(name)
		current, getErr := client.Get(ctx, daemon.GetRequest{Name: name, Cwd: manifest.root})
		if getErr != nil || len(current.Argv) == 0 {
			return definition, manifestLaunchError(definition, fmt.Errorf("no process definition or retained launch specification for %q", name)), app.Process{}
		}
		definition.Source, definition.Argv, definition.Cwd = current.Source, append([]string(nil), current.Argv...), current.Cwd
		if current.State == app.StateRunning {
			return definition, manifestLaunchResultFor(definition, current, "already_running"), current
		}
		process, startErr := client.Start(ctx, daemon.StartRequest{Name: name, Root: manifest.root, Cwd: manifest.root, TTY: current.TTY, Restart: string(app.RestartNever)})
		if startErr != nil {
			return definition, manifestLaunchError(definition, startErr), app.Process{}
		}
		return definition, manifestLaunchResultFor(definition, process, "started"), process
	}
	result, process, _, ensureErr := ensureManifestStart(ctx, client, cwd, manifest.root, definition, env, preserveRecovery)
	if ensureErr != nil {
		return definition, manifestLaunchError(definition, ensureErr), app.Process{}
	}
	return definition, result, process
}

func manifestStartConcurrent(ctx context.Context, cmd *urfavecli.Command, client *daemon.Client, cwd string, manifest manifestState, names []string, env []string, timeoutOverride time.Duration) ([]manifestLaunchResult, error) {
	states := make([]manifestLaunchState, len(names))
	var launches sync.WaitGroup
	for index, name := range names {
		launches.Add(1)
		go func(index int, name string) {
			defer launches.Done()
			observedAt := time.Now()
			definition, result, process := ensureNamedManifestStart(ctx, client, cwd, manifest, name, env, false)
			if result.Outcome == "started" && !process.Start.IsZero() {
				observedAt = process.Start
			} else if result.Outcome == "already_running" {
				observedAt = time.Now()
			}
			states[index] = manifestLaunchState{definition: definition, process: process, result: result, observedAt: observedAt}
		}(index, name)
	}
	launches.Wait()

	var waits sync.WaitGroup
	for index := range states {
		state := states[index]
		if cmd.Bool("no-wait") || state.result.Outcome == "error" {
			continue
		}
		if state.process.State != app.StateRunning {
			if state.definition.Ready == nil {
				continue
			}
			waits.Add(1)
			go func(index int, state manifestLaunchState) {
				defer waits.Done()
				timeout, timeoutErr := manifestTimeoutForResult(cmd, state.definition, timeoutOverride)
				if timeoutErr != nil {
					states[index].result = manifestLaunchError(state.definition, timeoutErr)
					return
				}
				timeout = manifestRemainingTimeout(timeout, state.observedAt)
				result, waitErr := manifestReadinessResult(client, ctx, cwd, state.definition, state.process, state.result.Outcome, timeout)
				if waitErr != nil {
					result = manifestLaunchError(state.definition, waitErr)
				}
				states[index].result = result
			}(index, state)
			continue
		}
		if state.process.Readiness == nil || state.process.Readiness.State != app.ReadinessStarting {
			continue
		}
		waits.Add(1)
		go func(index int, state manifestLaunchState) {
			defer waits.Done()
			timeout, timeoutErr := manifestTimeoutForResult(cmd, state.definition, timeoutOverride)
			if timeoutErr != nil {
				states[index].result = manifestLaunchError(state.definition, timeoutErr)
				return
			}
			timeout = manifestRemainingTimeout(timeout, state.observedAt)
			result, waitErr := manifestReadinessResult(client, ctx, cwd, state.definition, state.process, state.result.Outcome, timeout)
			if waitErr != nil {
				result = manifestLaunchError(state.definition, waitErr)
			}
			states[index].result = result
		}(index, state)
	}
	waits.Wait()
	results := make([]manifestLaunchResult, len(states))
	for index, state := range states {
		results[index] = state.result
	}
	return results, nil
}

func manifestTimeoutForResult(cmd *urfavecli.Command, definition project.Definition, override time.Duration) (time.Duration, error) {
	if override != 0 {
		return override, nil
	}
	return parseManifestTimeout(cmd, definition)
}

func manifestRemainingTimeout(timeout time.Duration, observedAt time.Time) time.Duration {
	remaining := timeout - time.Since(observedAt)
	if remaining <= 0 {
		return 0
	}
	return remaining
}

type manifestUpScheduleOps struct {
	start     func(context.Context, project.Definition) (manifestLaunchResult, app.Process, time.Time)
	readiness func(context.Context, project.Definition, app.Process, string, time.Duration) (manifestLaunchResult, error)
	skipped   func(context.Context, project.Definition, []string) manifestLaunchResult
}

func manifestUpSchedule(ctx context.Context, cmd *urfavecli.Command, client *daemon.Client, cwd string, manifest manifestState, names []string, env []string, timeoutOverride time.Duration, preserveRecovery bool, progress *manifestProgressRenderer) ([]manifestLaunchResult, error) {
	ops := manifestUpScheduleOps{
		start: func(ctx context.Context, definition project.Definition) (manifestLaunchResult, app.Process, time.Time) {
			observedAt := time.Now()
			_, result, process := ensureNamedManifestStart(ctx, client, cwd, manifest, definition.Name, env, preserveRecovery)
			if result.Outcome == "started" && !process.Start.IsZero() {
				observedAt = process.Start
			} else if result.Outcome == "already_running" {
				observedAt = time.Now()
			}
			return result, process, observedAt
		},
		readiness: func(ctx context.Context, definition project.Definition, process app.Process, outcome string, timeout time.Duration) (manifestLaunchResult, error) {
			return manifestReadinessResult(client, ctx, cwd, definition, process, outcome, timeout)
		},
		skipped: func(ctx context.Context, definition project.Definition, blocked []string) manifestLaunchResult {
			return manifestLaunchSkipped(ctx, client, manifest.root, definition, blocked)
		},
	}
	return manifestUpScheduleWithOps(ctx, cmd, manifest, names, timeoutOverride, progress, ops)
}

func manifestUpScheduleWithOps(ctx context.Context, cmd *urfavecli.Command, manifest manifestState, names []string, timeoutOverride time.Duration, progress *manifestProgressRenderer, ops manifestUpScheduleOps) ([]manifestLaunchResult, error) {
	definitions := make([]project.Definition, 0, len(names))
	for _, name := range names {
		if definition, ok := manifest.byName[name]; ok {
			definitions = append(definitions, definition)
		}
	}
	sort.Slice(definitions, func(i, j int) bool { return definitions[i].Name < definitions[j].Name })
	results := make([]manifestLaunchResult, len(definitions))
	done := make([]bool, len(definitions))
	byName := make(map[string]int, len(definitions))
	for index, definition := range definitions {
		byName[definition.Name] = index
	}
	var mu sync.Mutex
	cond := sync.NewCond(&mu)
	wakeDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			mu.Lock()
			cond.Broadcast()
			mu.Unlock()
		case <-wakeDone:
		}
	}()
	defer close(wakeDone)
	var workers sync.WaitGroup
	for index, definition := range definitions {
		workers.Add(1)
		go func(index int, definition project.Definition) {
			defer workers.Done()
			mu.Lock()
			for {
				allDone := true
				for _, dependency := range definition.After {
					dependencyIndex, ok := byName[dependency]
					if !ok || !done[dependencyIndex] {
						allDone = false
						break
					}
				}
				if allDone || ctx.Err() != nil {
					break
				}
				cond.Wait()
			}
			if ctx.Err() != nil {
				result := manifestLaunchError(definition, ctx.Err())
				if progress != nil {
					progress.writeInitial(definition, result)
				}
				results[index] = result
				done[index] = true
				cond.Broadcast()
				mu.Unlock()
				return
			}
			blocked := make([]string, 0, len(definition.After))
			for _, dependency := range definition.After {
				dependencyIndex := byName[dependency]
				if !manifestResultSatisfiesGate(results[dependencyIndex]) {
					blocked = append(blocked, dependency)
				}
			}
			if len(blocked) != 0 {
				sort.Strings(blocked)
				mu.Unlock()
				skipped := ops.skipped(ctx, definition, blocked)
				if progress != nil {
					progress.writeInitial(definition, skipped)
				}
				mu.Lock()
				results[index] = skipped
				done[index] = true
				cond.Broadcast()
				mu.Unlock()
				return
			}
			mu.Unlock()

			result, process, observedAt := ops.start(ctx, definition)
			waitsForReadiness := manifestProgressWaitsForReadiness(definition, result)
			if progress != nil {
				progress.writeInitial(definition, result)
			}
			if result.Outcome != "error" && !cmd.Bool("no-wait") && definition.Ready != nil && process.State == app.StateRunning {
				timeout, timeoutErr := manifestTimeoutForResult(cmd, definition, timeoutOverride)
				if timeoutErr != nil {
					result = manifestLaunchError(definition, timeoutErr)
				} else {
					timeout = manifestRemainingTimeout(timeout, observedAt)
					result, timeoutErr = ops.readiness(ctx, definition, process, result.Outcome, timeout)
					if timeoutErr != nil {
						result = manifestLaunchError(definition, timeoutErr)
					}
				}
			}
			if progress != nil && waitsForReadiness {
				progress.writeTerminal(result)
			}
			mu.Lock()
			results[index] = result
			done[index] = true
			cond.Broadcast()
			mu.Unlock()
		}(index, definition)
	}
	workers.Wait()
	if progress != nil {
		if err := progress.Close(); err != nil {
			return results, err
		}
	}
	return results, nil
}

func manifestProgressWaitsForReadiness(definition project.Definition, result manifestLaunchResult) bool {
	if definition.Ready == nil {
		return false
	}
	if result.Outcome != "started" && result.Outcome != "already_running" {
		return false
	}
	return result.Readiness == app.ReadinessStarting || (result.Outcome == "started" && result.Readiness == "")
}

func manifestResultSatisfiesGate(result manifestLaunchResult) bool {
	return (result.Outcome == "started" || result.Outcome == "already_running") && result.Readiness == app.ReadinessReady
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
				if errors.Is(result.err, context.Canceled) || errors.Is(result.err, context.DeadlineExceeded) || errors.Is(result.err, io.EOF) {
					return 0, false, nil
				}
				return 0, false, result.err
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
