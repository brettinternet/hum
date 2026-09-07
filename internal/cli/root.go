package cli

import (
	"bytes"
	"context"
	"encoding/json"
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

const jsonErrorStateMetadataKey = "hum.json_error_state"

type jsonOutputTracker struct {
	writer         io.Writer
	mu             sync.Mutex
	bytesWritten   int
	terminalErrors bool
}

func newJSONOutputTracker(writer io.Writer) *jsonOutputTracker {
	if writer == nil {
		writer = io.Discard
	}
	return &jsonOutputTracker{writer: writer}
}

func (w *jsonOutputTracker) Write(data []byte) (int, error) {
	n, err := w.writer.Write(data)
	if n > 0 {
		w.mu.Lock()
		w.bytesWritten += n
		if !w.terminalErrors && containsJSONTerminalError(data[:n]) {
			w.terminalErrors = true
		}
		w.mu.Unlock()
	}
	return n, err
}

// Close preserves the interruptibility contract required by the MCP stdio
// transport without taking ownership of ordinary in-memory test writers.
func (w *jsonOutputTracker) Close() error {
	closer, ok := w.writer.(io.Closer)
	if !ok {
		return nil
	}
	return closer.Close()
}

func (w *jsonOutputTracker) hasOutput() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.bytesWritten != 0
}

func (w *jsonOutputTracker) hasTerminalError() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.terminalErrors
}

func containsJSONTerminalError(data []byte) bool {
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var value struct {
			Type  string          `json:"type"`
			Error json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal(line, &value); err == nil && value.Type == "error" && len(value.Error) != 0 && string(value.Error) != "null" {
			return true
		}
	}
	return false
}

type jsonErrorState struct {
	root   *urfavecli.Command
	output *jsonOutputTracker

	mu             sync.Mutex
	invocationArgs []string
	command        *urfavecli.Command
	json           bool
	stream         bool
	streamName     string
	handled        bool
}

// SetInvocationArgs supplies the raw invocation to the error boundary. The
// urfave parser intentionally discards tokens after a positional payload, so
// the raw form is needed to distinguish a real --json flag from payload text.
// The binary calls this before Run; direct command users still get a parser
// state fallback when they invoke NewRootCommand(...).Run directly.
func SetInvocationArgs(root *urfavecli.Command, args []string) {
	if root == nil || root.Metadata == nil {
		return
	}
	state, ok := root.Metadata[jsonErrorStateMetadataKey].(*jsonErrorState)
	if !ok || state == nil {
		return
	}
	state.mu.Lock()
	state.invocationArgs = append([]string(nil), args...)
	state.mu.Unlock()
}

func installJSONErrorBoundary(root *urfavecli.Command, state *jsonErrorState) {
	if root == nil || state == nil {
		return
	}
	var visit func(*urfavecli.Command)
	visit = func(command *urfavecli.Command) {
		if command == nil {
			return
		}
		if action := command.Action; action != nil {
			command.Action = func(ctx context.Context, cmd *urfavecli.Command) error {
				err := action(ctx, cmd)
				return state.handle(cmd, err)
			}
		}
		if usage := command.OnUsageError; usage != nil {
			command.OnUsageError = func(ctx context.Context, cmd *urfavecli.Command, err error, isSubcommand bool) error {
				usageErr := usage(ctx, cmd, err, isSubcommand)
				return state.handle(cmd, usageErr)
			}
		}
		if before := command.Before; before != nil {
			command.Before = func(ctx context.Context, cmd *urfavecli.Command) (context.Context, error) {
				beforeCtx, err := before(ctx, cmd)
				return beforeCtx, state.handle(cmd, err)
			}
		}
		if after := command.After; after != nil {
			command.After = func(ctx context.Context, cmd *urfavecli.Command) error {
				return state.handle(cmd, after(ctx, cmd))
			}
		}
		for _, child := range command.Commands {
			visit(child)
		}
	}
	visit(root)
}

func (s *jsonErrorState) handle(cmd *urfavecli.Command, err error) error {
	if err == nil || JSONErrorHandled(err) {
		return err
	}
	// start/up and wait use an empty ExitCoder after they have emitted their
	// response records. Those records already carry the outcome and changing
	// them into a second error object would alter the established JSON shape.
	var exitErr urfavecli.ExitCoder
	if errors.As(err, &exitErr) && err.Error() == "" {
		return err
	}
	if cmd != nil {
		s.noteCommand(cmd)
	}

	s.mu.Lock()
	jsonMode := s.json
	stream := s.stream
	streamName := s.streamName
	if s.handled {
		s.mu.Unlock()
		return &jsonHandledError{cause: err}
	}
	if jsonMode {
		s.handled = true
	}
	s.mu.Unlock()
	if !jsonMode {
		return err
	}

	wire := classifyJSONError(err, isCLIUsageError(err))
	if !s.output.hasOutput() {
		_ = writeJSONError(s.output, wire)
	} else if stream && !s.output.hasTerminalError() {
		_ = writeJSONErrorEvent(s.output, streamName, wire)
	}
	return &jsonHandledError{cause: err}
}

func (s *jsonErrorState) noteCommand(cmd *urfavecli.Command) {
	if cmd == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.command = cmd
	s.json = commandJSONRequested(cmd, s.root, s.invocationArgs) && (cmd.Name != "run" || cmd.Bool("detach"))
	s.stream = s.json && (cmd.Name == "start" || cmd.Name == "up" || cmd.Name == "logs" && cmd.Bool("follow"))
	s.streamName = ""
	if cmd.Name == "logs" && cmd.Args() != nil {
		args := cmd.Args().Slice()
		if len(args) != 0 {
			s.streamName = args[0]
		}
	}
}

func isCLIUsageError(err error) bool {
	var usageErr cliUsageError
	return errors.As(err, &usageErr)
}

func commandSupportsJSON(command *urfavecli.Command) (bool, bool) {
	if command == nil {
		return false, false
	}
	supports, alias := false, false
	for _, flag := range cliCommandFlags(command) {
		if flag == nil {
			continue
		}
		for _, name := range flag.Names() {
			switch name {
			case "json":
				supports = true
			case "j":
				alias = true
			}
		}
	}
	return supports, alias
}

func commandJSONRequested(command, root *urfavecli.Command, args []string) bool {
	supports, alias := commandSupportsJSON(command)
	if !supports {
		return false
	}
	if len(args) == 0 {
		return command.Bool("json")
	}
	if command.Name == "run" {
		return rawRunJSONRequested(args, root, command, alias)
	}
	return rawJSONRequested(args, root, command, alias)
}

func flagTakesValue(flag urfavecli.Flag) bool {
	docFlag, ok := flag.(interface{ TakesValue() bool })
	return ok && docFlag.TakesValue()
}

func invocationFlagValues(root, command *urfavecli.Command) map[string]bool {
	values := make(map[string]bool)
	add := func(flags []urfavecli.Flag) {
		for _, flag := range flags {
			if flag == nil {
				continue
			}
			takesValue := flagTakesValue(flag)
			for _, name := range flag.Names() {
				values[name] = takesValue
			}
		}
	}
	if root != nil {
		add(root.Flags)
	}
	if command != nil {
		add(command.Flags)
	}
	return values
}

func rawJSONRequested(args []string, root, command *urfavecli.Command, alias bool) bool {
	values := invocationFlagValues(root, command)
	for index := 0; index < len(args); index++ {
		token := args[index]
		if token == "--" {
			return false
		}
		if token == "--json" || alias && token == "-j" {
			return true
		}
		if !strings.HasPrefix(token, "-") || token == "-" {
			continue
		}
		name, _, hasValue := strings.Cut(strings.TrimLeft(token, "-"), "=")
		if !hasValue && values[name] && index+1 < len(args) {
			index++
		}
	}
	return false
}

func rawRunJSONRequested(args []string, root, command *urfavecli.Command, alias bool) bool {
	values := invocationFlagValues(root, command)
	runIndex := -1
	for index := 1; index < len(args); index++ {
		token := args[index]
		if token == "run" {
			runIndex = index
			break
		}
		if strings.HasPrefix(token, "-") && token != "-" {
			name, _, hasValue := strings.Cut(strings.TrimLeft(token, "-"), "=")
			if !hasValue && values[name] && index+1 < len(args) {
				index++
			}
		}
	}
	if runIndex < 0 {
		return command.Bool("json")
	}
	requested := false
	seenName := false
	for index := runIndex + 1; index < len(args); index++ {
		token := args[index]
		if token == "--" {
			return requested
		}
		if token == "--json" || alias && token == "-j" {
			requested = true
			continue
		}
		if !seenName {
			if !strings.HasPrefix(token, "-") || token == "-" {
				seenName = true
			}
			if strings.HasPrefix(token, "-") && token != "-" {
				name, _, hasValue := strings.Cut(strings.TrimLeft(token, "-"), "=")
				if !hasValue && values[name] && index+1 < len(args) {
					index++
				}
			}
			continue
		}
		if strings.HasPrefix(token, "-") && token != "-" {
			name, _, hasValue := strings.Cut(strings.TrimLeft(token, "-"), "=")
			if !hasValue && values[name] && index+1 < len(args) {
				index++
			}
			continue
		}
		// Once the first non-flag argument after NAME appears, run treats the
		// remainder as a raw command payload unless a separator was used.
		return requested
	}
	return requested
}

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
	outputTracker := newJSONOutputTracker(writer)
	state := &jsonErrorState{output: outputTracker}
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
		Writer:                          outputTracker,
		ErrWriter:                       errWriter,
		Metadata:                        map[string]any{jsonErrorStateMetadataKey: state},
		ExitErrHandler:                  func(context.Context, *urfavecli.Command, error) {},
		Flags: []urfavecli.Flag{
			&urfavecli.StringFlag{Name: "project", Aliases: []string{"C"}, Usage: "project directory; omit for the current directory; ad-hoc run uses it as cwd, manifest cwd stays project-relative"},
			&urfavecli.StringFlag{Name: "runtime-dir", Usage: "runtime directory for the hum daemon [$HUM_RUNTIME_DIR, then $XDG_RUNTIME_DIR/hum]", DefaultText: "$TMPDIR/hum-UID"},
			&urfavecli.StringFlag{Name: "stop-grace", Usage: "grace period between SIGTERM and SIGKILL when stopping a process [$HUM_STOP_GRACE]", DefaultText: config.DefaultStopGrace.String()},
			&urfavecli.StringFlag{Name: "output-bytes", Usage: "charged retained output bytes per process (text + 128 bytes per entry; read byte limits count text only), at least " + strconv.FormatInt(config.MinOutputBytes, 10) + " [$HUM_OUTPUT_BYTES]", DefaultText: strconv.FormatInt(config.DefaultOutputBytes, 10)},
			&urfavecli.StringFlag{Name: "completed-records", Usage: "completed process records to retain [$HUM_COMPLETED_RECORDS]", DefaultText: strconv.Itoa(config.DefaultCompletedRecords)},
		},
		Commands:     newCLICommands(version, buildTime, outputTracker, errWriter),
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
	state.root = root
	installJSONErrorBoundary(root, state)
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
	return newCLIUsageError(newUserFacingError(fmt.Sprintf("%s: %s", cmd.FullName(), err.Error())))
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
	return newCLIUsageError(newUserFacingError(message + " Run hum --help to list commands."))
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
