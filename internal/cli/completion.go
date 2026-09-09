package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"sort"
	"strings"
	"syscall"
	"unicode/utf8"

	"hum/internal/daemon"
	"hum/internal/project"

	urfavecli "github.com/urfave/cli/v3"
)

const (
	completionDescription     = "Output an installable shell completion script for bash, zsh, or fish. Completion is opt-in and does not start a daemon.\n\nExamples:\n  hum completion bash\n  hum completion zsh\n  hum completion fish"
	completionNamePositionEnv = "HUM_COMPLETION_NAME_POSITION"
)

// configureCompletionCommand makes urfave/cli's generated completion command
// part of hum's public command surface. PowerShell is intentionally omitted;
// it is outside hum's supported shell-completion scope.
func configureCompletionCommand(command *urfavecli.Command) {
	if command == nil {
		return
	}
	command.Hidden = false
	command.Usage = "Output shell completion script for bash, zsh, or fish"
	command.Description = completionDescription

	appName := command.Root().Name
	children := make([]*urfavecli.Command, 0, len(command.Commands))
	for _, child := range command.Commands {
		if child == nil || child.Name == "pwsh" {
			continue
		}
		child.Hidden = false
		child.Usage = fmt.Sprintf("Output %s completion script", child.Name)
		child.Description = fmt.Sprintf("Output the %s shell completion script.\n\nExamples:\n  %s completion %s", child.Name, appName, child.Name)

		// urfave intentionally omits a non-dash word at the cursor from the
		// completion request. Mark that branch so it remains distinguishable
		// from a completed --flag=value token immediately before NAME.
		action := child.Action
		shell := child.Name
		child.Action = func(ctx context.Context, childCommand *urfavecli.Command) error {
			root := childCommand.Root()
			writer := root.Writer
			var rendered bytes.Buffer
			root.Writer = &rendered
			err := action(ctx, childCommand)
			root.Writer = writer
			if err != nil {
				return err
			}
			script := markCompletionNamePosition(shell, rendered.String())
			_, err = fmt.Fprint(writer, script)
			return err
		}
		children = append(children, child)
	}
	command.Commands = children
}

func markCompletionNamePosition(shell, script string) string {
	var request, marked string
	switch shell {
	case "bash":
		request = `printf '%s --generate-shell-completion' "${words_before_cursor[*]}"`
		marked = `printf '` + completionNamePositionEnv + `=1 %s --generate-shell-completion' "${words_before_cursor[*]}"`
	case "zsh":
		request = `${words[@]:0:#words[@]-1} --generate-shell-completion`
		marked = completionNamePositionEnv + `=1 ${words[@]:0:#words[@]-1} --generate-shell-completion`
	case "fish":
		request = `$args[1] $args[2..-1] --generate-shell-completion`
		marked = `env ` + completionNamePositionEnv + `=1 $args[1] $args[2..-1] --generate-shell-completion`
	default:
		return script
	}
	return strings.Replace(script, request, marked, 1)
}

// completeProcessNames preserves urfave/cli's command and flag completion and
// replaces only NAME positions with project-scoped process candidates.
func completeProcessNames(ctx context.Context, command *urfavecli.Command) {
	if command == nil || command.Root() == nil || command.Root().Writer == nil {
		return
	}

	tokens := completionCommandTokens(command)
	if completionFlagValuePosition(command, tokens) {
		return
	}
	if os.Getenv(completionNamePositionEnv) != "1" {
		finalToken := completionFinalToken(tokens)
		if strings.HasPrefix(finalToken, "-") && !completionCompleteFlag(command, finalToken) {
			completeVisibleFlags(command, finalToken)
			return
		}
	}
	if completionNamePosition(command) {
		for _, name := range completionProcessNames(ctx, command) {
			_, _ = fmt.Fprintln(command.Root().Writer, name)
		}
		return
	}

	if len(command.Path()) == 1 {
		completeVisibleCommands(command)
	}
}

func completionNamePosition(command *urfavecli.Command) bool {
	args := command.Args()
	if args == nil {
		return false
	}
	values := args.Slice()
	if completionFlagPositionValues(values) {
		return false
	}

	switch command.Name {
	case "logs", "start", "restart", "stop", "remove":
		return true
	case "run", "status", "wait", "input", "attach", "signal":
		return len(values) == 0
	default:
		return false
	}
}

func completionFlagPositionValues(values []string) bool {
	return len(values) > 0 && strings.HasPrefix(values[len(values)-1], "-")
}

// completionCommandTokens returns the raw tokens supplied after the command.
// urfave/cli parses recognized flags out of Command.Args, so the root's raw
// argument slice is needed to identify a partial final flag such as --j.
func completionCommandTokens(command *urfavecli.Command) []string {
	if command == nil {
		return nil
	}
	if root := command.Root(); root != nil && root.Args() != nil {
		args := root.Args().Slice()
		path := command.Path()
		if len(path) > 1 && len(args) >= len(path)-1 {
			matches := true
			for i, part := range path[1:] {
				if args[i] != part {
					matches = false
					break
				}
			}
			if matches {
				return args[len(path)-1:]
			}
		}
		if len(path) == 1 {
			return args
		}
	}
	if command.Args() == nil {
		return nil
	}
	return command.Args().Slice()
}

func completionFinalToken(tokens []string) string {
	if len(tokens) == 0 {
		return ""
	}
	return tokens[len(tokens)-1]
}

func completionVisibleFlags(command *urfavecli.Command) []urfavecli.Flag {
	if command == nil {
		return nil
	}
	flags := append([]urfavecli.Flag(nil), command.VisibleFlags()...)
	flags = append(flags, command.VisiblePersistentFlags()...)

	seen := make(map[string]struct{}, len(flags))
	visible := make([]urfavecli.Flag, 0, len(flags))
	for _, flag := range flags {
		if flag == nil || len(flag.Names()) == 0 {
			continue
		}
		key := strings.Join(flag.Names(), "\x00")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		visible = append(visible, flag)
	}
	return visible
}

func completionFlagForToken(command *urfavecli.Command, token string) (urfavecli.Flag, bool, bool) {
	option, _, hasValue := strings.Cut(token, "=")
	option = strings.TrimLeft(option, "-")
	if option == "" {
		return nil, false, hasValue
	}
	for _, flag := range completionVisibleFlags(command) {
		for _, name := range flag.Names() {
			if name == option {
				return flag, true, hasValue
			}
		}
	}
	return nil, false, hasValue
}

func completionFlagTakesValue(flag urfavecli.Flag) bool {
	docFlag, ok := flag.(urfavecli.DocGenerationFlag)
	return ok && docFlag.TakesValue()
}

func completionFlagValuePosition(command *urfavecli.Command, tokens []string) bool {
	if len(tokens) == 0 {
		return false
	}

	expectingValue := false
	for index, token := range tokens {
		if expectingValue {
			if index == len(tokens)-1 {
				// A dash-leading final token may be the value currently being
				// completed (for example, --text -payload). A non-dash value
				// is complete, so the cursor has advanced to the NAME position.
				return strings.HasPrefix(token, "-") && os.Getenv(completionNamePositionEnv) != "1"
			}
			expectingValue = false
			continue
		}
		if token == "--" {
			return false
		}
		flag, known, hasValue := completionFlagForToken(command, token)
		if known && !hasValue && completionFlagTakesValue(flag) {
			expectingValue = true
		}
	}

	finalFlag, known, hasValue := completionFlagForToken(command, completionFinalToken(tokens))
	return known && !hasValue && completionFlagTakesValue(finalFlag)
}

func completionCompleteFlag(command *urfavecli.Command, token string) bool {
	flag, known, hasValue := completionFlagForToken(command, token)
	if !known || hasValue || len(flag.Names()) == 0 {
		return false
	}
	name := strings.TrimLeft(token, "-")
	if strings.HasPrefix(token, "--") {
		return name == flag.Names()[0]
	}
	for _, flagName := range flag.Names() {
		if name == flagName {
			return true
		}
	}
	return false
}

func completeVisibleCommands(command *urfavecli.Command) {
	if command == nil || command.Root() == nil || command.Root().Writer == nil {
		return
	}
	for _, child := range command.Commands {
		if child == nil || child.Hidden {
			continue
		}
		if child.Usage == "" {
			_, _ = fmt.Fprintln(command.Root().Writer, child.Name)
			continue
		}
		_, _ = fmt.Fprintf(command.Root().Writer, "%s:%s\n", child.Name, child.Usage)
	}
}

func completeVisibleFlags(command *urfavecli.Command, token string) {
	partial := strings.TrimLeft(token, "-")
	for _, flag := range completionVisibleFlags(command) {
		names := flag.Names()
		if len(names) == 0 {
			continue
		}
		name := strings.TrimSpace(names[0])
		count := utf8.RuneCountInString(name)
		if count > 2 {
			count = 2
		}
		if strings.HasPrefix(token, "--") && count == 1 {
			continue
		}
		if !strings.HasPrefix(name, partial) || name == partial {
			continue
		}

		completion := fmt.Sprintf("%s%s", strings.Repeat("-", count), name)
		if docFlag, ok := flag.(urfavecli.DocGenerationFlag); ok && docFlag.GetUsage() != "" {
			completion += ":" + docFlag.GetUsage()
		}
		_, _ = fmt.Fprintln(command.Root().Writer, completion)
	}
}

// completionProcessNames resolves the same declaration/runtime name union as
// project-scoped list, without using a daemon-starting client. Any manifest,
// configuration, daemon, or list error is deliberately converted to no
// candidates so shell completion remains quiet.
func completionProcessNames(ctx context.Context, command *urfavecli.Command) []string {
	if command == nil {
		return nil
	}
	ctx = nonNilContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil
	}

	selection, err := selectedProjectDirectory(command)
	if err != nil {
		return nil
	}
	manifest := manifestState{byName: make(map[string]project.Definition)}
	if selection.scope != "global" {
		manifest, err = loadManifestOrEmpty(selection.cwd)
		if err != nil {
			return nil
		}
	}

	cfg, err := cliConfig(command, "", "")
	if err != nil {
		return nil
	}
	completionCtx, cancel := context.WithTimeout(ctx, daemonDialTimeout)
	defer cancel()
	client, err := daemonClient(completionCtx, cfg)
	if err != nil {
		if client != nil {
			_ = client.Close()
		}
		if completionDaemonUnavailable(err) {
			return completionManifestNames(manifest)
		}
		return nil
	}
	defer client.Close()

	processes, err := client.List(completionCtx, daemon.ListRequest{Scope: selection.scope, Cwd: selection.cwd, IncludeCompleted: true})
	if err != nil {
		return nil
	}

	names := make(map[string]struct{}, len(manifest.defs)+len(processes))
	for _, definition := range manifest.defs {
		if definition.Name != "" {
			names[definition.Name] = struct{}{}
		}
	}
	for _, process := range processes {
		if (selection.scope == "global" && process.Scope == "global" || selection.scope != "global" && process.Root == manifest.root) && process.Name != "" {
			names[process.Name] = struct{}{}
		}
	}
	return sortedCompletionNames(names)
}

// completionDaemonUnavailable recognizes only errors that prove the daemon is
// absent. Completion must not treat malformed or otherwise invalid socket paths
// as an absent daemon, because declaration fallback would hide that failure.
func completionDaemonUnavailable(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, net.ErrClosed)
}

func completionManifestNames(manifest manifestState) []string {
	names := make(map[string]struct{}, len(manifest.defs))
	for _, definition := range manifest.defs {
		if definition.Name != "" {
			names[definition.Name] = struct{}{}
		}
	}
	return sortedCompletionNames(names)
}

func sortedCompletionNames(names map[string]struct{}) []string {
	result := make([]string, 0, len(names))
	for name := range names {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}
