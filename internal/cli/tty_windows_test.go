//go:build windows

package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hum/internal/daemon"
	"hum/internal/output"
	"hum/internal/process"
	"hum/internal/project"

	"golang.org/x/sys/windows"
	"golang.org/x/term"
)

// The Windows CI runner has no attached console. Host the test process in a
// real ConPTY so that MakeRaw and Restore operate on a console input buffer.
func TestWindowsTTYLocalConsoleRestored(t *testing.T) {
	if os.Getenv("HUM_TTY_CONSOLE_HELPER") == "cancel" {
		if !ttySupported() || !termIsStdinTerminal() {
			t.Fatal("ConPTY child has no terminal")
		}
		request := ttyInputRequest("test", "", project.Definition{}, nil)
		if request.Columns != 100 || request.Rows != 35 {
			t.Fatalf("initial ConPTY dimensions = %dx%d, want 100x35", request.Columns, request.Rows)
		}
		var before, raw, after uint32
		handle := windows.Handle(os.Stdin.Fd())
		if err := windows.GetConsoleMode(handle, &before); err != nil {
			t.Fatal(err)
		}
		var retained windows.Handle
		self := windows.CurrentProcess()
		if err := windows.DuplicateHandle(self, handle, self, &retained, 0, false, windows.DUPLICATE_SAME_ACCESS); err != nil {
			t.Fatal(err)
		}
		defer windows.CloseHandle(retained)
		input, err := newTTYInput(&daemon.InputSession{}, os.Stderr)
		if err != nil {
			t.Fatal(err)
		}
		if err := windows.GetConsoleMode(handle, &raw); err != nil || raw == before {
			t.Fatalf("raw console mode = %x, original %x, err %v", raw, before, err)
		}
		// A canceled attached command invokes close even if the stdin read is
		// blocked. A nil session keeps this test local to console cleanup.
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		<-ctx.Done()
		input.session = nil
		input.close()
		if err := windows.GetConsoleMode(retained, &after); err != nil || after != before {
			t.Fatalf("restored console mode after cancellation = %x, original %x, err %v", after, before, err)
		}
		fmt.Fprintln(os.Stdout, "console-restored")
		return
	}
	runWindowsTTYConsoleHelper(t, "TestWindowsTTYLocalConsoleRestored", "cancel", "console-restored")
}

func runWindowsTTYConsoleHelper(t *testing.T, name, mode, marker string) {
	t.Helper()
	store, err := output.NewStore(output.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child, err := process.Start(process.Spec{
		Argv: []string{exe, "-test.run=^" + name + "$", "-test.v"},
		Env:  append(os.Environ(), "HUM_TTY_CONSOLE_HELPER="+mode),
		TTY:  true, TTYSize: &process.TTYSize{Columns: 100, Rows: 35}, Output: store, MaxLineBytes: 4096,
	})
	if err != nil {
		t.Fatal(err)
	}
	result := child.Wait()
	read, readErr := store.Read(output.ReadOptions{})
	if readErr != nil {
		t.Fatal(readErr)
	}
	var text strings.Builder
	for _, entry := range read.Entries {
		text.WriteString(entry.Text)
	}
	if result.Err != nil || result.ExitCode != 0 || !strings.Contains(text.String(), marker) {
		t.Fatalf("console helper: result=%+v output=%q", result, text.String())
	}
}

func termIsStdinTerminal() bool {
	return os.Stdin != nil && term.IsTerminal(int(os.Stdin.Fd()))
}

func TestWindowsTTYFailedStartupDoesNotChangeLocalMode(t *testing.T) {
	if os.Getenv("HUM_TTY_CONSOLE_HELPER") != "failed" {
		runWindowsTTYConsoleHelper(t, "TestWindowsTTYFailedStartupDoesNotChangeLocalMode", "failed", "startup-mode-preserved")
		return
	}
	if !termIsStdinTerminal() {
		t.Fatal("ConPTY child has no terminal")
	}
	handle := windows.Handle(os.Stdin.Fd())
	var before, after uint32
	if err := windows.GetConsoleMode(handle, &before); err != nil {
		t.Fatal(err)
	}
	if _, err := newTTYInput(nil, os.Stderr); err == nil {
		t.Fatal("missing input session accepted")
	}
	invalid := filepath.Join(t.TempDir(), "invalid.EXE")
	if err := os.WriteFile(invalid, []byte("invalid PE"), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := output.NewStore(output.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := process.Start(process.Spec{Argv: []string{invalid}, TTY: true, Output: store, MaxLineBytes: 4096}); err == nil {
		t.Fatal("invalid PE started under ConPTY")
	}
	if err := windows.GetConsoleMode(handle, &after); err != nil || after != before {
		t.Fatalf("failed startup changed local mode: before=%x after=%x err=%v", before, after, err)
	}
	fmt.Fprintln(os.Stdout, "startup-mode-preserved")
}
