package output

import (
	"errors"
	"regexp"
	"testing"
	"time"
)

func TestReadFilters(t *testing.T) {
	r, err := newRing(Limits{RetainedBytes: 1024, DefaultReadEntries: 100, DefaultReadBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	entries := []struct {
		stream Stream
		text   string
	}{
		{Stdout, "ready\n"},
		{Stderr, "warn\n"},
		{Stdout, "done\n"},
		{System, "exit\n"},
	}
	for _, entry := range entries {
		if _, err := r.append(entry.stream, time.Unix(1, 0), entry.text); err != nil {
			t.Fatal(err)
		}
	}

	stdout, err := r.read(ReadOptions{Streams: StdoutMask, Match: regexp.MustCompile(`ready|done`)})
	if err != nil {
		t.Fatal(err)
	}
	if len(stdout.Entries) != 2 || stdout.Entries[0].Cursor != 0 || stdout.Entries[1].Cursor != 2 {
		t.Fatalf("stdout/match entries = %#v", stdout.Entries)
	}
	if stdout.Next == nil || *stdout.Next != 3 {
		t.Fatalf("stdout/match next = %v, want 3", stdout.Next)
	}

	stderr, err := r.read(ReadOptions{Streams: StderrMask})
	if err != nil {
		t.Fatal(err)
	}
	if len(stderr.Entries) != 1 || stderr.Entries[0].Cursor != 1 {
		t.Fatalf("stderr entries = %#v", stderr.Entries)
	}
	if stderr.Next == nil || *stderr.Next != 3 {
		t.Fatalf("stderr next = %v, want 3", stderr.Next)
	}

	both, err := r.read(ReadOptions{Streams: BothStreams})
	if err != nil {
		t.Fatal(err)
	}
	if len(both.Entries) != 3 || both.Entries[0].Cursor != 0 || both.Entries[2].Cursor != 2 {
		t.Fatalf("stdout/stderr entries = %#v", both.Entries)
	}

	tail, err := r.read(ReadOptions{Tail: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(tail.Entries) != 2 || tail.Entries[0].Cursor != 2 || tail.Entries[1].Cursor != 3 {
		t.Fatalf("tail entries = %#v", tail.Entries)
	}
	if tail.Next == nil || *tail.Next != 3 {
		t.Fatalf("tail next = %v, want 3", tail.Next)
	}

	bounded, err := r.read(ReadOptions{MaxEntries: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(bounded.Entries) != 2 || !bounded.More {
		t.Fatalf("entry-bounded read = %#v, want two entries and More", bounded)
	}
	if bounded.Next == nil || *bounded.Next != 1 {
		t.Fatalf("entry-bounded next = %v, want 1", bounded.Next)
	}
	after := *bounded.Next
	continued, err := r.read(ReadOptions{After: &after, MaxEntries: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(continued.Entries) != 2 || continued.Entries[0].Cursor != 2 || continued.Entries[1].Cursor != 3 {
		t.Fatalf("continued bounded read = %#v", continued.Entries)
	}

	byteBounded, err := r.read(ReadOptions{MaxBytes: len("ready\n") + len("warn\n")})
	if err != nil {
		t.Fatal(err)
	}
	if len(byteBounded.Entries) != 2 || byteBounded.Entries[0].Text != "ready\n" || !byteBounded.More {
		t.Fatalf("byte-bounded read = %#v", byteBounded)
	}
	whole, err := r.read(ReadOptions{MaxBytes: len("ready\n") + 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(whole.Entries) != 1 || whole.Entries[0].Cursor != 0 || !whole.More {
		t.Fatalf("whole-entry byte cap = %#v", whole)
	}

	invalidBytes := string([]byte{0x00, 0xff, '\n'})
	if _, err := r.append(System, time.Time{}, invalidBytes); err != nil {
		t.Fatal(err)
	}
	raw, err := r.read(ReadOptions{Streams: SystemMask, Match: regexp.MustCompile(`^\x00`)})
	if err != nil {
		t.Fatal(err)
	}
	if len(raw.Entries) != 1 || raw.Entries[0].Text != invalidBytes {
		t.Fatalf("raw bytes = %#v, want %q", raw.Entries, invalidBytes)
	}
}

func TestTailResultCapacityHonorsByteLimit(t *testing.T) {
	r, err := newRing(Limits{RetainedBytes: 32, DefaultReadEntries: 32, DefaultReadBytes: 32})
	if err != nil {
		t.Fatal(err)
	}
	for range 8 {
		if _, err := r.append(Stdout, time.Time{}, "x"); err != nil {
			t.Fatal(err)
		}
	}

	result, err := r.read(ReadOptions{Tail: 8, MaxEntries: 1 << 20, MaxBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) != 1 || result.Entries[0].Cursor != 0 {
		t.Fatalf("tail byte-bounded entries = %#v, want only cursor 0", result.Entries)
	}
	if !result.More {
		t.Fatalf("tail byte-bounded result = %#v, want More", result)
	}
	if cap(result.Entries) > 1 {
		t.Fatalf("tail byte-bounded capacity = %d, want at most 1", cap(result.Entries))
	}
}

func TestTailResultCapacityHonorsSparseFilter(t *testing.T) {
	r, err := newRing(Limits{RetainedBytes: 32, DefaultReadEntries: 8, DefaultReadBytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	for i := range 8 {
		stream := Stdout
		if i == 3 {
			stream = System
		}
		if _, err := r.append(stream, time.Time{}, "x"); err != nil {
			t.Fatal(err)
		}
	}

	result, err := r.read(ReadOptions{
		Tail:       8,
		Streams:    SystemMask,
		MaxEntries: 8,
		MaxBytes:   8,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) != 1 || result.Entries[0].Cursor != 3 {
		t.Fatalf("sparse tail entries = %#v, want only system cursor 3", result.Entries)
	}
	if result.More {
		t.Fatalf("sparse tail result = %#v, want More=false", result)
	}
	if result.Next == nil || *result.Next != 7 {
		t.Fatalf("sparse tail next = %v, want 7", result.Next)
	}
	if cap(result.Entries) > 1 {
		t.Fatalf("sparse tail capacity = %d, want at most 1", cap(result.Entries))
	}
}

func TestLargeTailPreservesChronologicalOrder(t *testing.T) {
	const (
		total    = 5000
		retained = 2048
		tail     = 1024
	)
	r, err := newRing(Limits{RetainedBytes: retained, DefaultReadEntries: 8, DefaultReadBytes: retained})
	if err != nil {
		t.Fatal(err)
	}
	for range total {
		if _, err := r.append(Stdout, time.Time{}, "x"); err != nil {
			t.Fatal(err)
		}
	}

	result, err := r.read(ReadOptions{Tail: tail, MaxEntries: total, MaxBytes: retained})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) != tail || result.More {
		t.Fatalf("large tail result = %#v, want %d entries without More", result, tail)
	}
	first := Cursor(total - tail)
	for i, entry := range result.Entries {
		want := first + Cursor(i)
		if entry.Cursor != want {
			t.Fatalf("large tail entry %d cursor = %d, want %d", i, entry.Cursor, want)
		}
	}
	if result.Next == nil || *result.Next != Cursor(total-1) {
		t.Fatalf("large tail next = %v, want %d", result.Next, total-1)
	}
}

func TestRingEviction(t *testing.T) {
	r, err := newRing(Limits{RetainedBytes: 5, DefaultReadEntries: 16, DefaultReadBytes: 64})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Unix(7, 11)
	for i, text := range []string{"aa", "bb", "ccc", "d"} {
		cursor, err := r.append(Stdout, at, text)
		if err != nil {
			t.Fatalf("append %q: %v", text, err)
		}
		if cursor != Cursor(i) {
			t.Fatalf("append %q cursor = %d, want %d", text, cursor, i)
		}
	}
	if r.bytes > r.limits.RetainedBytes {
		t.Fatalf("retained bytes = %d, bound = %d", r.bytes, r.limits.RetainedBytes)
	}

	result, err := r.read(ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) != 2 || result.Entries[0].Cursor != 2 || result.Entries[1].Cursor != 3 {
		t.Fatalf("retained entries = %#v, want cursors 2 and 3", result.Entries)
	}
	if result.Entries[0].Text != "ccc" || result.Entries[1].Text != "d" {
		t.Fatalf("retained text = %#v", result.Entries)
	}
	if result.Next == nil || *result.Next != 3 {
		t.Fatalf("next = %v, want 3", result.Next)
	}

	before := r.next
	if _, err := r.append(Stream(0), at, "invalid"); !errors.Is(err, ErrInvalidStream) {
		t.Fatalf("invalid stream error = %v", err)
	}
	if _, err := r.append(Stdout, at, ""); !errors.Is(err, ErrEmptyText) {
		t.Fatalf("empty text error = %v", err)
	}
	if _, err := r.append(Stdout, at, "123456"); !errors.Is(err, ErrEntryTooLarge) {
		t.Fatalf("oversized entry error = %v", err)
	}
	if r.next != before {
		t.Fatalf("failed append advanced next from %d to %d", before, r.next)
	}
	cursor, err := r.append(Stderr, at, "ok")
	if err != nil {
		t.Fatal(err)
	}
	if cursor != before {
		t.Fatalf("cursor after rejected appends = %d, want %d", cursor, before)
	}
	if r.bytes > r.limits.RetainedBytes {
		t.Fatalf("retained bytes after append = %d, bound = %d", r.bytes, r.limits.RetainedBytes)
	}
	for i := range r.entries {
		if r.entries[i].Text == "aa" || r.entries[i].Text == "bb" {
			t.Fatalf("evicted slot %d still retains text %q", i, r.entries[i].Text)
		}
	}
}

func TestCursorTruncation(t *testing.T) {
	r, err := newRing(Limits{RetainedBytes: 6, DefaultReadEntries: 16, DefaultReadBytes: 64})
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"aa", "bb", "cc", "dd", "ee"} {
		if _, err := r.append(Stdout, time.Time{}, text); err != nil {
			t.Fatal(err)
		}
	}
	// Cursors 0 and 1 are gone; retained cursors are 2, 3, and 4.
	all, err := r.read(ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !all.Truncated || all.EvictedThrough == nil || *all.EvictedThrough != 1 {
		t.Fatalf("stale metadata = %#v, want truncation through 1", all)
	}
	if len(all.Entries) != 3 || all.Entries[0].Cursor != 2 || all.Entries[2].Cursor != 4 {
		t.Fatalf("stale entries = %#v", all.Entries)
	}
	if all.Next == nil || *all.Next != 4 {
		t.Fatalf("stale next = %v, want 4", all.Next)
	}

	exactStart := Cursor(1)
	exact, err := r.read(ReadOptions{After: &exactStart})
	if err != nil {
		t.Fatal(err)
	}
	if exact.Truncated || exact.EvictedThrough != nil {
		t.Fatalf("exact boundary metadata = %#v", exact)
	}
	if len(exact.Entries) != 3 || exact.Entries[0].Cursor != 2 {
		t.Fatalf("exact boundary entries = %#v", exact.Entries)
	}

	staleAfter := Cursor(0)
	stale, err := r.read(ReadOptions{After: &staleAfter})
	if err != nil {
		t.Fatal(err)
	}
	if !stale.Truncated || stale.EvictedThrough == nil || *stale.EvictedThrough != 1 {
		t.Fatalf("stale cursor metadata = %#v", stale)
	}

	latest := Cursor(4)
	exactEnd, err := r.read(ReadOptions{After: &latest})
	if err != nil {
		t.Fatal(err)
	}
	if len(exactEnd.Entries) != 0 || exactEnd.Next == nil || *exactEnd.Next != latest {
		t.Fatalf("exact end = %#v", exactEnd)
	}

	future := Cursor(5)
	_, err = r.read(ReadOptions{After: &future})
	var futureErr *FutureCursorError
	if !errors.As(err, &futureErr) {
		t.Fatalf("future read error = %v, want FutureCursorError", err)
	}
	if futureErr.After != future {
		t.Fatalf("future error after = %d, want %d", futureErr.After, future)
	}

	tooSmall := Cursor(2)
	_, err = r.read(ReadOptions{After: &tooSmall, MaxBytes: 1})
	var largeErr *EntryTooLargeError
	if !errors.As(err, &largeErr) {
		t.Fatalf("small byte cap error = %v, want EntryTooLargeError", err)
	}
}
