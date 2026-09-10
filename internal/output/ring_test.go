package output

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
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

func TestReadMatchContext(t *testing.T) {
	newContextRing := func(t *testing.T) *ring {
		t.Helper()
		r, err := newRing(Limits{RetainedBytes: 4096, DefaultReadEntries: 100, DefaultReadBytes: 4096})
		if err != nil {
			t.Fatal(err)
		}
		for i := range 9 {
			stream := Stdout
			if i%2 != 0 {
				stream = Stderr
			}
			if _, err := r.append(stream, time.Unix(int64(i), 0), fmt.Sprintf("line-%d\n", i)); err != nil {
				t.Fatal(err)
			}
		}
		return r
	}
	cursors := func(entries []Entry) []Cursor {
		result := make([]Cursor, len(entries))
		for i, entry := range entries {
			result[i] = entry.Cursor
		}
		return result
	}
	read := func(t *testing.T, r *ring, opts ReadOptions, want []Cursor) ReadResult {
		t.Helper()
		result, err := r.read(opts)
		if err != nil {
			t.Fatal(err)
		}
		if got := cursors(result.Entries); !reflect.DeepEqual(got, want) {
			t.Fatalf("context cursors = %v, want %v (result=%#v)", got, want, result)
		}
		return result
	}

	t.Run("before and after", func(t *testing.T) {
		read(t, newContextRing(t), ReadOptions{Match: regexp.MustCompile(`line-4`), Context: 2}, []Cursor{2, 3, 4, 5, 6})
	})
	t.Run("overlapping adjacent multiple matches", func(t *testing.T) {
		read(t, newContextRing(t), ReadOptions{Match: regexp.MustCompile(`line-(2|4|7)`), Context: 1}, []Cursor{1, 2, 3, 4, 5, 6, 7, 8})
	})
	t.Run("large context is clipped without overflow", func(t *testing.T) {
		read(t, newContextRing(t), ReadOptions{Match: regexp.MustCompile(`line-4`), Context: int(^uint(0) >> 1)}, []Cursor{0, 1, 2, 3, 4, 5, 6, 7, 8})
	})
	t.Run("after clips context", func(t *testing.T) {
		after := Cursor(3)
		read(t, newContextRing(t), ReadOptions{After: &after, Match: regexp.MustCompile(`line-5`), Context: 2}, []Cursor{4, 5, 6, 7})
	})
	t.Run("since clips context", func(t *testing.T) {
		read(t, newContextRing(t), ReadOptions{Since: time.Unix(4, 0), Match: regexp.MustCompile(`line-5`), Context: 2}, []Cursor{4, 5, 6, 7})
	})
	t.Run("stream counts eligible entries", func(t *testing.T) {
		read(t, newContextRing(t), ReadOptions{Streams: StdoutMask, Match: regexp.MustCompile(`line-4`), Context: 1}, []Cursor{2, 4, 6})
	})
	t.Run("retention clips context and reports stale", func(t *testing.T) {
		r, err := newRing(Limits{RetainedBytes: 3 * (RetainedEntryOverhead + 2), DefaultReadEntries: 10, DefaultReadBytes: 100})
		if err != nil {
			t.Fatal(err)
		}
		for i := range 5 {
			if _, err := r.append(Stdout, time.Time{}, fmt.Sprintf("%d\n", i)); err != nil {
				t.Fatal(err)
			}
		}
		result := read(t, r, ReadOptions{Match: regexp.MustCompile(`3`), Context: 2}, []Cursor{2, 3, 4})
		if !result.Truncated || result.EvictedThrough == nil || *result.EvictedThrough != 1 {
			t.Fatalf("retention metadata = %#v, want truncation through 1", result)
		}
	})
	t.Run("snapshot boundary", func(t *testing.T) {
		store, err := NewStore(Limits{RetainedBytes: 4096, DefaultReadEntries: 10, DefaultReadBytes: 4096})
		if err != nil {
			t.Fatal(err)
		}
		for i := range 5 {
			if _, err := store.Append(Stdout, time.Time{}, fmt.Sprintf("line-%d\n", i)); err != nil {
				t.Fatal(err)
			}
		}
		store.mu.Lock()
		result, err := store.readThroughLocked(ReadOptions{Match: regexp.MustCompile(`line-3`), Context: 2}, 3, true)
		store.mu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
		if got, want := cursors(result.Entries), []Cursor{1, 2, 3}; !reflect.DeepEqual(got, want) {
			t.Fatalf("snapshot cursors = %v, want %v", got, want)
		}
	})
	t.Run("zero context and no match", func(t *testing.T) {
		r := newContextRing(t)
		read(t, r, ReadOptions{Match: regexp.MustCompile(`line-(2|6)`), Context: 0}, []Cursor{2, 6})
		result := read(t, r, ReadOptions{Match: regexp.MustCompile(`absent`), Context: 2}, []Cursor{})
		if result.Next == nil || *result.Next != 8 || result.More {
			t.Fatalf("no-match metadata = %#v, want next 8 without more", result)
		}
	})
}

func TestReadMatchContextBounds(t *testing.T) {
	r, err := newRing(Limits{RetainedBytes: 4096, DefaultReadEntries: 10, DefaultReadBytes: 4096})
	if err != nil {
		t.Fatal(err)
	}
	for i := range 7 {
		if _, err := r.append(Stdout, time.Time{}, fmt.Sprintf("line-%d\n", i)); err != nil {
			t.Fatal(err)
		}
	}
	match := regexp.MustCompile(`line-3`)
	first, err := r.read(ReadOptions{Match: match, Context: 2, MaxEntries: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := []Cursor{first.Entries[0].Cursor, first.Entries[1].Cursor}, []Cursor{1, 2}; !reflect.DeepEqual(got, want) || !first.More || first.Next == nil || *first.Next != 2 {
		t.Fatalf("first bounded context = %#v, cursors %v want %v", first, got, want)
	}
	after := *first.Next
	continued, err := r.read(ReadOptions{After: &after, Match: match, Context: 2, MaxEntries: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := []Cursor{continued.Entries[0].Cursor, continued.Entries[1].Cursor}, []Cursor{3, 4}; !reflect.DeepEqual(got, want) || continued.Next == nil || *continued.Next != 4 {
		t.Fatalf("continued context = %#v, cursors %v want %v", continued, got, want)
	}
	after = *continued.Next
	last, err := r.read(ReadOptions{After: &after, Match: match, Context: 2, MaxEntries: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := []Cursor{last.Entries[0].Cursor}, []Cursor{5}; !reflect.DeepEqual(got, want) || last.More {
		t.Fatalf("last context page = %#v, cursors %v want %v", last, got, want)
	}

	tail, err := r.read(ReadOptions{Match: regexp.MustCompile(`line-(1|5)`), Context: 1, Tail: 3, MaxEntries: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := []Cursor{tail.Entries[0].Cursor, tail.Entries[1].Cursor}, []Cursor{5, 6}; !reflect.DeepEqual(got, want) || !tail.More || tail.Next == nil || *tail.Next != 6 {
		t.Fatalf("tail context = %#v, cursors %v want %v", tail, got, want)
	}

	if _, err := r.read(ReadOptions{Context: 1}); !errors.Is(err, ErrMatchRequired) {
		t.Fatalf("context without match error = %v, want ErrMatchRequired", err)
	}
	if _, err := r.read(ReadOptions{Match: match, Context: -1}); !errors.Is(err, ErrReadLimit) {
		t.Fatalf("negative context error = %v, want ErrReadLimit", err)
	}
}

func TestTailResultCapacityHonorsByteLimit(t *testing.T) {
	r, err := newRing(Limits{RetainedBytes: 8 * (RetainedEntryOverhead + 1), DefaultReadEntries: 32, DefaultReadBytes: 32})
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
	if len(result.Entries) != 1 || result.Entries[0].Cursor != 7 {
		t.Fatalf("tail byte-bounded entries = %#v, want only newest cursor 7", result.Entries)
	}
	if !result.More {
		t.Fatalf("tail byte-bounded result = %#v, want More", result)
	}
	if cap(result.Entries) > 1 {
		t.Fatalf("tail byte-bounded capacity = %d, want at most 1", cap(result.Entries))
	}
}

func TestTailResultCapacityHonorsSparseFilter(t *testing.T) {
	r, err := newRing(Limits{RetainedBytes: 8 * (RetainedEntryOverhead + 1), DefaultReadEntries: 8, DefaultReadBytes: 8})
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
		retained = 2048 * (RetainedEntryOverhead + 1)
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
	r, err := newRing(Limits{RetainedBytes: 2*RetainedEntryOverhead + 5, DefaultReadEntries: 16, DefaultReadBytes: 64})
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
	if _, err := r.append(Stdout, at, strings.Repeat("x", r.limits.RetainedBytes)); !errors.Is(err, ErrEntryTooLarge) {
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
	r, err := newRing(Limits{RetainedBytes: 3 * (RetainedEntryOverhead + 2), DefaultReadEntries: 16, DefaultReadBytes: 64})
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

func TestTailKeepsNewestBoundedWindow(t *testing.T) {
	r, err := newRing(Limits{RetainedBytes: 1024, DefaultReadEntries: 4, DefaultReadBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		if _, err := r.append(Stdout, time.Unix(int64(i), 0), fmt.Sprintf("line-%d\n", i)); err != nil {
			t.Fatal(err)
		}
	}

	entryBounded, err := r.read(ReadOptions{Tail: 6, MaxEntries: 2, MaxBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if got := []Cursor{entryBounded.Entries[0].Cursor, entryBounded.Entries[1].Cursor}; !reflect.DeepEqual(got, []Cursor{6, 7}) {
		t.Fatalf("entry-bounded tail cursors = %v, want newest [6 7]", got)
	}
	if !entryBounded.More || entryBounded.Next == nil || *entryBounded.Next != 7 {
		t.Fatalf("entry-bounded tail = %#v, want More and highest consumed cursor 7", entryBounded)
	}

	byteBounded, err := r.read(ReadOptions{Tail: 8, MaxEntries: 8, MaxBytes: len("line-7\n") * 2})
	if err != nil {
		t.Fatal(err)
	}
	if got := []Cursor{byteBounded.Entries[0].Cursor, byteBounded.Entries[1].Cursor}; !reflect.DeepEqual(got, []Cursor{6, 7}) {
		t.Fatalf("byte-bounded tail cursors = %v, want newest [6 7] chronologically", got)
	}
	if !byteBounded.More || byteBounded.Next == nil || *byteBounded.Next != 7 {
		t.Fatalf("byte-bounded tail = %#v, want More and highest consumed cursor 7", byteBounded)
	}

	filtered, err := r.read(ReadOptions{Tail: 4, Streams: StdoutMask, Match: regexp.MustCompile(`line-[02468]`), MaxEntries: 2, MaxBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if got := []Cursor{filtered.Entries[0].Cursor, filtered.Entries[1].Cursor}; !reflect.DeepEqual(got, []Cursor{4, 6}) {
		t.Fatalf("filtered tail cursors = %v, want newest matching [4 6] chronologically", got)
	}
	if !filtered.More {
		t.Fatalf("filtered tail = %#v, want More=true", filtered)
	}

	truncatedRing, err := newRing(Limits{RetainedBytes: 4 * (RetainedEntryOverhead + 1), DefaultReadEntries: 4, DefaultReadBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		if _, err := truncatedRing.append(Stdout, time.Time{}, "x"); err != nil {
			t.Fatal(err)
		}
	}
	truncated, err := truncatedRing.read(ReadOptions{Tail: 2, MaxEntries: 1, MaxBytes: 4})
	if err != nil {
		t.Fatal(err)
	}
	if len(truncated.Entries) != 1 || truncated.Entries[0].Cursor != 5 || !truncated.More || !truncated.Truncated || truncated.EvictedThrough == nil || *truncated.EvictedThrough != 1 {
		t.Fatalf("truncated tail = %#v, want newest cursor, More, and eviction metadata", truncated)
	}

	after := Cursor(1)
	forward, err := r.read(ReadOptions{After: &after, MaxEntries: 2, MaxBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if got := []Cursor{forward.Entries[0].Cursor, forward.Entries[1].Cursor}; !reflect.DeepEqual(got, []Cursor{2, 3}) || !forward.More {
		t.Fatalf("explicit after read = %#v, want oldest eligible forward page [2 3] with More", forward)
	}

	tailedForward, err := r.read(ReadOptions{After: &after, Tail: 2, MaxEntries: 2, MaxBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if got := []Cursor{tailedForward.Entries[0].Cursor, tailedForward.Entries[1].Cursor}; !reflect.DeepEqual(got, []Cursor{6, 7}) || tailedForward.More {
		t.Fatalf("explicit after with tail read = %#v, want newest eligible tail [6 7] without More", tailedForward)
	}
}

func TestTailLargerThanDefaultEntriesReturnsNewest(t *testing.T) {
	r, err := newRing(Limits{RetainedBytes: 1024, DefaultReadEntries: 2, DefaultReadBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, err := r.append(Stdout, time.Time{}, "line\n"); err != nil {
			t.Fatal(err)
		}
	}
	result, err := r.read(ReadOptions{Tail: 4})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) != 4 || result.Entries[0].Cursor != 1 || result.Entries[3].Cursor != 4 || result.More {
		t.Fatalf("tail 4 with default entry limit 2 = %#v, want cursors 1-4 without More", result)
	}
	explicit, err := r.read(ReadOptions{Tail: 4, MaxEntries: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(explicit.Entries) != 2 || !explicit.More {
		t.Fatalf("tail 4 with explicit entry limit 2 = %#v, want two entries and More", explicit)
	}
}

func TestRetentionChargesEntryOverhead(t *testing.T) {
	const firstText = "a"
	const secondText = "bbbb"
	firstCharge := len(firstText) + RetainedEntryOverhead
	secondCharge := len(secondText) + RetainedEntryOverhead
	limit := firstCharge + secondCharge
	r, err := newRing(Limits{RetainedBytes: limit, DefaultReadEntries: 100, DefaultReadBytes: 100})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.append(Stdout, time.Time{}, firstText); err != nil {
		t.Fatal(err)
	}
	if r.bytes != firstCharge {
		t.Fatalf("first retained charge = %d, want %d", r.bytes, firstCharge)
	}
	if _, err := r.append(Stdout, time.Time{}, secondText); err != nil {
		t.Fatal(err)
	}
	if r.bytes != limit || r.bytes > r.limits.RetainedBytes {
		t.Fatalf("retained charge after two entries = %d, limit %d", r.bytes, r.limits.RetainedBytes)
	}

	next, count, bytes, entries := r.next, r.count, r.bytes, append([]Entry(nil), r.entries...)
	tooLargeText := strings.Repeat("c", limit)
	_, err = r.append(Stdout, time.Time{}, tooLargeText)
	var tooLarge *EntryTooLargeError
	tooLargeCharge := len(tooLargeText) + RetainedEntryOverhead
	if !errors.As(err, &tooLarge) || tooLarge.Size != tooLargeCharge || tooLarge.Limit != limit {
		t.Fatalf("oversized charged append error = %v, want size %d and limit %d", err, tooLargeCharge, limit)
	}
	if r.next != next || r.count != count || r.bytes != bytes || !equalEntries(r.entries, entries) {
		t.Fatalf("oversized append mutated ring: next/count/bytes %d/%d/%d -> %d/%d/%d", next, count, bytes, r.next, r.count, r.bytes)
	}

	if _, err := r.append(Stdout, time.Time{}, "x"); err != nil {
		t.Fatal(err)
	}
	if r.bytes > r.limits.RetainedBytes {
		t.Fatalf("retained charge after eviction = %d, limit %d", r.bytes, r.limits.RetainedBytes)
	}
	if r.entries[r.head].Text != secondText && r.entries[r.head].Text != "x" {
		t.Fatalf("oldest retained entry after eviction = %#v", r.entries[r.head])
	}
}

func TestRetentionBoundsShortEntryCardinality(t *testing.T) {
	const (
		limit = 4 << 20
		text  = "0123456789"
		total = 300000
	)
	perEntry := len(text) + RetainedEntryOverhead
	maxEntries := limit / perEntry
	r, err := newRing(Limits{RetainedBytes: limit, DefaultReadEntries: 100, DefaultReadBytes: total * len(text)})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < total; i++ {
		if _, err := r.append(Stdout, time.Time{}, text); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	if r.count > maxEntries || len(r.entries) > maxEntries || r.bytes > limit {
		t.Fatalf("retention bounds count/capacity/bytes = %d/%d/%d, want count/capacity <= %d and bytes <= %d", r.count, len(r.entries), r.bytes, maxEntries, limit)
	}
	if r.bytes != r.count*perEntry {
		t.Fatalf("retained accounting = %d, want %d entries * %d", r.bytes, r.count, perEntry)
	}
	result, err := r.read(ReadOptions{MaxEntries: maxEntries, MaxBytes: total * len(text)})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) != r.count || result.Next == nil || *result.Next != Cursor(total-1) {
		t.Fatalf("chronological read count/next = %d/%v, want %d/%d", len(result.Entries), result.Next, r.count, total-1)
	}
	first := Cursor(total - r.count)
	for i, entry := range result.Entries {
		if entry.Cursor != first+Cursor(i) || entry.Text != text {
			t.Fatalf("entry %d = %#v, want cursor %d and text %q", i, entry, first+Cursor(i), text)
		}
	}
	active := make(map[int]bool, r.count)
	for i := 0; i < r.count; i++ {
		active[(r.head+i)%len(r.entries)] = true
	}
	for i, entry := range r.entries {
		if !active[i] && entry != (Entry{}) {
			t.Fatalf("evicted slot %d retains entry %#v", i, entry)
		}
	}
}

func equalEntries(a, b []Entry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestReadSince(t *testing.T) {
	r, err := newRing(Limits{RetainedBytes: 4096, DefaultReadEntries: 16, DefaultReadBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	cutoff := time.Unix(10, 0)
	entries := []struct {
		stream Stream
		at     time.Time
		text   string
	}{
		{Stdout, cutoff.Add(-time.Second), "old\n"},
		{Stdout, cutoff, "exact\n"},
		{Stderr, cutoff.Add(time.Second), "stderr\n"},
		{Stdout, cutoff.Add(2 * time.Second), "keep\n"},
		{System, cutoff.Add(3 * time.Second), "system\n"},
	}
	for _, entry := range entries {
		if _, err := r.append(entry.stream, entry.at, entry.text); err != nil {
			t.Fatal(err)
		}
	}

	inclusive, err := r.read(ReadOptions{Since: cutoff})
	if err != nil {
		t.Fatal(err)
	}
	if got := []Cursor{inclusive.Entries[0].Cursor, inclusive.Entries[1].Cursor, inclusive.Entries[2].Cursor, inclusive.Entries[3].Cursor}; !reflect.DeepEqual(got, []Cursor{1, 2, 3, 4}) {
		t.Fatalf("since-inclusive cursors = %v, want [1 2 3 4]", got)
	}
	if inclusive.Next == nil || *inclusive.Next != 4 {
		t.Fatalf("since next = %v, want 4", inclusive.Next)
	}

	after := Cursor(1)
	filtered, err := r.read(ReadOptions{After: &after, Since: cutoff, Streams: StdoutMask, Match: regexp.MustCompile(`keep`)})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered.Entries) != 1 || filtered.Entries[0].Cursor != 3 {
		t.Fatalf("after/since/stream/match entries = %#v, want cursor 3", filtered.Entries)
	}
	if filtered.Next == nil || *filtered.Next != 4 {
		t.Fatalf("filtered next = %v, want 4", filtered.Next)
	}

	bounded, err := newRing(Limits{RetainedBytes: 4096, DefaultReadEntries: 16, DefaultReadBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, err := bounded.append(Stdout, time.Unix(int64(i), 0), fmt.Sprintf("line-%d\n", i)); err != nil {
			t.Fatal(err)
		}
	}
	window, err := bounded.read(ReadOptions{Since: time.Unix(0, 0), Tail: 4, MaxEntries: 2, MaxBytes: len("line-3\n") + len("line-4\n")})
	if err != nil {
		t.Fatal(err)
	}
	if got := []Cursor{window.Entries[0].Cursor, window.Entries[1].Cursor}; !reflect.DeepEqual(got, []Cursor{3, 4}) {
		t.Fatalf("since tail bounds cursors = %v, want [3 4]", got)
	}
	if !window.More || window.Next == nil || *window.Next != 4 {
		t.Fatalf("since tail bounds result = %#v, want More and Next 4", window)
	}

	evicted, err := newRing(Limits{RetainedBytes: 2 * retainedEntryCharge(len("x\n")), DefaultReadEntries: 8, DefaultReadBytes: 64})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if _, err := evicted.append(Stdout, time.Unix(int64(i), 0), "x\n"); err != nil {
			t.Fatal(err)
		}
	}
	staleAfter := Cursor(0)
	truncated, err := evicted.read(ReadOptions{After: &staleAfter, Since: time.Unix(2, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if len(truncated.Entries) != 2 || truncated.Entries[0].Cursor != 2 || truncated.Entries[1].Cursor != 3 {
		t.Fatalf("evicted since entries = %#v, want retained cursors 2 and 3", truncated.Entries)
	}
	if !truncated.Truncated || truncated.EvictedThrough == nil || *truncated.EvictedThrough != 1 || truncated.Next == nil || *truncated.Next != 3 {
		t.Fatalf("evicted since result = %#v, want truncation through 1 and next 3", truncated)
	}

	store, err := NewStore(Limits{RetainedBytes: 4096, DefaultReadEntries: 16, DefaultReadBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	subscriptionCutoff := time.Unix(10, 0)
	if _, err := store.Append(Stdout, subscriptionCutoff.Add(-time.Second), "historical\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(Stdout, subscriptionCutoff.Add(time.Second), "retained\n"); err != nil {
		t.Fatal(err)
	}
	sub := store.Subscribe(ReadOptions{Since: subscriptionCutoff})
	defer sub.Close()
	if _, err := store.Append(Stdout, subscriptionCutoff.Add(-2*time.Second), "live-old-timestamp\n"); err != nil {
		t.Fatal(err)
	}
	store.NotifyExit(Exit{Code: 7, Time: subscriptionCutoff.Add(2 * time.Second)})
	readContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	first, err := sub.Next(readContext)
	if err != nil {
		t.Fatal(err)
	}
	if first.Read == nil || len(first.Read.Entries) != 1 || first.Read.Entries[0].Text != "retained\n" {
		t.Fatalf("since subscription initial replay = %#v, want retained entry only", first)
	}
	second, err := sub.Next(readContext)
	if err != nil {
		t.Fatal(err)
	}
	if second.Read == nil || len(second.Read.Entries) != 1 || second.Read.Entries[0].Text != "live-old-timestamp\n" {
		t.Fatalf("since subscription live read = %#v, want post-subscription old-timestamp entry", second)
	}
	third, err := sub.Next(readContext)
	if err != nil {
		t.Fatal(err)
	}
	if third.Exit == nil || third.Exit.Code != 7 {
		t.Fatalf("since subscription exit = %#v, want exit after filtered replay and live output", third)
	}
}
