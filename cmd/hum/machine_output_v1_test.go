package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type machineOutputResult struct {
	stdout string
	stderr string
	code   int
}

func TestBuiltCLIMachineOutputV1(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "hum")
	build := exec.Command("go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build hum: %v\n%s", err, output)
	}

	project := t.TempDir()
	if err := os.Mkdir(filepath.Join(project, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	runtimeDir := t.TempDir()

	aggregate := runMachineOutputCommand(t, binary, project, runtimeDir, "list", "--json")
	if aggregate.code != 0 || aggregate.stderr != "" {
		t.Fatalf("list --json = code %d, stdout=%q stderr=%q", aggregate.code, aggregate.stdout, aggregate.stderr)
	}
	assertBuiltMachineOutputV1(t, aggregate.stdout, "processes")

	human := runMachineOutputCommand(t, binary, project, runtimeDir, "list")
	if human.code != 0 || strings.Contains(human.stdout, "schema_version") {
		t.Fatalf("human list changed by machine schema = code %d, stdout=%q stderr=%q", human.code, human.stdout, human.stderr)
	}

	terminal := runMachineOutputCommand(t, binary, project, runtimeDir, "status", "missing", "--json")
	if terminal.code != 1 || terminal.stderr != "" {
		t.Fatalf("JSON error exit = code %d, stdout=%q stderr=%q; want code 1 and empty stderr", terminal.code, terminal.stdout, terminal.stderr)
	}
	assertBuiltMachineOutputV1(t, terminal.stdout, "error")

	t.Cleanup(func() {
		_ = runMachineOutputCommand(t, binary, project, runtimeDir, "shutdown", "--stop-processes", "--json")
	})
	attached := runMachineOutputCommand(t, binary, project, runtimeDir, "run", "raw", "--", "/bin/sh", "-c", "printf 'raw-child-output\\n'; exit 7")
	if attached.code != 7 || attached.stdout != "raw-child-output\n" || attached.stderr != "" {
		t.Fatalf("attached run = code %d, stdout=%q stderr=%q; want raw output and code 7", attached.code, attached.stdout, attached.stderr)
	}
}

func runMachineOutputCommand(t *testing.T, binary, project, runtimeDir string, args ...string) machineOutputResult {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Dir = project
	cmd.Env = append(os.Environ(), "HUM_RUNTIME_DIR="+runtimeDir)
	stdout, stderr := new(strings.Builder), new(strings.Builder)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("run hum %v: %v", args, err)
		}
	}
	return machineOutputResult{stdout: stdout.String(), stderr: stderr.String(), code: code}
}

func assertBuiltMachineOutputV1(t *testing.T, raw string, required ...string) {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &object); err != nil {
		t.Fatalf("decode built CLI JSON: %v (%q)", err, raw)
	}
	var version int
	if err := json.Unmarshal(object["schema_version"], &version); err != nil || version != 1 {
		t.Fatalf("schema_version = %d, %v; want 1 (%q)", version, err, raw)
	}
	for _, field := range required {
		if _, ok := object[field]; !ok {
			t.Errorf("built CLI JSON omitted %q: %q", field, raw)
		}
	}
}
