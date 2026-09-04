package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"hum/internal/project"
)

func TestInitGeneratedHuman(t *testing.T) {
	root := stopShutdownTestProject(t)
	runtimeDir := initTestRuntime(t)
	writeDiscoveredBin(t, root)

	stdout, stderr, err := stopShutdownRun(t, "init")
	if err != nil {
		t.Fatalf("hum init: %v (stdout=%q stderr=%q)", err, stdout, stderr)
	}
	path := filepath.Join(root, "hum.yaml")
	initAssertOutput(t, stdout, path, string(project.InitOutcomeGenerated))
	if !strings.Contains(stdout, "next_command: hum up") {
		t.Fatalf("human init output missing next command: %q", stdout)
	}
	if stderr != "" {
		t.Fatalf("human init stderr = %q, want empty", stderr)
	}
	initAssertManifest(t, root)
	initAssertMode(t, path)
	assertRuntimeDirEmpty(t, runtimeDir)
}

func TestInitGeneratedJSON(t *testing.T) {
	root := stopShutdownTestProject(t)
	runtimeDir := initTestRuntime(t)
	writeDiscoveredBin(t, root)

	stdout, stderr, err := stopShutdownRun(t, "init", "--json")
	if err != nil {
		t.Fatalf("hum init --json: %v (stdout=%q stderr=%q)", err, stdout, stderr)
	}
	var result initJSON
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("decode init JSON: %v (stdout=%q)", err, stdout)
	}
	if result.Path != filepath.Join(root, "hum.yaml") {
		t.Fatalf("init JSON path = %q, want %q", result.Path, filepath.Join(root, "hum.yaml"))
	}
	if result.Outcome != project.InitOutcomeGenerated {
		t.Fatalf("init JSON outcome = %q, want %q", result.Outcome, project.InitOutcomeGenerated)
	}
	if result.NextCommand != initNextCommand {
		t.Fatalf("init JSON next command = %q, want %q", result.NextCommand, initNextCommand)
	}
	if len(result.Candidates) != 1 {
		t.Fatalf("init JSON candidates = %#v, want one candidate", result.Candidates)
	}
	candidate := result.Candidates[0]
	if candidate.Name != "dev" || candidate.Source != "bin_dev" || !reflect.DeepEqual(candidate.Argv, []string{"./bin/dev"}) {
		t.Fatalf("init JSON candidate = %#v, want dev/bin_dev/./bin/dev", candidate)
	}
	if stderr != "" {
		t.Fatalf("JSON init stderr = %q, want empty", stderr)
	}
	initAssertManifest(t, root)
	assertRuntimeDirEmpty(t, runtimeDir)
}

func TestInitTemplateHuman(t *testing.T) {
	root := stopShutdownTestProject(t)
	runtimeDir := initTestRuntime(t)

	stdout, stderr, err := stopShutdownRun(t, "init")
	if err != nil {
		t.Fatalf("hum init with no candidate: %v (stdout=%q stderr=%q)", err, stdout, stderr)
	}
	path := filepath.Join(root, "hum.yaml")
	initAssertOutput(t, stdout, path, string(project.InitOutcomeTemplate))
	if !strings.Contains(stdout, "next_command: hum up") {
		t.Fatalf("template human output missing next command: %q", stdout)
	}
	if stderr != "" {
		t.Fatalf("template human stderr = %q, want empty", stderr)
	}
	initAssertManifest(t, root)
	initAssertMode(t, path)
	assertRuntimeDirEmpty(t, runtimeDir)
}

func TestInitTemplateJSON(t *testing.T) {
	root := stopShutdownTestProject(t)
	runtimeDir := initTestRuntime(t)
	writeDiscoveredBin(t, root)
	if err := os.WriteFile(filepath.Join(root, "Makefile"), []byte("dev:\n\t@echo dev\n"), 0o600); err != nil {
		t.Fatalf("write Makefile: %v", err)
	}

	stdout, stderr, err := stopShutdownRun(t, "init", "--json")
	if err != nil {
		t.Fatalf("hum init --json with ambiguity: %v (stdout=%q stderr=%q)", err, stdout, stderr)
	}
	var result initJSON
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("decode template init JSON: %v (stdout=%q)", err, stdout)
	}
	if result.Path != filepath.Join(root, "hum.yaml") {
		t.Fatalf("template JSON path = %q, want %q", result.Path, filepath.Join(root, "hum.yaml"))
	}
	if result.Outcome != project.InitOutcomeTemplate {
		t.Fatalf("template JSON outcome = %q, want %q", result.Outcome, project.InitOutcomeTemplate)
	}
	if result.NextCommand != initNextCommand {
		t.Fatalf("template JSON next command = %q, want %q", result.NextCommand, initNextCommand)
	}
	if len(result.Candidates) != 2 {
		t.Fatalf("template JSON candidates = %#v, want two candidates", result.Candidates)
	}
	wantCandidates := []initCandidateJSON{
		{Name: "dev", Source: "make", Argv: []string{"make", "dev"}},
		{Name: "dev", Source: "bin_dev", Argv: []string{"./bin/dev"}},
	}
	if !reflect.DeepEqual(result.Candidates, wantCandidates) {
		t.Fatalf("template JSON candidates = %#v, want %#v", result.Candidates, wantCandidates)
	}
	if stderr != "" {
		t.Fatalf("template JSON stderr = %q, want empty", stderr)
	}
	initAssertManifest(t, root)
	assertRuntimeDirEmpty(t, runtimeDir)
}

func TestInitNoOverwrite(t *testing.T) {
	root := stopShutdownTestProject(t)
	runtimeDir := initTestRuntime(t)
	path := filepath.Join(root, "hum.yaml")
	contents := []byte("version: 1\nprocesses: {}\n")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("write existing hum.yaml: %v", err)
	}

	stdout, stderr, err := stopShutdownRun(t, "init", "--json")
	if err == nil || initCLIExitCode(err) != 1 {
		t.Fatalf("hum init existing manifest: err=%v code=%d stdout=%q stderr=%q, want exit 1", err, initCLIExitCode(err), stdout, stderr)
	}
	var result initJSON
	if decodeErr := json.Unmarshal([]byte(stdout), &result); decodeErr != nil {
		t.Fatalf("decode existing-manifest init JSON: %v (stdout=%q)", decodeErr, stdout)
	}
	if result.Path != path || result.Outcome != project.InitOutcomeExists || result.NextCommand != initNextCommand || len(result.Candidates) != 0 {
		t.Fatalf("existing-manifest init JSON = %#v", result)
	}
	if err.Error() != "" {
		t.Fatalf("existing-manifest error = %q, want empty Exit error", err.Error())
	}
	if stderr != "" {
		t.Fatalf("existing-manifest stderr = %q, want empty", stderr)
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("read existing hum.yaml: %v", readErr)
	}
	if !reflect.DeepEqual(got, contents) {
		t.Fatalf("existing hum.yaml changed from %q to %q", contents, got)
	}
	assertRuntimeDirEmpty(t, runtimeDir)
}

func TestInitDiscoveryFailure(t *testing.T) {
	root := stopShutdownTestProject(t)
	runtimeDir := initTestRuntime(t)
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte("{\n"), 0o600); err != nil {
		t.Fatalf("write malformed package.json: %v", err)
	}

	stdout, stderr, err := stopShutdownRun(t, "init")
	if err == nil || initCLIExitCode(err) != 1 {
		t.Fatalf("hum init malformed discovery: err=%v code=%d stdout=%q stderr=%q, want exit 1", err, initCLIExitCode(err), stdout, stderr)
	}
	var configuration *project.ConfigurationError
	if !errors.As(err, &configuration) {
		t.Fatalf("hum init error = %v, want ConfigurationError", err)
	}
	if stdout != "" || stderr != "" {
		t.Fatalf("discovery failure output: stdout=%q stderr=%q, want empty", stdout, stderr)
	}
	if _, statErr := os.Lstat(filepath.Join(root, "hum.yaml")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("hum.yaml after discovery failure: %v, want absent", statErr)
	}
	assertRuntimeDirEmpty(t, runtimeDir)
}

func TestInitRootFailure(t *testing.T) {
	root := t.TempDir()
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir to test root: %v", err)
	}
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	if err := os.RemoveAll(root); err != nil {
		t.Fatalf("remove test root: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldwd); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})

	stdout, stderr, runErr := stopShutdownRun(t, "init")
	if runErr == nil || initCLIExitCode(runErr) != 1 {
		t.Fatalf("hum init missing root: err=%v code=%d stdout=%q stderr=%q, want exit 1", runErr, initCLIExitCode(runErr), stdout, stderr)
	}
	if stdout != "" || stderr != "" {
		t.Fatalf("root failure output: stdout=%q stderr=%q, want empty", stdout, stderr)
	}
	assertRuntimeDirEmpty(t, runtimeDir)
}

func TestInitDiscoveryGuidance(t *testing.T) {
	tests := []struct {
		name      string
		ambiguous bool
	}{
		{name: "no candidate"},
		{name: "ambiguity", ambiguous: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := stopShutdownTestProject(t)
			runtimeDir := initTestRuntime(t)
			if tt.ambiguous {
				writeDiscoveredBin(t, root)
				if err := os.WriteFile(filepath.Join(root, "Makefile"), []byte("dev:\n\t@echo dev\n"), 0o600); err != nil {
					t.Fatalf("write Makefile: %v", err)
				}
			}

			_, _, err := stopShutdownRun(t, "start", "dev")
			if err == nil || !strings.Contains(err.Error(), "hum init") {
				t.Fatalf("guidance error = %v, want hum init guidance", err)
			}
			assertRuntimeDirEmpty(t, runtimeDir)
		})
	}
}

func TestInitRejectsPositionalArguments(t *testing.T) {
	root := stopShutdownTestProject(t)
	runtimeDir := initTestRuntime(t)

	stdout, stderr, err := stopShutdownRun(t, "init", "extra")
	if err == nil || initCLIExitCode(err) != 1 {
		t.Fatalf("hum init positional argument: err=%v code=%d stdout=%q stderr=%q, want exit 1", err, initCLIExitCode(err), stdout, stderr)
	}
	if !strings.Contains(err.Error(), "init accepts no positional arguments") {
		t.Fatalf("positional argument error = %v", err)
	}
	if _, statErr := os.Lstat(filepath.Join(root, "hum.yaml")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("hum.yaml after positional argument: %v, want absent", statErr)
	}
	assertRuntimeDirEmpty(t, runtimeDir)
}

func initTestRuntime(t *testing.T) string {
	t.Helper()
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	return runtimeDir
}

func initAssertOutput(t *testing.T, output, path, outcome string) {
	t.Helper()
	if !strings.Contains(output, "path: "+path) {
		t.Fatalf("init output missing path %q: %q", path, output)
	}
	if !strings.Contains(output, "outcome: "+outcome) {
		t.Fatalf("init output missing outcome %q: %q", outcome, output)
	}
}

func initAssertManifest(t *testing.T, root string) {
	t.Helper()
	if _, err := project.LoadDefinitions(root); err != nil {
		t.Fatalf("load generated hum.yaml: %v", err)
	}
}

func initAssertMode(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("%s mode = %04o, want 0600", path, got)
	}
}

func initCLIExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitCoder interface{ ExitCode() int }
	if errors.As(err, &exitCoder) {
		return exitCoder.ExitCode()
	}
	return 1
}
