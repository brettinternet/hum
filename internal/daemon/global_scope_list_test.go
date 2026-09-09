package daemon

import (
	"path/filepath"
	"testing"
	"time"

	"hum/internal/app"
)

// TestGlobalScopeAllListingIsDeduplicated pins the all-scope listing against a
// global record. The server tracks a global record under the empty root, so
// listing that root as a project would rediscover the caller's own root and
// return every project record twice.
func TestGlobalScopeAllListingIsDeduplicated(t *testing.T) {
	server := testServer(t, Config{RuntimeDir: filepath.Join(shortRuntimeDir(t), "runtime"), StopGrace: 50 * time.Millisecond})
	// The server tracks the global record under the empty root, which resolves
	// through the process working directory. Using that same root as the
	// project root is what exposes a rediscovered duplicate listing.
	root, err := app.DiscoverProjectRoot("")
	if err != nil {
		t.Fatal(err)
	}
	sleeper := []string{"/bin/sh", "-c", "trap 'exit 0' TERM; while :; do sleep 1; done"}
	env := []string{"PATH=/usr/bin:/bin"}

	global, err := server.Supervisor().Start(app.StartRequest{Name: "global-one", Scope: app.ScopeGlobal, Cwd: root, Argv: sleeper, Env: env})
	if err != nil {
		t.Fatalf("start global: %v", err)
	}
	// Dispatch tracks every observed record; the global one lands under the
	// empty root exactly as a real --global run would.
	server.trackProcess(global)
	for _, name := range []string{"project-one", "project-two"} {
		if _, err := server.Supervisor().Start(app.StartRequest{Name: name, Root: root, Cwd: root, Argv: sleeper, Env: env}); err != nil {
			t.Fatalf("start %s: %v", name, err)
		}
	}

	items, err := server.listProcessesScoped(root, app.ScopeProject, true, false)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	counts := make(map[string]int, len(items))
	for _, item := range items {
		counts[item.Scope+"|"+item.Root+"|"+item.Name]++
	}
	if len(items) != 3 {
		t.Fatalf("all-scope list returned %d records, want 3: %v", len(items), counts)
	}
	for key, count := range counts {
		if count != 1 {
			t.Errorf("record %s appears %d times, want 1", key, count)
		}
	}
}
