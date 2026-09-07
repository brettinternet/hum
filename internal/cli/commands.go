package cli

import (
	"bytes"
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
	"hum/internal/orchestrate"
	"hum/internal/output"
	"hum/internal/project"
	"hum/internal/protocol"
	"hum/internal/skill"

	urfavecli "github.com/urfave/cli/v3"
)

func newCLICommands(version, buildTime string, writer, errWriter io.Writer) []*urfavecli.Command {
	runStopOnNthArg := 1
	mcpCommand := mcpCLICommand(version, buildTime, writer)
	mcpCommand.Usage = "serve MCP lifecycle tools over stdio"
	mcpCommand.Description = "Run a stdio Model Context Protocol server for one-time coding-agent registration; every tool requires an absolute existing project_root; start and up differ: start targets only named sessions without pulling prerequisites, while up resolves manifest readiness dependencies; resolved records and ad_hoc sessions handed off by hum run remain available until daemon shutdown or replacement; requests with IDs run concurrently up to 64 in flight, a 65th request returns -32001 without starting, duplicate in-flight IDs return -32600, notifications/cancelled returns -32800, responses are serialized, and EOF or parent cancellation cancels handlers and joins the response writer. Bounded output uses terminal-control-stripped text with no raw opt-out and explicit definitions use deterministic argv-based environment activation. The eleven tools are start, up, down, list, status, logs, wait, input, restart, and stop, plus remove; run, serve, and shutdown are not MCP tools.\n\nExamples:\n  hum mcp"
	commands := []*urfavecli.Command{
		{
			Name:        "serve",
			Usage:       "run the daemon attached or detached",
			UsageText:   "hum serve [--daemon]",
			ArgsUsage:   "",
			Description: "Run the daemon in the foreground and write diagnostics to stderr, or use --daemon: it starts a detached daemon, waits for readiness, and prints its PID and socket.\n\nExamples:\n  hum serve\n  hum serve --daemon",
			Flags: []urfavecli.Flag{
				&urfavecli.BoolFlag{Name: "daemon", Aliases: []string{"d"}, DefaultText: "false", Usage: "start detached; default is foreground"},
			},
			OnUsageError: onUsageError,
			Action: func(ctx context.Context, cmd *urfavecli.Command) error {
				return serveCommand(ctx, cmd, version, buildTime, errWriter)
			},
		},
		{
			Name:        "init",
			Usage:       "create hum.yaml from discovery",
			UsageText:   "hum init [--json]",
			ArgsUsage:   "",
			Description: "Create a hum.yaml manifest from strict project discovery without starting a daemon. A single candidate is generated; no candidate or ambiguity produces a commented template, and output reports the absolute path and next command hum up; --json emits stable JSON.\n\nExamples:\n  hum init\n  hum init --json",
			Flags: []urfavecli.Flag{
				&urfavecli.BoolFlag{Name: "json", Aliases: []string{"j"}, DefaultText: "false", Usage: "write stable JSON; default is human-readable output"},
			},
			OnUsageError: onUsageError,
			Action: func(ctx context.Context, cmd *urfavecli.Command) error {
				return initCommand(ctx, cmd, writer)
			},
		},
		mcpCommand,
		{
			Name:         "skill",
			Usage:        "print the fallback skill",
			UsageText:    "hum skill",
			ArgsUsage:    "",
			Description:  "Print the embedded shell-only fallback skill when MCP is unavailable. MCP-capable agents should use hum mcp.\n\nExamples:\n  hum skill",
			OnUsageError: onUsageError,
			Action: func(ctx context.Context, cmd *urfavecli.Command) error {
				return skillCommand(ctx, cmd, writer)
			},
		},
		{
			Name:          "run",
			Usage:         "run a named process",
			UsageText:     "hum run NAME [-- COMMAND [ARGS...]]",
			ArgsUsage:     "NAME [-- COMMAND [ARGS...]]",
			StopOnNthArg:  &runStopOnNthArg,
			ShellComplete: completeProcessNames,
			Description:   "Run a named session across process exits and launches, automatically starts a detached daemon when needed; without --detach, it stays attached by default and Ctrl+C detaches the observer, while with --detach it returns immediately and the daemon keeps owning it. Add --tty for an ad-hoc pseudo-terminal; stable JSON for detached runs is available, while attached runs stream raw child output; TTY input forwards terminal bytes and Ctrl-] detaches input.\n\nExamples:\n  hum run api\n  hum run api -- bun run api\n  hum run api --detach -- bun run api",
			Flags: []urfavecli.Flag{
				&urfavecli.BoolFlag{Name: "detach", Aliases: []string{"d"}, DefaultText: "false", Usage: "return without attaching; default is attached"},
				&urfavecli.BoolFlag{Name: "json", Aliases: []string{"j"}, DefaultText: "false", Usage: "write JSON for detached runs; default is raw attached output"},
				&urfavecli.BoolFlag{Name: "tty", DefaultText: "false", Usage: "use a pseudo-terminal for an ad-hoc command; omit to preserve declared or retained TTY mode"},
			},
			OnUsageError: onUsageError,
			Action: func(ctx context.Context, cmd *urfavecli.Command) error {
				return runCommand(ctx, cmd, version, buildTime, writer, errWriter)
			},
		},
		{
			Name:          "start",
			Usage:         "ensure named sessions are running",
			UsageText:     "hum start NAME... [--no-wait] [--timeout DURATION] [--json]",
			ArgsUsage:     "NAME...",
			ShellComplete: completeProcessNames,
			Description:   "Ensure each named session is running idempotently, relaunching retained stopped sessions from hum.yaml or conventional discovery; start never pulls in after prerequisites. It waits for readiness unless --no-wait and reports definition drift without adopting edits. Exit codes: 0 success; exit 1 for request error or definition drift; exit 2 for readiness timeout; exit 3 for early exit before ready.\n\nExamples:\n  hum start api\n  hum start api --no-wait\n  hum start api --json",
			Flags: []urfavecli.Flag{
				&urfavecli.BoolFlag{Name: "no-wait", DefaultText: "false", Usage: "return after spawn; default waits for readiness"},
				&urfavecli.StringFlag{Name: "timeout", Aliases: []string{"t"}, Usage: "readiness limit; omit for the manifest timeout"},
				&urfavecli.BoolFlag{Name: "json", Aliases: []string{"j"}, DefaultText: "false", Usage: "write JSON; default is human-readable output"},
			},
			OnUsageError: onUsageError,
			Action: func(ctx context.Context, cmd *urfavecli.Command) error {
				return startCommand(ctx, cmd, version, buildTime, writer)
			},
		},
		{
			Name:        "up",
			Usage:       "ensure manifest processes are running",
			UsageText:   "hum up [--no-wait] [--timeout DURATION] [--json]",
			ArgsUsage:   "",
			Description: "Resolve processes lexically; launch independent roots concurrently, gate dependents on readiness, and continue after failures. Report skipped blockers and definition_drift changed_fields with hum restart guidance; removed_definition suggests hum stop NAME or hum remove NAME; progress uses at most two lines; see hum logs NAME; reject --no-wait before daemon contact when after is present; bounded recovery reports recovery_pending or recovery_exhausted as not running without start request; targeted start NAME or restart NAME cancels it; one invocation never follows an automatic successor (automatic prerequisite successor), so rerun hum up after recovery. Exit codes: 0 success; exit 1 for request error or definition drift; exit 2 for readiness timeout; exit 3 for early exit or recovery not running.\n\nExamples:\n  hum up",
			Flags: []urfavecli.Flag{
				&urfavecli.BoolFlag{Name: "no-wait", DefaultText: "false", Usage: "return after spawn; default waits for readiness"},
				&urfavecli.StringFlag{Name: "timeout", Aliases: []string{"t"}, Usage: "readiness limit; omit for the manifest timeout"},
				&urfavecli.BoolFlag{Name: "json", Aliases: []string{"j"}, DefaultText: "false", Usage: "write JSON; default is human-readable output"},
			},
			OnUsageError: onUsageError,
			Action: func(ctx context.Context, cmd *urfavecli.Command) error {
				return upCommand(ctx, cmd, version, buildTime, writer, errWriter)
			},
		},
		{
			Name:        "down",
			Usage:       "stop current-project processes",
			UsageText:   "hum down [--json]",
			ArgsUsage:   "",
			Description: "Stop every process that is active in the current project, including resolved manifest and ad-hoc processes, concurrently; declared names without records are not running, and the daemon stays up. Down never starts or shuts down the daemon; it is idempotent and emits one result per name, or a no-work message.\n\nExamples:\n  hum down\n  hum down --json",
			Flags: []urfavecli.Flag{
				&urfavecli.BoolFlag{Name: "json", Aliases: []string{"j"}, DefaultText: "false", Usage: "write JSON; default is human-readable output"},
			},
			OnUsageError: onUsageError,
			Action: func(ctx context.Context, cmd *urfavecli.Command) error {
				return downCommand(ctx, cmd, version, buildTime, writer)
			},
		},
		{
			Name:        "list",
			Usage:       "list supervised processes",
			UsageText:   "hum list [--all] [--json]",
			ArgsUsage:   "",
			Description: "List is read-only and does not start an empty daemon. Use --all for every project; followed records show their followers count, while unfollowed human output is unchanged.\n\nExamples:\n  hum list\n  hum list --all\n  hum list --json",
			Flags: []urfavecli.Flag{
				&urfavecli.BoolFlag{Name: "all", Aliases: []string{"a"}, DefaultText: "false", Usage: "include every project; default is the current project"},
				&urfavecli.BoolFlag{Name: "json", Aliases: []string{"j"}, DefaultText: "false", Usage: "write JSON; default is human-readable output"},
			},
			OnUsageError: onUsageError,
			Action: func(ctx context.Context, cmd *urfavecli.Command) error {
				return listCommand(ctx, cmd, version, buildTime, writer, errWriter)
			},
		},
		{
			Name:          "status",
			Usage:         "show one supervised process",
			UsageText:     "hum status NAME [--json]",
			ArgsUsage:     "NAME",
			ShellComplete: completeProcessNames,
			Description:   "Show one process read-only and never starts a daemon. Human and JSON output include followers, the live attached run and logs --follow count, and recovery state; when no daemon exists, resolved names point to hum start NAME and other names point to hum run <name> -- <command>.\n\nExamples:\n  hum status api\n  hum status api --json",
			Flags: []urfavecli.Flag{
				&urfavecli.BoolFlag{Name: "json", Aliases: []string{"j"}, DefaultText: "false", Usage: "write JSON; default is human-readable output"},
			},
			OnUsageError: onUsageError,
			Action: func(ctx context.Context, cmd *urfavecli.Command) error {
				return statusCommand(ctx, cmd, version, buildTime, writer, errWriter)
			},
		},
		{
			Name:          "logs",
			Usage:         "read retained process output",
			UsageText:     "hum logs [NAME...] [--follow] [--json]",
			ArgsUsage:     "[NAME...]",
			ShellComplete: completeProcessNames,
			Description:   "Read bounded retained output for one or more names in command-line order; no names uses lexical order with no ad-hoc sessions, duplicate names are rejected, and --after-cursor requires one explicit name. Aggregate filters and limits apply independently, human entries use an atomic [NAME] prefix, and JSON is named NDJSON; --follow can attach before the first launch and crosses exit, wait, and launch boundaries with one follower per name, isolated per-session errors, and cancellation on daemon loss or output failure; following is read-only: Ctrl+C cancels only the follower, closes all followers in an aggregate, and never signals the managed process. Child output is terminal-control-stripped per entry while system entries remain raw: raw ESC bytes do not match, a ^ anchor now matches colourised output, stored bytes, cursors, and limit accounting remain raw, control-only bounded child entries remain present with empty text, follow --match selects stripped text and selected entries are emitted raw, attached run output is also raw, no --raw flag exists, and split sequences and carriage-return redraw frames remain separate.\n\nExamples:\n  hum logs api\n  hum logs api --tail 50\n  hum logs api --follow",
			Flags: []urfavecli.Flag{
				&urfavecli.StringFlag{Name: "stream", Aliases: []string{"s"}, Value: "both", Usage: "select stdout, stderr, or both"},
				&urfavecli.IntFlag{Name: "tail", Aliases: []string{"n"}, HideDefault: true, Usage: "select final N entries; omit for the newest default window"},
				&urfavecli.Uint64Flag{Name: "after-cursor", Aliases: []string{"c"}, HideDefault: true, Usage: "read after this cursor; without it, use the newest default window"},
				&urfavecli.IntFlag{Name: "limit-bytes", Aliases: []string{"b"}, HideDefault: true, Usage: "limit output bytes; omit for the default read limit"},
				&urfavecli.StringFlag{Name: "match", Aliases: []string{"m"}, Usage: "filter by regex; omit to include every entry"},
				&urfavecli.BoolFlag{Name: "follow", Aliases: []string{"f"}, DefaultText: "false", Usage: "follow launches; default is a bounded read"},
				&urfavecli.BoolFlag{Name: "json", Aliases: []string{"j"}, DefaultText: "false", Usage: "write JSON; default is human-readable output"},
			},
			OnUsageError: onUsageError,
			Action: func(ctx context.Context, cmd *urfavecli.Command) error {
				return logsCommand(ctx, cmd, version, buildTime, writer, errWriter)
			},
		},
		{
			Name:          "wait",
			Usage:         "wait for output or process exit",
			UsageText:     "hum wait NAME [--match REGEX] [--timeout DURATION] [--json]",
			ArgsUsage:     "NAME",
			ShellComplete: completeProcessNames,
			Description:   "Without --match, wait returns when one process incarnation exits; with --match, it returns when output matches or that incarnation exits. Without --after-cursor, a stopped or never-launched session waits for its next launch; it starts a daemon when needed and waits 30s by default unless --timeout is set. Exit codes: 0 for a match or unfiltered exit; exit 1 for a request or usage error; exit 2 for timeout; exit 3 when process exit precedes --match.\n\nExamples:\n  hum wait api\n  hum wait api --match ready\n  hum wait api --timeout 10s",
			Flags: []urfavecli.Flag{
				&urfavecli.Uint64Flag{Name: "after-cursor", Aliases: []string{"c"}, HideDefault: true, Usage: "search after this cursor; omit to evaluate from the current or next launch cursor"},
				&urfavecli.StringFlag{Name: "match", Aliases: []string{"m"}, Usage: "wait for matching non-empty regular expression; omit to wait only for exit"},
				&urfavecli.StringFlag{Name: "timeout", Aliases: []string{"t"}, DefaultText: "30s", Usage: "maximum wait duration"},
				&urfavecli.BoolFlag{Name: "json", Aliases: []string{"j"}, DefaultText: "false", Usage: "write JSON; default is human-readable output"},
			},
			OnUsageError: onUsageError,
			Action: func(ctx context.Context, cmd *urfavecli.Command) error {
				return waitCommand(ctx, cmd, version, buildTime, writer)
			},
		},
		{
			Name:          "input",
			Usage:         "write one payload to a running TTY",
			UsageText:     "hum input NAME (--text TEXT | --base64 VALUE) [--json]",
			ArgsUsage:     "NAME",
			ShellComplete: completeProcessNames,
			Description:   "Write one payload to the initial running TTY incarnation at its launch cursor and return after acknowledgement; input is at-most-once and never starts, waits, queues, retries, retains, or echoes bytes. Use exactly one of --text or --base64: text is exact bytes without appending a newline, while base64 is strict padded base64 without whitespace; Observe with logs or wait --match, answer with input, then confirm using the launch cursor, and an ownership conflict fails immediately.\n\nExamples:\n  hum input console --text \"yes\"\n  hum input console --base64 eWVz\n  hum input console --text \"yes\" --json",
			Flags: []urfavecli.Flag{
				&urfavecli.StringFlag{Name: "text", Usage: "write exact bytes; omit when using --base64"},
				&urfavecli.StringFlag{Name: "base64", Usage: "write padded base64 bytes; omit when using --text"},
				&urfavecli.BoolFlag{Name: "json", DefaultText: "false", Usage: "write JSON; default is human-readable output"},
			},
			OnUsageError: onUsageError,
			Action: func(ctx context.Context, cmd *urfavecli.Command) error {
				return inputCommand(ctx, cmd, version, buildTime, writer)
			},
		},
		{
			Name:          "restart",
			Usage:         "restart named processes",
			UsageText:     "hum restart NAME... [--no-wait] [--timeout DURATION] [--json]",
			ArgsUsage:     "NAME...",
			ShellComplete: completeProcessNames,
			Description:   "Apply a graceful stop and relaunch to named processes by name with their recorded launch details; restart does not restart the daemon and is not the daemon itself; by default, wait for each replacement incarnation to become ready or running_unverified when no matcher exists. --timeout sets a positive per-name readiness limit measured from that name's launch; --no-wait returns after spawn; names are attempted in order, readiness failures are reported and later names continue, but the first error stops the remaining restarts for request or validation errors; only successful attempts are reported with a new PID. Exit codes: 0 success; exit 1 for request or validation error; exit 2 for readiness timeout; exit 3 for exited before readiness.\n\nExamples:\n  hum restart api\n  hum restart api web --timeout 10s --json",
			Flags: []urfavecli.Flag{
				&urfavecli.BoolFlag{Name: "no-wait", DefaultText: "false", Usage: "return after spawn; default waits for readiness"},
				&urfavecli.StringFlag{Name: "timeout", Aliases: []string{"t"}, Usage: "readiness limit; omit for the manifest timeout"},
				&urfavecli.BoolFlag{Name: "json", Aliases: []string{"j"}, DefaultText: "false", Usage: "write JSON; default is human-readable output"},
			},
			OnUsageError: onUsageError,
			Action: func(ctx context.Context, cmd *urfavecli.Command) error {
				return restartCommand(ctx, cmd, version, buildTime, writer)
			},
		},
		{
			Name:          "stop",
			Usage:         "stop named processes",
			UsageText:     "hum stop NAME... [--json]",
			ArgsUsage:     "NAME...",
			ShellComplete: completeProcessNames,
			Description:   "Stop multiple names with graceful group termination and emit one result per name. An already-stopped or unknown name succeeds as not running, so stop is idempotent and does not shut down the daemon.\n\nExamples:\n  hum stop api\n  hum stop api web --json",
			Flags: []urfavecli.Flag{
				&urfavecli.BoolFlag{Name: "json", Aliases: []string{"j"}, DefaultText: "false", Usage: "write JSON; default is human-readable output"},
			},
			OnUsageError: onUsageError,
			Action: func(ctx context.Context, cmd *urfavecli.Command) error {
				return stopCommand(ctx, cmd, version, buildTime, writer)
			},
		},
		{
			Name:          "remove",
			Usage:         "remove supervision sessions",
			UsageText:     "hum remove NAME... [--json]",
			ArgsUsage:     "NAME...",
			ShellComplete: completeProcessNames,
			Description:   "It stops each running incarnation, closes attached followers, and discards retained output and launch state for named supervision sessions. It never edits hum.yaml, and the followers count never warns, prompts, or blocks removal.\n\nExamples:\n  hum remove api\n  hum remove api web --json",
			Flags: []urfavecli.Flag{
				&urfavecli.BoolFlag{Name: "json", Aliases: []string{"j"}, DefaultText: "false", Usage: "write JSON; default is human-readable output"},
			},
			OnUsageError: onUsageError,
			Action: func(ctx context.Context, cmd *urfavecli.Command) error {
				return removeCommand(ctx, cmd, version, buildTime, writer)
			},
		},
		{
			Name:        "shutdown",
			Usage:       "shut down the daemon",
			UsageText:   "hum shutdown [--stop-processes] [--json]",
			ArgsUsage:   "",
			Description: "Shut down daemon lifetime rather than a named process. By default it refuses while managed processes are active and lists their names; --stop-processes stops every managed process first, and with no daemon is running it succeeds.\n\nExamples:\n  hum shutdown\n  hum shutdown --stop-processes",
			Flags: []urfavecli.Flag{
				&urfavecli.BoolFlag{Name: "stop-processes", DefaultText: "false", Usage: "stop active processes first; default refuses them"},
				&urfavecli.BoolFlag{Name: "json", Aliases: []string{"j"}, DefaultText: "false", Usage: "write JSON; default is human-readable output"},
			},
			OnUsageError: onUsageError,
			Action: func(ctx context.Context, cmd *urfavecli.Command) error {
				return shutdownCommand(ctx, cmd, version, buildTime, writer)
			},
		},
	}
	for _, command := range commands {
		if command != nil {
			command.ShellComplete = completeProcessNames
		}
	}
	return commands
}

func serveCommand(ctx context.Context, cmd *urfavecli.Command, version, buildTime string, errWriter io.Writer) error {
	if err := rejectProjectOverride(cmd, "serve"); err != nil {
		return err
	}
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
		// Flag parsing stops at NAME, so options written after it arrive here as
		// positional arguments: hum run NAME --detach is the documented grammar.
		if err := applyRunOptions(cmd, args[1:], false); err != nil {
			return "", nil, err
		}
		if cmd.Bool("tty") {
			return "", nil, errors.New("--tty requires an ad-hoc command after --")
		}
		return args[0], nil, nil
	}
	if separator < 2 {
		return "", nil, errors.New("run accepts exactly one process name before --")
	}
	if err := applyRunOptions(cmd, args[1:separator], true); err != nil {
		return "", nil, err
	}
	argv := append([]string(nil), args[separator+1:]...)
	if len(argv) == 0 || argv[0] == "" {
		return "", nil, errors.New("run requires a non-empty command after --")
	}
	return args[0], argv, nil
}

// applyRunOptions applies run and global options that appear after NAME,
// where urfave/cli no longer parses them. Without a separator, a stray word is
// most likely a command missing its --.
func applyRunOptions(cmd *urfavecli.Command, options []string, hasSeparator bool) error {
	for i := 0; i < len(options); i++ {
		flag := options[i]
		flagName, value, hasValue := strings.Cut(flag, "=")
		name := ""
		switch flagName {
		case "-d":
			name = "detach"
		case "-j":
			name = "json"
		case "-C":
			name = "project"
		default:
			if strings.HasPrefix(flagName, "--") {
				name = strings.TrimPrefix(flagName, "--")
			}
		}
		switch name {
		case "detach", "json", "tty":
			if hasValue {
				return fmt.Errorf("--%s does not take a value", name)
			}
			if err := cmd.Set(name, "true"); err != nil {
				return err
			}
		case "runtime-dir", "stop-grace", "output-bytes", "completed-records", "project":
			if !hasValue {
				i++
				if i >= len(options) {
					return fmt.Errorf("--%s requires a value", name)
				}
				value = options[i]
			}
			if err := cmd.Set(name, value); err != nil {
				return err
			}
		default:
			if !hasSeparator && !strings.HasPrefix(flag, "-") {
				selector := ""
				if cmd.IsSet("project") {
					selection, selectionErr := selectedProjectDirectory(cmd)
					if selectionErr != nil {
						return selectionErr
					}
					selector = selection.selector
				}
				return newUserFacingError(fmt.Sprintf("run requires -- before the command: %s", projectCommand(selector, "run NAME [options] -- "+flag+" ...")))
			}
			return fmt.Errorf("unknown run option %q", flag)
		}
	}
	return nil
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
	selection, err := selectedProjectDirectory(cmd)
	if err != nil {
		return err
	}
	cwd := selection.cwd
	cfg, err := cliConfig(cmd, version, buildTime)
	if err != nil {
		return err
	}
	manifest, err := loadManifestOrEmpty(cwd)
	if err != nil {
		return err
	}
	manifest.selector = selection.selector
	definition, declared := manifest.byName[name]
	if (cmd.Bool("tty") || cmd.IsSet("tty")) && len(argv) == 0 {
		return errors.New("--tty requires an ad-hoc command after --")
	}
	if len(argv) != 0 && declared {
		return fmt.Errorf("process %q is declared in hum.yaml; use %s", name, projectCommand(selection.selector, "start "+name))
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
				return fmt.Errorf("%w; watch it with %s", startErr, projectCommand(selection.selector, "logs "+name+" --follow"))
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
				return fmt.Errorf("%w; watch it with %s", err, projectCommand(selection.selector, "logs "+name+" --follow"))
			}
			return err
		}
	} else if getErr == nil && current.State != app.StateRunning {
		if len(current.Argv) == 0 && !declared {
			_, err = fmt.Fprintf(writer, "%s waiting for first launch (name does not resolve; %s may create it)\n", name, projectCommand(selection.selector, "run "+name+" -- COMMAND"))
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

func listCommand(ctx context.Context, cmd *urfavecli.Command, version, buildTime string, writer io.Writer, errWriters ...io.Writer) error {
	errWriter := io.Discard
	if len(errWriters) != 0 && errWriters[0] != nil {
		errWriter = errWriters[0]
	}
	if err := requireNoArgs(cmd, "list"); err != nil {
		return err
	}
	ctx = nonNilContext(ctx)
	selection, err := selectedProjectDirectory(cmd)
	if err != nil {
		return err
	}
	cwd := selection.cwd
	manifest, err := loadManifestOrEmpty(cwd)
	if err != nil {
		return err
	}
	manifest.selector = selection.selector
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
	processes, err := client.List(ctx, daemon.ListRequest{Cwd: cwd, All: cmd.Bool("all"), IncludeCompleted: true})
	if err != nil {
		return err
	}
	processes = mergeManifestProcesses(manifest, processes)
	warnings := client.StartupWarnings()
	if !cmd.Bool("json") {
		if err := writeStartupWarnings(errWriter, warnings); err != nil {
			return err
		}
	}
	if cmd.Bool("json") {
		items := make([]listProcessJSON, 0, len(processes))
		for _, process := range processes {
			items = append(items, processJSON(process))
		}
		return encodeJSON(writer, listJSON{Processes: items, Warnings: warnings})
	}
	return renderListHuman(writer, processes, cmd.Bool("all"))
}

func statusCommand(ctx context.Context, cmd *urfavecli.Command, version, buildTime string, writer io.Writer, errWriters ...io.Writer) error {
	errWriter := io.Discard
	if len(errWriters) != 0 && errWriters[0] != nil {
		errWriter = errWriters[0]
	}
	args := cmd.Args().Slice()
	if len(args) == 0 {
		return errors.New("status requires a process name")
	}
	if len(args) != 1 {
		return errors.New("status accepts exactly one process name")
	}
	name := args[0]
	ctx = nonNilContext(ctx)
	selection, err := selectedProjectDirectory(cmd)
	if err != nil {
		return err
	}
	cwd := selection.cwd
	manifest, err := loadManifestOrEmpty(cwd)
	if err != nil {
		return err
	}
	manifest.selector = selection.selector
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
				return manifestUnavailableMessage(definition, manifest.selector)
			}
			return newUserFacingError(logsUnavailableMessageFor(manifest.selector))
		}
		return err
	}
	defer client.Close()
	process, err := client.Get(ctx, daemon.GetRequest{Name: name, Cwd: cwd})
	warnings := client.StartupWarnings()
	if !cmd.Bool("json") {
		if warningErr := writeStartupWarnings(errWriter, warnings); warningErr != nil {
			return warningErr
		}
	}
	if err != nil {
		if !isNotFound(err) {
			return err
		}
		definition, declared := manifest.byName[name]
		if !declared {
			return wrapUserFacingError(err, err.Error()+". Run "+projectCommand(manifest.selector, "list --all")+" to see known processes.")
		}
		// A declared process that has never launched has no daemon record yet;
		// report it stopped, exactly as list does, instead of a raw lookup error.
		process = manifestProcess(definition, manifest.root)
	}
	if cmd.Bool("json") {
		result := statusJSONFor(process)
		result.Warnings = warnings
		return encodeJSON(writer, result)
	}
	return renderStatusHuman(writer, process)
}

// logReadBounds keeps bounded reads newest-first by selecting the configured
// default entry window as a tail when no cursor or explicit tail was supplied.
// Follow requests retain their existing initial-read bounds and delivery.
func logReadBounds(after *protocol.Cursor, tail, defaultEntries int, tailSet, follow bool) (int, int) {
	if follow {
		return tail, defaultEntries
	}
	if after == nil && !tailSet && tail == 0 {
		tail = defaultEntries
	}
	if tail > 0 {
		// A tail is already an entry bound. Leave MaxEntries unset so the ring
		// can honor a requested tail larger than its ordinary default cap.
		return tail, 0
	}
	return tail, defaultEntries
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
	selection, err := selectedProjectDirectory(cmd)
	if err != nil {
		return err
	}
	cwd := selection.cwd
	manifest, err := loadManifestOrEmpty(cwd)
	if err != nil {
		return err
	}
	manifest.selector = selection.selector
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
				return manifestUnavailableMessage(definition, manifest.selector)
			}
			return newUserFacingError(logsUnavailableMessageFor(manifest.selector))
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
	requestTail, maxEntries := logReadBounds(after, tail, cfg.ReadEntries, cmd.IsSet("tail"), cmd.Bool("follow"))
	request := daemon.OutputRequest{
		Name: name, Cwd: cwd, After: after, Tail: requestTail, Stream: protocol.Stream(stream), Match: cmd.String("match"),
		MaxEntries: maxEntries, MaxBytes: maxBytes,
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
	selection, err := selectedProjectDirectory(cmd)
	if err != nil {
		return err
	}
	cwd := selection.cwd

	var manifest manifestState
	if len(args) == 0 {
		// The no-name form is intentionally strict: it has the same definition
		// set as up, rather than falling back to an ad-hoc session.
		manifest, err = loadManifest(cwd)
	} else {
		manifest, err = loadManifestOrEmpty(cwd)
	}
	if err != nil {
		return projectGuidanceError(err, selection.selector)
	}
	manifest.selector = selection.selector
	names := append([]string(nil), args...)
	if len(args) == 0 {
		names = make([]string, 0, len(manifest.defs))
		for _, definition := range manifest.defs {
			names = append(names, definition.Name)
		}
	}
	if len(names) == 0 {
		return newUserFacingError(fmt.Sprintf("No process declarations resolve for logs. Define processes in hum.yaml or run %s.", projectCommand(selection.selector, "init")))
	}

	cfg, err := cliConfig(cmd, version, buildTime)
	if err != nil {
		return err
	}
	maxBytes := int(cfg.ReadBytes)
	if limitBytes != 0 {
		maxBytes = limitBytes
	}
	requestTail, maxEntries := logReadBounds(nil, tail, cfg.ReadEntries, cmd.IsSet("tail"), cmd.Bool("follow"))
	request := daemon.OutputRequest{
		Cwd: cwd, Tail: requestTail, Stream: protocol.Stream(stream), Match: cmd.String("match"),
		MaxEntries: maxEntries, MaxBytes: maxBytes,
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
		if cmd.Bool("follow") || cmd.Bool("json") || !daemonUnavailable(err) {
			return err
		}
		return renderAggregateLogsUnavailable(writer, errWriter, false, names, manifest)
	}
	defer client.Close()
	if cmd.Bool("follow") {
		return aggregateLogsFollow(ctx, cmd, client, request, names, manifest, writer, errWriter)
	}
	return aggregateLogsRead(ctx, cmd, client, request, names, manifest, writer, errWriter)
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
		nameErr := newUserFacingError(logsUnavailableMessageFor(manifest.selector))
		if definition, ok := manifest.byName[name]; ok {
			nameErr = manifestUnavailableMessage(definition, manifest.selector)
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

func aggregateLogsRead(ctx context.Context, cmd *urfavecli.Command, client *daemon.Client, request daemon.OutputRequest, names []string, manifest manifestState, writer, errWriter io.Writer) error {
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
			if _, declared := manifest.byName[name]; declared && isNotFound(err) {
				// A declared name with no daemon record yet (never launched, or
				// skipped behind a blocked dependency) is not a real failure: it
				// must not print a not_found error to both stdout and stderr, or
				// fail the aggregate when every other name succeeds.
				if writeErr := renderer.writeNotLaunched(name); writeErr != nil {
					return writeErr
				}
				continue
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
			message = fmt.Sprintf("%s waiting for first launch (name does not resolve; %s may create it)\n", name, projectCommand(manifest.selector, "run "+name+" -- COMMAND"))
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
	selection, err := selectedProjectDirectory(cmd)
	if err != nil {
		return err
	}
	cwd := selection.cwd
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
	selection, err := selectedProjectDirectory(cmd)
	if err != nil {
		return err
	}
	cwd := selection.cwd
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
	selection, err := selectedProjectDirectory(cmd)
	if err != nil {
		return err
	}
	cwd := selection.cwd
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
	selection, err := selectedProjectDirectory(cmd)
	if err != nil {
		return err
	}
	cwd := selection.cwd
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
	manifest.selector = selection.selector
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

// restartOutputResult is the response shared by the human and JSON restart
// renderers. The historical identity fields remain present for callers that
// use restart for launch inspection; the outcome/readiness fields describe the
// replacement incarnation observed by this invocation.
type restartOutputResult struct {
	Name         string           `json:"name"`
	Outcome      string           `json:"outcome"`
	Readiness    string           `json:"readiness"`
	PID          int              `json:"pid"`
	LaunchCursor protocol.Cursor  `json:"launch_cursor"`
	Message      string           `json:"message,omitempty"`
	Source       string           `json:"source,omitempty"`
	Argv         []string         `json:"argv"`
	Restarts     int              `json:"restarts"`
	Restart      string           `json:"restart"`
	Relaunches   int              `json:"relaunches"`
	NextLaunchAt *time.Time       `json:"next_launch_at,omitempty"`
	ReadyCursor  *protocol.Cursor `json:"ready_cursor,omitempty"`
}

func restartOutputFromProcess(process app.Process, definition project.Definition, outcome, message string) restartOutputResult {
	result := restartOutputResult{
		Name:         process.Name,
		Outcome:      outcome,
		Readiness:    restartReadiness(process, definition),
		PID:          process.PID,
		LaunchCursor: protocol.Cursor(process.LaunchCursor),
		Message:      message,
		Source:       process.Source,
		Argv:         append([]string(nil), process.Argv...),
		Restarts:     process.RestartCount,
		Restart:      string(effectiveProcessRestart(process)),
		Relaunches:   process.Relaunches,
		NextLaunchAt: process.NextLaunchAt,
	}
	if result.Name == "" {
		result.Name = definition.Name
	}
	if result.Source == "" {
		result.Source = definition.Source
	}
	if len(result.Argv) == 0 && len(definition.Argv) != 0 {
		result.Argv = append([]string(nil), definition.Argv...)
	}
	if result.Argv == nil {
		result.Argv = []string{}
	}
	if process.Readiness != nil && process.Readiness.Cursor != nil {
		cursor := protocol.Cursor(*process.Readiness.Cursor)
		result.ReadyCursor = &cursor
	}
	return result
}

func restartOutputFromManifest(result manifestLaunchResult, process app.Process, message string) restartOutputResult {
	output := restartOutputResult{
		Name:         result.Name,
		Outcome:      result.Outcome,
		Readiness:    result.Readiness,
		Message:      message,
		Source:       result.Source,
		Argv:         append([]string(nil), result.Argv...),
		Restarts:     process.RestartCount,
		Restart:      result.Restart,
		Relaunches:   result.Relaunches,
		NextLaunchAt: result.NextLaunchAt,
	}
	if result.PID != nil {
		output.PID = *result.PID
	}
	if result.LaunchCursor != nil {
		output.LaunchCursor = protocol.Cursor(*result.LaunchCursor)
	}
	if output.Argv == nil {
		output.Argv = []string{}
	}
	if result.ReadyCursor != nil {
		cursor := protocol.Cursor(*result.ReadyCursor)
		output.ReadyCursor = &cursor
	}
	return output
}

func restartReadiness(process app.Process, definition project.Definition) string {
	if process.Readiness != nil {
		return process.Readiness.State
	}
	if process.State != app.StateRunning {
		return ""
	}
	if definition.Ready == nil {
		return app.ReadinessRunningUnverified
	}
	return app.ReadinessStarting
}

func restartFailureMessage(outcome string, process app.Process) string {
	switch outcome {
	case "exited_before_ready":
		return "process exited before readiness"
	case "timed_out":
		return "readiness timed out"
	default:
		if process.Exit != nil && process.Exit.Err != nil {
			return process.Exit.Err.Error()
		}
		return ""
	}
}

func renderRestartOutputHuman(writer io.Writer, result restartOutputResult) error {
	if result.Outcome == "error" {
		if result.Message == "" {
			result.Message = "restart failed"
		}
		_, err := fmt.Fprintf(writer, "%s error: %s\n", result.Name, result.Message)
		return err
	}
	legacy := legacyRestartResult{
		Name: result.Name, PID: result.PID, Restarts: result.Restarts, LaunchCursor: result.LaunchCursor,
	}
	legacyOutput := restartResult{
		Name: legacy.Name, Source: result.Source, Argv: append([]string(nil), result.Argv...), PID: legacy.PID,
		Restarts: legacy.Restarts, LaunchCursor: legacy.LaunchCursor, Restart: result.Restart,
		Relaunches: result.Relaunches, NextLaunchAt: result.NextLaunchAt, Readiness: result.Readiness,
		ReadyCursor: result.ReadyCursor,
	}
	var base bytes.Buffer
	if err := renderRestartHuman(&base, legacyOutput); err != nil {
		return err
	}
	line := strings.TrimSuffix(base.String(), "\n")
	line = strings.Replace(line, " restarted ", " "+result.Outcome+" ", 1)
	if result.Source != "" && !strings.Contains(line, " source=") {
		line += fmt.Sprintf(" source=%s argv=%s", result.Source, shellJoin(result.Argv))
	}
	if result.Readiness != "" && !strings.Contains(line, " readiness=") {
		line += " readiness=" + result.Readiness
	}
	if result.ReadyCursor != nil && !strings.Contains(line, " ready_cursor=") {
		line += fmt.Sprintf(" ready_cursor=%d", *result.ReadyCursor)
	}
	if result.Message != "" {
		line += " message=" + result.Message
	}
	_, err := fmt.Fprintln(writer, line)
	return err
}

func aggregateRestartExit(results []restartOutputResult) error {
	for _, result := range results {
		if result.Outcome == "error" {
			return urfavecli.Exit("", 1)
		}
	}
	for _, result := range results {
		if result.Outcome == "exited_before_ready" {
			return urfavecli.Exit("", 3)
		}
	}
	for _, result := range results {
		if result.Outcome == "timed_out" {
			return urfavecli.Exit("", 2)
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
	timeoutOverride, err := manifestTimeoutOverride(cmd)
	if err != nil {
		return err
	}
	selection, err := selectedProjectDirectory(cmd)
	if err != nil {
		return err
	}
	cwd := selection.cwd
	manifest, err := loadManifestOrEmpty(cwd)
	if err != nil {
		return err
	}
	manifest.selector = selection.selector
	cfg, err := cliConfig(cmd, version, buildTime)
	if err != nil {
		return err
	}
	client, err := daemonClient(ctx, cfg)
	if err != nil {
		if daemonUnavailable(err) {
			for _, name := range names {
				if definition, ok := manifest.byName[name]; ok {
					return manifestUnavailableMessage(definition, manifest.selector)
				}
			}
			return newUserFacingError(logsUnavailableMessageFor(manifest.selector))
		}
		return err
	}
	defer client.Close()

	results := make([]restartOutputResult, 0, len(names))
	for _, name := range names {
		request := daemon.RestartRequest{Name: name, Cwd: cwd}
		definition, manifestLaunch := manifest.byName[name]
		if !manifestLaunch {
			definition = undefinedManifestDefinition(name)
		}
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

		launchedAt := time.Now()
		process, restartErr := client.Restart(ctx, request)
		if restartErr != nil {
			result := restartOutputResult{Name: name, Outcome: "error", Message: restartErr.Error(), Source: definition.Source, Argv: append([]string{}, definition.Argv...)}
			results = append(results, result)
			if cmd.Bool("json") {
				if encodeErr := encodeJSON(writer, result); encodeErr != nil {
					return encodeErr
				}
			} else if writeErr := renderRestartOutputHuman(writer, result); writeErr != nil {
				return writeErr
			}
			break
		}
		if process.Name == "" {
			process.Name = name
		}
		if definition.Ready == nil && process.Readiness != nil && (process.Readiness.State == app.ReadinessStarting || process.Readiness.State == app.ReadinessReady) {
			definition.Ready = &project.ReadyDefinition{Match: process.Readiness.Match}
		}

		var result restartOutputResult
		if cmd.Bool("no-wait") || definition.Ready == nil {
			outcome := "restarted"
			if definition.Ready == nil {
				outcome = app.ReadinessRunningUnverified
			}
			result = restartOutputFromProcess(process, definition, outcome, "")
		} else {
			timeout, timeoutErr := manifestTimeoutForResult(cmd, definition, timeoutOverride)
			if timeoutErr != nil {
				result = restartOutputFromProcess(process, definition, "error", timeoutErr.Error())
				results = append(results, result)
				if cmd.Bool("json") {
					if encodeErr := encodeJSON(writer, result); encodeErr != nil {
						return encodeErr
					}
				} else if writeErr := renderRestartOutputHuman(writer, result); writeErr != nil {
					return writeErr
				}
				break
			}
			timeout = manifestRemainingTimeout(timeout, func() time.Time {
				if process.Start.IsZero() {
					return launchedAt
				}
				return process.Start
			}())
			waited, waitErr := cliReadinessResult(client, ctx, cwd, definition, process, "restarted", timeout)
			if waitErr != nil {
				result = restartOutputFromProcess(process, definition, "error", waitErr.Error())
				results = append(results, result)
				if cmd.Bool("json") {
					if encodeErr := encodeJSON(writer, result); encodeErr != nil {
						return encodeErr
					}
				} else if writeErr := renderRestartOutputHuman(writer, result); writeErr != nil {
					return writeErr
				}
				break
			}
			result = restartOutputFromManifest(waited, process, restartFailureMessage(waited.Outcome, process))
		}
		results = append(results, result)
		if cmd.Bool("json") {
			if encodeErr := encodeJSON(writer, result); encodeErr != nil {
				return encodeErr
			}
		} else if writeErr := renderRestartOutputHuman(writer, result); writeErr != nil {
			return writeErr
		}
	}
	return aggregateRestartExit(results)
}

func shutdownCommand(ctx context.Context, cmd *urfavecli.Command, version, buildTime string, writer io.Writer) error {
	if err := rejectProjectOverride(cmd, "shutdown"); err != nil {
		return err
	}
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
		if isActiveProcesses(shutdownErr) {
			if cmd.Bool("json") {
				return shutdownErr
			}
			return newUserFacingError(activeProcessesShutdownMessage(activeProcessNames(shutdownErr)))
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
	selection, err := selectedProjectDirectory(cmd)
	if err != nil {
		return err
	}
	cwd := selection.cwd
	// A genuinely empty hum.yaml stays inert (HUM-033). No hum.yaml and no
	// discovered convention is an error when there is nothing to report: the
	// error is deferred so an existing daemon can still surface removed manifest
	// sessions, and it replaces the empty-manifest message otherwise.
	manifest, err := loadManifest(cwd)
	var noCandidateErr error
	if err != nil {
		var noCandidate *project.NoCandidateError
		if !errors.As(err, &noCandidate) {
			return err
		}
		noCandidateErr = err
		manifest, err = loadManifestOrEmpty(cwd)
		if err != nil {
			return err
		}
	}
	names := make([]string, 0, len(manifest.defs))
	for _, definition := range manifest.defs {
		names = append(names, definition.Name)
	}
	manifest.selector = selection.selector
	if cmd.Bool("no-wait") && manifestHasAfter(manifest.defs) {
		return errors.New("hum up --no-wait is not allowed when hum.yaml declares after dependencies")
	}
	return manifestLaunchCommandWithStateMode(ctx, cmd, version, buildTime, writer, manifest, names, true, true, errWriter, noCandidateErr)
}

func manifestLaunchCommand(ctx context.Context, cmd *urfavecli.Command, version, buildTime string, writer io.Writer, names []string) error {
	selection, err := selectedProjectDirectory(cmd)
	if err != nil {
		return err
	}
	cwd := selection.cwd
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
			return projectGuidanceError(err, selection.selector)
		}
		manifest, err = loadManifestOrEmpty(cwd)
		if err != nil {
			return err
		}
	}
	manifest.selector = selection.selector
	return manifestLaunchCommandWithState(ctx, cmd, version, buildTime, writer, manifest, names, false)
}

func manifestLaunchCommandWithState(ctx context.Context, cmd *urfavecli.Command, version, buildTime string, writer io.Writer, manifest manifestState, names []string, preserveRecovery bool) error {
	return manifestLaunchCommandWithStateMode(ctx, cmd, version, buildTime, writer, manifest, names, preserveRecovery, false, nil, nil)
}

func manifestLaunchCommandWithStateMode(ctx context.Context, cmd *urfavecli.Command, version, buildTime string, writer io.Writer, manifest manifestState, names []string, preserveRecovery, ordered bool, progressWriter io.Writer, noCandidateErr error) error {
	ctx = nonNilContext(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	selection, err := selectedProjectDirectory(cmd)
	if err != nil {
		return err
	}
	cwd := selection.cwd
	if manifest.selector == "" {
		manifest.selector = selection.selector
	}
	if ordered && cmd.Bool("no-wait") && manifestHasAfter(manifest.defs) {
		return errors.New("hum up --no-wait is not allowed when hum.yaml declares after dependencies")
	}
	timeoutOverride, err := manifestTimeoutOverride(cmd)
	if err != nil {
		return err
	}
	cfg, err := cliConfig(cmd, version, buildTime)
	if err != nil {
		return err
	}
	var client *daemon.Client
	if ordered && len(manifest.defs) == 0 {
		// An empty manifest must remain inert when no daemon exists, but an
		// existing daemon may still retain removed manifest sessions to report.
		client, err = daemonClient(ctx, cfg)
		if err != nil {
			if daemonUnavailable(err) {
				if noCandidateErr != nil {
					return projectGuidanceError(noCandidateErr, manifest.selector)
				}
				if cmd.Bool("json") {
					return nil
				}
				_, printErr := fmt.Fprintln(writer, "No processes are declared in hum.yaml.")
				return printErr
			}
			return err
		}
	} else {
		client, err = runDaemonClient(ctx, cfg)
		if err != nil {
			return err
		}
	}
	defer client.Close()
	env := manifestProcessEnv()
	var results []manifestLaunchResult
	var progress *manifestProgressRenderer
	if ordered && !cmd.Bool("json") && !cmd.Bool("no-wait") && progressWriter != nil {
		progress = newManifestProgressRenderer(progressWriter, len(names), manifest.selector)
	}
	if ordered {
		results, err = manifestUpSchedule(ctx, cmd, client, cwd, manifest, names, env, timeoutOverride, preserveRecovery, progress)
		if err == nil {
			var removed []manifestLaunchResult
			removed, err = removedManifestResults(ctx, client, manifest)
			results = append(results, removed...)
			sort.SliceStable(results, func(i, j int) bool { return results[i].Name < results[j].Name })
		}
	} else {
		results, err = manifestStartConcurrent(ctx, cmd, client, cwd, manifest, names, env, timeoutOverride)
	}
	if err != nil {
		return err
	}
	for index := range results {
		results[index] = manifestResultWithSelector(results[index], manifest.selector)
	}
	if ordered {
		warnings := client.StartupWarnings()
		if len(warnings) != 0 {
			if cmd.Bool("json") {
				if err := encodeJSON(writer, protocol.StreamEvent{Op: protocol.OpEvent, Type: protocol.EventWarning, Warnings: warnings}); err != nil {
					return err
				}
			} else if progressWriter != nil {
				if err := writeStartupWarnings(progressWriter, warnings); err != nil {
					return err
				}
			}
		}
	}
	if ordered && len(results) == 0 && len(manifest.defs) == 0 {
		if noCandidateErr != nil {
			return projectGuidanceError(noCandidateErr, manifest.selector)
		}
		if cmd.Bool("json") {
			return nil
		}
		_, err = fmt.Fprintln(writer, "No processes are declared in hum.yaml.")
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
		if cmd.Bool("no-wait") || (state.result.Outcome != "started" && state.result.Outcome != "already_running") {
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
				result, waitErr := cliReadinessResult(client, ctx, cwd, state.definition, state.process, state.result.Outcome, timeout)
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
			result, waitErr := cliReadinessResult(client, ctx, cwd, state.definition, state.process, state.result.Outcome, timeout)
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
			return cliReadinessResult(client, ctx, cwd, definition, process, outcome, timeout)
		},
		skipped: func(ctx context.Context, definition project.Definition, blocked []string) manifestLaunchResult {
			return manifestLaunchSkipped(ctx, client, manifest.root, definition, blocked)
		},
	}
	return manifestUpScheduleWithOps(ctx, cmd, manifest, names, timeoutOverride, progress, ops)
}

func manifestUpScheduleWithOps(ctx context.Context, cmd *urfavecli.Command, manifest manifestState, names []string, timeoutOverride time.Duration, progress *manifestProgressRenderer, ops manifestUpScheduleOps) ([]manifestLaunchResult, error) {
	definitions := make([]orchestrate.Definition, 0, len(names))
	for _, name := range names {
		if definition, ok := manifest.byName[name]; ok {
			definitions = append(definitions, cliOrchestrateDefinition(definition))
		}
	}
	sharedResults, err := orchestrate.OrchestrateUp(ctx, orchestrate.UpOptions{
		Root: manifest.root, Definitions: definitions, Names: names, NoWait: cmd.Bool("no-wait"),
		TimeoutFor: func(definition orchestrate.Definition) (time.Duration, error) {
			projectDefinition := manifest.byName[definition.Name]
			return manifestTimeoutForResult(cmd, projectDefinition, timeoutOverride)
		},
	}, orchestrate.UpOperations{
		Start: func(ctx context.Context, definition orchestrate.Definition) (orchestrate.StartResult, error) {
			projectDefinition := manifest.byName[definition.Name]
			result, process, observedAt := ops.start(ctx, projectDefinition)
			shared := cliSharedLaunchResult(projectDefinition, result, &process)
			return orchestrate.StartResult{Result: shared, Process: cliOrchestrateProcess(process), ObservedAt: observedAt}, nil
		},
		Readiness: func(ctx context.Context, definition orchestrate.Definition, process orchestrate.Process, outcome string, timeout time.Duration) (orchestrate.Result, error) {
			projectDefinition := manifest.byName[definition.Name]
			appProcess := cliAppProcess(process)
			result, waitErr := ops.readiness(ctx, projectDefinition, appProcess, outcome, timeout)
			return cliSharedLaunchResult(projectDefinition, result, &appProcess), waitErr
		},
		Skipped: func(ctx context.Context, definition orchestrate.Definition, blocked []string) orchestrate.Result {
			projectDefinition := manifest.byName[definition.Name]
			return cliSharedLaunchResult(projectDefinition, ops.skipped(ctx, projectDefinition, blocked), nil)
		},
		OnProgress: func(event orchestrate.ProgressEvent) {
			if progress == nil {
				return
			}
			projectDefinition := manifest.byName[event.Definition.Name]
			result := cliManifestLaunchResult(projectDefinition, event.Result)
			if event.Terminal {
				progress.writeTerminal(result)
			} else {
				progress.writeInitial(projectDefinition, result)
			}
		},
	})
	if progress != nil {
		if closeErr := progress.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}
	if err != nil {
		return nil, err
	}
	results := make([]manifestLaunchResult, 0, len(sharedResults))
	for _, shared := range sharedResults {
		definition := manifest.byName[shared.Name]
		if definition.Name == "" {
			definition = undefinedManifestDefinition(shared.Name)
		}
		results = append(results, cliManifestLaunchResult(definition, shared))
	}
	return results, nil
}

func manifestProgressWaitsForReadiness(definition project.Definition, result manifestLaunchResult) bool {
	return orchestrate.ProgressWaitsForReadiness(cliOrchestrateDefinition(definition), cliSharedLaunchResult(definition, result, nil))
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
	if err := rejectProjectOverride(cmd, "skill"); err != nil {
		return err
	}
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
