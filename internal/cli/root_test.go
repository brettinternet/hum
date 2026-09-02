package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRootCommandNoArgsShowsHelp(t *testing.T) {
	var output, errorOutput bytes.Buffer

	err := NewRootCommand("dev", "unknown", &output, &errorOutput).Run(context.Background(), []string{"devproc"})
	if err != nil {
		t.Fatalf("run without arguments: %v", err)
	}
	if errorOutput.Len() != 0 {
		t.Fatalf("unexpected stderr: %q", errorOutput.String())
	}

	help := strings.ToLower(output.String())
	for _, want := range []string{"usage:", "devproc", "local development process supervisor"} {
		if !strings.Contains(help, want) {
			t.Errorf("help output missing %q: %q", want, output.String())
		}
	}
}

func TestRootCommandVersion(t *testing.T) {
	var output, errorOutput bytes.Buffer

	err := NewRootCommand("build-42", "2026-09-02T12:00:00Z", &output, &errorOutput).Run(context.Background(), []string{"devproc", "--version"})
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
	var output, errorOutput bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := NewRootCommand("dev", "unknown", &output, &errorOutput).Run(ctx, []string{"devproc"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("run with canceled context = %v, want %v", err, context.Canceled)
	}
	if output.Len() != 0 {
		t.Fatalf("canceled run rendered help: %q", output.String())
	}
}
