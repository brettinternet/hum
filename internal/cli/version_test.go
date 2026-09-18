package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestVersionCommand(t *testing.T) {
	const (
		version   = "build-42"
		commit    = "714a19f123456789"
		buildTime = "2026-09-02T12:00:00Z"
	)

	t.Run("human output matches version flag", func(t *testing.T) {
		var flagOutput, commandOutput, errorOutput bytes.Buffer
		if err := NewRootCommandWithCommit(version, commit, buildTime, &flagOutput, &errorOutput).Run(context.Background(), []string{"hum", "--version"}); err != nil {
			t.Fatalf("--version: %v", err)
		}
		if err := NewRootCommandWithCommit(version, commit, buildTime, &commandOutput, &errorOutput).Run(context.Background(), []string{"hum", "version"}); err != nil {
			t.Fatalf("version: %v", err)
		}
		if commandOutput.String() != flagOutput.String() {
			t.Fatalf("version output = %q, want --version output %q", commandOutput.String(), flagOutput.String())
		}
		if want := "hum build-42 (714a19f, built 2026-09-02T12:00:00Z)\n"; commandOutput.String() != want {
			t.Fatalf("version output = %q, want %q", commandOutput.String(), want)
		}
		if errorOutput.Len() != 0 {
			t.Fatalf("version stderr = %q", errorOutput.String())
		}
	})

	t.Run("json capability", func(t *testing.T) {
		var output, errorOutput bytes.Buffer
		if err := NewRootCommandWithCommit(version, commit, buildTime, &output, &errorOutput).Run(context.Background(), []string{"hum", "version", "--json"}); err != nil {
			t.Fatalf("version --json: %v", err)
		}
		var got map[string]any
		if err := json.Unmarshal(output.Bytes(), &got); err != nil {
			t.Fatalf("decode %q: %v", output.String(), err)
		}
		want := map[string]any{"schema_version": float64(1), "version": version, "build_time": buildTime}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("version JSON = %#v, want exactly %#v", got, want)
		}
		if errorOutput.Len() != 0 {
			t.Fatalf("version --json stderr = %q", errorOutput.String())
		}
	})

	t.Run("does not resolve project or contact daemon", func(t *testing.T) {
		workingDirectory := t.TempDir()
		runtimeDir := filepath.Join(t.TempDir(), "absent-runtime")
		t.Chdir(workingDirectory)
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)

		var output, errorOutput bytes.Buffer
		if err := NewRootCommand(version, buildTime, &output, &errorOutput).Run(context.Background(), []string{"hum", "version", "--json"}); err != nil {
			t.Fatalf("version outside a project: %v", err)
		}
		if _, err := os.Stat(runtimeDir); !os.IsNotExist(err) {
			t.Fatalf("runtime directory was touched: %v", err)
		}
	})

	for _, args := range [][]string{{"hum", "version", "--project", "/tmp"}, {"hum", "version", "--global"}} {
		t.Run(strings.Join(args[2:], " "), func(t *testing.T) {
			var output, errorOutput bytes.Buffer
			err := NewRootCommand(version, buildTime, &output, &errorOutput).Run(context.Background(), args)
			if err == nil {
				t.Fatalf("version scope args %v returned nil error", args)
			}
			if output.Len() != 0 {
				t.Fatalf("version scope args %v wrote stdout %q", args, output.String())
			}
		})
	}
}
