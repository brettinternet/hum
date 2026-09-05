//go:build darwin || linux

package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"hum/internal/testutil"
)

func TestOneShotInputAnswersPrompt(t *testing.T) {
	hum := testutil.BuildHum(t)
	fixture := testutil.BuildFixture(t)
	runtimeDir := testutil.RuntimeDir(t)
	root := t.TempDir()
	env := testutil.RuntimeEnv(runtimeDir)
	marker := filepath.Join(t.TempDir(), "prompt")
	t.Cleanup(func() { _ = testutil.Run(t, hum, root, env, "shutdown", "--stop-processes") })

	started := testutil.Run(t, hum, root, env, "run", "prompt", "--tty", "--detach", "--", fixture, "prompt", marker)
	if started.Err != nil {
		t.Fatalf("detached prompt: %#v", started)
	}
	observed := testutil.Run(t, hum, root, env, "wait", "prompt", "--match", "^prompt>")
	if observed.Err != nil || observed.Code != 0 {
		t.Fatalf("observe prompt: %#v", observed)
	}
	answered := testutil.Run(t, hum, root, env, "input", "prompt", "--text", "yes\n")
	if answered.Err != nil || answered.Code != 0 || !strings.Contains(answered.Stdout, "wrote 4 bytes to prompt at launch cursor") {
		t.Fatalf("answer prompt: %#v", answered)
	}
	testutil.WaitForFile(t, marker+".input", 5*time.Second)
	input, err := os.ReadFile(marker + ".input")
	if err != nil || string(input) != "yes\n" {
		t.Fatalf("fixture input=%q err=%v", input, err)
	}
	confirmed := testutil.Run(t, hum, root, env, "wait", "prompt", "--after-cursor", "0", "--match", "confirmed:yes")
	if confirmed.Err != nil || confirmed.Code != 0 {
		t.Fatalf("confirm prompt: %#v", confirmed)
	}
	logs := testutil.Run(t, hum, root, env, "logs", "prompt")
	if logs.Err != nil || !strings.Contains(logs.Stdout, "confirmed:yes") {
		t.Fatalf("prompt logs: %#v", logs)
	}
	if strings.Contains(logs.Stdout, "input payload") {
		t.Fatalf("logs retained a daemon input diagnostic: %q", logs.Stdout)
	}
	stoppedInput := testutil.Run(t, hum, root, env, "input", "prompt", "--text", "again")
	if stoppedInput.Code == 0 || !strings.Contains(stoppedInput.Stderr, "hum start prompt") {
		t.Fatalf("stopped input: %#v", stoppedInput)
	}

	plain := testutil.Run(t, hum, root, env, "run", "plain", "--detach", "--", "/bin/sh", "-c", "sleep 5")
	if plain.Err != nil {
		t.Fatalf("plain launch: %#v", plain)
	}
	nonTTY := testutil.Run(t, hum, root, env, "input", "plain", "--text", "x")
	if nonTTY.Code == 0 || (!strings.Contains(nonTTY.Stderr, "tty") && !strings.Contains(nonTTY.Stdout, "tty")) {
		t.Fatalf("non-tty input: %#v", nonTTY)
	}
	_ = testutil.Run(t, hum, root, env, "stop", "plain")

	owner := exec.Command(hum, "run", "owner", "--tty", "--", "/bin/sh", "-c", "printf owner-ready; sleep 5")
	owner.Dir = root
	owner.Env = env
	ownerPTY, err := pty.Start(owner)
	if err != nil {
		t.Fatal(err)
	}
	readPTYUntil(t, ownerPTY, "owner-ready", 5*time.Second)
	conflict := testutil.Run(t, hum, root, env, "input", "owner", "--text", "x")
	if conflict.Code == 0 || !strings.Contains(conflict.Stderr+conflict.Stdout, "already owned") {
		t.Fatalf("occupied input: %#v", conflict)
	}
	_, _ = ownerPTY.Write([]byte{0x1d})
	_ = ownerPTY.Close()
	if owner.Process != nil && testutil.ProcessAlive(owner.Process.Pid) {
		_ = owner.Process.Signal(syscall.SIGTERM)
	}
	_ = owner.Wait()
	_ = testutil.Run(t, hum, root, env, "stop", "owner")
}

func TestTTYInteractiveSession(t *testing.T) {
	hum := testutil.BuildHum(t)
	runtimeDir := testutil.RuntimeDir(t)
	root := t.TempDir()
	env := testutil.RuntimeEnv(runtimeDir)
	manifest := "version: 1\nprocesses:\n  dev:\n    argv: [/bin/sh, -c, 'printf \\\"\\033[31mred\\033[0m\\n\\\"']\n    tty: true\n"
	if err := os.WriteFile(filepath.Join(root, "hum.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = testutil.Run(t, hum, root, env, "shutdown", "--stop-processes") })
	started := testutil.Run(t, hum, root, env, "start", "dev", "--no-wait")
	if started.Err != nil {
		t.Fatalf("manifest tty start: %v\nstdout=%s\nstderr=%s", started.Err, started.Stdout, started.Stderr)
	}
	status := testutil.Run(t, hum, root, env, "status", "dev", "--json")
	if status.Err != nil || !strings.Contains(status.Stdout, `"tty":true`) {
		t.Fatalf("tty status = %#v", status)
	}
	logs := testutil.Run(t, hum, root, env, "logs", "dev", "--stream", "stdout")
	if logs.Err != nil || !strings.Contains(logs.Stdout, "red") {
		t.Fatalf("tty logs = %#v", logs)
	}

	interactive := exec.Command(hum, "run", "interactive", "--tty", "--", "/bin/sh", "-c", "printf ready; read line; printf 'received=%s\\n' \"$line\"; sleep 5")
	interactive.Dir = root
	interactive.Env = env
	master, err := pty.Start(interactive)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = master.Close()
		if interactive.Process != nil {
			_ = interactive.Process.Signal(syscall.SIGTERM)
		}
		_ = interactive.Wait()
		_ = testutil.Run(t, hum, root, env, "stop", "interactive")
	}()
	readPTYUntil(t, master, "ready", 5*time.Second)
	if _, err := master.Write([]byte("typed\n")); err != nil {
		t.Fatalf("write typed input: %v", err)
	}
	readPTYUntil(t, master, "received=typed", 5*time.Second)
	if _, err := master.Write([]byte{0x1d}); err != nil {
		t.Fatalf("write detach chord: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if !testutil.ProcessAlive(interactive.Process.Pid) {
		t.Fatal("Ctrl-] terminated the attached CLI")
	}

	adHoc := testutil.Run(t, hum, root, env, "run", "adhoc", "--tty", "--detach", "--", "/bin/echo", "adhoc")
	if adHoc.Err != nil {
		t.Fatalf("ad-hoc tty launch: %v", adHoc)
	}
}

func readPTYUntil(t *testing.T, master *os.File, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	var output strings.Builder
	for !strings.Contains(output.String(), want) {
		result := make(chan struct {
			data []byte
			err  error
		}, 1)
		go func() {
			buffer := make([]byte, 4096)
			n, err := master.Read(buffer)
			result <- struct {
				data []byte
				err  error
			}{data: append([]byte(nil), buffer[:n]...), err: err}
		}()
		select {
		case value := <-result:
			if len(value.data) > 0 {
				output.Write(value.data)
			}
			if value.err != nil && !strings.Contains(output.String(), want) {
				t.Fatalf("PTY read failed before %q: %q (%v)", want, output.String(), value.err)
			}
		case <-deadline.C:
			t.Fatalf("PTY output missing %q: %q", want, output.String())
		}
	}
}
