package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"hum/internal/protocol"

	urfavecli "github.com/urfave/cli/v3"
)

func TestFlagAliasParityReadCommands(t *testing.T) {
	t.Run("wait request output and exit", func(t *testing.T) {
		response := protocol.WaitResponse{
			Op:      protocol.OpWait,
			OK:      true,
			Outcome: protocol.WaitExited,
			Cursor:  5,
			Exit:    &protocol.Exit{Code: 23},
		}
		short := flagAliasReadWait(t, "short", response, "wait", "api", "-c", "0", "-m", "ready", "-t", "1500ms", "-j")
		long := flagAliasReadWait(t, "long", response, "wait", "api", "--after-cursor", "0", "--match", "ready", "--timeout", "1500ms", "--json")
		if !reflect.DeepEqual(short, long) {
			t.Fatalf("short wait = %#v, long wait = %#v", short, long)
		}
		if short.exitCode != 3 || short.request.After == nil || *short.request.After != 0 || short.request.Match != "ready" || short.request.TimeoutMS != 1500 {
			t.Fatalf("wait semantic result = %#v", short)
		}
	})

	t.Run("list logs and status actual actions", func(t *testing.T) {
		runtimeDir := hum006ListLogsTempDir(t, "alias-parity-runtime")
		hum006ListLogsStartDaemon(t, runtimeDir, 2048)
		project := hum006ListLogsProject(t, "alias-parity-project")
		otherProject := hum006ListLogsProject(t, "alias-parity-other")

		if stdout, stderr, err := hum006ListLogsRunAt(t, project, context.Background(), "run", "api", "--detach", "--", "/bin/sh", "-c", "printf 'api-ready\\n'; sleep 30"); err != nil {
			t.Fatalf("start api: %v (stdout=%q stderr=%q)", err, stdout, stderr)
		}
		if stdout, stderr, err := hum006ListLogsRunAt(t, otherProject, context.Background(), "run", "worker", "--detach", "--", "/bin/sh", "-c", "printf 'worker-ready\\n'; sleep 30"); err != nil {
			t.Fatalf("start worker: %v (stdout=%q stderr=%q)", err, stdout, stderr)
		}

		allOutput := flagAliasReadCompareAction(t, project,
			[]string{"list", "-a"},
			[]string{"list", "--all"},
		)
		for _, want := range []string{"api", "worker", project, otherProject} {
			if !strings.Contains(allOutput, want) {
				t.Fatalf("list -a output %q missing %q", allOutput, want)
			}
		}
		listJSON := flagAliasReadCompareAction(t, project,
			[]string{"list", "-j"},
			[]string{"list", "--json"},
		)
		hum006ListLogsAssertProcessNames(t, hum006ListLogsProcessObjects(t, listJSON), []string{"api"}, []string{"worker"})
		statusJSON := flagAliasReadCompareAction(t, project,
			[]string{"status", "api", "-j"},
			[]string{"status", "api", "--json"},
		)
		if !strings.Contains(statusJSON, `"name":"api"`) {
			t.Fatalf("status -j output = %q, want api JSON", statusJSON)
		}

		script := "printf 'stdout-first\\n'; printf 'stderr-first\\n' >&2; printf 'stdout-match\\n'; printf 'stdout-last\\n'"
		if stdout, stderr, err := hum006ListLogsRunAt(t, project, context.Background(), "run", "alias-logs", "--detach", "--", "/bin/sh", "-c", script); err != nil {
			t.Fatalf("start logs fixture: %v (stdout=%q stderr=%q)", err, stdout, stderr)
		}
		hum006ListLogsWaitForText(t, project, "alias-logs", "stdout-last\n")

		allJSON := flagAliasReadCompareAction(t, project,
			[]string{"logs", "alias-logs", "-j"},
			[]string{"logs", "alias-logs", "--json"},
		)
		allObjects := hum006ListLogsDecodeJSONLines(t, allJSON)
		if len(allObjects) != 1 {
			t.Fatalf("logs -j output = %q, want one JSON object", allJSON)
		}
		allEntries := hum006ListLogsEntries(t, allObjects[0])
		matchCursor, ok := hum006ListLogsEntryCursor(allEntries, "stdout-match\n")
		if !ok {
			t.Fatalf("logs entries = %#v, missing stdout-match", allEntries)
		}

		streamJSON := flagAliasReadCompareAction(t, project,
			[]string{"logs", "alias-logs", "-s", "stderr", "--json"},
			[]string{"logs", "alias-logs", "--stream", "stderr", "--json"},
		)
		streamEntries := hum006ListLogsEntries(t, hum006ListLogsDecodeJSONLines(t, streamJSON)[0])
		if len(streamEntries) != 1 || streamEntries[0]["stream"] != "stderr" {
			t.Fatalf("logs -s entries = %#v, want only stderr", streamEntries)
		}

		tailJSON := flagAliasReadCompareAction(t, project,
			[]string{"logs", "alias-logs", "-n", "1", "--json"},
			[]string{"logs", "alias-logs", "--tail", "1", "--json"},
		)
		tailEntries := hum006ListLogsEntries(t, hum006ListLogsDecodeJSONLines(t, tailJSON)[0])
		if len(tailEntries) != 1 || !reflect.DeepEqual(tailEntries[0], allEntries[len(allEntries)-1]) {
			t.Fatalf("logs -n entries = %#v, want exact final retained entry %#v", tailEntries, allEntries[len(allEntries)-1])
		}

		afterJSON := flagAliasReadCompareAction(t, project,
			[]string{"logs", "alias-logs", "-c", fmt.Sprint(matchCursor), "--json"},
			[]string{"logs", "alias-logs", "--after-cursor", fmt.Sprint(matchCursor), "--json"},
		)
		afterTexts := hum006ListLogsEntryTexts(t, hum006ListLogsEntries(t, hum006ListLogsDecodeJSONLines(t, afterJSON)[0]))
		afterJoined := strings.Join(afterTexts, "")
		if !strings.Contains(afterJoined, "stdout-last\n") || strings.Contains(afterJoined, "stdout-first\n") || strings.Contains(afterJoined, "stdout-match\n") {
			t.Fatalf("logs -c texts = %#v, want entries strictly after stdout-match", afterTexts)
		}

		boundedJSON := flagAliasReadCompareAction(t, project,
			[]string{"logs", "alias-logs", "-b", "20", "--json"},
			[]string{"logs", "alias-logs", "--limit-bytes", "20", "--json"},
		)
		boundedObject := hum006ListLogsDecodeJSONLines(t, boundedJSON)[0]
		if !hum006ListLogsBool(boundedObject, "more") {
			t.Fatalf("logs -b output = %q, want more=true", boundedJSON)
		}

		matchJSON := flagAliasReadCompareAction(t, project,
			[]string{"logs", "alias-logs", "-m", "^stdout-match", "--json"},
			[]string{"logs", "alias-logs", "--match", "^stdout-match", "--json"},
		)
		matchTexts := hum006ListLogsEntryTexts(t, hum006ListLogsEntries(t, hum006ListLogsDecodeJSONLines(t, matchJSON)[0]))
		if !hum006ListLogsEqualStrings(matchTexts, []string{"stdout-match\n"}) {
			t.Fatalf("logs -m texts = %#v, want stdout-match", matchTexts)
		}

		shortFollow := flagAliasReadLiveFollow(t, project, "short", "-f")
		longFollow := flagAliasReadLiveFollow(t, project, "long", "--follow")
		if !reflect.DeepEqual(shortFollow, longFollow) {
			t.Fatalf("short follow = %#v, long follow = %#v", shortFollow, longFollow)
		}
	})

	t.Run("real validation", func(t *testing.T) {
		pairs := []struct {
			short []string
			long  []string
		}{
			{[]string{"logs", "api", "-n", "-1"}, []string{"logs", "api", "--tail", "-1"}},
			{[]string{"logs", "api", "-b", "-1"}, []string{"logs", "api", "--limit-bytes", "-1"}},
			{[]string{"wait", "api", "-t", "invalid"}, []string{"wait", "api", "--timeout", "invalid"}},
			{[]string{"wait", "api", "-m", ""}, []string{"wait", "api", "--match", ""}},
		}
		for _, pair := range pairs {
			short := flagAliasParityRunRoot(t, pair.short)
			long := flagAliasParityRunRoot(t, pair.long)
			if short.err != long.err || short.exitCode != long.exitCode || short.stdout != long.stdout || short.stderr != long.stderr {
				t.Fatalf("short validation = %#v, long validation = %#v", short, long)
			}
			if short.err == "" || short.exitCode != 1 {
				t.Fatalf("validation unexpectedly succeeded: %#v", short)
			}
		}
	})
}

type flagAliasReadWaitResult struct {
	request  protocol.WaitRequest
	stdout   string
	stderr   string
	exitCode int
}

func flagAliasReadWait(t *testing.T, name string, response protocol.WaitResponse, args ...string) flagAliasReadWaitResult {
	t.Helper()
	var result flagAliasReadWaitResult
	t.Run(name, func(t *testing.T) {
		runtimeDir, requests := waitCLIStubDaemon(t, response)
		t.Setenv("HUM_RUNTIME_DIR", runtimeDir)
		stdout, stderr, err := waitCLIRun(t, args...)
		result.stdout = stdout
		result.stderr = stderr
		result.exitCode = waitCLIExitCode(err)
		select {
		case result.request = <-requests:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for wait request")
		}
	})
	return result
}

func flagAliasReadCompareAction(t *testing.T, cwd string, shortArgs, longArgs []string) string {
	t.Helper()
	shortOut, shortErrOut, shortErr := hum006ListLogsRunAt(t, cwd, context.Background(), shortArgs...)
	longOut, longErrOut, longErr := hum006ListLogsRunAt(t, cwd, context.Background(), longArgs...)
	if shortOut != longOut || shortErrOut != longErrOut || waitCLIExitCode(shortErr) != waitCLIExitCode(longErr) {
		t.Fatalf("short %v = (%q, %q, %v), long %v = (%q, %q, %v)", shortArgs, shortOut, shortErrOut, shortErr, longArgs, longOut, longErrOut, longErr)
	}
	return shortOut
}

type flagAliasReadFollowResult struct {
	texts    []string
	sawExit  bool
	exitCode int
}

func flagAliasReadLiveFollow(t *testing.T, project, variant, flag string) flagAliasReadFollowResult {
	t.Helper()
	name := "alias-follow-" + variant
	fixtureDir := t.TempDir()
	probe := filepath.Join(fixtureDir, "probe")
	release := filepath.Join(fixtureDir, "release")
	script := fmt.Sprintf("printf 'before\\n'; while [ ! -f %q ]; do sleep 0.01; done; printf 'probe\\n'; while [ ! -f %q ]; do sleep 0.01; done; printf 'after\\n'", probe, release)
	if stdout, stderr, err := hum006ListLogsRunAt(t, project, context.Background(), "run", name, "--detach", "--", "/bin/sh", "-c", script); err != nil {
		t.Fatalf("start %s: %v (stdout=%q stderr=%q)", name, err, stdout, stderr)
	}
	hum006ListLogsWaitForText(t, project, name, "before\n")
	baseline, stderr, err := hum006ListLogsRunAt(t, project, context.Background(), "logs", name, "--json")
	if err != nil || stderr != "" {
		t.Fatalf("baseline %s logs: %v (stdout=%q stderr=%q)", name, err, baseline, stderr)
	}
	beforeCursor, ok := hum006ListLogsEntryCursor(hum006ListLogsEntries(t, hum006ListLogsDecodeJSONLines(t, baseline)[0]), "before\n")
	if !ok {
		t.Fatalf("baseline %s logs = %q, missing before cursor", name, baseline)
	}
	if err := os.WriteFile(probe, []byte("probe"), 0o600); err != nil {
		t.Fatalf("release follower probe: %v", err)
	}
	hum006ListLogsWaitForText(t, project, name, "probe\n")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	capture := hum006ListLogsFirstWriteWriter()
	commandResult := make(chan hum006ListLogsCommandResult, 1)
	oldwd := hum006ListLogsEnterDir(t, project)
	defer hum006ListLogsLeaveDir(t, oldwd)
	go func() {
		var stderr strings.Builder
		command := NewRootCommand("test", "test", capture, &stderr)
		command.ExitErrHandler = func(context.Context, *urfavecli.Command, error) {}
		err := command.Run(ctx, []string{"hum", "logs", name, flag, "--after-cursor", fmt.Sprint(beforeCursor), "--json"})
		commandResult <- hum006ListLogsCommandResult{stdout: capture.String(), stderr: stderr.String(), err: err}
	}()

	select {
	case <-capture.first:
	case <-ctx.Done():
		t.Fatalf("follower %s did not establish: %v", flag, ctx.Err())
	}
	select {
	case premature := <-commandResult:
		t.Fatalf("follower %s returned before release: %#v", flag, premature)
	default:
	}
	if err := os.WriteFile(release, []byte("release"), 0o600); err != nil {
		t.Fatalf("release follower producer: %v", err)
	}
	hum006ListLogsWaitForText(t, project, name, "after\n")
	cancel()

	var commandOutput hum006ListLogsCommandResult
	select {
	case commandOutput = <-commandResult:
	case <-time.After(time.Second):
		t.Fatalf("follower %s did not detach after cancellation", flag)
	}
	result := flagAliasReadFollowResult{exitCode: waitCLIExitCode(commandOutput.err)}
	for _, object := range hum006ListLogsDecodeJSONLines(t, commandOutput.stdout) {
		if object["op"] != "event" {
			t.Fatalf("live follow %s %s object = %#v, want op=event", name, flag, object)
		}
		if object["type"] == "exit" {
			result.sawExit = true
		}
		if entries, ok := hum006ListLogsMaybeEntries(object); ok {
			for _, text := range hum006ListLogsEntryTexts(t, entries) {
				if text == "probe\n" || text == "after\n" {
					result.texts = append(result.texts, text)
				}
			}
		}
	}
	joined := strings.Join(result.texts, "")
	if result.exitCode != 0 || result.sawExit || commandOutput.stderr != "" || !strings.Contains(joined, "probe\n") || !strings.Contains(joined, "after\n") {
		t.Fatalf("live follow %s %s = %#v (stdout=%q stderr=%q err=%v ctx=%v)", name, flag, result, commandOutput.stdout, commandOutput.stderr, commandOutput.err, ctx.Err())
	}
	return result
}
