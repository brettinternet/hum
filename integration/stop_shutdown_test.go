//go:build darwin || linux

package integration

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"hum/internal/testutil"
)

const (
	stopitPollInterval = 10 * time.Millisecond
	stopitReadyWait    = 8 * time.Second
	stopitShutdownWait = 10 * time.Second
	stopitCleanupWait  = 2 * time.Second
)

type stopitRunProcess struct {
	Name string `json:"name"`
	PID  int    `json:"pid"`
}

type stopitListedProcess struct {
	Name  string `json:"name"`
	Root  string `json:"root"`
	PID   int    `json:"pid"`
	PGID  int    `json:"pgid"`
	State string `json:"state"`
}

type stopitListResponse struct {
	Processes []stopitListedProcess `json:"processes"`
}

type stopitActionResult struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

type stopitTree struct {
	name          string
	marker        string
	release       string
	parentPID     int
	childPID      int
	grandchildPID int
	pgid          int
}

func TestStopTree(t *testing.T) {
	stopitRequireUnix(t)

	hum := testutil.BuildHum(t)
	fixture := testutil.BuildFixture(t)
	projectRoot := stopitCanonicalTempDir(t)
	runtimeDir := testutil.RuntimeDir(t)
	env := testutil.RuntimeEnv(runtimeDir, "HUM_STOP_GRACE=250ms")

	testutil.Start(t, hum, projectRoot, env, "serve")
	paths := stopitRuntimePaths(runtimeDir)
	testutil.WaitForFile(t, paths.ready, stopitReadyWait)
	daemonPID := stopitReadPID(t, paths.pid)
	if !testutil.ProcessAlive(daemonPID) {
		t.Fatalf("daemon PID %d is not alive after readiness", daemonPID)
	}

	var groups []int
	t.Cleanup(func() { stopitCleanupGroups(groups) })
	tree := stopitLaunchTree(t, hum, fixture, projectRoot, env, "tree", "ignore-term", &groups, nil)
	stopitAssertTreeAlive(t, tree)

	stopCommand := testutil.Start(t, hum, projectRoot, env, "stop", "--json", tree.name)
	if err := stopCommand.Wait(stopitShutdownWait); err != nil {
		t.Fatalf("stop: %v (stdout=%q stderr=%q)", err, stopCommand.Stdout(), stopCommand.Stderr())
	}
	stopOutput := stopCommand.Stdout()
	var stopped stopitActionResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(stopOutput)), &stopped); err != nil {
		t.Fatalf("decode stop result: %v (stdout=%q)", err, stopOutput)
	}
	if stopped.Name != tree.name || stopped.Status != "stopped" {
		t.Fatalf("stop result = %+v, want name %q and status stopped", stopped, tree.name)
	}

	for _, marker := range stopitTreeTermMarkers(tree) {
		testutil.WaitForText(t, marker, "term", stopitReadyWait)
	}
	testutil.WaitForProcessGone(t, tree.parentPID, stopitShutdownWait)
	testutil.WaitForProcessGone(t, tree.childPID, stopitShutdownWait)
	testutil.WaitForProcessGone(t, tree.grandchildPID, stopitShutdownWait)
	testutil.WaitForProcessGroupGone(t, tree.pgid, stopitShutdownWait)

	if !testutil.ProcessAlive(daemonPID) {
		t.Fatal("daemon exited while stopping one process tree")
	}
	stopitRequirePath(t, paths.socket)
	stopitRequirePath(t, paths.pid)
	stopitRequirePath(t, paths.ready)
}

func TestShutdown(t *testing.T) {
	stopitRequireUnix(t)

	hum := testutil.BuildHum(t)
	fixture := testutil.BuildFixture(t)
	projectRoot := stopitCanonicalTempDir(t)
	runtimeDir := testutil.RuntimeDir(t)
	// The longer grace period makes the TERM barrier observable before the
	// forced shutdown escalates the ignore-term tree to SIGKILL.
	env := testutil.RuntimeEnv(runtimeDir, "HUM_STOP_GRACE=3s")

	testutil.Start(t, hum, projectRoot, env, "serve")
	paths := stopitRuntimePaths(runtimeDir)
	testutil.WaitForFile(t, paths.ready, stopitReadyWait)
	daemonPID := stopitReadPID(t, paths.pid)
	if !testutil.ProcessAlive(daemonPID) {
		t.Fatalf("daemon PID %d is not alive after readiness", daemonPID)
	}

	var groups []int
	var releases []string
	t.Cleanup(func() {
		for _, release := range releases {
			_ = os.WriteFile(release, []byte("release"), 0o600)
		}
		stopitCleanupGroups(groups)
	})

	graceful := stopitLaunchTree(t, hum, fixture, projectRoot, env, "graceful", "graceful", &groups, &releases)
	stubborn := stopitLaunchTree(t, hum, fixture, projectRoot, env, "stubborn", "ignore-term", &groups, &releases)
	stopitAssertTreeAlive(t, graceful)
	stopitAssertTreeAlive(t, stubborn)

	refused := testutil.Run(t, hum, projectRoot, env, "shutdown")
	if refused.Code == 0 {
		t.Fatalf("shutdown unexpectedly succeeded with active trees: stdout=%q stderr=%q", refused.Stdout, refused.Stderr)
	}
	refusalText := refused.Stdout + refused.Stderr
	if !strings.Contains(refusalText, "active supervised processes prevent daemon shutdown") {
		t.Fatalf("shutdown refusal omitted active-process error: %q", refusalText)
	}
	for _, name := range []string{graceful.name, stubborn.name} {
		want := projectRoot + ": " + name
		if !strings.Contains(refusalText, want) {
			t.Errorf("shutdown refusal missing exact entry %q: %q", want, refusalText)
		}
	}
	if !testutil.ProcessAlive(daemonPID) {
		t.Fatal("daemon exited after refusing shutdown")
	}
	stopitRequirePath(t, paths.socket)
	stopitRequirePath(t, paths.pid)
	stopitRequirePath(t, paths.ready)
	stopitAssertTreeAlive(t, graceful)
	stopitAssertTreeAlive(t, stubborn)

	forced := testutil.Start(t, hum, projectRoot, env, "shutdown", "--stop-processes")
	for _, marker := range stopitTreeTermMarkers(graceful) {
		testutil.WaitForText(t, marker, "term", stopitReadyWait)
	}
	for _, marker := range stopitTreeTermMarkers(stubborn) {
		testutil.WaitForText(t, marker, "term", stopitReadyWait)
	}

	// Both trees must still be alive while the graceful TERM window is open.
	// The graceful tree is blocked on .release; the stubborn tree ignores TERM,
	// so either one would expose an implementation that skipped the grace.
	if forced.Exited() {
		t.Fatalf("forced shutdown exited before graceful release: stdout=%q stderr=%q", forced.Stdout(), forced.Stderr())
	}
	if !testutil.ProcessAlive(daemonPID) {
		t.Fatal("daemon exited before graceful release")
	}
	stopitRequirePath(t, paths.socket)
	stopitRequirePath(t, paths.pid)
	stopitRequirePath(t, paths.ready)
	stopitAssertTreeAlive(t, graceful)
	stopitAssertTreeAlive(t, stubborn)

	if err := os.WriteFile(graceful.release, []byte("release"), 0o600); err != nil {
		t.Fatalf("release graceful tree: %v", err)
	}
	if err := forced.Wait(stopitShutdownWait); err != nil {
		t.Fatalf("forced shutdown: %v (stdout=%q stderr=%q)", err, forced.Stdout(), forced.Stderr())
	}
	if forced.Stdout() != "hum daemon shut down\n" {
		t.Fatalf("forced shutdown stdout = %q, want exact success message", forced.Stdout())
	}
	if forced.Stderr() != "" {
		t.Fatalf("forced shutdown stderr = %q, want empty", forced.Stderr())
	}

	for _, tree := range []stopitTree{graceful, stubborn} {
		testutil.WaitForProcessGone(t, tree.parentPID, stopitShutdownWait)
		testutil.WaitForProcessGone(t, tree.childPID, stopitShutdownWait)
		testutil.WaitForProcessGone(t, tree.grandchildPID, stopitShutdownWait)
		testutil.WaitForProcessGroupGone(t, tree.pgid, stopitShutdownWait)
	}
	stopitWaitForPathGone(t, paths.socket, stopitShutdownWait)
	stopitWaitForPathGone(t, paths.pid, stopitShutdownWait)
	stopitWaitForPathGone(t, paths.ready, stopitShutdownWait)
	testutil.WaitForProcessGone(t, daemonPID, stopitShutdownWait)
	if !forced.Exited() {
		t.Fatal("forced shutdown process was not reaped")
	}
}

// stopitLaunchTree starts a detached fixture tree and records every PID marker
// before returning. The fallback group registration happens as soon as the
// detached run reports its leader, so a failed readiness assertion cannot leak
// a process group.
func stopitLaunchTree(t *testing.T, hum, fixture, projectRoot string, env []string, name, mode string, groups *[]int, releases *[]string) stopitTree {
	t.Helper()
	marker := filepath.Join(t.TempDir(), "tree")
	release := marker + ".release"
	if releases != nil {
		*releases = append(*releases, release)
	}

	started := testutil.Run(t, hum, projectRoot, env, "run", name, "--detach", "--json", "--", fixture, "tree", marker, mode)
	if started.Code != 0 {
		t.Fatalf("run %s tree exit code = %d: stdout=%q stderr=%q err=%v", name, started.Code, started.Stdout, started.Stderr, started.Err)
	}
	var runProcess stopitRunProcess
	if err := json.Unmarshal([]byte(strings.TrimSpace(started.Stdout)), &runProcess); err != nil {
		t.Fatalf("decode run %s result: %v (stdout=%q)", name, err, started.Stdout)
	}
	if runProcess.Name != name || runProcess.PID <= 0 {
		t.Fatalf("run %s result = %+v, want positive leader PID", name, runProcess)
	}
	if groups != nil {
		*groups = append(*groups, runProcess.PID)
	}

	startedMarker := marker + ".started"
	testutil.WaitForFile(t, startedMarker, stopitReadyWait)
	tree := stopitTree{
		name:          name,
		marker:        marker,
		release:       release,
		parentPID:     stopitReadPID(t, marker+".parent.pid"),
		childPID:      stopitReadPID(t, marker+".child.pid"),
		grandchildPID: stopitReadPID(t, marker+".grandchild.pid"),
	}
	if tree.parentPID != runProcess.PID {
		t.Fatalf("tree %s parent PID = %d, want run PID %d", name, tree.parentPID, runProcess.PID)
	}
	listed := stopitLookupProcess(t, hum, projectRoot, env, name)
	if listed.Root != projectRoot {
		t.Fatalf("tree %s root = %q, want canonical project root %q", name, listed.Root, projectRoot)
	}
	if listed.PID != tree.parentPID {
		t.Fatalf("tree %s listed PID = %d, want marker PID %d", name, listed.PID, tree.parentPID)
	}
	if listed.PGID <= 0 {
		t.Fatalf("tree %s listed invalid process group %d", name, listed.PGID)
	}
	if listed.State != "running" {
		t.Fatalf("tree %s listed state = %q, want running", name, listed.State)
	}
	tree.pgid = listed.PGID
	if groups != nil {
		*groups = append(*groups, tree.pgid)
	}
	return tree
}

func stopitLookupProcess(t *testing.T, hum, projectRoot string, env []string, name string) stopitListedProcess {
	t.Helper()
	listed := testutil.Run(t, hum, projectRoot, env, "list", "--json")
	if listed.Code != 0 {
		t.Fatalf("list exit code = %d: stdout=%q stderr=%q err=%v", listed.Code, listed.Stdout, listed.Stderr, listed.Err)
	}
	var response stopitListResponse
	if err := json.Unmarshal([]byte(strings.TrimSpace(listed.Stdout)), &response); err != nil {
		t.Fatalf("decode list result: %v (stdout=%q)", err, listed.Stdout)
	}
	for _, process := range response.Processes {
		if process.Name == name {
			return process
		}
	}
	t.Fatalf("list omitted running process %q: %q", name, listed.Stdout)
	return stopitListedProcess{}
}

func stopitReadPID(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read PID marker %q: %v", path, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		t.Fatalf("PID marker %q = %q, want positive integer: %v", path, data, err)
	}
	return pid
}

func stopitAssertTreeAlive(t *testing.T, tree stopitTree) {
	t.Helper()
	for label, pid := range map[string]int{
		"parent":     tree.parentPID,
		"child":      tree.childPID,
		"grandchild": tree.grandchildPID,
	} {
		if !testutil.ProcessAlive(pid) {
			t.Fatalf("%s PID %d for %s is not alive", label, pid, tree.name)
		}
	}
	if !stopitProcessGroupAlive(tree.pgid) {
		t.Fatalf("process group %d for %s is not alive", tree.pgid, tree.name)
	}
}

func stopitTreeTermMarkers(tree stopitTree) []string {
	return []string{
		tree.marker + ".parent.term",
		tree.marker + ".child.term",
		tree.marker + ".grandchild.term",
	}
}

type stopitRuntimeFiles struct {
	socket string
	pid    string
	ready  string
}

func stopitRuntimePaths(runtimeDir string) stopitRuntimeFiles {
	return stopitRuntimeFiles{
		socket: filepath.Join(runtimeDir, "hum.sock"),
		pid:    filepath.Join(runtimeDir, "hum.pid"),
		ready:  filepath.Join(runtimeDir, "hum.ready"),
	}
}

func stopitCanonicalTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	canonical, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("canonicalize project directory %q: %v", dir, err)
	}
	return filepath.Clean(canonical)
}

func stopitRequireUnix(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("process-group integration requires macOS or Linux")
	}
}

func stopitRequirePath(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("runtime artifact %q is unavailable: %v", path, err)
	}
}

func stopitWaitForPathGone(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	if stopitWaitForCondition(timeout, func() bool {
		_, err := os.Stat(path)
		return errors.Is(err, os.ErrNotExist)
	}) {
		return
	}
	_, err := os.Stat(path)
	t.Fatalf("runtime artifact %q remained after %s (err=%v)", path, timeout, err)
}

func stopitWaitForCondition(timeout time.Duration, condition func() bool) bool {
	if condition() {
		return true
	}
	if timeout <= 0 {
		return false
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	poll := time.NewTicker(stopitPollInterval)
	defer poll.Stop()
	for {
		select {
		case <-poll.C:
			if condition() {
				return true
			}
		case <-deadline.C:
			return condition()
		}
	}
}

func stopitProcessGroupAlive(pgid int) bool {
	if pgid <= 0 {
		return false
	}
	err := syscall.Kill(-pgid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func stopitCleanupGroups(groups []int) {
	ownGroup, _ := syscall.Getpgid(os.Getpid())
	seen := make(map[int]struct{}, len(groups))
	for _, pgid := range groups {
		if pgid <= 1 || pgid == ownGroup {
			continue
		}
		if _, ok := seen[pgid]; ok {
			continue
		}
		seen[pgid] = struct{}{}
		if stopitProcessGroupAlive(pgid) {
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
		}
	}
	stopitWaitForGroupsGone(groups, stopitCleanupWait, ownGroup)
}

func stopitWaitForGroupsGone(groups []int, timeout time.Duration, ownGroup int) {
	stopitWaitForCondition(timeout, func() bool {
		for _, pgid := range groups {
			if pgid > 1 && pgid != ownGroup && stopitProcessGroupAlive(pgid) {
				return false
			}
		}
		return true
	})
}
