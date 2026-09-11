package cli

import (
	"bytes"
	"strings"
	"testing"

	urfavecli "github.com/urfave/cli/v3"
)

func TestWriteManPageCoversPublicCommandTree(t *testing.T) {
	root := NewRootCommand("1.2.3", "2026-01-02T03:04:05Z", &bytes.Buffer{}, &bytes.Buffer{})
	paths := visibleHelpPaths(root)
	paths = append(paths,
		[]string{"hum", "completion"},
		[]string{"hum", "completion", "bash"},
		[]string{"hum", "completion", "zsh"},
		[]string{"hum", "completion", "fish"},
	)
	var output bytes.Buffer
	if err := WriteManPage(&output, root, "2026-01-02"); err != nil {
		t.Fatal(err)
	}
	page := output.String()

	for _, want := range []string{
		`.TH HUM 1 "2026-01-02" "hum 1.2.3 (built 2026-01-02T03:04:05Z)" "User Commands"`,
		`.SH "GLOBAL OPTIONS"`,
		`.SH "QUICK START"`,
		`.SH "CONFIGURATION"`,
		`.SH "COMMAND REFERENCE"`,
		`.SH "FILES"`,
		`.SH "ENVIRONMENT"`,
		`.SS "hum help"`,
		`hum.yaml`,
		`HUM_RUNTIME_DIR`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("man page missing %q", want)
		}
	}

	assertManFlags(t, page, ".SH \"GLOBAL OPTIONS\"", ".SH \"COMMANDS\"", root.VisibleFlags())
	if len(paths) < 2 {
		t.Fatal("generated command tree is empty")
	}
	for index, path := range paths[1:] {
		command := commandAtPath(root, path)
		if command == nil {
			t.Fatalf("command path disappeared: %v", path)
		}
		heading := `.SS "` + roffQuote(strings.Join(path, " ")) + `"`
		end := `.SS "hum help"`
		if index+2 < len(paths) {
			end = `.SS "` + roffQuote(strings.Join(paths[index+2], " ")) + `"`
		}
		flags := command.VisibleFlags()
		if !command.HideHelp && urfavecli.HelpFlag != nil {
			flags = append(flags, urfavecli.HelpFlag)
		}
		assertManFlags(t, page, heading, end, flags)
	}

	for _, want := range []string{
		"hum init\nhum up \\-\\-detach\nhum status\nhum logs api \\-\\-follow",
		"argv: [bun, run, api]",
		"hum logs api \\-\\-since 10m \\-\\-match 'error|panic' \\-\\-context 2",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("man page missing practical example %q", want)
		}
	}
	for _, heading := range []string{`.SS "hum init"`, `.SS "hum up"`} {
		section := manTestSection(page, heading)
		if strings.Contains(section, `\-\-global`) {
			t.Errorf("%s exposes hidden --global flag", heading)
		}
	}
	if section := manTestSection(page, `.SS "hum run"`); !strings.Contains(section, `\-\-global`) {
		t.Error("hum run section omits its public --global flag")
	}
	if strings.Contains(page, "(default: false)") {
		t.Error("man page prints noisy false defaults")
	}
}

func TestRoffTextEscapesControlCharacters(t *testing.T) {
	got := roffText(".line-with\\slash\n'quote")
	want := `\&.line\-with\eslash
\&'quote`
	if got != want {
		t.Fatalf("roffText() = %q, want %q", got, want)
	}
}

func manTestSection(page, heading string) string {
	start := strings.Index(page, heading)
	if start < 0 {
		return ""
	}
	section := page[start+len(heading):]
	if end := strings.Index(section, `.SS "`); end >= 0 {
		section = section[:end]
	}
	return section
}

func assertManFlags(t *testing.T, page, heading, nextHeading string, flags []urfavecli.Flag) {
	t.Helper()
	start := strings.Index(page, heading)
	if start < 0 {
		t.Fatalf("man page missing heading %q", heading)
	}
	section := page[start+len(heading):]
	if nextHeading != "" {
		if end := strings.Index(section, nextHeading); end >= 0 {
			section = section[:end]
		}
	}
	for _, flag := range flags {
		if len(flag.Names()) == 0 {
			continue
		}
		name := flag.Names()[0]
		prefix := "--"
		if len(name) == 1 {
			prefix = "-"
		}
		if !strings.Contains(section, roffText(prefix+name)) {
			t.Errorf("%s section missing flag %s", heading, prefix+name)
		}
	}
}
