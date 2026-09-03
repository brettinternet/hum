package output

import (
	"context"
	"errors"
	"regexp"
	"sync"
	"testing"
	"time"
)

func TestStatusNextCursorTracksSequenceWithoutMutatingRing(t *testing.T) {
	store, err := NewStore(Limits{
		RetainedBytes:      4,
		DefaultReadEntries: 16,
		DefaultReadBytes:   16,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := store.NextCursor(); got != 0 {
		t.Fatalf("empty store next cursor = %d, want 0", got)
	}
	for _, text := range []string{"aa", "bb", "cc"} {
		if _, err := store.Append(Stdout, time.Unix(1, 0), text); err != nil {
			t.Fatal(err)
		}
	}

	store.mu.Lock()
	beforeHead := store.ring.head
	beforeCount := store.ring.count
	beforeBytes := store.ring.bytes
	beforeOldest := store.ring.entries[store.ring.head]
	beforeNewest := store.ring.entries[(store.ring.head+store.ring.count-1)%len(store.ring.entries)]
	store.mu.Unlock()

	if got := store.NextCursor(); got != 3 {
		t.Fatalf("next cursor after eviction = %d, want 3", got)
	}

	store.mu.Lock()
	if store.ring.head != beforeHead || store.ring.count != beforeCount || store.ring.bytes != beforeBytes {
		t.Fatalf("next cursor mutated ring metadata: before head/count/bytes = %d/%d/%d, after = %d/%d/%d",
			beforeHead, beforeCount, beforeBytes, store.ring.head, store.ring.count, store.ring.bytes)
	}
	oldest := store.ring.entries[store.ring.head]
	newest := store.ring.entries[(store.ring.head+store.ring.count-1)%len(store.ring.entries)]
	store.mu.Unlock()
	if oldest != beforeOldest || newest != beforeNewest {
		t.Fatalf("next cursor mutated retained entries: before oldest/newest = %#v/%#v, after = %#v/%#v",
			beforeOldest, beforeNewest, oldest, newest)
	}

	if _, err := store.Append(Stdout, time.Unix(1, 0), ""); err == nil {
		t.Fatal("empty append unexpectedly succeeded")
	}
	if got := store.NextCursor(); got != 3 {
		t.Fatalf("next cursor after failed append = %d, want 3", got)
	}
}
func TestSubscriptionCursorTracksConsumedOutputWait(t *testing.T) {
	store, err := NewStore(Limits{RetainedBytes: 1024, DefaultReadEntries: 16, DefaultReadBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	sub := store.Subscribe(ReadOptions{})
	defer sub.Close()

	if got := sub.Cursor(); got != 0 {
		t.Fatalf("initial subscription cursor = %d, want 0", got)
	}
	if _, err := store.Append(Stdout, time.Unix(1, 0), "first\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(Stdout, time.Unix(2, 0), "second\n"); err != nil {
		t.Fatal(err)
	}
	event := nextStoreEvent(t, sub)
	if event.Read == nil || len(event.Read.Entries) != 2 {
		t.Fatalf("subscription output event = %#v, want two entries", event)
	}
	if got := sub.Cursor(); got != 1 {
		t.Fatalf("subscription cursor after read = %d, want 1", got)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	nextDone := make(chan struct{})
	go func() {
		_, _ = sub.Next(ctx)
		close(nextDone)
	}()
	if _, err := store.Append(Stderr, time.Unix(3, 0), "third\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-nextDone:
	case <-time.After(time.Second):
		t.Fatal("subscription did not consume appended output")
	}
	if got := sub.Cursor(); got != 2 {
		t.Fatalf("concurrent cursor observation = %d, want 2", got)
	}
}

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

func TestExitRecordsRetainedWithoutFollowers(t *testing.T) {
	store, err := NewStore(Limits{RetainedBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	const total = 128
	for code := range total {
		exit := Exit{Code: code, Time: time.Unix(int64(code), 0)}
		store.NotifyExit(exit)
		if got := len(store.exits); got != 1 {
			t.Fatalf("exit history after notification %d = %d, want one latest record", code, got)
		}
		if got := store.exits[0].exit; got != exit {
			t.Fatalf("retained exit after notification %d = %#v, want %#v", code, got, exit)
		}
		if got := cap(store.exits); got > maxExitHistory {
			t.Fatalf("exit history capacity after notification %d = %d, want at most %d", code, got, maxExitHistory)
		}
	}
}

func TestSubscribeSkipsRetainedExit(t *testing.T) {
	store, err := NewStore(Limits{RetainedBytes: 1024, DefaultReadEntries: 100, DefaultReadBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.Append(Stdout, time.Unix(1, 0), "before\n")
	if err != nil {
		t.Fatal(err)
	}
	store.NotifyExit(Exit{Code: 7, Time: time.Unix(2, 0)})

	afterSub := store.Subscribe(ReadOptions{After: &before})
	after, err := store.Append(Stdout, time.Unix(3, 0), "after\n")
	if err != nil {
		t.Fatal(err)
	}
	event := nextStoreEvent(t, afterSub)
	if event.Exit != nil || event.Read == nil || len(event.Read.Entries) != 1 || event.Read.Entries[0].Cursor != after {
		t.Fatalf("new subscription event = %#v, want post-subscribe read without retained exit", event)
	}
}

func TestReplayLatestExitDrainsWatermarkedOutput(t *testing.T) {
	store, err := NewStore(Limits{RetainedBytes: 1024, DefaultReadEntries: 100, DefaultReadBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.Append(Stdout, time.Unix(1, 0), "before\n")
	if err != nil {
		t.Fatal(err)
	}
	exit := Exit{Code: 11, Time: time.Unix(2, 0)}
	store.NotifyExit(exit)
	after, err := store.Append(Stderr, time.Unix(3, 0), "after\n")
	if err != nil {
		t.Fatal(err)
	}

	sub := store.Subscribe(ReadOptions{})
	if !sub.ReplayLatestExit() {
		t.Fatal("ReplayLatestExit() = false, want true for retained pre-subscribe exit")
	}

	event := nextStoreEvent(t, sub)
	if event.Exit != nil || event.Read == nil || len(event.Read.Entries) != 1 || event.Read.Entries[0].Cursor != before {
		t.Fatalf("replayed output event = %#v, want output through retained watermark", event)
	}
	event = nextStoreEvent(t, sub)
	if event.Read != nil || event.Exit == nil || *event.Exit != exit {
		t.Fatalf("replayed exit event = %#v, want exact exit %#v", event, exit)
	}
	event = nextStoreEvent(t, sub)
	if event.Exit != nil || event.Read == nil || len(event.Read.Entries) != 1 || event.Read.Entries[0].Cursor != after {
		t.Fatalf("post-exit output event = %#v, want output after exact exit", event)
	}
}

func TestReplayLatestExitSinceReplaysExitBeforeDone(t *testing.T) {
	store, err := NewStore(Limits{RetainedBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	exit := Exit{Code: 11, Time: time.Unix(101, 0)}
	processStart := exit.Time
	store.NotifyExit(exit)

	sub := store.Subscribe(ReadOptions{})
	defer sub.Close()
	if !sub.ReplayLatestExitSince(processStart) {
		t.Fatalf("ReplayLatestExitSince(%v) = false, want true for retained exit after process start", processStart)
	}
	event := nextStoreEvent(t, sub)
	if event.Read != nil || event.Exit == nil || *event.Exit != exit {
		t.Fatalf("replayed exit event = %#v, want exact exit %#v", event, exit)
	}
}

func TestReplayLatestExitSinceSkipsHistoricalExit(t *testing.T) {
	store, err := NewStore(Limits{RetainedBytes: 1024, DefaultReadEntries: 100, DefaultReadBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	store.NotifyExit(Exit{Code: 7, Time: time.Unix(99, 0)})
	processStart := time.Unix(100, 0)

	sub := store.Subscribe(ReadOptions{})
	defer sub.Close()
	if sub.ReplayLatestExitSince(processStart) {
		t.Fatalf("ReplayLatestExitSince(%v) = true, want false for historical exit", processStart)
	}
	if _, err := store.Append(Stdout, time.Unix(101, 0), "current\n"); err != nil {
		t.Fatal(err)
	}
	event := nextStoreEvent(t, sub)
	if event.Exit != nil || event.Read == nil || len(event.Read.Entries) != 1 || event.Read.Entries[0].Text != "current\n" {
		t.Fatalf("event after historical exit = %#v, want current output without replay", event)
	}
}

func TestReplayLatestExitSinceDoesNotDuplicatePostSubscribeExit(t *testing.T) {
	store, err := NewStore(Limits{RetainedBytes: 1024, DefaultReadEntries: 100, DefaultReadBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	processStart := time.Unix(100, 0)
	sub := store.Subscribe(ReadOptions{})
	defer sub.Close()
	exit := Exit{Code: 13, Time: time.Unix(101, 0)}
	store.NotifyExit(exit)

	if sub.ReplayLatestExitSince(processStart) {
		t.Fatalf("ReplayLatestExitSince(%v) = true, want false for exit appended after Subscribe", processStart)
	}
	event := nextStoreEvent(t, sub)
	if event.Read != nil || event.Exit == nil || *event.Exit != exit {
		t.Fatalf("post-subscribe exit event = %#v, want exact exit %#v", event, exit)
	}
}

func TestSubscriptionCloseIsIdempotentAndWakesNext(t *testing.T) {
	store, err := NewStore(Limits{RetainedBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	sub := store.Subscribe(ReadOptions{})
	nextResult := make(chan error, 1)
	go func() {
		_, err := sub.Next(context.Background())
		nextResult <- err
	}()

	registered := time.NewTimer(time.Second)
	defer registered.Stop()
	for {
		store.mu.Lock()
		live := len(store.subscriptions)
		store.mu.Unlock()
		if live == 1 {
			break
		}
		select {
		case <-registered.C:
			t.Fatal("subscription was not registered before Close")
		default:
			time.Sleep(time.Millisecond)
		}
	}

	sub.Close()
	sub.Close()
	select {
	case err := <-nextResult:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("closed subscription Next = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("subscription Next remained blocked after Close")
	}

	if len(store.subscriptions) != 0 {
		t.Fatalf("live subscriptions after repeated Close = %d, want 0", len(store.subscriptions))
	}
	for code := range maxExitHistory * 2 {
		store.NotifyExit(Exit{Code: code, Time: time.Unix(int64(code), 0)})
	}
	if len(store.subscriptions) != 0 {
		t.Fatalf("live subscriptions after retained exits = %d, want 0", len(store.subscriptions))
	}
	if len(store.exits) != 1 || store.exits[0].exit.Code != maxExitHistory*2-1 {
		t.Fatalf("exit history after repeated Close = %#v, want latest exit only", store.exits)
	}
}

func TestReplayLatestExitDoesNotDuplicatePostSubscribeExit(t *testing.T) {
	store, err := NewStore(Limits{RetainedBytes: 1024, DefaultReadEntries: 100, DefaultReadBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	sub := store.Subscribe(ReadOptions{})
	exit := Exit{Code: 13, Time: time.Unix(4, 0)}
	store.NotifyExit(exit)
	if sub.ReplayLatestExit() {
		t.Fatal("ReplayLatestExit() = true, want false for exit appended after Subscribe")
	}

	event := nextStoreEvent(t, sub)
	if event.Read != nil || event.Exit == nil || *event.Exit != exit {
		t.Fatalf("post-subscribe exit event = %#v, want exact exit %#v", event, exit)
	}
	if _, err := store.Append(Stdout, time.Unix(5, 0), "after\n"); err != nil {
		t.Fatal(err)
	}
	event = nextStoreEvent(t, sub)
	if event.Exit != nil || event.Read == nil || len(event.Read.Entries) != 1 || event.Read.Entries[0].Text != "after\n" {
		t.Fatalf("event after post-subscribe exit = %#v, want one read without duplicate exit", event)
	}
}

func TestClosedSubscriptionCompactsToLatestExit(t *testing.T) {
	store, err := NewStore(Limits{RetainedBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	sub := store.Subscribe(ReadOptions{})
	const total = maxExitHistory*3 + 7
	for code := range total {
		store.NotifyExit(Exit{Code: code, Time: time.Unix(int64(code), 0)})
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = sub.Next(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("closed subscription Next = %v, want context.Canceled", err)
	}
	if len(store.subscriptions) != 0 {
		t.Fatalf("live subscriptions after close = %d, want 0", len(store.subscriptions))
	}
	if len(store.exits) != 1 {
		t.Fatalf("exit history after close = %d, want latest record only", len(store.exits))
	}
	if got, want := store.exits[0].exit.Code, total-1; got != want {
		t.Fatalf("retained exit after close = %d, want latest code %d", got, want)
	}
	if got := cap(store.exits); got > maxExitHistory {
		t.Fatalf("exit history capacity after close = %d, want at most %d", got, maxExitHistory)
	}
}

func nextStoreEvent(t *testing.T, sub *Subscription) Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	event, err := sub.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return event
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
func TestCanceledFollowerCompactsExits(t *testing.T) {
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
	if len(store.exits) != 1 || store.exits[0].exit.Code != 1 {
		t.Fatalf("exit history after canceled follower = %#v, want latest exit code 1", store.exits)
	}
	if len(store.subscriptions) != 0 {
		t.Fatalf("live subscriptions after cancellation = %d, want 0", len(store.subscriptions))
	}
	_, err = sub.Next(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("repeated canceled follower Next = %v, want context.Canceled", err)
	}
	if len(store.exits) != 1 || store.exits[0].exit.Code != 1 || len(store.subscriptions) != 0 {
		t.Fatalf("store state after repeated cancellation = exits %#v, subscriptions %d; want one code 1 and 0 subscriptions", store.exits, len(store.subscriptions))
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
	if len(store.exits) != 1 || store.exits[0].exit.Code != 9 || len(store.subscriptions) != 0 {
		t.Fatalf("store state after follower read error = exits %#v, subscriptions %d; want one code 9 and 0 subscriptions", store.exits, len(store.subscriptions))
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
	if len(store.exits) != 1 || store.exits[0].exit.Code != 2 {
		t.Fatalf("exit history after all followers consumed = %#v, want latest exit code 2", store.exits)
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
	if len(store.exits) != 1 || store.exits[0].exit.Code != 7 {
		t.Fatalf("exit history after both followers consumed = %#v, want latest exit code 7", store.exits)
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

func TestSystemEntryRestartMarkerIsMonotonicAndVisible(t *testing.T) {
	store, err := NewStore(Limits{})
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.Append(Stdout, time.Unix(1, 0), "before\n")
	if err != nil {
		t.Fatal(err)
	}
	follower := store.Subscribe(ReadOptions{After: &first})
	defer follower.Close()

	marker, err := store.Append(System, time.Unix(2, 0), "api restarted")
	if err != nil {
		t.Fatal(err)
	}
	if marker <= first {
		t.Fatalf("marker cursor = %d, want after %d", marker, first)
	}
	bounded, err := store.Read(ReadOptions{After: &first})
	if err != nil {
		t.Fatal(err)
	}
	if len(bounded.Entries) != 1 || bounded.Entries[0].Cursor != marker || bounded.Entries[0].Stream != System {
		t.Fatalf("bounded restart entries = %#v", bounded.Entries)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	event, err := follower.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if event.Read == nil || len(event.Read.Entries) != 1 || event.Read.Entries[0].Cursor != marker || event.Read.Entries[0].Stream != System {
		t.Fatalf("follower restart event = %#v", event)
	}
}

func TestAppendObserverCapturesBeforeEviction(t *testing.T) {
	store, err := NewStore(Limits{RetainedBytes: 8, DefaultReadEntries: 8, DefaultReadBytes: 64})
	if err != nil {
		t.Fatal(err)
	}
	type observed struct {
		entry Entry
	}
	seen := make(chan observed, 2)
	observer := store.ObserveAppend(func(entry Entry) {
		seen <- observed{entry: entry}
	})
	defer observer.Close()

	first, err := store.Append(Stdout, time.Unix(1, 0), "ready\n")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(Stdout, time.Unix(2, 0), "filler\n"); err != nil {
		t.Fatal(err)
	}
	if first != 0 {
		t.Fatalf("first cursor = %d, want 0", first)
	}
	if len(seen) != 2 {
		t.Fatalf("observer callbacks = %d, want 2", len(seen))
	}
	got := <-seen
	if got.entry.Cursor != first || got.entry.Text != "ready\n" {
		t.Fatalf("first observed entry = %#v, want ready at cursor %d", got.entry, first)
	}
	if _, err := store.Read(ReadOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestAppendObserverCloseWaitsForInFlightAppend(t *testing.T) {
	store, err := NewStore(Limits{RetainedBytes: 64})
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	observer := store.ObserveAppend(func(Entry) {
		close(entered)
		<-release
	})

	appendDone := make(chan struct{})
	go func() {
		_, _ = store.Append(Stdout, time.Unix(1, 0), "one\n")
		close(appendDone)
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("append observer did not run")
	}

	closeDone := make(chan struct{})
	go func() {
		observer.Close()
		close(closeDone)
	}()
	select {
	case <-closeDone:
		t.Fatal("observer Close returned while callback was in flight")
	case <-time.After(10 * time.Millisecond):
	}
	close(release)
	select {
	case <-appendDone:
	case <-time.After(time.Second):
		t.Fatal("append did not finish after releasing observer")
	}
	select {
	case <-closeDone:
	case <-time.After(time.Second):
		t.Fatal("observer Close did not finish after append")
	}

	if _, err := store.Append(Stdout, time.Unix(2, 0), "two\n"); err != nil {
		t.Fatal(err)
	}
}
