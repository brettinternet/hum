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

	// Every valid entry consumes at least its text byte and the fixed retention
	// charge, so the charged budget bounds the number of retained entries.
	// Start with the common read size, but avoid a large allocation for a tiny
	// ring.
	capacity := limits.DefaultReadEntries
	if maxEntries := maxRetainedEntries(limits.RetainedBytes); capacity > maxEntries {
		capacity = maxEntries
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
	charge := retainedEntryCharge(len(text))
	if charge > r.limits.RetainedBytes {
		return 0, &EntryTooLargeError{Bytes: charge, Size: charge, Limit: r.limits.RetainedBytes}
	}
	if r.next == ^Cursor(0) {
		return 0, &CursorOverflowError{}
	}

	// Evict before adding when necessary. Besides preserving the charged byte
	// bound at every observable point, subtraction-form arithmetic prevents
	// bytes from overflowing int for a hostile near-MaxInt limit.
	available := r.limits.RetainedBytes - charge
	for r.count > 0 && r.bytes > available {
		r.evictOldest()
	}

	r.ensureCapacity(r.count+1, charge)
	index := (r.head + r.count) % len(r.entries)
	cursor := r.next
	r.entries[index] = Entry{Cursor: cursor, Stream: stream, Time: at, Text: text}
	r.count++
	r.bytes += charge
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
	if opts.Context < 0 {
		return ReadResult{}, &ReadLimitError{Field: "context", Requested: opts.Context}
	}
	if opts.Context > 0 && opts.Match == nil {
		return ReadResult{}, ErrMatchRequired
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
		next := latest
		if opts.After != nil {
			next = *opts.After
		}
		result.Next = &next
		return result, nil
	}

	if opts.Context > 0 {
		selected := r.matchContextSelection(opts, start)
		if opts.Tail > 0 {
			return r.readSelectedTail(opts, result, start, selected, maxEntries, maxBytes)
		}
		return r.readSelectedBounded(opts, result, start, selected, maxEntries, maxBytes)
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
		// A tail read asks for the final N entries; the default entry cap must
		// not clip that window from the front and hide the newest output.
		if opts.Tail > maxEntries {
			maxEntries = opts.Tail
		}
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

	if consumed {
		result.Next = &next
	} else if opts.After != nil {
		boundary := *opts.After
		result.Next = &boundary
	}
	result.Entries = output
	return result, nil
}

// readTail walks backwards from the newest retained entry until the final
// Tail matches are known, evaluating the filter once per entry. Bounds are
// applied while walking backwards so entry- and byte-clipped tails retain the
// newest matches, then the selected entries are returned chronologically. The
// newest source cursor is consumed by this scan, so Next remains the newest
// retained cursor even when older matches are clipped or nothing matched.
func (r *ring) readTail(opts ReadOptions, result ReadResult, start, maxEntries, maxBytes int) (ReadResult, error) {
	tail := opts.Tail
	if tail > r.count-start {
		tail = r.count - start
	}
	if tail < 1 {
		return result, nil
	}
	next := r.entries[(r.head+r.count-1)%len(r.entries)].Cursor

	// selected is newest-first. Keeping that order until bounds are applied is
	// what makes a clipped tail keep its newest entries rather than its oldest
	// prefix.
	selected := make([]int, 0, tail)
	for offset := r.count - 1; offset >= start && len(selected) < tail; offset-- {
		if matchesRead(r.entries[(r.head+offset)%len(r.entries)], opts) {
			selected = append(selected, offset)
		}
	}
	if len(selected) == 0 {
		result.Next = &next
		return result, nil
	}

	// Valid entries are nonempty, so MaxBytes also bounds the tail result's
	// entry capacity.
	tailResultCapacity := minInt(minInt(maxEntries, maxBytes), len(selected))
	var tailResult []Entry
	usedBytes := 0
	for _, offset := range selected {
		entry := r.entries[(r.head+offset)%len(r.entries)]
		if len(tailResult) >= maxEntries {
			result.More = true
			break
		}
		if len(entry.Text) > maxBytes && len(tailResult) == 0 {
			return result, &EntryTooLargeError{Cursor: entry.Cursor, Bytes: len(entry.Text), Size: len(entry.Text), Limit: maxBytes}
		}
		if len(entry.Text) > maxBytes-usedBytes {
			result.More = true
			break
		}
		if tailResult == nil {
			tailResult = make([]Entry, 0, tailResultCapacity)
		}
		tailResult = append(tailResult, entry)
		usedBytes += len(entry.Text)
	}
	for i, j := 0, len(tailResult)-1; i < j; i, j = i+1, j-1 {
		tailResult[i], tailResult[j] = tailResult[j], tailResult[i]
	}
	result.Entries = tailResult
	// Tail selection scans from the newest source entry. Even when an older
	// matching entry is blocked by the result bound, Next must remain the
	// highest consumed source cursor so subscriptions do not replay entries
	// already delivered in this newest-first scan.
	result.Next = &next
	return result, nil
}

// matchContextSelection snapshots the eligible sequence, then expands every
// regex match by Context positions in that sequence. A boolean selection is
// the merged union of those windows, so source order is preserved without
// duplicates and ineligible entries never count toward or enter a window.
func (r *ring) matchContextSelection(opts ReadOptions, start int) []bool {
	// Include eligible match anchors before start so a continuation cursor can
	// recover the unreturned trailing edge of a context window. Entries at or
	// before start are never selected for return, so context still does not cross
	// the exclusive After boundary.
	eligible := make([]int, 0, r.count)
	matches := make([]int, 0)
	for offset := 0; offset < r.count; offset++ {
		entry := r.entries[(r.head+offset)%len(r.entries)]
		if !matchesEligible(entry, opts) {
			continue
		}
		eligible = append(eligible, offset)
		if opts.Match.MatchString(matchText(entry)) {
			matches = append(matches, len(eligible)-1)
		}
	}

	selected := make([]bool, r.count-start)
	windowFirst, windowLast := -1, -1
	markWindow := func() {
		for index := windowFirst; index <= windowLast; index++ {
			if eligible[index] >= start {
				selected[eligible[index]-start] = true
			}
		}
	}
	for _, match := range matches {
		first := match - opts.Context
		if first < 0 {
			first = 0
		}
		last := len(eligible) - 1
		if opts.Context < len(eligible)-match {
			last = match + opts.Context
		}
		if windowFirst < 0 {
			windowFirst, windowLast = first, last
			continue
		}
		if first <= windowLast+1 {
			if last > windowLast {
				windowLast = last
			}
			continue
		}
		markWindow()
		windowFirst, windowLast = first, last
	}
	if windowFirst >= 0 {
		markWindow()
	}
	return selected
}

// readSelectedBounded uses a precomputed match-context selection while keeping
// the forward cursor contract: unselected source entries are consumed, but the
// first selected entry blocked by a result bound is not.
func (r *ring) readSelectedBounded(opts ReadOptions, result ReadResult, start int, selected []bool, maxEntries, maxBytes int) (ReadResult, error) {
	var entries []Entry
	usedBytes := 0
	consumed := false
	var next Cursor

	for offset := start; offset < r.count; offset++ {
		entry := r.entries[(r.head+offset)%len(r.entries)]
		if !selected[offset-start] {
			consumed = true
			next = entry.Cursor
			continue
		}
		if len(entries) >= maxEntries {
			result.More = true
			break
		}
		if len(entry.Text) > maxBytes && len(entries) == 0 {
			return result, &EntryTooLargeError{Cursor: entry.Cursor, Bytes: len(entry.Text), Size: len(entry.Text), Limit: maxBytes}
		}
		if len(entry.Text) > maxBytes-usedBytes {
			result.More = true
			break
		}
		if entries == nil {
			capacity := minInt(minInt(maxEntries, maxBytes), len(selected))
			entries = make([]Entry, 0, capacity)
		}
		entries = append(entries, entry)
		usedBytes += len(entry.Text)
		consumed = true
		next = entry.Cursor
	}
	if consumed {
		result.Next = &next
	} else if opts.After != nil {
		boundary := *opts.After
		result.Next = &boundary
	}
	result.Entries = entries
	return result, nil
}

func (r *ring) readSelectedTail(opts ReadOptions, result ReadResult, start int, selected []bool, maxEntries, maxBytes int) (ReadResult, error) {
	next := r.entries[(r.head+r.count-1)%len(r.entries)].Cursor
	selectedOffsets := make([]int, 0, minInt(opts.Tail, len(selected)))
	for offset := r.count - 1; offset >= start && len(selectedOffsets) < opts.Tail; offset-- {
		if selected[offset-start] {
			selectedOffsets = append(selectedOffsets, offset)
		}
	}
	if len(selectedOffsets) == 0 {
		result.Next = &next
		return result, nil
	}

	capacity := minInt(minInt(maxEntries, maxBytes), len(selectedOffsets))
	entries := make([]Entry, 0, capacity)
	usedBytes := 0
	for _, offset := range selectedOffsets {
		entry := r.entries[(r.head+offset)%len(r.entries)]
		if len(entries) >= maxEntries {
			result.More = true
			break
		}
		if len(entry.Text) > maxBytes && len(entries) == 0 {
			return result, &EntryTooLargeError{Cursor: entry.Cursor, Bytes: len(entry.Text), Size: len(entry.Text), Limit: maxBytes}
		}
		if len(entry.Text) > maxBytes-usedBytes {
			result.More = true
			break
		}
		entries = append(entries, entry)
		usedBytes += len(entry.Text)
	}
	for left, right := 0, len(entries)-1; left < right; left, right = left+1, right-1 {
		entries[left], entries[right] = entries[right], entries[left]
	}
	result.Entries = entries
	result.Next = &next
	return result, nil
}

func matchesRead(entry Entry, opts ReadOptions) bool {
	if !matchesEligible(entry, opts) {
		return false
	}
	return opts.Match == nil || opts.Match.MatchString(matchText(entry))
}

func matchesEligible(entry Entry, opts ReadOptions) bool {
	if !opts.Since.IsZero() && entry.Time.Before(opts.Since) {
		return false
	}
	return opts.Streams == 0 || opts.Streams&streamBit(entry.Stream) != 0
}

func matchText(entry Entry) string {
	if entry.Stream == Stdout || entry.Stream == Stderr {
		return StripTerminalControl(entry.Text)
	}
	return entry.Text
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

func (r *ring) ensureCapacity(need, charge int) {
	if need <= len(r.entries) {
		return
	}
	capacity := len(r.entries)
	if capacity < 1 {
		capacity = 1
	}
	maxCapacity := maxRetainedEntriesForCharge(r.limits.RetainedBytes, charge)
	if maxCapacity < need {
		// Existing entries may be shorter than this incoming entry. The current
		// count is already budget-valid after eviction, so never shrink below
		// the capacity needed to place it.
		maxCapacity = need
	}
	if capacity > maxCapacity {
		capacity = maxCapacity
	}
	for capacity < need {
		if capacity > int(^uint(0)>>1)/2 {
			capacity = need
			break
		}
		capacity *= 2
		if capacity > maxCapacity {
			capacity = maxCapacity
			break
		}
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
	r.bytes -= retainedEntryCharge(len(entry.Text))
	r.entries[index] = Entry{}
	r.head = (r.head + 1) % len(r.entries)
	r.count--
	r.evictedThrough = entry.Cursor
	r.hasEvicted = true
	if r.count == 0 {
		r.head = 0
	}
}

func retainedEntryCharge(textLen int) int {
	maxInt := int(^uint(0) >> 1)
	if textLen > maxInt-RetainedEntryOverhead {
		return maxInt
	}
	return textLen + RetainedEntryOverhead
}

func maxRetainedEntries(limit int) int {
	return maxRetainedEntriesForCharge(limit, RetainedEntryOverhead+1)
}

func maxRetainedEntriesForCharge(limit, charge int) int {
	return limit / charge
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
