package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hum/internal/protocol"

	urfavecli "github.com/urfave/cli/v3"
)

func TestJSONErrorsBeforeOutput(t *testing.T) {
	tests := []struct {
		name string
		args []string
		code string
	}{
		{
			name: "usage",
			args: []string{"start", "--json"},
			code: string(jsonErrorUsage),
		},
		{
			name: "parse usage with later short JSON flag",
			args: []string{"start", "--unknown", "-j"},
			code: string(jsonErrorUsage),
		},
		{
			name: "daemon unavailable",
			args: []string{"status", "missing", "--project", "PROJECT", "--json"},
			code: string(jsonErrorDaemonUnavailable),
		},
		{
			name: "aggregate logs daemon unavailable",
			args: []string{"logs", "one", "two", "--project", "PROJECT", "--json"},
			code: string(jsonErrorDaemonUnavailable),
		},
		{
			name: "manifest invalid",
			args: []string{"up", "--project", "PROJECT", "--json"},
			code: string(jsonErrorManifestInvalid),
		},
		{
			name: "since out of range usage",
			args: []string{"logs", "api", "--since", "9223372036854775807ns", "--project", "PROJECT", "--json"},
			code: string(jsonErrorUsage),
		},
		{
			name: "global conflicts with project selector",
			args: []string{"--global", "--project", "PROJECT", "status", "--json"},
			code: string(jsonErrorUsage),
		},
		{
			name: "global conflicts with all-scope list",
			args: []string{"--global", "list", "--all", "--json"},
			code: string(jsonErrorUsage),
		},
		{
			name: "global up is rejected",
			args: []string{"--global", "up", "--json"},
			code: string(jsonErrorUsage),
		},
		{
			name: "input wire invalid request",
			args: []string{"input", "api", "--base64", "eA", "--json"},
			code: string(protocol.ErrorInvalidRequest),
		},
		{
			name: "input daemon unavailable",
			args: []string{"input", "api", "--text", "x", "--project", "PROJECT", "--json"},
			code: string(jsonErrorDaemonUnavailable),
		},
		{
			name: "invalid CLI config",
			args: []string{"--global", "--output-bytes", "1", "stop", "api", "--json"},
			code: string(jsonErrorUsage),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			projectRoot := t.TempDir()
			if err := os.Mkdir(filepath.Join(projectRoot, ".git"), 0o755); err != nil {
				t.Fatal(err)
			}
			runtimeDir := t.TempDir()
			t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
			args := append([]string(nil), test.args...)
			for index := range args {
				if args[index] == "PROJECT" {
					args[index] = projectRoot
				}
			}
			if test.name == "manifest invalid" {
				if err := os.WriteFile(filepath.Join(projectRoot, "hum.yaml"), []byte("version: 1\nprocesses:\n  broken: [not, a, process]\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			stdout, stderr, err := jsonErrorTestRun(t, args...)
			if err == nil {
				t.Fatal("JSON command unexpectedly succeeded")
			}
			if stderr != "" {
				t.Fatalf("stderr = %q, want empty", stderr)
			}
			got := decodeJSONErrorObject(t, stdout)
			if got.Code != test.code {
				t.Fatalf("error code = %q, want %q (stdout=%q)", got.Code, test.code, stdout)
			}
			if !strings.HasSuffix(stdout, "\n") {
				t.Fatalf("stdout = %q, want newline-terminated JSON", stdout)
			}
		})
	}

	t.Run("wire not found retains code", func(t *testing.T) {
		projectRoot := stopShutdownTestProject(t)
		_, runtimeDir := stopShutdownTestServer(t, time.Second)
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
		stdout, stderr, err := jsonErrorTestRun(t, "status", "missing", "--project", projectRoot, "--json")
		if err == nil || stderr != "" {
			t.Fatalf("status error = %v, stdout=%q stderr=%q", err, stdout, stderr)
		}
		if got := decodeJSONErrorObject(t, stdout); got.Code != string(protocol.ErrorNotFound) {
			t.Fatalf("wire error code = %q, want %q", got.Code, protocol.ErrorNotFound)
		}
	})

	t.Run("shutdown wire error retains code", func(t *testing.T) {
		projectRoot := stopShutdownTestProject(t)
		server, runtimeDir := stopShutdownTestServer(t, time.Second)
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
		stopShutdownStartProcess(t, server, projectRoot, "active", []string{"/bin/sh", "-c", "sleep 30"})
		stdout, stderr, err := jsonErrorTestRun(t, "shutdown", "--json")
		if err == nil || stderr != "" {
			t.Fatalf("shutdown error = %v, stdout=%q stderr=%q", err, stdout, stderr)
		}
		if got := decodeJSONErrorObject(t, stdout); got.Code != string(protocol.ErrorActiveProcesses) {
			t.Fatalf("wire error code = %q, want %q", got.Code, protocol.ErrorActiveProcesses)
		}
	})

	t.Run("internal", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		root := NewRootCommand("test", "test", &stdout, &stderr)
		SetInvocationArgs(root, []string{"hum", "status", "x", "--json"})
		state := root.Metadata[jsonErrorStateMetadataKey].(*jsonErrorState)
		state.handle(root.Command("status"), errors.New("injected internal failure"))
		if stderr.Len() != 0 {
			t.Fatalf("stderr = %q, want empty", stderr.String())
		}
		if got := decodeJSONErrorObject(t, stdout.String()); got.Code != string(jsonErrorInternal) {
			t.Fatalf("internal error code = %q, want %q", got.Code, jsonErrorInternal)
		}
	})

	t.Run("preserves exit code", func(t *testing.T) {
		var stdout bytes.Buffer
		root := NewRootCommand("test", "test", &stdout, &bytes.Buffer{})
		SetInvocationArgs(root, []string{"hum", "status", "x", "--json"})
		state := root.Metadata[jsonErrorStateMetadataKey].(*jsonErrorState)
		err := state.handle(root.Command("status"), urfavecli.Exit("classified failure", 7))
		var exitErr urfavecli.ExitCoder
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 7 {
			t.Fatalf("handled exit error = %#v, want exit code 7", err)
		}
		if got := decodeJSONErrorObject(t, stdout.String()); got.Code != string(jsonErrorInternal) {
			t.Fatalf("error code = %q, want %q", got.Code, jsonErrorInternal)
		}
	})
}

func TestJSONErrorClassificationIgnoresMessageText(t *testing.T) {
	for _, message := range []string{
		"runtime requires a process name",
		"runtime must retain state",
		"internal duplicate record",
		"Nothing is running. Start a process with hum run <name> -- <command>.",
		"No hum daemon is running. Start it with hum serve --daemon.",
	} {
		t.Run(message, func(t *testing.T) {
			if got := classifyJSONError(errors.New(message)); got.Code != jsonErrorInternal {
				t.Fatalf("message-only error code = %q, want %q", got.Code, jsonErrorInternal)
			}
		})
	}
	if got := classifyJSONError(newCLIUsageError(errors.New("opaque validation failure"))); got.Code != jsonErrorUsage {
		t.Fatalf("typed usage error code = %q, want %q", got.Code, jsonErrorUsage)
	}
	if got := classifyJSONError(newCLIUnavailableError(errors.New("opaque unavailable failure"))); got.Code != jsonErrorDaemonUnavailable {
		t.Fatalf("typed unavailable error code = %q, want %q", got.Code, jsonErrorDaemonUnavailable)
	}
}

func TestJSONCommandValidationClassification(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
	}{
		{name: "status argument count", args: []string{"status", "one", "two", "--json"}},
		{name: "logs stream", args: []string{"logs", "one", "--stream", "invalid", "--json"}},
		{name: "logs duplicate", args: []string{"logs", "one", "one", "--json"}},
		{name: "wait regex", args: []string{"wait", "one", "--match", "[", "--json"}},
		{name: "signal argument count", args: []string{"signal", "one", "--json"}},
		{name: "stop missing name", args: []string{"stop", "--json"}},
		{name: "remove missing selector", args: []string{"remove", "--json"}},
		{name: "restart missing name", args: []string{"restart", "--json"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stdout, stderr, err := jsonErrorTestRun(t, test.args...)
			if err == nil || stderr != "" {
				t.Fatalf("validation error = %v, stdout=%q stderr=%q", err, stdout, stderr)
			}
			if got := decodeJSONErrorObject(t, stdout); got.Code != string(jsonErrorUsage) {
				t.Fatalf("validation code = %q, want %q (stdout=%q)", got.Code, jsonErrorUsage, stdout)
			}
		})
	}
}

func TestJSONStreamingErrors(t *testing.T) {
	t.Parallel()
	for _, commandName := range []string{"start", "up"} {
		t.Run(commandName+" appends error event after NDJSON", func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			root := NewRootCommand("test", "test", &stdout, &stderr)
			SetInvocationArgs(root, []string{"hum", commandName, "--json"})
			state := root.Metadata[jsonErrorStateMetadataKey].(*jsonErrorState)
			command := root.Command(commandName)
			state.noteCommand(command)
			if err := encodeJSON(state.output, map[string]string{"name": "first"}); err != nil {
				t.Fatal(err)
			}
			if err := state.handle(command, errors.New("late start failure")); err == nil {
				t.Fatal("late failure returned nil")
			}
			lines := decodeJSONLines(t, stdout.String())
			if len(lines) != 2 {
				t.Fatalf("NDJSON lines = %d, want 2: %q", len(lines), stdout.String())
			}
			var terminal protocol.StreamEvent
			if err := json.Unmarshal(lines[1], &terminal); err != nil {
				t.Fatal(err)
			}
			if terminal.Type != protocol.EventError || terminal.Error == nil || terminal.Error.Code != jsonErrorInternal {
				t.Fatalf("terminal event = %#v", terminal)
			}
			if stderr.Len() != 0 {
				t.Fatalf("stderr = %q, want empty", stderr.String())
			}
		})
	}

	t.Run("logs follow appends typed wire error event", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		root := NewRootCommand("test", "test", &stdout, &stderr)
		SetInvocationArgs(root, []string{"hum", "logs", "api", "--follow", "--json"})
		state := root.Metadata[jsonErrorStateMetadataKey].(*jsonErrorState)
		command := root.Command("logs")
		if err := command.Set("follow", "true"); err != nil {
			t.Fatal(err)
		}
		state.noteCommand(command)
		if err := encodeJSON(state.output, protocol.StreamEvent{Op: protocol.OpEvent, Type: protocol.EventOutput, Name: "api"}); err != nil {
			t.Fatal(err)
		}
		lateFailure := protocol.NewWireError(protocol.ErrorCode("read_failed"), "late follower failure", nil)
		if err := state.handle(command, lateFailure); err == nil {
			t.Fatal("late failure returned nil")
		}
		lines := decodeJSONLines(t, stdout.String())
		if len(lines) != 2 {
			t.Fatalf("NDJSON lines = %d, want 2: %q", len(lines), stdout.String())
		}
		var terminal protocol.StreamEvent
		if err := json.Unmarshal(lines[1], &terminal); err != nil {
			t.Fatal(err)
		}
		if terminal.Type != protocol.EventError || terminal.Error == nil || terminal.Error.Code != lateFailure.Code {
			t.Fatalf("terminal event = %#v", terminal)
		}
		if stderr.Len() != 0 {
			t.Fatalf("stderr = %q, want empty", stderr.String())
		}
	})
}

func TestJSONErrorModeDetection(t *testing.T) {
	t.Parallel()
	root := NewRootCommand("test", "test", &bytes.Buffer{}, &bytes.Buffer{})
	jsonCommands := map[string]bool{
		"init": true, "run": true, "start": true, "up": true, "down": true,
		"list": true, "status": true, "logs": true, "wait": true, "input": false,
		"restart": true, "stop": true, "remove": true, "signal": true, "shutdown": true,
	}
	for commandName, shortAlias := range jsonCommands {
		t.Run(commandName+" long flag", func(t *testing.T) {
			command := root.Command(commandName)
			args := []string{"hum", commandName, "--json"}
			if commandName == "run" {
				args = []string{"hum", "run", "api", "--json", "--", "echo"}
			}
			if command == nil || !commandJSONRequested(command, root, args) {
				t.Fatalf("%s did not detect --json", commandName)
			}
		})
		t.Run(commandName+" short alias", func(t *testing.T) {
			command := root.Command(commandName)
			args := []string{"hum", commandName, "-j"}
			if commandName == "run" {
				args = []string{"hum", "run", "api", "-j", "--", "echo"}
			}
			if got := commandJSONRequested(command, root, args); got != shortAlias {
				t.Fatalf("%s -j detection = %t, want %t", commandName, got, shortAlias)
			}
		})
	}
	for _, command := range root.Commands {
		if supports, _ := commandSupportsJSON(command); supports {
			if _, ok := jsonCommands[command.Name]; !ok {
				t.Errorf("JSON-capable command %q is missing from mode detection coverage", command.Name)
			}
		}
	}

	cases := []struct {
		name string
		cmd  string
		args []string
		want bool
	}{
		{name: "long flag", cmd: "start", args: []string{"hum", "start", "--json"}, want: true},
		{name: "documented short alias", cmd: "start", args: []string{"hum", "start", "-j"}, want: true},
		{name: "later parse failure", cmd: "start", args: []string{"hum", "start", "--bad", "--json"}, want: true},
		{name: "attached value is not standalone", cmd: "start", args: []string{"hum", "start", "--json=true"}, want: false},
		{name: "input has no short alias", cmd: "input", args: []string{"hum", "input", "-j"}, want: false},
		{name: "run payload separator", cmd: "run", args: []string{"hum", "run", "api", "--", "--json"}, want: false},
		{name: "run payload lookalike", cmd: "run", args: []string{"hum", "run", "api", "echo", "--json"}, want: false},
		{name: "run flag before separator", cmd: "run", args: []string{"hum", "run", "api", "--json", "--", "echo"}, want: true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			command := root.Command(test.cmd)
			if command == nil {
				t.Fatalf("missing command %q", test.cmd)
			}
			if got := commandJSONRequested(command, root, test.args); got != test.want {
				t.Fatalf("commandJSONRequested(%v) = %t, want %t", test.args, got, test.want)
			}
		})
	}
}

func TestHumanErrorsUnchanged(t *testing.T) {
	t.Parallel()
	t.Run("usage", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		root := NewRootCommand("test", "test", &stdout, &stderr)
		err := root.Run(context.Background(), []string{"hum", "start"})
		if err == nil || err.Error() != "start requires at least one process name" {
			t.Fatalf("human usage error = %v", err)
		}
		if stdout.String() != "" || stderr.String() != "" {
			t.Fatalf("human usage output = stdout %q stderr %q", stdout.String(), stderr.String())
		}
	})

	t.Run("init validation order", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		root := NewRootCommand("test", "test", &stdout, &stderr)
		err := root.Run(context.Background(), []string{"hum", "init", "--global", "extra"})
		if err == nil || err.Error() != "init accepts no positional arguments" {
			t.Fatalf("combined init usage error = %v", err)
		}
	})

	t.Run("attached run keeps raw child channels", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		root := NewRootCommand("test", "test", &stdout, &stderr)
		SetInvocationArgs(root, []string{"hum", "run", "api", "--json", "--", "child"})
		command := root.Command("run")
		if err := command.Set("json", "true"); err != nil {
			t.Fatal(err)
		}
		state := root.Metadata[jsonErrorStateMetadataKey].(*jsonErrorState)
		state.noteCommand(command)
		if state.json {
			t.Fatal("attached run unexpectedly enabled structured JSON errors")
		}
		_, _ = io.WriteString(state.output, "child stdout")
		_, _ = io.WriteString(&stderr, "child stderr")
		if stdout.String() != "child stdout" || stderr.String() != "child stderr" {
			t.Fatalf("attached child channels changed: stdout=%q stderr=%q", stdout.String(), stderr.String())
		}
	})
}

func TestJSONErrorDocs(t *testing.T) {
	design, err := os.ReadFile("../../docs/design.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := strings.ToLower(string(design))
	for _, phrase := range []string{
		"daemon_unavailable", "manifest_invalid", "newline-terminated", "stdout", "stderr",
		"start`/`up", "logs --follow", "terminal", "attached `run` does not support cli json mode", "payload text",
	} {
		if !strings.Contains(doc, phrase) {
			t.Errorf("design docs missing %q", phrase)
		}
	}
}

type decodedJSONError struct {
	Code    string
	Message string
}

func decodeJSONErrorObject(t *testing.T, text string) decodedJSONError {
	t.Helper()
	lines := decodeJSONLines(t, text)
	if len(lines) != 1 {
		t.Fatalf("JSON error output has %d lines, want one: %q", len(lines), text)
	}
	var object jsonErrorEnvelope
	if err := json.Unmarshal(lines[0], &object); err != nil {
		t.Fatal(err)
	}
	if object.Error == nil {
		t.Fatalf("JSON error object omitted error: %q", text)
	}
	return decodedJSONError{Code: string(object.Error.Code), Message: object.Error.Message}
}

func decodeJSONLines(t *testing.T, text string) [][]byte {
	t.Helper()
	var lines [][]byte
	for _, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !json.Valid([]byte(line)) {
			t.Fatalf("invalid JSON line %q", line)
		}
		lines = append(lines, []byte(line))
	}
	return lines
}

func jsonErrorTestRun(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	root := NewRootCommand("test", "test", &stdout, &stderr)
	argv := append([]string{"hum"}, args...)
	SetInvocationArgs(root, argv)
	err := root.Run(context.Background(), argv)
	return stdout.String(), stderr.String(), err
}
