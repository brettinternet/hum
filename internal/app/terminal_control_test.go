package app

import (
	"context"
	"regexp"
	"testing"
	"time"

	"hum/internal/output"
)

func TestMatchUsesStrippedText(t *testing.T) {
	root := makeProject(t, false)
	colorRaw := "\x1b[31mready\x1b[0m\n"
	crlfRaw := "\x1b[32mready\x1b[0m\r\n"
	children := map[string]*subscriptionChild{
		"color": newSubscriptionChild(6101, 0, time.Unix(61, 0), colorRaw),
		"crlf":  newSubscriptionChild(6102, 0, time.Unix(62, 0), crlfRaw),
	}
	s := testSupervisor(t, Options{
		OutputLimits: output.Limits{
			RetainedBytes:      1024,
			DefaultReadEntries: 16,
			DefaultReadBytes:   1024,
		},
		StartProcess: subscriptionStarter(children),
	})

	for name := range children {
		started, err := s.Start(StartRequest{
			Name: name, Source: "manifest", Cwd: root, Argv: []string{"fake", name},
			Ready: &ReadinessConfig{Match: `^ready`},
		})
		if err != nil {
			t.Fatalf("start %s: %v", name, err)
		}
		if started.Readiness == nil || started.Readiness.State != ReadinessReady || started.Readiness.Cursor == nil || *started.Readiness.Cursor != 0 {
			t.Fatalf("readiness %s = %#v, want ready at raw cursor 0", name, started.Readiness)
		}
	}

	for name, raw := range map[string]string{"color": colorRaw, "crlf": crlfRaw} {
		store, err := s.Output(root, name)
		if err != nil {
			t.Fatalf("output %s: %v", name, err)
		}
		stored, err := store.Read(output.ReadOptions{})
		if err != nil {
			t.Fatalf("read stored %s: %v", name, err)
		}
		if len(stored.Entries) != 1 || stored.Entries[0].Cursor != 0 || stored.Entries[0].Text != raw {
			t.Fatalf("stored %s entries = %#v, want raw entry %q at cursor 0", name, stored.Entries, raw)
		}

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		waited, err := s.Wait(ctx, root, name, WaitOptions{Match: regexp.MustCompile(`^ready`)})
		cancel()
		if err != nil {
			t.Fatalf("wait %s: %v", name, err)
		}
		if waited.Outcome != WaitMatched || waited.Cursor != 0 || waited.Exit != nil {
			t.Fatalf("wait %s = %#v, want matched at raw cursor 0", name, waited)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	literalEscape, err := s.Wait(ctx, root, "color", WaitOptions{Match: regexp.MustCompile("\x1b")})
	cancel()
	if err != nil {
		t.Fatalf("literal ESC wait: %v", err)
	}
	if literalEscape.Outcome != WaitTimedOut || literalEscape.Cursor != 0 {
		t.Fatalf("literal ESC wait = %#v, want timed out at cursor 0", literalEscape)
	}
}
