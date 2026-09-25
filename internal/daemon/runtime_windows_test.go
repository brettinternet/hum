//go:build windows

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"

	"hum/internal/app"
	"hum/internal/config"
	"hum/internal/process"
	"hum/internal/protocol"
)

func windowsRuntimeDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "runtime")
}

// Not parallel: changes process-wide APPDATA and LOCALAPPDATA.
func TestWindowsDefaultRuntimeDirIsAbsoluteWithoutAppData(t *testing.T) {
	t.Setenv("LOCALAPPDATA", "")
	t.Setenv("APPDATA", "")
	cfg, err := config.New(config.BuildOpts{}, config.Input{})
	if err != nil {
		t.Fatal(err)
	}
	dir := defaultRuntimeDir()
	if !filepath.IsAbs(dir) || cfg.RuntimeDir != dir {
		t.Fatalf("daemon default %q, config default %q; want one absolute runtime", dir, cfg.RuntimeDir)
	}
}

func TestWindowsTransportAndACL(t *testing.T) {
	t.Parallel()
	dir := windowsRuntimeDir(t)
	server, err := NewServer(Config{RuntimeDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(context.Background()) }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{server.Paths().Dir, server.Paths().Lock, server.Paths().State, server.Paths().PID} {
		if err := checkPrivateACL(name); err != nil {
			t.Fatalf("private ACL %s: %v", name, err)
		}
	}
	client, err := Dial(ctx, server.Paths().Socket)
	if err != nil {
		t.Fatal(err)
	}
	pipe, ok := client.conn.(*securePipeConn)
	if !ok {
		t.Fatalf("client connection type %T", client.conn)
	}
	sd, err := windows.GetSecurityInfo(pipe.handle, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkPrivateDescriptor(sd); err != nil {
		t.Fatalf("pipe ACL: %v", err)
	}
	items, err := client.List(ctx, protocol.NewListRequest(t.TempDir(), false, false))
	if err != nil || len(items) != 0 {
		t.Fatalf("list round trip: %+v, %v", items, err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-serveDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestWindowsConcurrentStartup(t *testing.T) {
	t.Parallel()
	dir := windowsRuntimeDir(t)
	owner, err := NewServer(Config{RuntimeDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	// A competing process must not become a second owner of the same runtime.
	cmd := exec.Command(os.Args[0], "-test.run=^TestWindowsStartupContender$")
	cmd.Env = append(os.Environ(), "HUM_WINDOWS_CONTENDER="+dir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("contender: %v: %s", err, out)
	}
	if strings.Contains(string(out), "FAIL") {
		t.Fatalf("contender output: %s", out)
	}
}
func TestWindowsStartupContender(t *testing.T) {
	t.Parallel()
	dir := os.Getenv("HUM_WINDOWS_CONTENDER")
	if dir == "" {
		return
	}
	if owner, err := NewServer(Config{RuntimeDir: dir}); err == nil {
		_ = owner.Close()
		t.Fatal("second owner acquired runtime")
	} else if !errors.Is(err, ErrAlreadyRunning) && !errors.Is(err, ErrRuntimeOwned) {
		t.Fatal(err)
	}
	t.Log("owner refused")
}

// Not parallel: simulates another user with a process-wide runtimeSIDOverride.
func TestWindowsForeignRuntimeAndPipeRefused(t *testing.T) {
	dir := windowsRuntimeDir(t)
	if err := ensurePrivateDir(dir); err != nil {
		t.Fatal(err)
	}
	sid := "S-1-5-18" // LocalSystem is not the interactive user.
	runtimeSIDOverride.Store(&sid)
	if _, err := NewServer(Config{RuntimeDir: dir}); err == nil {
		t.Fatal("foreign runtime accepted")
	}
	if _, err := StartupBudget(NewRuntimePaths(dir), time.Second, time.Second); err == nil {
		t.Fatal("foreign state accepted")
	}
	runtimeSIDOverride.Store(nil)
	t.Cleanup(func() { runtimeSIDOverride.Store(nil) })
	// An attacker can create the name first with a DACL permitting this user.
	// Neither its owner nor a permissive DACL may pass the client check.
	path := NewRuntimePaths(dir).Socket
	listener, err := winio.ListenPipe(path, &winio.PipeConfig{SecurityDescriptor: "D:P(A;;GA;;;WD)"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if client, err := Dial(ctx, path); err == nil {
		_ = client.Close()
		t.Fatal("foreign endpoint accepted")
	}
	if owner, err := NewServer(Config{RuntimeDir: dir}); err == nil {
		_ = owner.Close()
		t.Fatal("preempted endpoint accepted")
	}
}

func TestWindowsStaleOwnerRecovery(t *testing.T) {
	t.Parallel()
	dir := windowsRuntimeDir(t)
	if err := ensurePrivateDir(dir); err != nil {
		t.Fatal(err)
	}
	state := RuntimeState{Version: RuntimeStateVersion, Daemon: RuntimeIdentity{PID: 2147483646, StartIdentity: "dead"}, Groups: []RuntimeGroup{{Scope: "global", Name: "finished", LeaderPID: 2147483645, PGID: 2147483645, StartIdentity: "dead"}}}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(NewRuntimePaths(dir).State, data, 0o600); err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(Config{RuntimeDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	warnings := server.StartupWarnings()
	if len(warnings) != 1 || warnings[0].Outcome != "reclaimed" {
		t.Fatalf("startup warnings: %+v", warnings)
	}
	if !processAlive(os.Getpid()) {
		t.Fatal("unrelated process affected")
	}
}

// A real owner process is terminated without Close. Its child belongs to the
// kill-on-close Job Object; a fresh daemon must reconcile its durable record.
func TestWindowsCrashRecovery(t *testing.T) {
	t.Parallel()
	dir := windowsRuntimeDir(t)
	cmd := exec.Command(os.Args[0], "-test.run=^TestWindowsCrashOwnerHelper$")
	cmd.Env = append(os.Environ(), "HUM_WINDOWS_CRASH_OWNER="+dir)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	marker := filepath.Join(dir, "crash-child.pid")
	var childPID int
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(marker)
		if err == nil {
			childPID, err = strconv.Atoi(strings.TrimSpace(string(data)))
		}
		if err == nil && childPID > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if childPID <= 0 {
		t.Fatal("owner did not record its child")
	}
	if !processAlive(childPID) {
		t.Fatal("recorded child was not running before daemon crash")
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && processAlive(childPID) {
		time.Sleep(10 * time.Millisecond)
	}
	if processAlive(childPID) {
		t.Fatal("child survived crashed daemon's Job Object")
	}
	server, err := NewServer(Config{RuntimeDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	warnings := server.StartupWarnings()
	if len(warnings) != 1 || warnings[0].Name != "crash-child" || warnings[0].Outcome != "reclaimed" {
		t.Fatalf("crash recovery warnings: %+v", warnings)
	}
	if !processAlive(os.Getpid()) {
		t.Fatal("unrelated parent affected")
	}
}
func TestWindowsCrashOwnerHelper(t *testing.T) {
	t.Parallel()
	dir := os.Getenv("HUM_WINDOWS_CRASH_OWNER")
	if dir == "" || os.Getenv("HUM_WINDOWS_CRASH_CHILD") != "" {
		return
	}
	server, err := NewServer(Config{RuntimeDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	child, err := server.Supervisor().Start(app.StartRequest{
		Scope: app.ScopeGlobal, Name: "crash-child", Cwd: dir, Argv: []string{os.Args[0], "-test.run=^TestWindowsCrashChildHelper$"},
		Env: append(os.Environ(), "HUM_WINDOWS_CRASH_CHILD=1"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "crash-child.pid"), []byte(strconv.Itoa(child.PID)), 0o600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Second)
}
func TestWindowsCrashChildHelper(t *testing.T) {
	t.Parallel()
	if os.Getenv("HUM_WINDOWS_CRASH_CHILD") == "" {
		return
	}
	time.Sleep(30 * time.Second)
}

func TestWindowsReusedPIDAndUncertainChild(t *testing.T) {
	t.Parallel()
	identity, err := process.ProcessStartIdentity(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	group := RuntimeGroup{Scope: "global", Name: "unrelated", LeaderPID: os.Getpid(), PGID: os.Getpid(), StartIdentity: "old-incarnation"}
	outcome, err := reclaimRuntimeGroup(group, 0)
	if err != nil || outcome != "reclaimed" {
		t.Fatalf("reused pid: %q, %v", outcome, err)
	}
	group.StartIdentity = identity
	outcome, err = reclaimRuntimeGroup(group, 0)
	if outcome != "unresolved" || err == nil {
		t.Fatalf("uncertain owner: %q, %v", outcome, err)
	}
	if !recordedGroupAlive(group) {
		t.Fatal("live group not retained")
	}
	if !processAlive(os.Getpid()) {
		t.Fatal("unrelated process was terminated")
	}
}
func TestWindowsACLRejectsOtherUsers(t *testing.T) {
	t.Parallel()
	dir := windowsRuntimeDir(t)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// OS-created directories inherit their parent's ACL; either they happen to
	// be already private or this must fail closed. A deliberately broad ACE must
	// always be rejected regardless of inheritance.
	sid, err := currentSID()
	if err != nil {
		t.Fatal(err)
	}
	broad := "O:" + sid.String() + "D:P(A;;GA;;;" + sid.String() + ")(A;;GR;;;WD)"
	sd, err := windows.SecurityDescriptorFromString(broad)
	if err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(t.TempDir(), "broad")
	name, err := windows.UTF16PtrFromString(foreign)
	if err != nil {
		t.Fatal(err)
	}
	attrs := &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	if err := windows.CreateDirectory(name, attrs); err != nil {
		t.Fatal(err)
	}
	if err := checkPrivateDir(foreign); err == nil {
		t.Fatal("broad directory ACL accepted")
	}
}
