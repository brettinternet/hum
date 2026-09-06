package cli

import (
	"bytes"
	"context"
	"io"
	"regexp"
	"strings"
	"testing"

	urfavecli "github.com/urfave/cli/v3"
)

func TestHelpContract(t *testing.T) {
	paths := visibleHelpPaths(NewRootCommand("test", "test", &bytes.Buffer{}, &bytes.Buffer{}))
	if len(paths) == 0 {
		t.Fatal("visible command tree is empty")
	}

	for _, path := range paths {
		t.Run(strings.Join(path, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			root := NewRootCommand("test", "test", &stdout, &stderr)
			command := commandAtPath(root, path)
			if command == nil {
				t.Fatalf("command path disappeared: %v", path)
			}
			if strings.Contains(command.Usage, "\n") || strings.TrimSpace(command.Usage) == "" {
				t.Fatalf("usage must be one line: %q", command.Usage)
			}
			if err := root.Run(context.Background(), append([]string{"hum"}, append(path[1:], "--help")...)); err != nil {
				t.Fatalf("help: %v", err)
			}
			if stderr.Len() != 0 {
				t.Fatalf("help wrote stderr: %q", stderr.String())
			}

			help := stdout.String()
			usage := helpSection(help, "USAGE:")
			if len(nonEmptyLines(usage)) != 1 {
				t.Fatalf("usage is not one line: %q", usage)
			}
			prose := helpDescription(command.Description)
			if countSentences(prose) > 3 {
				t.Fatalf("description has more than three sentences (%d): %q", countSentences(prose), prose)
			}
			if countSentences(prose) == 0 {
				t.Fatalf("description has no sentence: %q", prose)
			}

			examples := helpExamples(command.Description)
			if len(examples) < 1 || len(examples) > 3 {
				t.Fatalf("examples = %d, want 1..3: %q", len(examples), command.Description)
			}
			for _, example := range examples {
				if !strings.HasPrefix(example, "hum ") || !strings.Contains(help, example) {
					t.Errorf("example is not copy-pasteable and displayed: %q", example)
				}
			}

			for _, flag := range helpContractFlags(command) {
				name := flag.Names()[0]
				block := helpFlagBlock(help, "--"+name)
				if block == "" {
					t.Errorf("flag --%s is not displayed", name)
					continue
				}
				if !helpDefaultOrOmissionRE.MatchString(strings.ToLower(block)) {
					t.Errorf("flag --%s lacks a displayed default or omission meaning: %q", name, block)
				}
			}
		})
	}

	fixtureRoot := &urfavecli.Command{
		Name:      "fixture",
		Writer:    io.Discard,
		ErrWriter: io.Discard,
		Flags: []urfavecli.Flag{
			&urfavecli.BoolFlag{Name: "persistent"},
			&urfavecli.BoolFlag{Name: "hidden-persistent", Hidden: true},
		},
		Commands: []*urfavecli.Command{{
			Name: "child",
			Flags: []urfavecli.Flag{
				&urfavecli.BoolFlag{Name: "local", Local: true},
				&urfavecli.BoolFlag{Name: "hidden-local", Local: true, Hidden: true},
			},
		}},
	}
	if err := fixtureRoot.Run(context.Background(), []string{"fixture", "child", "--help"}); err != nil {
		t.Fatalf("set up flag visibility fixture: %v", err)
	}
	var visibleNames []string
	for _, flag := range helpContractFlags(fixtureRoot.Command("child")) {
		visibleNames = append(visibleNames, flag.Names()[0])
	}
	visible := strings.Join(visibleNames, ",")
	if !strings.Contains(visible, "local") || !strings.Contains(visible, "persistent") {
		t.Fatalf("visible flags missing local or persistent fixture: %q", visible)
	}
	if strings.Contains(visible, "hidden-local") || strings.Contains(visible, "hidden-persistent") {
		t.Fatalf("hidden flags included in fixture: %q", visible)
	}
}

func TestHelpExitCodes(t *testing.T) {
	exitContracts := map[string]string{
		"start": "Exit codes: 0 success; exit 1 for request error or definition drift; exit 2 for readiness timeout; exit 3 for early exit before ready.",
		"up":    "Exit codes: 0 success; exit 1 for request error or definition drift; exit 2 for readiness timeout; exit 3 for early exit or recovery not running.",
		"wait":  "Exit codes: 0 for a match or unfiltered exit; exit 1 for a request or usage error; exit 2 for timeout; exit 3 when process exit precedes --match.",
	}
	paths := visibleHelpPaths(NewRootCommand("test", "test", &bytes.Buffer{}, &bytes.Buffer{}))
	for _, path := range paths {
		root := NewRootCommand("test", "test", &bytes.Buffer{}, &bytes.Buffer{})
		command := commandAtPath(root, path)
		if command == nil {
			t.Fatalf("command path disappeared: %v", path)
		}
		prose := helpDescription(command.Description)
		contract, needsExitContract := exitContracts[command.Name]
		if needsExitContract {
			if strings.Count(prose, contract) != 1 {
				t.Errorf("%s must document the exact exit contract once: %q", command.Name, prose)
			}
			continue
		}
		if strings.Contains(strings.ToLower(prose), "exit code") {
			t.Errorf("%s duplicates the start/up/wait exit-code contract: %q", strings.Join(path, " "), prose)
		}
	}
}

var helpDefaultOrOmissionRE = regexp.MustCompile(`\b(default|omit|optional|required|exactly one|when using)\b`)
var helpSentenceRE = regexp.MustCompile(`[.!?](?:\s|$)`)

func visibleHelpPaths(root *urfavecli.Command) [][]string {
	var paths [][]string
	var walk func(*urfavecli.Command, []string)
	walk = func(command *urfavecli.Command, prefix []string) {
		if command == nil || command.Hidden {
			return
		}
		path := append(append([]string(nil), prefix...), command.Name)
		paths = append(paths, path)
		for _, child := range command.Commands {
			walk(child, path)
		}
	}
	walk(root, nil)
	return paths
}

func commandAtPath(root *urfavecli.Command, path []string) *urfavecli.Command {
	if len(path) == 0 || root == nil || root.Name != path[0] {
		return nil
	}
	command := root
	for _, name := range path[1:] {
		command = command.Command(name)
		if command == nil {
			return nil
		}
	}
	return command
}

func helpContractFlags(command *urfavecli.Command) []urfavecli.Flag {
	flags := append([]urfavecli.Flag(nil), command.VisibleFlags()...)
	return append(flags, command.VisiblePersistentFlags()...)
}

func helpDescription(description string) string {
	if index := strings.Index(description, "\n\nExamples:"); index >= 0 {
		return strings.TrimSpace(description[:index])
	}
	return strings.TrimSpace(description)
}

func helpExamples(description string) []string {
	index := strings.Index(description, "\n\nExamples:")
	if index < 0 {
		return nil
	}
	var examples []string
	for _, line := range strings.Split(description[index+len("\n\nExamples:"):], "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			examples = append(examples, line)
		}
	}
	return examples
}

func countSentences(description string) int {
	return len(helpSentenceRE.FindAllStringIndex(description, -1))
}

func helpSection(help, heading string) string {
	start := strings.Index(help, heading)
	if start < 0 {
		return ""
	}
	section := help[start+len(heading):]
	if end := strings.Index(section, "\n\n"); end >= 0 {
		return section[:end]
	}
	return section
}

func nonEmptyLines(text string) []string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func helpFlagBlock(help, name string) string {
	start := strings.Index(help, "\n   "+name)
	if start < 0 {
		return ""
	}
	block := help[start:]
	if end := strings.Index(block[1:], "\n   --"); end >= 0 {
		block = block[:end+1]
	}
	return block
}
