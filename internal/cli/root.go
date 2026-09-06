package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"hum/internal/config"

	urfavecli "github.com/urfave/cli/v3"
)

// NewRootCommand builds the hum command with the supplied build metadata
// and output writers.
func NewRootCommand(version, buildTime string, writer, errWriter io.Writer) *urfavecli.Command {
	root := &urfavecli.Command{
		Name:  "hum",
		Usage: "A local development process supervisor",
		Description: "The ordinary workflow is hum run NAME -- COMMAND [ARGS...]. run automatically starts a detached daemon when needed and stays attached by default; add --detach to return immediately. " +
			"Manifest projects use hum start NAME and hum up to ensure declared processes are running, waiting for readiness unless --no-wait is set. " +
			"hum serve runs the daemon in the foreground, while hum serve --daemon runs it detached. " +
			"Bounded reads and controls do not start an empty daemon: list, status, logs without --follow, wait, input, restart, stop, remove, and shutdown inspect or control existing work. " +
			"logs --follow and wait ensure a daemon exists so they can observe a future launch. When nothing is running, bounded status, logs, and restart provide launch guidance. " +
			"Manifest processes may declare `after: [name]` readiness dependencies; hum up launches independent roots concurrently, gates dependents on ready prerequisites, reports lexical skipped results with direct blocked_by names, and rejects --no-wait before daemon contact when after is present. start remains explicitly named, and down remains concurrent. They may also opt into restart: on-failure; it retries crashes at 1s, 2s, 4s, 8s, and 16s, at most five times, while discovered and ad-hoc processes remain never. Spawn failures consume attempts and a 30-second survivor resets the loop; controls cancel pending work. Read retained failing output before editing again. " +
			"Stopping named processes and shutting down the daemon are separate operations.",
		Version:   version + " (built " + buildTime + ")",
		Writer:    writer,
		ErrWriter: errWriter,
		Flags: []urfavecli.Flag{
			&urfavecli.StringFlag{Name: "runtime-dir", Usage: "runtime directory for the hum daemon [$HUM_RUNTIME_DIR, then $XDG_RUNTIME_DIR/hum]", DefaultText: "$TMPDIR/hum-UID"},
			&urfavecli.StringFlag{Name: "stop-grace", Usage: "grace period between SIGTERM and SIGKILL when stopping a process [$HUM_STOP_GRACE]", DefaultText: config.DefaultStopGrace.String()},
			&urfavecli.StringFlag{Name: "output-bytes", Usage: "retained output bytes per process, at least " + strconv.FormatInt(config.MinOutputBytes, 10) + " [$HUM_OUTPUT_BYTES]", DefaultText: strconv.FormatInt(config.DefaultOutputBytes, 10)},
			&urfavecli.StringFlag{Name: "completed-records", Usage: "completed process records to retain [$HUM_COMPLETED_RECORDS]", DefaultText: strconv.Itoa(config.DefaultCompletedRecords)},
		},
		Commands:     newCLICommands(version, buildTime, writer, errWriter),
		OnUsageError: onUsageError,
		Action: func(ctx context.Context, cmd *urfavecli.Command) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if cmd.Args().Len() > 0 {
				return unknownCommandError(cmd, cmd.Args().First())
			}
			return urfavecli.ShowRootCommandHelp(cmd)
		},
	}
	if err := validateCLICommandTree(root); err != nil {
		panic(err)
	}
	return root
}

// onUsageError formats a flag-parsing usage error as a single line naming
// the full command path, instead of urfave/cli's default double-printed
// "Incorrect Usage" message plus a full help dump.
func onUsageError(_ context.Context, cmd *urfavecli.Command, err error, _ bool) error {
	return newUserFacingError(fmt.Sprintf("%s: %s", cmd.FullName(), err.Error()))
}

func validateCLICommandTree(root *urfavecli.Command) error {
	if root == nil {
		return nil
	}
	return validateCLICommand(root, nil, true, true, nil)
}

func validateCLICommand(cmd *urfavecli.Command, inherited []urfavecli.Flag, helpVisible, isRoot bool, path []string) error {
	path = append(path, cmd.Name)
	flags := append([]urfavecli.Flag(nil), inherited...)
	ownFlags := cliCommandFlags(cmd)
	if helpVisible && !cmd.HideHelp && urfavecli.HelpFlag != nil {
		ownFlags = append(ownFlags, urfavecli.HelpFlag)
	}
	if isRoot && !cmd.HideVersion && cmd.Version != "" && urfavecli.VersionFlag != nil {
		ownFlags = append(ownFlags, urfavecli.VersionFlag)
	}
	flags = append(flags, ownFlags...)
	if err := validateCLIFlags(strings.Join(path, " "), flags); err != nil {
		return err
	}

	nextInherited := append([]urfavecli.Flag(nil), inherited...)
	for _, flag := range ownFlags {
		if cliFlagIsPersistent(flag) {
			nextInherited = append(nextInherited, flag)
		}
	}
	nextHelpVisible := helpVisible && !cmd.HideHelp
	for _, child := range cmd.Commands {
		if err := validateCLICommand(child, nextInherited, nextHelpVisible, false, path); err != nil {
			return err
		}
	}
	return nil
}

func validateCLIFlags(commandName string, flags []urfavecli.Flag) error {
	seen := make(map[string]urfavecli.Flag)
	for _, flag := range flags {
		if flag == nil {
			return fmt.Errorf("nil flag in command %q", commandName)
		}
		for _, name := range flag.Names() {
			if previous, ok := seen[name]; ok {
				return fmt.Errorf("flag name %q is used by both %q and %q in command %q", name, previous.Names()[0], flag.Names()[0], commandName)
			}
			seen[name] = flag
		}
	}
	return nil
}

func cliFlagIsPersistent(flag urfavecli.Flag) bool {
	localFlag, ok := flag.(urfavecli.LocalFlag)
	return !ok || !localFlag.IsLocal()
}

func cliCommandFlags(cmd *urfavecli.Command) []urfavecli.Flag {
	flags := append([]urfavecli.Flag(nil), cmd.Flags...)
	for _, group := range cmd.MutuallyExclusiveFlags {
		for _, option := range group.Flags {
			flags = append(flags, option...)
		}
	}
	return flags
}

// unknownCommandError reports an unrecognized command name. urfave/cli passes
// unmatched names to the root action as positional arguments, so without this
// check a typo would print help and exit 0.
func unknownCommandError(root *urfavecli.Command, name string) error {
	message := fmt.Sprintf("Unknown command %q.", name)
	if suggestion := suggestCommandName(root.Commands, name); suggestion != "" {
		message += fmt.Sprintf(" Did you mean %q?", suggestion)
	}
	return newUserFacingError(message + " Run hum --help to list commands.")
}

// suggestCommandName returns the closest command name to the typed name, or
// an empty string when nothing is close enough to be a plausible typo.
func suggestCommandName(commands []*urfavecli.Command, typed string) string {
	typed = strings.ToLower(typed)
	if typed == "" {
		return ""
	}
	limit := 1
	if len(typed) >= 4 {
		limit = 2
	}
	best, bestDistance := "", limit+1
	prefixMatches := 0
	prefixMatch := ""
	for _, command := range commands {
		if command == nil || command.Hidden {
			continue
		}
		for _, candidate := range command.Names() {
			if strings.HasPrefix(candidate, typed) {
				prefixMatches++
				prefixMatch = candidate
			}
			if distance := editDistance(typed, candidate); distance < bestDistance {
				best, bestDistance = candidate, distance
			}
		}
	}
	if prefixMatches == 1 {
		return prefixMatch
	}
	return best
}

func editDistance(a, b string) int {
	previous := make([]int, len(b)+1)
	current := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(a); i++ {
		current[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			current[j] = min(previous[j]+1, current[j-1]+1, previous[j-1]+cost)
		}
		previous, current = current, previous
	}
	return previous[len(b)]
}
