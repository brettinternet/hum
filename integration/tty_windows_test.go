//go:build windows

package integration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hum/internal/daemon"
	"hum/internal/output"
	"hum/internal/process"
	"hum/internal/testutil"
)

func TestWindowsTTYAttachDetachesAndReattachesWithoutSecondOwner(t *testing.T) {
	hum := integrationHum(t)
	fixture := integrationFixture(t)
	runtimeDir := testutil.RuntimeDir(t)
	root := t.TempDir()
	env := testutil.RuntimeEnv(runtimeDir)
	marker := filepath.Join(root, "interactive")
	t.Cleanup(func() { _ = testutil.Run(t, hum, root, env, "shutdown", "--stop-processes") })
	started := testutil.Run(t, hum, root, env, "run", "interactive", "--tty", "--detach", "--", fixture, "stream", marker)
	if started.Err != nil || started.Code != 0 {
		t.Fatalf("start tty: %+v", started)
	}
	testutil.WaitForFile(t, marker+".started", 10*time.Second)
	client, err := daemon.DialRuntime(context.Background(), daemon.NewRuntimePaths(runtimeDir))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	current, err := client.Get(context.Background(), daemon.GetRequest{Name: "interactive", Cwd: root})
	if err != nil {
		t.Fatal(err)
	}
	request := daemon.InputAttachRequest{Name: current.Name, Cwd: current.Cwd, Root: current.Root, TTY: true, Argv: current.Argv, Source: current.Source}
	attach := func() (*process.Child, *output.Store) {
		t.Helper()
		store, err := output.NewStore(output.Limits{})
		if err != nil {
			t.Fatal(err)
		}
		child, err := process.Start(process.Spec{Argv: []string{hum, "attach", "interactive"}, Env: env, Dir: root, TTY: true, Output: store, MaxLineBytes: 4096})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			select {
			case <-child.Done():
			default:
				_ = child.Stop()
				<-child.Done()
			}
		})
		waitWindowsTTYText(t, store, "tty input attached", 10*time.Second)
		return child, store
	}
	first, _ := attach()
	if session, err := client.InputAttach(context.Background(), request); err == nil {
		_ = session.Release()
		t.Fatal("second input owner accepted while first attach is active")
	}
	if _, err := first.WriteContext(context.Background(), []byte{0x1d}); err != nil {
		t.Fatalf("send Ctrl+]: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		session, attachErr := client.InputAttach(context.Background(), request)
		if attachErr == nil {
			_ = session.Release()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Ctrl+] did not release input: %v", attachErr)
		}
		time.Sleep(25 * time.Millisecond)
	}
	second, _ := attach()
	if session, err := client.InputAttach(context.Background(), request); err == nil {
		_ = session.Release()
		t.Fatal("second input owner accepted after reattach")
	}
	if _, err := second.WriteContext(context.Background(), []byte{0x1d}); err != nil && !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("detach second owner: %v", err)
	}
}

func waitWindowsTTYText(t *testing.T, store *output.Store, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		read, err := store.Read(output.ReadOptions{})
		if err != nil {
			t.Fatal(err)
		}
		var text strings.Builder
		for _, entry := range read.Entries {
			text.WriteString(entry.Text)
		}
		if strings.Contains(text.String(), want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("ConPTY output missing %q: %q", want, text.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
}
