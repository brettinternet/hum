package output

import (
	"context"
	"errors"
	"regexp"
	"sync"
	"testing"
	"time"
)

func TestFollowerEviction(t *testing.T) {
	store, err := NewStore(Limits{RetainedBytes: 4, DefaultReadEntries: 100, DefaultReadBytes: 100})
	if err != nil {
		t.Fatal(err)
	}
	sub := store.Subscribe(ReadOptions{})

	for _, text := range []string{"aa", "bb", "cc"} {
		if _, err := store.Append(Stdout, time.Unix(0, 0), text); err != nil {
			t.Fatal(err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	event, err := sub.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if event.Read == nil || event.Exit != nil {
		t.Fatalf("first follower event = %#v, want read", event)
	}
	if !event.Read.Truncated {
		t.Fatalf("follower read was not marked truncated: %#v", event.Read)
	}
	if event.Read.EvictedThrough == nil || *event.Read.EvictedThrough != 0 {
		t.Fatalf("evicted through = %v, want cursor 0", event.Read.EvictedThrough)
	}
	if len(event.Read.Entries) != 2 || event.Read.Entries[0].Cursor != 1 || event.Read.Entries[1].Cursor != 2 {
		t.Fatalf("retained follower entries = %#v", event.Read.Entries)
	}

	cancel()
	_, err = sub.Next(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled follower Next = %v, want context.Canceled", err)
	}
}
func TestFollowerExitWatermarkBoundsTail(t *testing.T) {
	store, err := NewStore(Limits{RetainedBytes: 1024, DefaultReadEntries: 100, DefaultReadBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	sub := store.Subscribe(ReadOptions{Tail: 1})
	preExit, err := store.Append(Stdout, time.Unix(1, 0), "before\n")
	if err != nil {
		t.Fatal(err)
	}
	store.NotifyExit(Exit{Code: 7, Time: time.Unix(2, 0)})
	postExit, err := store.Append(Stdout, time.Unix(3, 0), "after\n")
	if err != nil {
		t.Fatal(err)
	}
	if preExit != 0 || postExit != 1 {
		t.Fatalf("cursors = %d, %d; want 0, 1", preExit, postExit)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	event, err := sub.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if event.Read == nil || event.Exit != nil || len(event.Read.Entries) != 1 || event.Read.Entries[0].Cursor != preExit {
		t.Fatalf("pre-exit event = %#v, want one pre-exit entry", event)
	}

	event, err = sub.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if event.Exit == nil || event.Read != nil || event.Exit.Code != 7 {
		t.Fatalf("exit event = %#v, want exit code 7", event)
	}

	event, err = sub.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if event.Read == nil || event.Exit != nil || len(event.Read.Entries) != 1 || event.Read.Entries[0].Cursor != postExit {
		t.Fatalf("post-exit event = %#v, want one post-exit entry", event)
	}
}

func TestExitRecordsDiscardedWithoutFollowers(t *testing.T) {
	store, err := NewStore(Limits{RetainedBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	for i := range 128 {
		store.NotifyExit(Exit{Code: i})
		if len(store.exits) != 0 {
			t.Fatalf("exit history after notification %d = %d, want empty", i, len(store.exits))
		}
	}
}

func TestIdleFollowerExitHistoryBounded(t *testing.T) {
	store, err := NewStore(Limits{RetainedBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	sub := store.Subscribe(ReadOptions{})
	const total = maxExitHistory*3 + 7
	for code := range total {
		store.NotifyExit(Exit{Code: code})
		if got := len(store.exits); got > maxExitHistory {
			t.Fatalf("exit history after notification %d = %d, want at most %d", code, got, maxExitHistory)
		}
		if got := cap(store.exits); got > maxExitHistory {
			t.Fatalf("exit history capacity after notification %d = %d, want at most %d", code, got, maxExitHistory)
		}
	}
	if len(store.exits) != maxExitHistory {
		t.Fatalf("retained exit history = %d, want %d", len(store.exits), maxExitHistory)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for want := total - maxExitHistory; want < total; want++ {
		event, err := sub.Next(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if event.Exit == nil || event.Read != nil || event.Exit.Code != want {
			t.Fatalf("retained exit event = %#v, want code %d", event, want)
		}
	}
}
func TestCanceledFollowerReclaimsExits(t *testing.T) {
	store, err := NewStore(Limits{RetainedBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	sub := store.Subscribe(ReadOptions{})
	store.NotifyExit(Exit{Code: 1})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = sub.Next(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled follower Next = %v, want context.Canceled", err)
	}
	if len(store.exits) != 0 {
		t.Fatalf("exit history after canceled follower = %d, want 0", len(store.exits))
	}
	if len(store.subscriptions) != 0 {
		t.Fatalf("live subscriptions after cancellation = %d, want 0", len(store.subscriptions))
	}
	_, err = sub.Next(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("repeated canceled follower Next = %v, want context.Canceled", err)
	}
	if len(store.exits) != 0 || len(store.subscriptions) != 0 {
		t.Fatalf("store state after repeated cancellation = exits %d, subscriptions %d; want 0, 0", len(store.exits), len(store.subscriptions))
	}
}

func TestFollowerReadErrorUnregisters(t *testing.T) {
	store, err := NewStore(Limits{RetainedBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(Stdout, time.Unix(1, 0), "output\n"); err != nil {
		t.Fatal(err)
	}
	future := Cursor(1)
	sub := store.Subscribe(ReadOptions{After: &future})
	store.NotifyExit(Exit{Code: 9})

	_, err = sub.Next(context.Background())
	var futureErr *FutureCursorError
	if !errors.As(err, &futureErr) {
		t.Fatalf("follower read error = %v, want FutureCursorError", err)
	}
	if len(store.exits) != 0 || len(store.subscriptions) != 0 {
		t.Fatalf("store state after follower read error = exits %d, subscriptions %d; want 0, 0", len(store.exits), len(store.subscriptions))
	}
}

func testSlowFollowerExitCompaction(t *testing.T) {
	store, err := NewStore(Limits{RetainedBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	first := store.Subscribe(ReadOptions{})
	second := store.Subscribe(ReadOptions{})
	store.NotifyExit(Exit{Code: 1})
	store.NotifyExit(Exit{Code: 2})

	readExit := func(sub *Subscription, want int) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		event, err := sub.Next(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if event.Exit == nil || event.Read != nil || event.Exit.Code != want {
			t.Fatalf("exit event = %#v, want code %d", event, want)
		}
	}

	readExit(first, 1)
	if len(store.exits) != 2 {
		t.Fatalf("exit history after first follower consumed one = %d, want 2", len(store.exits))
	}
	readExit(first, 2)
	if len(store.exits) != 2 {
		t.Fatalf("exit history while second follower is slow = %d, want 2", len(store.exits))
	}
	readExit(second, 1)
	if len(store.exits) != 1 {
		t.Fatalf("exit history after both consumed first = %d, want 1", len(store.exits))
	}
	readExit(second, 2)
	if len(store.exits) != 0 {
		t.Fatalf("exit history after all followers consumed = %d, want 0", len(store.exits))
	}
}

func TestStoreReadFilters(t *testing.T) {
	store, err := NewStore(Limits{RetainedBytes: 1024, DefaultReadEntries: 100, DefaultReadBytes: 1024})
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
		if _, err := store.Append(entry.stream, time.Unix(1, 0), entry.text); err != nil {
			t.Fatal(err)
		}
	}

	stdout := ReadOptions{Streams: StdoutMask, Match: regexp.MustCompile(`ready|done`)}
	result, err := store.Read(stdout)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) != 2 || result.Entries[0].Cursor != 0 || result.Entries[1].Cursor != 2 {
		t.Fatalf("stdout/match entries = %#v", result.Entries)
	}
	if result.Next == nil || *result.Next != 3 {
		t.Fatalf("stdout/match next = %v, want 3", result.Next)
	}

	tail, err := store.Read(ReadOptions{Tail: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(tail.Entries) != 2 || tail.Entries[0].Cursor != 2 || tail.Entries[1].Cursor != 3 {
		t.Fatalf("tail entries = %#v", tail.Entries)
	}

	maxBytes, err := store.Read(ReadOptions{MaxBytes: len("ready\n") + len("warn\n")})
	if err != nil {
		t.Fatal(err)
	}
	if len(maxBytes.Entries) != 2 || !maxBytes.More {
		t.Fatalf("bounded read = %#v, want two entries and More", maxBytes)
	}

	after := Cursor(0)
	next, err := store.Read(ReadOptions{After: &after, Streams: StderrMask})
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Entries) != 1 || next.Entries[0].Cursor != 1 {
		t.Fatalf("after/filter entries = %#v", next.Entries)
	}
}

func TestMultipleFollowers(t *testing.T) {
	store, err := NewStore(Limits{RetainedBytes: 1024, DefaultReadEntries: 100, DefaultReadBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	first := store.Subscribe(ReadOptions{})
	second := store.Subscribe(ReadOptions{})

	if _, err := store.Append(Stdout, time.Unix(2, 0), "before\n"); err != nil {
		t.Fatal(err)
	}

	read := func(sub *Subscription) Event {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		event, err := sub.Next(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return event
	}
	for _, sub := range []*Subscription{first, second} {
		event := read(sub)
		if event.Read == nil || len(event.Read.Entries) != 1 || event.Read.Entries[0].Text != "before\n" {
			t.Fatalf("follower read = %#v", event)
		}
	}

	store.NotifyExit(Exit{Code: 7, Time: time.Unix(3, 0)})
	if _, err := store.Append(Stderr, time.Unix(4, 0), "after\n"); err != nil {
		t.Fatal(err)
	}
	for _, sub := range []*Subscription{first, second} {
		event := read(sub)
		if event.Exit == nil || event.Exit.Code != 7 || event.Read != nil {
			t.Fatalf("exit event = %#v", event)
		}
	}
	if len(store.exits) != 0 {
		t.Fatalf("exit history after both followers consumed = %d, want 0", len(store.exits))
	}

	if _, err := store.Append(System, time.Unix(5, 0), "still-open\n"); err != nil {
		t.Fatal(err)
	}
	for _, sub := range []*Subscription{first, second} {
		event := read(sub)
		if event.Read == nil || len(event.Read.Entries) < 1 || event.Read.Entries[0].Text != "after\n" {
			t.Fatalf("appendable follower read = %#v", event)
		}
	}
	t.Run("Cancellation", testFollowerConcurrentAppendAndCancel)
	t.Run("SlowFollower", testFollowerAppendDoesNotBlock)
	t.Run("SlowFollowerExitCompaction", testSlowFollowerExitCompaction)
}

func testFollowerConcurrentAppendAndCancel(t *testing.T) {
	store, err := NewStore(Limits{RetainedBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	sub := store.Subscribe(ReadOptions{})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := sub.Next(ctx)
		result <- err
	}()
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("concurrent cancellation error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("follower remained blocked after cancellation")
	}
	store.mu.Lock()
	live := len(store.subscriptions)
	store.mu.Unlock()
	if live != 0 {
		t.Fatalf("live subscriptions after cancellation = %d, want 0", live)
	}
}

func testFollowerAppendDoesNotBlock(t *testing.T) {
	store, err := NewStore(Limits{RetainedBytes: 128})
	if err != nil {
		t.Fatal(err)
	}
	sub := store.Subscribe(ReadOptions{})
	const writers = 8
	const entriesPerWriter = 64
	var wg sync.WaitGroup
	wg.Add(writers)
	for range writers {
		go func() {
			defer wg.Done()
			for range entriesPerWriter {
				if _, err := store.Append(Stdout, time.Time{}, "x"); err != nil {
					t.Errorf("append: %v", err)
					return
				}
			}
		}()
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("appends blocked on unread follower")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	event, err := sub.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if event.Read == nil || event.Exit != nil {
		t.Fatalf("slow follower event = %#v, want read", event)
	}
	if !event.Read.Truncated || event.Read.EvictedThrough == nil {
		t.Fatalf("slow follower metadata = %#v, want truncation and eviction", event.Read)
	}
}
