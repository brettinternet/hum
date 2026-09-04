package output

import (
	"errors"
	"fmt"
	"regexp"
	"time"
)

// Cursor identifies an entry in a process's output sequence.
//
// Cursors are assigned from zero and are never reused for the life of the
// sequence, including when older entries have been evicted.
type Cursor uint64

// Stream identifies the source of an output entry.
type Stream uint8

const (
	// Zero is intentionally not a valid stream. Keeping it available for the
	// zero value makes malformed stream values fail closed at the append seam.
	Stdout Stream = iota + 1
	Stderr
	System
)

// StreamMask selects streams in a read. A zero mask selects every stream.
type StreamMask uint8

const (
	StdoutMask StreamMask = 1 << (Stdout - 1)
	StderrMask StreamMask = 1 << (Stderr - 1)
	SystemMask StreamMask = 1 << (System - 1)

	// AllStreams is the zero mask; zero has the useful "all" meaning required
	// by ReadOptions and avoids a special value outside the mask's bit range.
	AllStreams  StreamMask = 0
	BothStreams            = StdoutMask | StderrMask

	// The Mask* spellings are aliases kept alongside the stream-named masks so
	// callers can choose either the noun-first or mask-first form.
	MaskStdout = StdoutMask
	MaskStderr = StderrMask
	MaskSystem = SystemMask
	MaskBoth   = BothStreams
)

// Conservative defaults used when a Limits field is zero.
const (
	DefaultRetainedBytes = 4 << 20
	DefaultReadEntries   = 100
	DefaultReadBytes     = 16 << 10

	// RetainedBytesDefault is an alternate descriptive spelling for callers
	// that keep defaults grouped by field name.
	RetainedBytesDefault       = DefaultRetainedBytes
	DefaultRetainedOutputBytes = DefaultRetainedBytes
)

// Entry is one immutable output record. Text is kept as a string so arbitrary
// bytes, including embedded NULs and invalid UTF-8, are preserved exactly.
type Entry struct {
	Cursor Cursor
	Stream Stream
	Time   time.Time
	Text   string
}

// Limits controls retention and the defaults used by bounded reads.
type Limits struct {
	RetainedBytes      int
	DefaultReadEntries int
	DefaultReadBytes   int
}

// ReadOptions controls a deterministic read from a ring or Store.
type ReadOptions struct {
	// After is strict-exclusive. A nil pointer means before cursor zero.
	After *Cursor
	// Tail selects the final Tail matching entries in the requested range when
	// positive. Zero disables tail selection.
	Tail    int
	Streams StreamMask
	Match   *regexp.Regexp

	// Zero values use the configured defaults. Negative values are invalid.
	MaxEntries int
	MaxBytes   int
}

// ReadResult is the bounded result of a read. Next is the highest source
// cursor consumed by this read and can be passed directly as After for the
// following read. It is nil when no entries are retained.
type ReadResult struct {
	Entries        []Entry
	Next           *Cursor
	Oldest         *Cursor
	Latest         *Cursor
	EvictedThrough *Cursor
	Truncated      bool
	More           bool
}

// Exit describes the terminal transition delivered to followers.
type Exit struct {
	Code int
	Time time.Time
}

// Event is one follower notification. Exactly one of Read or Exit is normally
// set; an exit event is delivered only after output through the exit watermark
// has been drained.
type Event struct {
	Read *ReadResult
	Exit *Exit
}

var (
	ErrFutureCursor   = errors.New("output cursor is in the future")
	ErrEntryTooLarge  = errors.New("output entry exceeds the configured bound")
	ErrReadLimit      = errors.New("output read limit is invalid")
	ErrInvalidStream  = errors.New("output stream is invalid")
	ErrEmptyText      = errors.New("output entry text is empty")
	ErrCursorOverflow = errors.New("output cursor overflow")
	ErrInvalidLimits  = errors.New("output limits are invalid")
	ErrStoreClosed    = errors.New("output store is closed")
)

// FutureCursorError reports an After cursor beyond the current sequence
// boundary. The latest assigned cursor is the exact boundary that succeeds;
// a cursor greater than it is in the future.
type FutureCursorError struct {
	After  Cursor
	Latest Cursor
	Next   Cursor
}

func (e *FutureCursorError) Error() string {
	if e == nil {
		return ErrFutureCursor.Error()
	}
	return fmt.Sprintf("output cursor %d is in the future (next cursor %d)", e.After, e.Next)
}

func (e *FutureCursorError) Unwrap() error { return ErrFutureCursor }

// EntryTooLargeError reports an entry that cannot fit in the retention or
// requested read byte bound.
type EntryTooLargeError struct {
	Cursor Cursor
	Bytes  int
	Size   int
	Limit  int
}

func (e *EntryTooLargeError) Error() string {
	if e == nil {
		return ErrEntryTooLarge.Error()
	}
	size := e.Size
	if size == 0 {
		size = e.Bytes
	}
	return fmt.Sprintf("output entry %d is %d bytes, exceeds limit %d", e.Cursor, size, e.Limit)
}

func (e *EntryTooLargeError) Unwrap() error { return ErrEntryTooLarge }

// ReadLimitError reports an invalid or unavailable read limit.
type ReadLimitError struct {
	Field     string
	Requested int
	Limit     int
}

func (e *ReadLimitError) Error() string {
	if e == nil {
		return ErrReadLimit.Error()
	}
	if e.Field == "" {
		return fmt.Sprintf("output read limit %d is invalid", e.Requested)
	}
	if e.Limit > 0 {
		return fmt.Sprintf("output %s limit %d is invalid (maximum %d)", e.Field, e.Requested, e.Limit)
	}
	return fmt.Sprintf("output %s limit %d is invalid", e.Field, e.Requested)
}

func (e *ReadLimitError) Unwrap() error { return ErrReadLimit }

// InvalidStreamError reports an append using a value outside Stdout, Stderr,
// and System.
type InvalidStreamError struct {
	Stream Stream
}

func (e *InvalidStreamError) Error() string {
	if e == nil {
		return ErrInvalidStream.Error()
	}
	return fmt.Sprintf("output stream %d is invalid", e.Stream)
}

func (e *InvalidStreamError) Unwrap() error { return ErrInvalidStream }

// EmptyTextError reports an append with no bytes. Newline is part of a valid
// non-empty text value and is not stripped or normalized.
type EmptyTextError struct{}

func (e *EmptyTextError) Error() string { return ErrEmptyText.Error() }
func (e *EmptyTextError) Unwrap() error { return ErrEmptyText }

// CursorOverflowError reports that assigning another cursor would wrap the
// uint64 sequence.
type CursorOverflowError struct{}

func (e *CursorOverflowError) Error() string { return ErrCursorOverflow.Error() }
func (e *CursorOverflowError) Unwrap() error { return ErrCursorOverflow }

// InvalidLimitsError reports limits that cannot describe a bounded ring.
type InvalidLimitsError struct {
	Limits Limits
}

func (e *InvalidLimitsError) Error() string {
	if e == nil {
		return ErrInvalidLimits.Error()
	}
	return fmt.Sprintf("output limits are invalid: retained bytes=%d, default entries=%d, default bytes=%d", e.Limits.RetainedBytes, e.Limits.DefaultReadEntries, e.Limits.DefaultReadBytes)
}

func (e *InvalidLimitsError) Unwrap() error { return ErrInvalidLimits }

// Common aliases make the typed errors discoverable without duplicating error
// implementations.
type FutureError = FutureCursorError
type CursorFutureError = FutureCursorError
type OversizedEntryError = EntryTooLargeError
type InvalidReadLimitError = ReadLimitError
