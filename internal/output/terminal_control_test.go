package output

import (
	"errors"
	"regexp"
	"testing"
	"time"
)

func TestStripTerminalControl(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{name: "plain", text: "plain text\n", want: "plain text\n"},
		{name: "sgr and cursor csi", text: "\x1b[31mred\x1b[0m\x1b[2J\x1b[1;2H\x1b[K\n", want: "red\n"},
		{name: "osc bel", text: "before\x1b]0;title\aafter", want: "beforeafter"},
		{name: "osc st", text: "before\x1b]0;title\x1b\\after", want: "beforeafter"},
		{name: "dcs st", text: "before\x1bP1;payload\x1b\\after", want: "beforeafter"},
		{name: "dcs bel is not terminator", text: "\x1bPignored\aREADY", want: ""},
		{name: "sos st", text: "before\x1bXpayload\x1b\\after", want: "beforeafter"},
		{name: "pm st", text: "before\x1b^payload\x1b\\after", want: "beforeafter"},
		{name: "apc st", text: "before\x1b_payload\x1b\\after", want: "beforeafter"},
		{name: "charset designation", text: "a\x1b(Bb", want: "ab"},
		{name: "single final sequences", text: "a\x1b7b\x1b=c\x1bMd", want: "abcd"},
		{name: "crlf", text: "a\r\nb\rc\r\n", want: "a\nb\rc\n"},
		{name: "unterminated csi", text: "prefix\x1b[31", want: "prefix"},
		{name: "unterminated string", text: "prefix\x1b]title", want: "prefix"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := StripTerminalControl(test.text); got != test.want {
				t.Fatalf("StripTerminalControl(%q) = %q, want %q", test.text, got, test.want)
			}
		})
	}

	raw := string([]byte{'a', 0x00, 0xff, '\t', '\b', '\a', '\r', 'b', 0x9b, 'c'})
	if got := StripTerminalControl(raw); got != raw {
		t.Fatalf("preserved bytes = %q, want %q", got, raw)
	}
	plain := "no terminal controls here\n"
	allocs := testing.AllocsPerRun(100, func() {
		if got := StripTerminalControl(plain); got != plain {
			t.Fatalf("allocation check changed plain text to %q", got)
		}
	})
	if allocs != 0 {
		t.Fatalf("plain text allocations = %v, want zero", allocs)
	}

	t.Run("ring matches stripped child text and preserves raw entries", func(t *testing.T) {
		r, err := newRing(Limits{RetainedBytes: 1024, DefaultReadEntries: 16, DefaultReadBytes: 1024})
		if err != nil {
			t.Fatal(err)
		}
		at := time.Unix(7, 11)
		stdoutRaw := "\x1b[31mready\x1b[0m\r\n"
		stderrRaw := "\x1b[32mwarning\x1b[0m\n"
		systemRaw := "\x1b[35msystem\x1b[0m\n"
		controlOnlyRaw := "\x1b[2J\x1b[K"
		for _, entry := range []struct {
			stream Stream
			text   string
		}{
			{Stdout, stdoutRaw}, {Stderr, stderrRaw}, {System, systemRaw}, {Stdout, controlOnlyRaw},
		} {
			if _, err := r.append(entry.stream, at, entry.text); err != nil {
				t.Fatal(err)
			}
		}

		ready, err := r.read(ReadOptions{Streams: BothStreams, Match: regexp.MustCompile(`^ready`)})
		if err != nil {
			t.Fatal(err)
		}
		if len(ready.Entries) != 1 || ready.Entries[0].Cursor != 0 || ready.Entries[0].Text != stdoutRaw {
			t.Fatalf("stripped child match = %#v, want raw stdout cursor 0", ready.Entries)
		}
		if ready.Next == nil || *ready.Next != 3 {
			t.Fatalf("stripped child match next = %v, want raw newest cursor 3", ready.Next)
		}

		warning, err := r.read(ReadOptions{Streams: StderrMask, Match: regexp.MustCompile(`^warning`)})
		if err != nil {
			t.Fatal(err)
		}
		if len(warning.Entries) != 1 || warning.Entries[0].Cursor != 1 || warning.Entries[0].Stream != Stderr || warning.Entries[0].Text != stderrRaw {
			t.Fatalf("stripped stderr match = %#v, want raw stderr cursor 1", warning.Entries)
		}

		empty, err := r.read(ReadOptions{Streams: StdoutMask, Match: regexp.MustCompile(`^$`)})
		if err != nil {
			t.Fatal(err)
		}
		if len(empty.Entries) != 1 || empty.Entries[0].Cursor != 3 || empty.Entries[0].Text != controlOnlyRaw {
			t.Fatalf("control-only match = %#v, want raw entry at cursor 3", empty.Entries)
		}

		rawSystemMatch, err := r.read(ReadOptions{Match: regexp.MustCompile(`\x1b`)})
		if err != nil {
			t.Fatal(err)
		}
		if len(rawSystemMatch.Entries) != 1 || rawSystemMatch.Entries[0].Stream != System || rawSystemMatch.Entries[0].Text != systemRaw {
			t.Fatalf("system raw match = %#v, want only raw system entry", rawSystemMatch.Entries)
		}

		all, err := r.read(ReadOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if len(all.Entries) != 4 || all.Entries[0].Text != stdoutRaw || all.Entries[1].Text != stderrRaw || all.Entries[2].Text != systemRaw || all.Entries[3].Text != controlOnlyRaw {
			t.Fatalf("ring returned entries = %#v, want raw stored text", all.Entries)
		}
		if all.Next == nil || *all.Next != 3 {
			t.Fatalf("ring returned next = %v, want 3", all.Next)
		}

		store, err := NewStore(Limits{RetainedBytes: 1024, DefaultReadEntries: 16, DefaultReadBytes: 1024})
		if err != nil {
			t.Fatal(err)
		}
		cursor, err := store.Append(Stdout, at, stdoutRaw)
		if err != nil {
			t.Fatal(err)
		}
		stored, err := store.Read(ReadOptions{Match: regexp.MustCompile(`^ready`)})
		if err != nil {
			t.Fatal(err)
		}
		if len(stored.Entries) != 1 || stored.Entries[0].Cursor != cursor || stored.Entries[0].Text != stdoutRaw {
			t.Fatalf("store returned entry = %#v, want raw cursor %d", stored.Entries, cursor)
		}
		store.mu.Lock()
		storedRaw := store.ring.entries[store.ring.head].Text
		store.mu.Unlock()
		if storedRaw != stdoutRaw {
			t.Fatalf("stored text = %q, want raw %q", storedRaw, stdoutRaw)
		}

		heavy := "\x1b[31mfit\x1b[0m"
		heavyRing, err := newRing(Limits{RetainedBytes: 1024, DefaultReadEntries: 16, DefaultReadBytes: 1024})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := heavyRing.append(Stdout, at, heavy); err != nil {
			t.Fatal(err)
		}
		_, err = heavyRing.read(ReadOptions{MaxBytes: len("fit")})
		var tooLarge *EntryTooLargeError
		if !errors.As(err, &tooLarge) || tooLarge.Cursor != 0 || tooLarge.Size != len(heavy) || tooLarge.Limit != len("fit") {
			t.Fatalf("ANSI-heavy byte limit error = %v, want raw size %d and limit %d", err, len(heavy), len("fit"))
		}
	})
}
