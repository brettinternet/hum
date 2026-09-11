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
	writeManParagraphs(&page, manDescription(root.Description))

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
	writeManText(&page, "Optional project manifest. Hum searches from the selected directory to the nearest Git root.")
	writeManSection(&page, "ENVIRONMENT")
	writeManDefinition(&page, "HUM_RUNTIME_DIR", "Override the daemon runtime directory.")
	writeManDefinition(&page, "XDG_RUNTIME_DIR", "Base runtime directory when HUM_RUNTIME_DIR is unset.")
	writeManDefinition(&page, "TMPDIR", "Base for the per-user fallback runtime directory.")
	writeManDefinition(&page, "HUM_STOP_GRACE", "Default process stop grace period.")
	writeManDefinition(&page, "HUM_OUTPUT_BYTES", "Retained output bytes per process.")
	writeManDefinition(&page, "HUM_COMPLETED_RECORDS", "Maximum completed process records retained.")
	writeManSection(&page, "SEE ALSO")
	writeManText(&page, "Project documentation: https://github.com/brettinternet/hum")

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
	writeManParagraphs(page, manDescription(command.Description))
	if flags := manCommandFlags(command); len(flags) > 0 {
		fmt.Fprintln(page, ".PP\nOptions:")
		writeManFlags(page, flags)
	}
	if examples := manExamples(command.Description); len(examples) > 0 {
		fmt.Fprintln(page, ".PP\nExamples:")
		writeManLiteral(page, strings.Join(examples, "\n"))
	}
}

func manCommandFlags(command *urfavecli.Command) []urfavecli.Flag {
	if len(command.Path()) == 1 {
		return uniqueManFlags(command.VisibleFlags())
	}
	flags := cliCommandFlags(command)
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
		return strings.TrimSpace(description[:index])
	}
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

func writeManHelpCommand(page *bytes.Buffer, rootName string) {
	fmt.Fprintf(page, ".SS \"%s help\"\n", roffQuote(rootName))
	writeManLiteral(page, rootName+" help [COMMAND]")
	writeManText(page, "Show help for Hum or one command.")
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
		if doc.IsDefaultVisible() && doc.GetDefaultText() != "" {
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
