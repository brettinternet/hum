//go:build windows

package cli

import (
	"fmt"
	"os"
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
	if os.Getenv("HUM_TTY_CONSOLE_HELPER") == "1" {
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
		input, err := newTTYInput(&daemon.InputSession{}, os.Stderr)
		if err != nil {
			t.Fatal(err)
		}
		if err := windows.GetConsoleMode(handle, &raw); err != nil || raw == before {
			t.Fatalf("raw console mode = %x, original %x, err %v", raw, before, err)
		}
		input.restoreLocal()
		if err := windows.GetConsoleMode(handle, &after); err != nil || after != before {
			t.Fatalf("restored console mode = %x, original %x, err %v", after, before, err)
		}
		fmt.Fprintln(os.Stdout, "console-restored")
		return
	}
	store, err := output.NewStore(output.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child, err := process.Start(process.Spec{
		Argv: []string{exe, "-test.run=^TestWindowsTTYLocalConsoleRestored$", "-test.v"},
		Env:  append(os.Environ(), "HUM_TTY_CONSOLE_HELPER=1"),
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
	if result.Err != nil || result.ExitCode != 0 || !strings.Contains(text.String(), "console-restored") {
		t.Fatalf("console helper: result=%+v output=%q", result, text.String())
	}
}

func termIsStdinTerminal() bool {
	return os.Stdin != nil && term.IsTerminal(int(os.Stdin.Fd()))
}

func TestWindowsTTYFailedStartupDoesNotChangeLocalMode(t *testing.T) {
	if _, err := newTTYInput(nil, os.Stderr); err == nil {
		t.Fatal("missing input session accepted")
	}
	if err := validateTTYRequest(true); err != nil {
		t.Fatalf("Windows TTY rejected: %v", err)
	}
}
