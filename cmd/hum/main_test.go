package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	urfavecli "github.com/urfave/cli/v3"
)

func captureWriters(t *testing.T) (*bytes.Buffer, *bytes.Buffer) {
	t.Helper()

	output, errorOutput := new(bytes.Buffer), new(bytes.Buffer)
	previousOutputWriter, previousErrorWriter := outputWriter, errorWriter
	outputWriter, errorWriter = output, errorOutput
	t.Cleanup(func() {
		outputWriter, errorWriter = previousOutputWriter, previousErrorWriter
	})
	return output, errorOutput
}

func TestRunVersion(t *testing.T) {
	previousVersion, previousCommit, previousBuildTime := buildVersion, buildCommit, buildTime
	buildVersion, buildCommit, buildTime = "build-42", "714a19f123456789", "2026-09-02T12:00:00Z"
	t.Cleanup(func() {
		buildVersion, buildCommit, buildTime = previousVersion, previousCommit, previousBuildTime
	})

	output, errorOutput := captureWriters(t)

	err := run(context.Background(), []string{"hum", "--version"})
	if err != nil {
		t.Fatalf("run with --version: %v", err)
	}
	if !strings.Contains(output.String(), "build-42") {
		t.Fatalf("version output missing version: %q", output.String())
	}
	if !strings.Contains(output.String(), "714a19f") {
		t.Fatalf("version output missing commit: %q", output.String())
	}
	if !strings.Contains(output.String(), "2026-09-02T12:00:00Z") {
		t.Fatalf("version output missing build time: %q", output.String())
	}
	if errorOutput.Len() != 0 {
		t.Fatalf("unexpected stderr: %q", errorOutput.String())
	}
}

func TestRunInvalidCommandReturnsError(t *testing.T) {
	_, _ = captureWriters(t)

	err := run(context.Background(), []string{"hum", "--not-a-command"})
	if err == nil {
		t.Fatal("run with an invalid command returned nil")
	}
}

func TestExitCodePreservesExitCoder(t *testing.T) {
	if got := exitCode(urfavecli.Exit("", 7)); got != 7 {
		t.Fatalf("exitCode(urfavecli.Exit(\"\", 7)) = %d, want 7", got)
	}
}

func TestExitCodeOrdinaryError(t *testing.T) {
	if got := exitCode(errors.New("ordinary error")); got != 1 {
		t.Fatalf("exitCode(ordinary error) = %d, want 1", got)
	}
}
