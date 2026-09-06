package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"hum/internal/app"
	"hum/internal/daemon"
	"hum/internal/protocol"
)

func TestInputCommand(t *testing.T) {
	invalidRuntime := filepath.Join(t.TempDir(), "runtime")
	invalid := []struct {
		name string
		args []string
	}{
		{"missing name", []string{"input", "--text", "x"}},
		{"extra name", []string{"input", "one", "two", "--text", "x"}},
		{"missing payload", []string{"input", "one"}},
		{"both payloads", []string{"input", "one", "--text", "x", "--base64", "eA=="}},
		{"empty text", []string{"input", "one", "--text", ""}},
		{"empty base64", []string{"input", "one", "--base64", ""}},
		{"malformed base64", []string{"input", "one", "--base64", "not base64"}},
		{"un-padded base64", []string{"input", "one", "--base64", "eA"}},
		{"base64 whitespace", []string{"input", "one", "--base64", "eA==\n"}},
		{"oversized text", []string{"input", "one", "--text", strings.Repeat("x", protocol.MaxInputBytes+1)}},
		{"short payload alias", []string{"input", "one", "-t", "x"}},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			output, _, err := runInputCLI(t, invalidRuntime, tc.args...)
			if err == nil {
				t.Fatal("invalid input command succeeded")
			}
			if _, statErr := os.Stat(invalidRuntime); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("invalid input touched runtime directory: stat=%v output=%q", statErr, output)
			}
		})
	}
	oversizedBase64 := base64.StdEncoding.EncodeToString(make([]byte, protocol.MaxInputBytes+1))
	if output, _, err := runInputCLI(t, invalidRuntime, "input", "one", "--base64", oversizedBase64); err == nil || !strings.Contains(err.Error(), "32768") {
		t.Fatalf("oversized base64 error=%v output=%q", err, output)
	}
	if _, statErr := os.Stat(invalidRuntime); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("oversized base64 touched runtime directory: %v", statErr)
	}
	if _, _, err := runInputCLI(t, invalidRuntime, "input", "one", "--text", "x"); err == nil || !strings.Contains(err.Error(), "No hum daemon is running") {
		t.Fatalf("unavailable input error=%v", err)
	}

	decoded, err := decodeStrictInputBase64("AAEC")
	if err != nil || !bytes.Equal(decoded, []byte{0, 1, 2}) {
		t.Fatalf("strict base64 control bytes = %v/%v", decoded, err)
	}
	if got := []byte("hello"); bytes.Equal(got, append([]byte(nil), []byte("hello\n")...)) {
		t.Fatal("text payload unexpectedly appends a newline")
	}

	runtimeDir := t.TempDir()
	root, control, cleanup := newInputTestDaemon(t, runtimeDir)
	defer cleanup()
	oldCwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldCwd) })

	if _, err := control.Start(context.Background(), daemon.StartRequest{
		Name: "prompt", Root: root, Cwd: root, TTY: true,
		Argv: []string{"/bin/sh", "-c", "read -r line; printf 'got:%s' \"$line\""},
		Env:  []string{"PATH=/bin:/usr/bin"},
	}); err != nil {
		t.Fatal(err)
	}
	output, _, err := runInputCLI(t, runtimeDir, "input", "prompt", "--text", "hello\n")
	if err != nil || output != "wrote 6 bytes to prompt at launch cursor 0\n" {
		t.Fatalf("human input output=%q err=%v", output, err)
	}
	if wait, err := control.Wait(context.Background(), daemon.WaitRequest{Name: "prompt", Cwd: root, TimeoutMS: 4000}); err != nil || string(wait.Outcome) != string(protocol.WaitExited) {
		t.Fatalf("prompt wait=%+v err=%v", wait, err)
	}

	if _, err := control.Start(context.Background(), daemon.StartRequest{
		Name: "json-prompt", Root: root, Cwd: root, TTY: true,
		Argv: []string{"/bin/sh", "-c", "read -r line; printf 'got:%s' \"$line\""},
		Env:  []string{"PATH=/bin:/usr/bin"},
	}); err != nil {
		t.Fatal(err)
	}
	output, _, err = runInputCLI(t, runtimeDir, "input", "json-prompt", "--base64", "eWVzCg==", "--json")
	if err != nil {
		t.Fatalf("json input: %v", err)
	}
	var success struct {
		Name         string `json:"name"`
		Bytes        int    `json:"bytes"`
		LaunchCursor uint64 `json:"launch_cursor"`
	}
	if err := json.Unmarshal([]byte(output), &success); err != nil || success.Name != "json-prompt" || success.Bytes != 4 || success.LaunchCursor != 0 {
		t.Fatalf("json input output=%q err=%v", output, err)
	}
	if wait, err := control.Wait(context.Background(), daemon.WaitRequest{Name: "json-prompt", Cwd: root, TimeoutMS: 4000}); err != nil || string(wait.Outcome) != string(protocol.WaitExited) {
		t.Fatalf("json prompt wait=%+v err=%v", wait, err)
	}

	if _, _, err := runInputCLI(t, runtimeDir, "input", "missing", "--text", "x"); err == nil || !strings.Contains(err.Error(), "hum run missing") || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("not-found input error=%v", err)
	}
	if _, err := control.Start(context.Background(), daemon.StartRequest{
		Name: "plain", Root: root, Cwd: root, Argv: []string{"/bin/sh", "-c", "sleep 5"}, Env: []string{"PATH=/bin:/usr/bin"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runInputCLI(t, runtimeDir, "input", "plain", "--text", "x"); err == nil || !strings.Contains(err.Error(), "tty") || (!strings.Contains(err.Error(), "tty: true") && !strings.Contains(err.Error(), "--tty")) {
		t.Fatalf("non-tty input error=%v", err)
	}
	_ = control.Stop(context.Background(), daemon.StopRequest{Name: "plain", Cwd: root})

	if _, err := control.Start(context.Background(), daemon.StartRequest{
		Name: "owned", Root: root, Cwd: root, TTY: true, Argv: []string{"/bin/sh", "-c", "sleep 5"}, Env: []string{"PATH=/bin:/usr/bin"},
	}); err != nil {
		t.Fatal(err)
	}
	owner, err := control.InputAttach(context.Background(), daemon.InputAttachRequest{Name: "owned", Root: root, Cwd: root, TTY: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := runInputCLI(t, runtimeDir, "input", "owned", "--text", "x"); err == nil || !strings.Contains(err.Error(), "already owned") {
		t.Fatalf("conflict input error=%v", err)
	}
	_ = owner.Release()
	_ = control.Stop(context.Background(), daemon.StopRequest{Name: "owned", Cwd: root})

	if _, _, err := runInputCLI(t, runtimeDir, "input", "prompt", "--text", "x", "--json"); err == nil {
		t.Fatal("stopped input succeeded")
	} else {
		if !strings.Contains(err.Error(), "hum start prompt") {
			t.Fatalf("stopped input error=%v", err)
		}
	}

	for _, code := range []protocol.ErrorCode{protocol.ErrorInputClosed, protocol.ErrorInputStale} {
		t.Run(string(code)+" CLI error", func(t *testing.T) {
			fakeRuntime, mkdirErr := os.MkdirTemp("/tmp", "h-")
			if mkdirErr != nil {
				t.Fatal(mkdirErr)
			}
			t.Cleanup(func() { _ = os.RemoveAll(fakeRuntime) })
			stopFake := serveInputCLIFakeDaemon(t, fakeRuntime, code)
			defer stopFake()
			_, _, err := runInputCLI(t, fakeRuntime, "input", "fake", "--text", "x")
			if err == nil {
				t.Fatalf("%s input succeeded", code)
			}
			var wire *protocol.WireError
			if !errors.As(err, &wire) || wire.Code != code {
				t.Fatalf("%s input error=%v", code, err)
			}
		})
	}
	jsonError, _, err := runInputCLI(t, invalidRuntime, "input", "bad", "--base64", "eA", "--json")
	if err == nil {
		t.Fatal("JSON invalid input succeeded")
	}
	var failure inputErrorResult
	if decodeErr := json.Unmarshal([]byte(jsonError), &failure); decodeErr != nil || !strings.Contains(failure.Error, "base64") {
		t.Fatalf("JSON error=%q decode=%v", jsonError, decodeErr)
	}
}

func TestInputDocs(t *testing.T) {
	var help bytes.Buffer
	helpRoot := NewRootCommand("test", "test", &help, &bytes.Buffer{})
	if err := helpRoot.Run(context.Background(), []string{"hum", "input", "--help"}); err != nil {
		t.Fatal(err)
	}
	for _, phrase := range []string{"--text", "--base64", "exact bytes", "without appending a newline", "running TTY", "launch cursor", "ownership conflict", "never starts", "Observe", "answer", "confirm", "strict padded base64", "without whitespace"} {
		if !strings.Contains(help.String(), phrase) {
			t.Errorf("CLI input help missing %q", phrase)
		}
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repo := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	for _, relative := range []string{"docs/design.md", "docs/coding-agents.md", "internal/skill/SKILL.md", "plugins/hum/skills/hum/SKILL.md"} {
		content, err := os.ReadFile(filepath.Join(repo, relative))
		if err != nil {
			t.Fatal(err)
		}
		text := string(content)
		for _, phrase := range []string{"input", "base64", "launch cursor", "wait --match", "without a newline", "without whitespace", "strict padded base64", "ownership", "32", "at-most-once", "launch race", "observe", "answer", "confirm", "never"} {
			if !strings.Contains(strings.ToLower(text), strings.ToLower(phrase)) {
				t.Errorf("%s missing input documentation %q", relative, phrase)
			}
		}
	}
}

func runInputCLI(t *testing.T, runtimeDir string, args ...string) (string, string, error) {
	t.Helper()
	var output, errorOutput bytes.Buffer
	root := NewRootCommand("test", "test", &output, &errorOutput)
	t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
	argv := append([]string{"hum"}, args...)
	err := root.Run(context.Background(), argv)
	return output.String(), errorOutput.String(), err
}

func newInputTestDaemon(t *testing.T, runtimeDir string) (string, *daemon.Client, func()) {
	t.Helper()
	root := t.TempDir()
	if resolved, resolveErr := filepath.EvalSymlinks(root); resolveErr == nil {
		root = resolved
	}
	server, err := daemon.NewServer(daemon.Config{RuntimeDir: runtimeDir, StopGrace: 100 * time.Millisecond, CompletedLimit: 10})
	if err != nil {
		t.Fatal(err)
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(context.Background()) }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}
	client, err := daemon.Dial(ctx, server.SocketPath())
	if err != nil {
		t.Fatal(err)
	}
	cleanup := func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		shutdownClient, dialErr := daemon.Dial(shutdownCtx, server.SocketPath())
		if dialErr == nil {
			_ = shutdownClient.Shutdown(shutdownCtx, daemon.ShutdownRequest{Force: true})
			_ = shutdownClient.Close()
		}
		select {
		case <-serveDone:
		case <-time.After(5 * time.Second):
			t.Errorf("daemon did not stop")
		}
		_ = client.Close()
	}
	return root, client, cleanup
}

func serveInputCLIFakeDaemon(t *testing.T, runtimeDir string, failure protocol.ErrorCode) func() {
	t.Helper()
	paths := daemon.NewRuntimePaths(runtimeDir)
	listener, err := net.Listen("unix", paths.Socket)
	if err != nil {
		t.Fatal(err)
	}
	root, err := app.DiscoverProjectRoot(mustInputTestCWD(t))
	if err != nil {
		_ = listener.Close()
		t.Fatal(err)
	}
	var handlers sync.WaitGroup
	acceptDone := make(chan struct{})
	go func() {
		defer close(acceptDone)
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			handlers.Add(1)
			go func() {
				defer handlers.Done()
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				decoder := protocol.NewDecoder(conn, protocol.DefaultMaxLineBytes)
				encoder := protocol.NewEncoder(conn, protocol.DefaultMaxLineBytes)
				hello, helloErr := decoder.DecodeRequest()
				if helloErr != nil || hello.Op != protocol.OpHello {
					return
				}
				if err := encoder.EncodeResponse(protocol.Hello{Op: protocol.OpHello, Version: protocol.Version}); err != nil {
					return
				}
				request, requestErr := decoder.DecodeRequest()
				if requestErr != nil {
					return
				}
				switch request.Op {
				case protocol.OpGet:
					nextCursor := protocol.Cursor(8)
					process := protocol.Process{Name: "fake", Root: root, Cwd: root, TTY: true, State: "running", LaunchCursor: 7, NextCursor: &nextCursor}
					_ = encoder.EncodeResponse(protocol.NewGetResponse(process))
				case protocol.OpInputAttach:
					if err := encoder.EncodeResponse(protocol.InputAttachResponse{Op: protocol.OpInputAttach, OK: true}); err != nil {
						return
					}
					if err := encoder.EncodeResponse(protocol.InputStateEvent{Op: protocol.OpInputState, State: "running", LaunchCursor: 7, TTY: true}); err != nil {
						return
					}
					writeRequest, writeErr := decoder.DecodeRequest()
					if writeErr != nil || writeRequest.Op != protocol.OpInputWrite {
						return
					}
					if err := encoder.EncodeResponse(protocol.InputAckResponse{
						Op: protocol.OpInputWrite, Error: protocol.NewWireError(failure, "fake input failure", nil),
					}); err != nil {
						return
					}
					releaseRequest, releaseErr := decoder.DecodeRequest()
					if releaseErr == nil && releaseRequest.Op == protocol.OpInputRelease {
						_ = encoder.EncodeResponse(protocol.InputAckResponse{Op: protocol.OpInputRelease, OK: true})
					}
				}
			}()
		}
	}()
	return func() {
		_ = listener.Close()
		<-acceptDone
		handlers.Wait()
	}
}

func mustInputTestCWD(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return cwd
}
