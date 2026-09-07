package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"hum/internal/daemon"
	"hum/internal/project"

	urfavecli "github.com/urfave/cli/v3"
)

func runCompletionForTest(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	root := NewRootCommand("test", "test", &stdout, &stderr)
	root.ExitErrHandler = func(context.Context, *urfavecli.Command, error) {}
	err := root.Run(context.Background(), append([]string{"hum"}, args...))
	return stdout.String(), stderr.String(), err
}

func runNamePositionCompletionForTest(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	previous, existed := os.LookupEnv(completionNamePositionEnv)
	if err := os.Setenv(completionNamePositionEnv, "1"); err != nil {
		t.Fatalf("set completion cursor environment: %v", err)
	}
	defer func() {
		if existed {
			_ = os.Setenv(completionNamePositionEnv, previous)
		} else {
			_ = os.Unsetenv(completionNamePositionEnv)
		}
	}()
	return runCompletionForTest(t, args...)
}

func TestCompletionScripts(t *testing.T) {
	for shell, marker := range map[string]string{
		"bash": "__hum_bash_autocomplete",
		"zsh":  "#compdef hum",
		"fish": "function __hum_perform_completion",
	} {
		t.Run(shell, func(t *testing.T) {
			stdout, stderr, err := runCompletionForTest(t, "completion", shell)
			if err != nil {
				t.Fatalf("completion %s: %v", shell, err)
			}
			if stdout == "" || !strings.Contains(stdout, marker) {
				t.Fatalf("completion %s output missing %q: %q", shell, marker, stdout)
			}
			if !strings.Contains(stdout, completionNamePositionEnv+"=1") {
				t.Fatalf("completion %s output omitted NAME-position marker", shell)
			}
			if stderr != "" {
				t.Fatalf("completion %s wrote stderr: %q", shell, stderr)
			}
		})
	}

	stdout, stderr, err := runCompletionForTest(t, "--help")
	if err != nil {
		t.Fatalf("root help: %v", err)
	}
	if !strings.Contains(stdout, "completion") {
		t.Fatalf("root help omitted visible completion command: %q", stdout)
	}
	if stderr != "" {
		t.Fatalf("root help wrote stderr: %q", stderr)
	}

	stdout, stderr, err = runCompletionForTest(t, "completion", "--help")
	if err != nil {
		t.Fatalf("completion help: %v", err)
	}
	for _, shell := range []string{"bash", "zsh", "fish"} {
		if !strings.Contains(stdout, shell) {
			t.Errorf("completion help omitted %s: %q", shell, stdout)
		}
	}
	if strings.Contains(stdout, "pwsh") || stderr != "" {
		t.Fatalf("completion help exposed unsupported shell or stderr: stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestNameCompletion(t *testing.T) {
	projectRoot := stopShutdownTestProject(t)
	server, runtimeDir := stopShutdownTestServer(t, 100*time.Millisecond)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	writeManifestCLITestFile(t, projectRoot, `version: 1
processes:
  zeta:
    argv: [echo, zeta]
  alpha:
    argv: [echo, alpha]
`)
	stopShutdownStartProcess(t, server, projectRoot, "alpha", []string{"/bin/sh", "-c", "sleep 30"})
	stopShutdownStartProcess(t, server, projectRoot, "runtime", []string{"/bin/sh", "-c", "sleep 30"})

	otherRoot := filepath.Join(t.TempDir(), "other")
	if err := os.MkdirAll(otherRoot, 0o700); err != nil {
		t.Fatalf("create other project: %v", err)
	}
	stopShutdownStartProcess(t, server, otherRoot, "foreign", []string{"/bin/sh", "-c", "sleep 30"})

	want := "alpha\nruntime\nzeta\n"
	for _, command := range []string{"run", "start", "status", "logs", "wait", "input", "restart", "stop", "remove", "attach", "signal"} {
		t.Run(command, func(t *testing.T) {
			stdout, stderr, err := runCompletionForTest(t, command, "--generate-shell-completion")
			if err != nil {
				t.Fatalf("%s completion: %v", command, err)
			}
			if stdout != want || stderr != "" {
				t.Fatalf("%s completion = stdout %q stderr %q, want %q and no stderr", command, stdout, stderr, want)
			}
		})
	}

	// signal takes a signal name second, so only its first positional
	// completes process names.
	stdout, stderr, err := runCompletionForTest(t, "signal", "alpha", "--generate-shell-completion")
	if err != nil {
		t.Fatalf("completed signal name: %v", err)
	}
	if stdout != "" || stderr != "" {
		t.Fatalf("signal after NAME completion = stdout %q stderr %q, want empty", stdout, stderr)
	}

	stdout, stderr, err = runCompletionForTest(t, "status", "alpha", "--generate-shell-completion")
	if err != nil {
		t.Fatalf("completed status name: %v", err)
	}
	if stdout != "" || stderr != "" {
		t.Fatalf("status after NAME completion = stdout %q stderr %q, want empty", stdout, stderr)
	}

	stdout, stderr, err = runCompletionForTest(t, "start", "--json", "--generate-shell-completion")
	if err != nil {
		t.Fatalf("flag before NAME completion: %v", err)
	}
	if stdout != want || stderr != "" {
		t.Fatalf("flag before NAME completion = stdout %q stderr %q, want %q and no stderr", stdout, stderr, want)
	}

	stdout, stderr, err = runCompletionForTest(t, "start", "--project", projectRoot, "--generate-shell-completion")
	if err != nil || stdout != want || stderr != "" {
		t.Fatalf("valued flag before NAME completion = err %v stdout %q stderr %q, want %q and no stderr", err, stdout, stderr, want)
	}

	stdout, stderr, err = runCompletionForTest(t, "start", "--project="+projectRoot, "--generate-shell-completion")
	if err != nil || stdout != "" || stderr != "" {
		t.Fatalf("active equal-valued flag completion = err %v stdout %q stderr %q, want no NAME candidates", err, stdout, stderr)
	}

	stdout, stderr, err = runNamePositionCompletionForTest(t, "start", "--project="+projectRoot, "--generate-shell-completion")
	if err != nil || stdout != want || stderr != "" {
		t.Fatalf("equal-valued flag before NAME completion = err %v stdout %q stderr %q, want %q and no stderr", err, stdout, stderr, want)
	}

	stdout, stderr, err = runNamePositionCompletionForTest(t, "--project", "--generate-shell-completion")
	if err != nil || stdout != "" || stderr != "" {
		t.Fatalf("active non-dash flag value completion = err %v stdout %q stderr %q, want no command candidates", err, stdout, stderr)
	}

	stdout, stderr, err = runCompletionForTest(t, "start", "--jso", "--generate-shell-completion")
	if err != nil || !strings.Contains(stdout, "--json") || strings.Contains(stdout, "alpha") || stderr != "" {
		t.Fatalf("flag completion = err %v stdout %q stderr %q, want --json only", err, stdout, stderr)
	}

	stdout, stderr, err = runCompletionForTest(t, "up", "--pro", "--generate-shell-completion")
	if err != nil || !strings.Contains(stdout, "--project") || strings.Contains(stdout, "alpha") || stderr != "" {
		t.Fatalf("persistent flag completion = err %v stdout %q stderr %q, want --project only", err, stdout, stderr)
	}

	stdout, stderr, err = runCompletionForTest(t, "start", "alpha", "--j", "--generate-shell-completion")
	if err != nil || !strings.Contains(stdout, "--json") || strings.Contains(stdout, "alpha") || stderr != "" {
		t.Fatalf("final flag completion = err %v stdout %q stderr %q, want --json only", err, stdout, stderr)
	}

	stdout, stderr, err = runCompletionForTest(t, "input", "--text", "-payload", "--generate-shell-completion")
	if err != nil || stdout != "" || stderr != "" {
		t.Fatalf("flag value completion = err %v stdout %q stderr %q, want no NAME candidates", err, stdout, stderr)
	}

	stdout, stderr, err = runNamePositionCompletionForTest(t, "input", "--text", "-payload", "--generate-shell-completion")
	if err != nil || stdout != want || stderr != "" {
		t.Fatalf("dash-leading flag value before NAME completion = err %v stdout %q stderr %q, want %q and no stderr", err, stdout, stderr, want)
	}

	if !reflect.DeepEqual(completionManifestNames(manifestState{defs: []project.Definition{{Name: "z"}, {Name: "a"}, {Name: "z"}}}), []string{"a", "z"}) {
		t.Fatal("manifest completion helper did not sort and deduplicate names")
	}
}

func TestCompletionIsQuietAndInert(t *testing.T) {
	projectRoot := stopShutdownTestProject(t)
	runtimeParent, err := os.MkdirTemp("/tmp", "h-comp-")
	if err != nil {
		t.Fatalf("create runtime parent: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtimeParent) })
	runtimeDir := filepath.Join(runtimeParent, "runtime")
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	writeManifestCLITestFile(t, projectRoot, `version: 1
processes:
  beta:
    argv: [echo, beta]
  alpha:
    argv: [echo, alpha]
`)

	stdout, stderr, err := runCompletionForTest(t, "start", "--generate-shell-completion")
	if err != nil || stdout != "alpha\nbeta\n" || stderr != "" {
		t.Fatalf("absent daemon completion = err %v stdout %q stderr %q", err, stdout, stderr)
	}
	if _, statErr := os.Stat(runtimeDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("completion started or touched daemon runtime: %v", statErr)
	}

	writeManifestCLITestFile(t, projectRoot, "version: 1\nprocesses:\n  broken: [\n")
	stdout, stderr, err = runCompletionForTest(t, "start", "--generate-shell-completion")
	if err != nil || stdout != "" || stderr != "" {
		t.Fatalf("manifest error completion = err %v stdout %q stderr %q, want quiet empty output", err, stdout, stderr)
	}
	if _, statErr := os.Stat(runtimeDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("manifest error completion touched daemon runtime: %v", statErr)
	}

	malformedParent, err := os.MkdirTemp("/tmp", "h-comp-")
	if err != nil {
		t.Fatalf("create malformed runtime parent: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(malformedParent) })
	malformedRuntime := filepath.Join(malformedParent, "runtime-file")
	if err := os.WriteFile(malformedRuntime, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("create malformed runtime path: %v", err)
	}
	t.Setenv("HUM_RUNTIME_DIR", malformedRuntime)
	writeManifestCLITestFile(t, projectRoot, `version: 1
processes:
  broken-path:
    argv: [echo, broken-path]
`)
	stdout, stderr, err = runCompletionForTest(t, "start", "--generate-shell-completion")
	if err != nil || stdout != "" || stderr != "" {
		t.Fatalf("malformed runtime completion = err %v stdout %q stderr %q, want quiet empty output", err, stdout, stderr)
	}
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)

	writeManifestCLITestFile(t, projectRoot, `version: 1
processes:
  alpha:
    argv: [echo, alpha]
`)
	mismatchRuntime, err := os.MkdirTemp("/tmp", "h-comp-mismatch-")
	if err != nil {
		t.Fatalf("create mismatched daemon runtime: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(mismatchRuntime) })
	mismatch, err := daemon.NewServer(daemon.Config{RuntimeDir: mismatchRuntime, WireVersion: 999})
	if err != nil {
		t.Fatalf("create mismatched daemon: %v", err)
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- mismatch.Serve(context.Background()) }()
	readyCtx, cancel := context.WithTimeout(context.Background(), daemonStartupTimeout)
	if err := mismatch.WaitReady(readyCtx); err != nil {
		cancel()
		_ = mismatch.Close()
		<-serveDone
		t.Fatalf("mismatched daemon readiness: %v", err)
	}
	cancel()
	t.Setenv("HUM_RUNTIME_DIR", mismatchRuntime)
	t.Cleanup(func() {
		_ = mismatch.Close()
		select {
		case <-serveDone:
		case <-time.After(3 * time.Second):
			t.Error("mismatched daemon did not exit during cleanup")
		}
	})

	stdout, stderr, err = runCompletionForTest(t, "start", "--generate-shell-completion")
	if err != nil || stdout != "" || stderr != "" {
		t.Fatalf("daemon error completion = err %v stdout %q stderr %q, want quiet empty output", err, stdout, stderr)
	}
}

func TestCompletionDocs(t *testing.T) {
	readme, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	design, err := os.ReadFile("../../docs/design.md")
	if err != nil {
		t.Fatalf("read docs/design.md: %v", err)
	}
	for _, phrase := range []string{
		"source <(hum completion bash)",
		"source <(hum completion zsh)",
		"hum completion fish > ~/.config/fish/completions/hum.fish",
	} {
		if !strings.Contains(string(readme), phrase) {
			t.Errorf("README.md missing copy-pasteable command %q", phrase)
		}
	}
	for _, phrase := range []string{"hum completion bash|zsh|fish", "never starts a daemon", "retained runtime records"} {
		if !strings.Contains(string(design), phrase) {
			t.Errorf("docs/design.md missing %q", phrase)
		}
	}
}
