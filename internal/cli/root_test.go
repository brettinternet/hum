package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRootCommandNoArgsShowsHelp(t *testing.T) {
	t.Parallel()
	var output, errorOutput bytes.Buffer

	err := NewRootCommand("dev", "unknown", &output, &errorOutput).Run(context.Background(), []string{"hum"})
	if err != nil {
		t.Fatalf("run without arguments: %v", err)
	}
	if errorOutput.Len() != 0 {
		t.Fatalf("unexpected stderr: %q", errorOutput.String())
	}

	help := strings.ToLower(output.String())
	for _, want := range []string{"usage:", "hum", "local development process supervisor"} {
		if !strings.Contains(help, want) {
			t.Errorf("help output missing %q: %q", want, output.String())
		}
	}
}

func TestRootCommandVersion(t *testing.T) {
	t.Parallel()
	var output, errorOutput bytes.Buffer

	err := NewRootCommand("build-42", "2026-09-02T12:00:00Z", &output, &errorOutput).Run(context.Background(), []string{"hum", "--version"})
	if err != nil {
		t.Fatalf("run with --version: %v", err)
	}
	if got, want := output.String(), "hum build-42 (built 2026-09-02T12:00:00Z)\n"; got != want {
		t.Fatalf("version output = %q, want %q", got, want)
	}
	if errorOutput.Len() != 0 {
		t.Fatalf("unexpected stderr: %q", errorOutput.String())
	}
}

func TestRootCommandCanceledContext(t *testing.T) {
	t.Parallel()
	var output, errorOutput bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := NewRootCommand("dev", "unknown", &output, &errorOutput).Run(ctx, []string{"hum"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("run with canceled context = %v, want %v", err, context.Canceled)
	}
	if output.Len() != 0 {
		t.Fatalf("canceled run rendered help: %q", output.String())
	}
}

func TestRootCommandUnknownCommand(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		args       []string
		wantPhrase []string
		notWant    string
	}{
		{name: "typo suggests command", args: []string{"hum", "lst"}, wantPhrase: []string{`Unknown command "lst".`, `Did you mean "list"?`, "hum --help"}},
		{name: "prefix suggests command", args: []string{"hum", "stat"}, wantPhrase: []string{`Unknown command "stat".`, `Did you mean "status"?`}},
		{name: "distant name has no suggestion", args: []string{"hum", "frobnicate"}, wantPhrase: []string{`Unknown command "frobnicate".`, "hum --help"}, notWant: "Did you mean"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output, errorOutput bytes.Buffer
			err := NewRootCommand("dev", "unknown", &output, &errorOutput).Run(context.Background(), test.args)
			if err == nil {
				t.Fatalf("run %v returned nil error", test.args)
			}
			for _, want := range test.wantPhrase {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q missing %q", err.Error(), want)
				}
			}
			if test.notWant != "" && strings.Contains(err.Error(), test.notWant) {
				t.Errorf("error %q unexpectedly contains %q", err.Error(), test.notWant)
			}
			if output.Len() != 0 {
				t.Fatalf("unknown command rendered help on stdout: %q", output.String())
			}
		})
	}
}

func TestRootGlobalFlagsDocumentDefaultsAndEnv(t *testing.T) {
	t.Parallel()
	var output, errorOutput bytes.Buffer
	if err := NewRootCommand("dev", "unknown", &output, &errorOutput).Run(context.Background(), []string{"hum", "--help"}); err != nil {
		t.Fatalf("help: %v", err)
	}
	help := output.String()
	for _, want := range []string{"$HUM_RUNTIME_DIR", "$XDG_RUNTIME_DIR/hum", "$HUM_STOP_GRACE", "(default: 10s)", "$HUM_OUTPUT_BYTES", "(default: 4194304)", "$HUM_COMPLETED_RECORDS", "(default: 20)"} {
		if !strings.Contains(help, want) {
			t.Errorf("help missing %q:\n%s", want, help)
		}
	}
}

// TestProcessNotFoundSuggestsClosestName proves a mistyped process name gets
// a "Did you mean" hint drawn only from the requested project, in human
// output and as details.suggestion in a JSON not_found error.
func TestProcessNotFoundSuggestsClosestName(t *testing.T) {
	projectRoot := stopShutdownTestProject(t)
	server, runtimeDir := stopShutdownTestServer(t, 200*time.Millisecond)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// The test binary with no matching tests exits at once on every platform,
	// leaving a retained record without a long-lived child.
	argv := []string{executable, "-test.run=^$"}
	manifest := "version: 1\nprocesses:\n  web:\n    argv: [" + strings.Join([]string{`"` + filepath.ToSlash(executable) + `"`, `"-test.run=^$"`}, ", ") + "]\n"
	if err := os.WriteFile(filepath.Join(projectRoot, "hum.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	otherRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(otherRoot, "hum.yaml"), []byte("version: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stopShutdownStartProcess(t, server, projectRoot, "web", argv)
	stopShutdownStartProcess(t, server, otherRoot, "api", argv)

	humanOutput := func(args ...string) string {
		t.Helper()
		stdout, _, err := stopShutdownRun(t, args...)
		if err != nil {
			return stdout + err.Error()
		}
		return stdout
	}
	for _, command := range []string{"logs", "status", "stop"} {
		if got := humanOutput(command, "wbe"); !strings.Contains(got, `Did you mean "web"?`) {
			t.Errorf("hum %s wbe = %q, want suggestion of web", command, got)
		}
		// "apo" is one edit from "api", which is retained only in another project.
		for _, name := range []string{"zzzz", "apo"} {
			if got := humanOutput(command, name); strings.Contains(got, "Did you mean") {
				t.Errorf("hum %s %s = %q, want no suggestion", command, name, got)
			}
		}
	}

	startJSONError := func(name string) (string, map[string]any) {
		t.Helper()
		stdout, _, err := stopShutdownRun(t, "start", name, "--json")
		if err == nil {
			t.Fatalf("hum start %s --json succeeded: %q", name, stdout)
		}
		var document struct {
			Error struct {
				Code    string         `json:"code"`
				Details map[string]any `json:"details"`
			} `json:"error"`
		}
		if decodeErr := json.Unmarshal([]byte(stdout), &document); decodeErr != nil {
			t.Fatalf("hum start %s --json stdout %q: %v", name, stdout, decodeErr)
		}
		return document.Error.Code, document.Error.Details
	}
	if code, details := startJSONError("wbe"); code != "not_found" || details["suggestion"] != "web" {
		t.Errorf("hum start wbe --json error = %s %v, want not_found with suggestion web", code, details)
	}
	if code, details := startJSONError("apo"); code != "not_found" || details["suggestion"] != nil {
		t.Errorf("hum start apo --json error = %s %v, want not_found without suggestion", code, details)
	}
}
