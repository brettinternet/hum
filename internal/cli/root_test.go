package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
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
	if got := output.String(); !strings.Contains(got, "build-42") {
		t.Fatalf("version output missing version: %q", got)
	}
	if got := output.String(); !strings.Contains(got, "2026-09-02T12:00:00Z") {
		t.Fatalf("version output missing build time: %q", got)
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
