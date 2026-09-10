package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStartupBudgetIncludesEveryRecordedGroup(t *testing.T) {
	runtimeDir := filepath.Join(t.TempDir(), "runtime")
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	paths := NewRuntimePaths(runtimeDir)
	state := RuntimeState{
		Version: RuntimeStateVersion,
		Daemon:  RuntimeIdentity{PID: 2147483646, StartIdentity: "dead:1"},
		Groups: []RuntimeGroup{
			{ProjectRoot: t.TempDir(), Name: "one", LeaderPID: 2147483645, PGID: 2147483645, StartIdentity: "dead:2"},
			{ProjectRoot: t.TempDir(), Name: "two", LeaderPID: 2147483644, PGID: 2147483644, StartIdentity: "dead:3"},
		},
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.State, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := StartupBudget(paths, 3*time.Second, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if want := 17 * time.Second; got != want {
		t.Fatalf("StartupBudget() = %s, want %s", got, want)
	}
}

func TestStartupBudgetWithoutStateIsDialSlack(t *testing.T) {
	got, err := StartupBudget(NewRuntimePaths(t.TempDir()), time.Second, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got != 5*time.Second {
		t.Fatalf("StartupBudget() = %s, want 5s", got)
	}
}
