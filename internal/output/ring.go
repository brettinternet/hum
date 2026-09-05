package output

import "time"

// ring is a non-concurrent, byte-bounded sequence of whole entries. Store
// serializes access to it; keeping this type deliberately non-concurrent keeps
// append and read allocation-free apart from the caller-owned result.
type ring struct {
	limits Limits

	entries []Entry
	head    int
	count   int
	bytes   int

	// next is the cursor reserved for the next successful append.
	next Cursor

	evictedThrough Cursor
	hasEvicted     bool
}

// newRing constructs a bounded ring. Zero limits select the package defaults;
// negative limits are rejected before any ring is created.
func newRing(limits Limits) (*ring, error) {
	if limits.RetainedBytes < 0 || limits.DefaultReadEntries < 0 || limits.DefaultReadBytes < 0 {
		return nil, &InvalidLimitsError{Limits: limits}
	}
	if limits.RetainedBytes == 0 {
		limits.RetainedBytes = DefaultRetainedBytes
	}
	if limits.DefaultReadEntries == 0 {
		limits.DefaultReadEntries = DefaultReadEntries
	}
	if limits.DefaultReadBytes == 0 {
		limits.DefaultReadBytes = DefaultReadBytes
	}

	// Every valid entry contains at least one byte, so retained bytes are an
	// upper bound on the number of simultaneously retained entries. Start with
	// the common read size, but avoid a large allocation for a tiny ring.
	capacity := limits.DefaultReadEntries
	if capacity > limits.RetainedBytes {
		capacity = limits.RetainedBytes
	}
	if capacity < 1 {
		capacity = 1
	}
	return &ring{
		limits:  limits,
		entries: make([]Entry, capacity),
	}, nil
}

// append adds one complete output entry and returns its assigned cursor.
// Invalid input and entries larger than the retention bound leave the ring
// unchanged. Cursors advance only after the entry is accepted.
func (r *ring) append(stream Stream, at time.Time, text string) (Cursor, error) {
	if !validStream(stream) {
		return 0, &InvalidStreamError{Stream: stream}
	}
	if len(text) == 0 {
		return 0, &EmptyTextError{}
	}
	if len(text) > r.limits.RetainedBytes {
		return 0, &EntryTooLargeError{Bytes: len(text), Size: len(text), Limit: r.limits.RetainedBytes}
	}
	if r.next == ^Cursor(0) {
		return 0, &CursorOverflowError{}
	}

	// Evict before adding when necessary. Besides preserving the byte bound at
	// every observable point, subtraction-form arithmetic prevents bytes from
	// overflowing int for a hostile near-MaxInt limit.
	available := r.limits.RetainedBytes - len(text)
	for r.count > 0 && r.bytes > available {
		r.evictOldest()
	}

	r.ensureCapacity(r.count + 1)
	index := (r.head + r.count) % len(r.entries)
	cursor := r.next
	r.entries[index] = Entry{Cursor: cursor, Stream: stream, Time: at, Text: text}
	r.count++
	r.bytes += len(text)
	r.next++
	return cursor, nil
}

// read applies cursor, stream, regexp, tail, and whole-entry byte/entry
// limits without mutating the ring. Next advances through every source entry
// consumed while evaluating filters, not only entries returned to the caller.
func (r *ring) read(opts ReadOptions) (ReadResult, error) {
	maxEntries, maxBytes, err := r.readLimits(opts)
	if err != nil {
		return ReadResult{}, err
	}
	if opts.Tail < 0 {
		return ReadResult{}, &ReadLimitError{Field: "tail", Requested: opts.Tail}
	}

	// A cursor equal to the latest assigned cursor is an exact boundary and is
	// valid. Anything greater is future. With no successful append, cursor zero
	// is the empty boundary and remains valid for convenience.
	if opts.After != nil {
		after := *opts.After
		if r.next == 0 {
			if after > 0 {
				return ReadResult{}, &FutureCursorError{After: after, Next: r.next}
			}
		} else if after >= r.next {
			latest := r.next - 1
			if r.count > 0 {
				latest = r.entries[(r.head+r.count-1)%len(r.entries)].Cursor
			}
			return ReadResult{}, &FutureCursorError{After: after, Latest: latest, Next: r.next}
		}
	}

	result := r.metadata()
	if r.count == 0 {
		if r.afterIsStale(opts.After) {
			result.Truncated = true
			through := r.evictedThrough
			result.EvictedThrough = &through
		}
		return result, nil
	}

	oldest := r.entries[r.head].Cursor
	latest := r.entries[(r.head+r.count-1)%len(r.entries)].Cursor
	start := 0
	stale := false
	if opts.After == nil {
		stale = r.hasEvicted
	} else {
		after := *opts.After
		if oldest > 0 && after < oldest-1 {
			stale = true
		} else if after >= oldest {
			start = int(after-oldest) + 1
			if start > r.count {
				start = r.count
			}
		}
	}
	result.Truncated = stale
	if stale {
		through := oldest - 1
		if r.hasEvicted && r.evictedThrough > through {
			through = r.evictedThrough
		}
		result.EvictedThrough = &through
	}

	// An exact boundary after the newest retained entry consumes that boundary
	// even though it returns no entries. This makes the result directly reusable
	// by a follower without manufacturing a one-past cursor.
	if start >= r.count {
		if opts.After != nil {
			next := *opts.After
			result.Next = &next
		} else {
			next := latest
			result.Next = &next
		}
		return result, nil
	}

	if opts.Tail > 0 {
		return r.readTail(opts, result, start, maxEntries, maxBytes)
	}
	return r.readBounded(opts, result, start, maxEntries, maxBytes)
}

func (r *ring) readLimits(opts ReadOptions) (int, int, error) {
	if opts.MaxEntries < 0 {
		return 0, 0, &ReadLimitError{Field: "entries", Requested: opts.MaxEntries}
	}
	if opts.MaxBytes < 0 {
		return 0, 0, &ReadLimitError{Field: "bytes", Requested: opts.MaxBytes}
	}
	maxEntries := opts.MaxEntries
	if maxEntries == 0 {
		maxEntries = r.limits.DefaultReadEntries
	}
	maxBytes := opts.MaxBytes
	if maxBytes == 0 {
		maxBytes = r.limits.DefaultReadBytes
	}
	if maxEntries <= 0 {
		return 0, 0, &ReadLimitError{Field: "entries", Requested: maxEntries}
	}
	if maxBytes <= 0 {
		return 0, 0, &ReadLimitError{Field: "bytes", Requested: maxBytes}
	}
	return maxEntries, maxBytes, nil
}

func (r *ring) metadata() ReadResult {
	result := ReadResult{}
	if r.count == 0 {
		return result
	}
	oldest := r.entries[r.head].Cursor
	latest := r.entries[(r.head+r.count-1)%len(r.entries)].Cursor
	result.Oldest = &oldest
	result.Latest = &latest
	return result
}

func (r *ring) afterIsStale(after *Cursor) bool {
	if !r.hasEvicted {
		return false
	}
	if after == nil {
		return true
	}
	return *after < r.evictedThrough
}

// readBounded walks entries in source order. Once a result limit is reached,
// it consumes subsequent nonmatching entries (advancing Next) and stops just
// before the first matching entry that could not be returned. Thus a caller can
// pass Next back as After without either replaying skipped entries or dropping
// an unread match.
func (r *ring) readBounded(opts ReadOptions, result ReadResult, start, maxEntries, maxBytes int) (ReadResult, error) {
	var output []Entry
	usedBytes := 0
	consumed := false
	var next Cursor

	for offset := start; offset < r.count; offset++ {
		entry := r.entries[(r.head+offset)%len(r.entries)]
		if !matchesRead(entry, opts) {
			consumed = true
			next = entry.Cursor
			continue
		}

		if len(output) >= maxEntries {
			result.More = true
			break
		}
		if len(entry.Text) > maxBytes && len(output) == 0 {
			return result, &EntryTooLargeError{Cursor: entry.Cursor, Bytes: len(entry.Text), Size: len(entry.Text), Limit: maxBytes}
		}
		if len(entry.Text) > maxBytes-usedBytes {
			result.More = true
			break
		}
		if output == nil {
			// Valid entries are nonempty, so MaxBytes is also an entry-count
			// bound for the result backing slice.
			capacity := minInt(maxEntries, maxBytes)
			capacity = minInt(capacity, r.count-start)
			output = make([]Entry, 0, capacity)
		}
		output = append(output, entry)
		usedBytes += len(entry.Text)
		consumed = true
		next = entry.Cursor
	}

	if result.More {
		// The first matching entry at or after the limit remains unconsumed. Any
		// nonmatching entries before it are safe to consume and advance Next.
		for offset := start; offset < r.count; offset++ {
			entry := r.entries[(r.head+offset)%len(r.entries)]
			if !consumed || entry.Cursor <= next {
				continue
			}
			if matchesRead(entry, opts) {
				break
			}
			consumed = true
			next = entry.Cursor
		}
		if !hasMatchingAfter(r, opts, next) {
			result.More = false
			// If no later match exists, all skipped entries were consumed. The
			// loop above may have stopped only at a matching entry, so consume the
			// remainder now to make Next the newest source cursor.
			for offset := start; offset < r.count; offset++ {
				entry := r.entries[(r.head+offset)%len(r.entries)]
				if !consumed || entry.Cursor <= next {
					continue
				}
				consumed = true
				next = entry.Cursor
			}
		}
	}

	if consumed {
		result.Next = &next
	} else if opts.After != nil {
		boundary := *opts.After
		result.Next = &boundary
	}
	result.Entries = output
	return result, nil
}

// readTail scans the full requested range to count matching entries, then
// walks the final Tail matches in chronological order. All source entries are
// consumed during selection, so Next is the newest retained cursor even when
// no entry matched the filter.
func (r *ring) readTail(opts ReadOptions, result ReadResult, start, maxEntries, maxBytes int) (ReadResult, error) {
	tail := opts.Tail
	if tail > r.count-start {
		tail = r.count - start
	}
	if tail < 1 {
		return result, nil
	}

	matching := 0
	var next Cursor
	consumed := false
	for offset := start; offset < r.count; offset++ {
		entry := r.entries[(r.head+offset)%len(r.entries)]
		consumed = true
		next = entry.Cursor
		if matchesRead(entry, opts) {
			matching++
		}
	}

	if !consumed {
		if opts.After != nil {
			boundary := *opts.After
			result.Next = &boundary
		}
		return result, nil
	}
	if matching == 0 {
		result.Next = &next
		return result, nil
	}

	selected := minInt(matching, tail)
	skip := matching - selected
	// Valid entries are nonempty, so MaxBytes also bounds the tail result's
	// entry capacity.
	tailResultCapacity := minInt(maxEntries, maxBytes)
	tailResultCapacity = minInt(tailResultCapacity, selected)
	var tailResult []Entry
	usedBytes := 0
	var blocked Cursor
	blockedSet := false
	matchingSeen := 0
	for offset := start; offset < r.count; offset++ {
		entry := r.entries[(r.head+offset)%len(r.entries)]
		if !matchesRead(entry, opts) {
			continue
		}
		if matchingSeen < skip {
			matchingSeen++
			continue
		}
		matchingSeen++
		if len(tailResult) >= maxEntries {
			result.More = true
			blocked = entry.Cursor
			blockedSet = true
			break
		}
		if len(entry.Text) > maxBytes && len(tailResult) == 0 {
			return result, &EntryTooLargeError{Cursor: entry.Cursor, Bytes: len(entry.Text), Size: len(entry.Text), Limit: maxBytes}
		}
		if len(entry.Text) > maxBytes-usedBytes {
			result.More = true
			blocked = entry.Cursor
			blockedSet = true
			break
		}
		if tailResult == nil {
			tailResult = make([]Entry, 0, tailResultCapacity)
		}
		tailResult = append(tailResult, entry)
		usedBytes += len(entry.Text)
	}
	result.Entries = tailResult
	if blockedSet {
		// The blocked matching entry and everything after it remains unread.
		// Cursors are contiguous while retained, so its predecessor is the
		// greatest source cursor safely consumed by this bounded result.
		if blocked > 0 {
			previous := blocked - 1
			result.Next = &previous
		} else if opts.After != nil {
			boundary := *opts.After
			result.Next = &boundary
		}
	} else {
		result.Next = &next
	}
	return result, nil
}

func matchesRead(entry Entry, opts ReadOptions) bool {
	if opts.Streams != 0 && opts.Streams&streamBit(entry.Stream) == 0 {
		return false
	}
	if opts.Match != nil && !opts.Match.MatchString(matchText(entry)) {
		return false
	}
	return true
}

func matchText(entry Entry) string {
	if entry.Stream == Stdout || entry.Stream == Stderr {
		return StripTerminalControl(entry.Text)
	}
	return entry.Text
}

func hasMatchingAfter(r *ring, opts ReadOptions, after Cursor) bool {
	for offset := 0; offset < r.count; offset++ {
		entry := r.entries[(r.head+offset)%len(r.entries)]
		if entry.Cursor > after && matchesRead(entry, opts) {
			return true
		}
	}
	return false
}

func streamBit(stream Stream) StreamMask {
	if !validStream(stream) {
		return 0
	}
	return StreamMask(1 << (stream - 1))
}

func validStream(stream Stream) bool {
	return stream == Stdout || stream == Stderr || stream == System
}

func (r *ring) ensureCapacity(need int) {
	if need <= len(r.entries) {
		return
	}
	capacity := len(r.entries)
	if capacity < 1 {
		capacity = 1
	}
	for capacity < need {
		if capacity > int(^uint(0)>>1)/2 {
			capacity = need
			break
		}
		capacity *= 2
	}
	grown := make([]Entry, capacity)
	for i := range r.count {
		grown[i] = r.entries[(r.head+i)%len(r.entries)]
	}
	r.entries = grown
	r.head = 0
}

func (r *ring) evictOldest() {
	if r.count == 0 {
		return
	}
	index := r.head
	entry := r.entries[index]
	r.bytes -= len(entry.Text)
	r.entries[index] = Entry{}
	r.head = (r.head + 1) % len(r.entries)
	r.count--
	r.evictedThrough = entry.Cursor
	r.hasEvicted = true
	if r.count == 0 {
		r.head = 0
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
