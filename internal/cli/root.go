package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"hum/internal/config"

	urfavecli "github.com/urfave/cli/v3"
)

var configureFrameworkFlagsOnce sync.Once

// NewRootCommand builds the hum command with the supplied build metadata
// and output writers.
func NewRootCommand(version, buildTime string, writer, errWriter io.Writer) *urfavecli.Command {
	configureFrameworkFlagsOnce.Do(func() {
		for _, flag := range []urfavecli.Flag{urfavecli.HelpFlag, urfavecli.VersionFlag} {
			if boolFlag, ok := flag.(*urfavecli.BoolFlag); ok {
				boolFlag.DefaultText = "false"
				boolFlag.HideDefault = false
			}
		}
	})
	root := &urfavecli.Command{
		Name:      "hum",
		Usage:     "A local development process supervisor",
		UsageText: "hum [global options] [command [command options]]",
		Description: "Manifest projects use hum start NAME; hum run starts a detached daemon and stays attached by default; hum serve --daemon runs detached. " +
			"Bounded controls, including logs without --follow, do not start an empty daemon; logs --follow and wait start one to observe future launches; stopping processes and daemon shutdown are separate. " +
			"restart: on-failure retries spawn failures at 1s, 2s, 4s, 8s, and 16s five times; a 30-second survivor resets recovery, so inspect retained failing output.\n\n" +
			"Examples:\n" +
			"  hum up",
		Version:                         version + " (built " + buildTime + ")",
		ShellComplete:                   completeProcessNames,
		EnableShellCompletion:           true,
		ConfigureShellCompletionCommand: configureCompletionCommand,
		Writer:                          writer,
		ErrWriter:                       errWriter,
		Flags: []urfavecli.Flag{
			&urfavecli.StringFlag{Name: "project", Aliases: []string{"C"}, Usage: "project directory; omit for the current directory; ad-hoc run uses it as cwd, manifest cwd stays project-relative"},
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

type projectSelection struct {
	cwd      string
	selector string
}

// selectedProjectDirectory resolves the optional project override against the
// invocation directory and validates it before any daemon operation. Without
// an override it returns the caller's existing working directory unchanged.
func selectedProjectDirectory(cmd *urfavecli.Command) (projectSelection, error) {
	invocationCwd, err := os.Getwd()
	if err != nil {
		return projectSelection{}, fmt.Errorf("current directory: %w", err)
	}
	if !cmd.IsSet("project") {
		return projectSelection{cwd: invocationCwd}, nil
	}

	value := cmd.String("project")
	if value == "" {
		return projectSelection{}, errors.New("--project requires a non-empty directory")
	}
	selected := value
	if !filepath.IsAbs(selected) {
		selected = filepath.Join(invocationCwd, selected)
	}
	selected = filepath.Clean(selected)
	info, err := os.Stat(selected)
	if err != nil {
		return projectSelection{}, fmt.Errorf("--project directory %q: %w", selected, err)
	}
	if !info.IsDir() {
		return projectSelection{}, fmt.Errorf("--project path %q is not a directory", selected)
	}
	return projectSelection{cwd: selected, selector: "--project " + shellEscape(selected)}, nil
}

func rejectProjectOverride(cmd *urfavecli.Command, commandName string) error {
	if !cmd.IsSet("project") {
		return nil
	}
	return fmt.Errorf("hum %s does not accept --project/-C", commandName)
}

// projectCommand returns the canonical follow-up command. An explicit
// override is rendered as an absolute, shell-safe --project selector.
func projectCommand(selector, command string) string {
	if selector == "" {
		return "hum " + command
	}
	return "hum " + selector + " " + command
}

func logsUnavailableMessageFor(selector string) string {
	if selector == "" {
		return logsUnavailableMessage
	}
	return fmt.Sprintf("Nothing is running. Start a process with %s.", projectCommand(selector, "run <name> -- <command>"))
}

func projectGuidanceError(err error, selector string) error {
	if err == nil || selector == "" {
		return err
	}
	message := strings.ReplaceAll(err.Error(), "hum init", projectCommand(selector, "init"))
	return wrapUserFacingError(err, message)
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
