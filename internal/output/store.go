package output

import (
	"context"
	"sync"
	"time"
)

// maxExitHistory bounds the best-effort exit notification history. Exit
// notifications are wakeups rather than durable lifecycle history; the
// output ring and cursor remain authoritative when older exits are dropped.
const maxExitHistory = 64

// Store owns the process output ring and the generation used to wake pull
// subscribers. The ring and all subscriber state are protected by mu.
type Store struct {
	mu         sync.Mutex
	ring       *ring
	generation chan struct{}
	latest     Cursor
	hasLatest  bool
	// exits is bounded so an idle subscriber cannot retain unbounded
	// notification history.
	exits         []storedExit
	subscriptions map[*Subscription]struct{}
}

type storedExit struct {
	exit       Exit
	through    Cursor
	hasThrough bool
}

// NewStore constructs a bounded output store.
func NewStore(limits Limits) (*Store, error) {
	r, err := newRing(limits)
	if err != nil {
		return nil, err
	}
	return &Store{
		ring:       r,
		generation: make(chan struct{}),
	}, nil
}

// Append adds one entry and wakes every pull subscriber. A failed append does
// not advance the store generation or cursor.
func (s *Store) Append(stream Stream, at time.Time, text string) (Cursor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cursor, err := s.ring.append(stream, at, text)
	if err != nil {
		return 0, err
	}
	s.latest = cursor
	s.hasLatest = true
	s.broadcastLocked()
	return cursor, nil
}

// Read performs a bounded read while holding the store lock, so its metadata
// and retained entries are a single view of the ring.
func (s *Store) Read(opts ReadOptions) (ReadResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ring.read(opts)
}

// Subscribe creates an independent pull subscription. It intentionally does
// not allocate a queue or start a goroutine. The subscription snapshots the
// exit sequence under the same lock used for append and notification.
func (s *Store) Subscribe(opts ReadOptions) *Subscription {
	s.mu.Lock()
	sub := &Subscription{
		store:     s,
		options:   opts,
		exitIndex: len(s.exits),
		firstRead: true,
	}
	if opts.After != nil {
		sub.after = *opts.After
		sub.hasAfter = true
		sub.options.After = nil
	}
	if s.subscriptions == nil {
		s.subscriptions = make(map[*Subscription]struct{})
	}
	s.subscriptions[sub] = struct{}{}
	s.mu.Unlock()
	return sub
}

// NotifyExit records an exit watermark and wakes every pull subscriber. The
// store remains appendable; subscribers deliver the recorded exit only after
// output through the watermark has been drained.
func (s *Store) NotifyExit(exit Exit) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.subscriptions) == 0 {
		s.exits = nil
		return
	}
	record := storedExit{exit: exit, through: s.latest, hasThrough: s.hasLatest}
	s.appendExitLocked(record)
	s.broadcastLocked()
}

// appendExitLocked retains the newest exits up to maxExitHistory. Dropping a
// prefix advances every subscriber past the same dropped notifications.
func (s *Store) appendExitLocked(record storedExit) {
	if len(s.exits) == 0 {
		s.exits = make([]storedExit, 0, maxExitHistory)
	}
	if len(s.exits) == maxExitHistory {
		copy(s.exits, s.exits[1:])
		s.exits[len(s.exits)-1] = record
		for sub := range s.subscriptions {
			if sub.exitIndex > 0 {
				sub.exitIndex--
			}
		}
		return
	}
	s.exits = append(s.exits, record)
}

func (s *Store) broadcastLocked() {
	close(s.generation)
	s.generation = make(chan struct{})
}

func (s *Store) unregisterLocked(sub *Subscription) {
	if sub.closed {
		return
	}
	sub.closed = true
	delete(s.subscriptions, sub)
	s.compactExitsLocked()
	if len(s.subscriptions) == 0 {
		s.subscriptions = nil
	}
}

func (s *Store) compactExitsLocked() {
	if len(s.exits) == 0 {
		return
	}
	if len(s.subscriptions) == 0 {
		s.exits = nil
		return
	}

	consumed := len(s.exits)
	for sub := range s.subscriptions {
		if sub.exitIndex < consumed {
			consumed = sub.exitIndex
		}
	}
	if consumed == 0 {
		return
	}
	if consumed == len(s.exits) {
		s.exits = nil
		for sub := range s.subscriptions {
			sub.exitIndex = 0
		}
		return
	}

	copy(s.exits, s.exits[consumed:])
	for i := len(s.exits) - consumed; i < len(s.exits); i++ {
		s.exits[i] = storedExit{}
	}
	s.exits = s.exits[:len(s.exits)-consumed]
	for sub := range s.subscriptions {
		sub.exitIndex -= consumed
	}
}

func (s *Store) discardSubscription(sub *Subscription) {
	s.mu.Lock()
	s.unregisterLocked(sub)
	s.mu.Unlock()
}

// Subscription is an independent pull view over a Store. Calls to Next are
// expected to be serialized by the consumer; all state is nevertheless read
// and advanced under Store.mu, avoiding per-subscriber locks and queues.
type Subscription struct {
	store     *Store
	options   ReadOptions
	after     Cursor
	hasAfter  bool
	exitIndex int
	firstRead bool
	closed    bool
}

func (sub *Subscription) discardSubscription() {
	sub.store.discardSubscription(sub)
}

// Next waits for the next bounded read result or exit event. Cancellation is
// checked without holding the store lock and cannot leave a blocked send or a
// registered follower behind.
func (sub *Subscription) Next(ctx context.Context) (Event, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	for {
		if err := ctx.Err(); err != nil {
			sub.discardSubscription()
			return Event{}, err
		}

		s := sub.store
		s.mu.Lock()
		if sub.closed {
			s.mu.Unlock()
			if err := ctx.Err(); err != nil {
				return Event{}, err
			}
			return Event{}, context.Canceled
		}

		result, ready, err := sub.readLocked()
		if err != nil {
			s.mu.Unlock()
			sub.discardSubscription()
			return Event{}, err
		}
		if ready {
			s.mu.Unlock()
			return Event{Read: &result}, nil
		}

		if sub.exitIndex < len(s.exits) {
			record := s.exits[sub.exitIndex]
			sub.exitIndex++
			s.compactExitsLocked()
			s.mu.Unlock()
			return Event{Exit: &record.exit}, nil
		}

		wake := s.generation
		s.mu.Unlock()

		select {
		case <-ctx.Done():
			sub.discardSubscription()
			return Event{}, ctx.Err()
		case <-wake:
		}
	}
}

// readLocked consumes output for the subscription's current cursor. It
// returns ready=false only when there is no output or metadata to deliver and
// the caller should wait on the generation channel.
func (sub *Subscription) readLocked() (ReadResult, bool, error) {
	s := sub.store

	for {
		opts := sub.options
		opts.After = nil
		if sub.hasAfter {
			after := sub.after
			opts.After = &after
		}
		if !sub.firstRead {
			opts.Tail = 0
		}
		sub.firstRead = false

		var result ReadResult
		var err error
		if sub.exitIndex < len(s.exits) {
			record := s.exits[sub.exitIndex]
			result, err = s.readThroughLocked(opts, record.through, record.hasThrough)
		} else {
			result, err = s.ring.read(opts)
		}
		if err != nil {
			return ReadResult{}, false, err
		}
		advanced := sub.advanceFromResult(result)

		if len(result.Entries) != 0 || result.Truncated || result.EvictedThrough != nil || result.More {
			return result, true, nil
		}

		// A filtered read may have scanned entries without producing one. Move
		// past those entries and retry once under the same lock; otherwise wait
		// for a later generation.
		if !advanced {
			return ReadResult{}, false, nil
		}
	}
}

// readThroughLocked applies the captured watermark to the source sequence
// before ring.read performs tail selection and filtering. The bounded copy
// shares the ring's backing entries, so this remains a read-only operation.
func (s *Store) readThroughLocked(opts ReadOptions, through Cursor, hasThrough bool) (ReadResult, error) {
	if !hasThrough {
		return ReadResult{}, nil
	}

	bounded := *s.ring
	throughCount := 0
	for throughCount < bounded.count {
		entry := bounded.entries[(bounded.head+throughCount)%len(bounded.entries)]
		if entry.Cursor > through {
			break
		}
		throughCount++
	}
	bounded.count = throughCount
	if bounded.hasEvicted && bounded.evictedThrough > through {
		bounded.evictedThrough = through
	}

	result, err := bounded.read(opts)
	if err != nil {
		return ReadResult{}, err
	}
	if bounded.count == 0 && result.Next == nil {
		result.Next = cursorPtr(through)
	}
	return result, nil
}

func (sub *Subscription) advanceFromResult(result ReadResult) bool {
	var next Cursor
	hasNext := false
	if result.Next != nil {
		next = *result.Next
		hasNext = true
	} else if len(result.Entries) != 0 {
		next = result.Entries[len(result.Entries)-1].Cursor
		hasNext = true
	}
	if !hasNext {
		return false
	}
	if sub.hasAfter && next <= sub.after {
		return false
	}
	sub.after = next
	sub.hasAfter = true
	return true
}

func cursorPtr(cursor Cursor) *Cursor {
	return &cursor
}
