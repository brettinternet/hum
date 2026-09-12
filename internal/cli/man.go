package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	urfavecli "github.com/urfave/cli/v3"
)

// WriteManPage renders the public CLI command tree as a section 1 manual page.
func WriteManPage(writer io.Writer, root *urfavecli.Command, date string) error {
	if writer == nil {
		return fmt.Errorf("man page writer is required")
	}
	if root == nil {
		return fmt.Errorf("man page command is required")
	}
	if date == "" {
		return fmt.Errorf("man page date is required")
	}
	if err := prepareManCommand(root); err != nil {
		return fmt.Errorf("prepare command tree: %w", err)
	}

	var page bytes.Buffer
	fmt.Fprintf(&page, ".TH %s 1 \"%s\" \"%s\" \"User Commands\"\n", strings.ToUpper(root.Name), roffMacroArg(date), roffMacroArg(root.Name+" "+root.Version))
	writeManSection(&page, "NAME")
	fmt.Fprintf(&page, "%s \\- %s\n", roffText(root.Name), roffText(root.Usage))
	writeManSection(&page, "SYNOPSIS")
	writeManLiteral(&page, commandUsage(root))
	writeManSection(&page, "DESCRIPTION")
	writeManParagraphs(&page, "Supervise local development processes. Use hum up to start processes in hum.yaml. Use hum run for ad-hoc work. Inspect processes with hum status and hum logs. Select another project with --project or an exact complete variant with --file, conventionally hum.dev.yaml. Use --global for machine-wide ad-hoc processes.")
	writeManQuickStart(&page)
	writeManConfiguration(&page)

	if flags := manCommandFlags(root); len(flags) > 0 {
		writeManSection(&page, "GLOBAL OPTIONS")
		writeManFlags(&page, flags)
	}

	commands := visibleManCommands(root)
	if len(commands) > 0 {
		writeManSection(&page, "COMMANDS")
		for _, command := range root.VisibleCommands() {
			fmt.Fprintln(&page, ".TP")
			fmt.Fprintf(&page, ".B %s\n", roffText(command.Name))
			writeManText(&page, command.Usage)
		}
		if !root.HideHelp && !root.HideHelpCommand {
			writeManDefinition(&page, "help, h", "Show command help.")
		}
		writeManSection(&page, "COMMAND REFERENCE")
		for _, command := range commands {
			writeManCommand(&page, command)
		}
		if !root.HideHelp && !root.HideHelpCommand {
			writeManHelpCommand(&page, root.Name)
		}
	}

	writeManSection(&page, "FILES")
	fmt.Fprintln(&page, ".TP")
	fmt.Fprintln(&page, ".B hum.yaml")
	writeManText(&page, "Optional project manifest. Hum searches from the selected directory to the nearest Git root. Use --file PATH or -F PATH to select one complete manifest inside the project; without it, hum.yaml is authoritative and discovery runs only when hum.yaml is absent.")
	writeManSection(&page, "ENVIRONMENT")
	writeManDefinition(&page, "HUM_RUNTIME_DIR", "Override the daemon runtime directory.")
	writeManDefinition(&page, "XDG_RUNTIME_DIR", "Base runtime directory when HUM_RUNTIME_DIR is unset.")
	writeManDefinition(&page, "TMPDIR", "Base for the per-user fallback runtime directory.")
	writeManDefinition(&page, "HUM_STOP_GRACE", "Default process stop grace period.")
	writeManDefinition(&page, "HUM_OUTPUT_BYTES", "Retained output bytes per process.")
	writeManDefinition(&page, "HUM_COMPLETED_RECORDS", "Maximum completed process records retained.")
	writeManSection(&page, "SEE ALSO")
	writeManDefinition(&page, "Project guide and examples", "https://github.com/brettinternet/hum")
	writeManDefinition(&page, "Detailed behavior", "https://github.com/brettinternet/hum/blob/main/docs/design.md")
	writeManDefinition(&page, "JSON interface", "https://github.com/brettinternet/hum/blob/main/docs/cli-json-v1.md")

	_, err := io.Copy(writer, &page)
	return err
}

func prepareManCommand(root *urfavecli.Command) error {
	writer, errWriter := root.Writer, root.ErrWriter
	root.Writer, root.ErrWriter = io.Discard, io.Discard
	err := root.Run(context.Background(), []string{root.Name, "--help"})
	root.Writer, root.ErrWriter = writer, errWriter
	return err
}

func visibleManCommands(root *urfavecli.Command) []*urfavecli.Command {
	var commands []*urfavecli.Command
	var visit func(*urfavecli.Command)
	visit = func(parent *urfavecli.Command) {
		for _, command := range parent.VisibleCommands() {
			commands = append(commands, command)
			visit(command)
		}
	}
	visit(root)
	return commands
}

func writeManCommand(page *bytes.Buffer, command *urfavecli.Command) {
	fmt.Fprintf(page, ".SS \"%s\"\n", roffQuote(strings.Join(command.Path(), " ")))
	writeManLiteral(page, commandUsage(command))
	writeManParagraphs(page, manCommandDescription(command))
	if flags := manCommandFlags(command); len(flags) > 0 {
		fmt.Fprintln(page, ".PP\nOptions:")
		writeManFlags(page, flags)
	}
	if examples := manCommandExamples(command); len(examples) > 0 {
		fmt.Fprintln(page, ".PP\nExamples:")
		writeManLiteral(page, strings.Join(examples, "\n"))
	}
}

func manCommandFlags(command *urfavecli.Command) []urfavecli.Flag {
	flags := command.VisibleFlags()
	if !command.HideHelp && urfavecli.HelpFlag != nil {
		flags = append(flags, urfavecli.HelpFlag)
	}
	return uniqueManFlags(flags)
}

func uniqueManFlags(candidates []urfavecli.Flag) []urfavecli.Flag {
	var flags []urfavecli.Flag
	seen := make(map[string]struct{})
	for _, flag := range candidates {
		if flag == nil || len(flag.Names()) == 0 {
			continue
		}
		key := strings.Join(flag.Names(), "\x00")
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		flags = append(flags, flag)
	}
	return flags
}

func manDescription(description string) string {
	if index := strings.Index(description, "\n\nExamples:"); index >= 0 {
		description = description[:index]
	}
	description = strings.ReplaceAll(description, "docs/design.md", "the detailed behavior guide in SEE ALSO")
	description = strings.ReplaceAll(description, "docs/coding-agents.md", "the coding-agent guide in SEE ALSO")
	return strings.TrimSpace(description)
}

func manExamples(description string) []string {
	index := strings.Index(description, "\n\nExamples:")
	if index < 0 {
		return nil
	}
	var examples []string
	for _, line := range strings.Split(description[index+len("\n\nExamples:"):], "\n") {
		if line = strings.TrimSpace(line); line != "" {
			examples = append(examples, line)
		}
	}
	return examples
}

func manCommandDescription(command *urfavecli.Command) string {
	switch command.Name {
	case "mcp":
		return "Run a one-time Model Context Protocol server over standard input and output. Coding agents can manage processes with the same lifecycle operations as the CLI. See the coding-agent guide in SEE ALSO for tools, scopes, and errors."
	case "run":
		return "Run a named process and start the daemon if needed. A declared process needs only its name. For an ad-hoc process, put -- before its command. By default Hum shows its output and Ctrl+C stops it; --detach leaves it running in the background."
	case "start":
		return "Start named processes from hum.yaml or project discovery. Already-running processes are left alone. Unlike hum up, this command does not start dependencies. It waits for configured readiness checks unless --no-wait is used. Readiness confirms startup only; it does not monitor later health.\n\nExit codes: 0 success; 1 request error or changed definition; 2 readiness timeout; 3 exit before ready."
	case "up":
		return "Start every process in hum.yaml. Independent processes start together; dependent processes wait for their prerequisites to become ready. By default Hum follows process output. Use --detach to return after readiness or --no-wait to return after spawning.\n\nExit codes: 0 success; 1 request error or changed definition; 2 readiness timeout; 3 early exit or failed recovery; 130 interrupted startup (processes launched by this invocation are stopped)."
	case "logs":
		return "Read retained output for one or more processes. Filters and limits apply separately to each process. Use --follow for new output; Ctrl+C stops following without stopping the process."
	case "wait":
		return "Wait for a process to exit, or use --match to wait for matching output. If the process is stopped, Hum waits for its next launch.\n\nExit codes: 0 matched or exited; 1 request or usage error; 2 timeout; 3 process exited before a match."
	case "input":
		return "Write one payload to a running process that has a TTY. Hum sends it once: it does not start the process, retry, save, or echo the input. --text sends exact text without adding a newline; --base64 accepts strict padded base64. Input fails while another client owns the TTY."
	case "restart":
		return "Gracefully stop and restart named processes. By default Hum waits for each process to become ready; --no-wait returns after spawning. A readiness failure does not prevent later names from restarting, but a request or validation error does.\n\nExit codes: 0 success; 1 request or validation error; 2 readiness timeout; 3 exit before ready."
	default:
		return manDescription(command.Description)
	}
}

func manCommandExamples(command *urfavecli.Command) []string {
	switch command.Name {
	case "init":
		return []string{"hum init", "hum init --json", "hum init --force"}
	case "run":
		return []string{"hum run api", "hum run api -- bun run api", "hum run api --detach -- bun run api", "hum --global run proxy --detach -- caddy run"}
	case "start":
		return []string{"hum start api", "hum start db api --timeout 45s", "hum --project ../service start api --json"}
	case "up":
		return []string{"hum up", "hum up --detach", "hum --project ../service up --timeout 45s --json"}
	case "logs":
		return []string{"hum logs api", "hum logs api --tail 50 --follow", "hum logs api --since 10m --match 'error|panic' --context 2", "hum --project ../service logs api --json"}
	case "wait":
		return []string{"hum wait api", "hum wait api --match 'ready on' --timeout 45s", "hum wait api --after-cursor 120 --match ready --json"}
	case "input":
		return []string{"hum input console --text \"yes\"", "hum input console --base64 eWVzCg==", "hum input console --text \"status\" --json"}
	case "restart":
		return []string{"hum restart api", "hum restart api web --timeout 45s", "hum --project ../service restart api --json"}
	default:
		return manExamples(command.Description)
	}
}

func writeManHelpCommand(page *bytes.Buffer, rootName string) {
	fmt.Fprintf(page, ".SS \"%s help\"\n", roffQuote(rootName))
	writeManLiteral(page, rootName+" help [COMMAND]")
	writeManText(page, "Show help for Hum or one command.")
}

func writeManQuickStart(page *bytes.Buffer) {
	writeManSection(page, "QUICK START")
	writeManText(page, "Create a manifest, start its processes in the background, inspect them, and follow one process:")
	writeManLiteral(page, "hum init\nhum up --detach\nhum status\nhum logs api --follow")
	writeManText(page, "Replace api with a process name shown by hum status.")
	fmt.Fprintln(page, ".PP")
	writeManText(page, "Run one command without a manifest:")
	writeManLiteral(page, "hum run api -- bun run api")
}

func writeManConfiguration(page *bytes.Buffer) {
	writeManSection(page, "CONFIGURATION")
	writeManText(page, "hum init creates hum.yaml. A minimal manifest is:")
	writeManLiteral(page, "version: 1\nprocesses:\n  api:\n    argv: [bun, run, api]")
	fmt.Fprintln(page, ".PP")
	writeManText(page, "A process can wait for another process to become ready and restart after failure:")
	writeManLiteral(page, "version: 1\nprocesses:\n  db:\n    argv: [docker, compose, up, db]\n    ready:\n      match: ready\n  api:\n    argv: [bun, run, api]\n    after: [db]\n    ready:\n      match: Listening\n      timeout: 30s\n    restart: on-failure")
	fmt.Fprintln(page, ".PP")
	writeManText(page, "argv is executed directly without shell parsing. after names readiness dependencies. ready.match waits for matching output. The project guide includes every field and a complete hum.example.yaml.")
}

func commandUsage(command *urfavecli.Command) string {
	if strings.TrimSpace(command.UsageText) != "" {
		return command.UsageText
	}
	usage := strings.Join(command.Path(), " ")
	if command.ArgsUsage != "" {
		usage += " " + command.ArgsUsage
	}
	return usage
}

func writeManFlags(page *bytes.Buffer, flags []urfavecli.Flag) {
	for _, flag := range flags {
		if flag == nil || len(flag.Names()) == 0 {
			continue
		}
		fmt.Fprintln(page, ".TP")
		fmt.Fprintf(page, ".B %s\n", roffText(manFlagNames(flag)))
		doc, ok := flag.(urfavecli.DocGenerationFlag)
		if !ok {
			continue
		}
		usage := doc.GetUsage()
		if doc.IsDefaultVisible() && doc.GetDefaultText() != "" && (doc.TakesValue() || doc.GetDefaultText() != "false") {
			usage += " (default: " + doc.GetDefaultText() + ")"
		}
		writeManText(page, usage)
	}
}

func manFlagNames(flag urfavecli.Flag) string {
	names := make([]string, 0, len(flag.Names()))
	for _, name := range flag.Names() {
		prefix := "--"
		if len(name) == 1 {
			prefix = "-"
		}
		names = append(names, prefix+name)
	}
	if doc, ok := flag.(urfavecli.DocGenerationFlag); ok && doc.TakesValue() {
		for index := range names {
			names[index] += " " + strings.ToUpper(doc.TypeName())
		}
	}
	return strings.Join(names, ", ")
}

func writeManDefinition(page *bytes.Buffer, term, description string) {
	fmt.Fprintln(page, ".TP")
	fmt.Fprintf(page, ".B %s\n", roffText(term))
	writeManText(page, description)
}

func writeManSection(page *bytes.Buffer, title string) {
	fmt.Fprintf(page, ".SH \"%s\"\n", roffQuote(title))
}

func writeManParagraphs(page *bytes.Buffer, text string) {
	for index, paragraph := range strings.Split(strings.TrimSpace(text), "\n\n") {
		if strings.TrimSpace(paragraph) == "" {
			continue
		}
		if index > 0 {
			fmt.Fprintln(page, ".PP")
		}
		writeManText(page, paragraph)
	}
}

func writeManLiteral(page *bytes.Buffer, text string) {
	fmt.Fprintln(page, ".nf")
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		fmt.Fprintln(page, roffText(line))
	}
	fmt.Fprintln(page, ".fi")
}

func writeManText(page *bytes.Buffer, text string) {
	const width = 72
	var line string
	for _, word := range strings.Fields(text) {
		word = roffText(word)
		if line != "" && len(line)+1+len(word) > width {
			fmt.Fprintln(page, line)
			line = word
			continue
		}
		if line != "" {
			line += " "
		}
		line += word
	}
	if line != "" {
		fmt.Fprintln(page, line)
	}
}

func roffQuote(text string) string {
	return strings.ReplaceAll(roffText(text), `"`, `\(dq`)
}

func roffMacroArg(text string) string {
	text = strings.ReplaceAll(text, `\`, `\e`)
	return strings.ReplaceAll(text, `"`, `\(dq`)
}

func roffText(text string) string {
	text = strings.ReplaceAll(text, `\`, `\e`)
	text = strings.ReplaceAll(text, "-", `\-`)
	lines := strings.Split(text, "\n")
	for index, line := range lines {
		if strings.HasPrefix(line, ".") || strings.HasPrefix(line, "'") {
			lines[index] = `\&` + line
		}
	}
	return strings.Join(lines, "\n")
}
