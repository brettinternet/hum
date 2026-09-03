package output

import (
	"errors"
	"sync"
	"time"
)

// ErrClosed is returned by Write after a LineWriter has been closed.
var ErrClosed = errors.New("output: line writer is closed")

// LineWriter converts arbitrary byte writes into LF-terminated or bounded
// entries for one output stream. Pending data is kept as an immutable string
// so invalid UTF-8 and NUL bytes are preserved exactly.
type LineWriter struct {
	mu sync.Mutex

	stream   Stream
	maxBytes int
	idle     time.Duration
	now      func() time.Time
	append   func(Stream, time.Time, string) (Cursor, error)
	pending  string
	// failedBoundary marks the exact pending prefix whose append callback failed.
	failedBoundary int
	closed         bool
	timer          *time.Timer
	timerGene      uint64
}

// NewLineWriter creates a line splitter. A non-positive idle duration disables
// automatic flushing; Flush and Close still flush a partial line.
func NewLineWriter(stream Stream, maxLineBytes int, idle time.Duration, now func() time.Time, appendFn func(Stream, time.Time, string) (Cursor, error)) (*LineWriter, error) {
	if maxLineBytes <= 0 {
		return nil, errors.New("output: max line bytes must be positive")
	}
	if appendFn == nil {
		return nil, errors.New("output: append callback is nil")
	}
	if now == nil {
		now = time.Now
	}
	return &LineWriter{
		stream:   stream,
		maxBytes: maxLineBytes,
		idle:     idle,
		now:      now,
		append:   appendFn,
	}, nil
}

// Write accepts all bytes into the splitter, emitting complete LF-inclusive
// lines and max-byte chunks as they become available.
func (w *LineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return 0, ErrClosed
	}
	if len(p) == 0 {
		return 0, nil
	}

	if w.failedBoundary > 0 {
		if err := w.flushChunkLocked(w.failedBoundary); err != nil {
			return 0, err
		}
	}

	consumed := 0
	for consumed < len(p) {
		// Drain complete pending chunks before accepting more input so an
		// earlier callback failure cannot merge them with this Write.
		if err := w.flushReadyLocked(); err != nil {
			return consumed, err
		}

		remaining := p[consumed:]
		space := w.maxBytes - len(w.pending)
		if len(remaining) > space {
			remaining = remaining[:space]
		}
		w.pending += string(remaining)
		consumed += len(remaining)

		if err := w.flushReadyLocked(); err != nil {
			return consumed, err
		}
	}

	w.scheduleIdleLocked()
	return len(p), nil
}

// Flush emits all pending bytes, including an unterminated partial line.
func (w *LineWriter) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return nil
	}
	return w.flushAllLocked()
}

// Close is idempotent and marks the writer closed before flushing pending
// bytes. A callback error is returned, but pending bytes remain for a later
// Close retry; subsequent Write calls return ErrClosed.
func (w *LineWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed && w.pending == "" {
		return nil
	}
	w.closed = true
	return w.flushAllLocked()
}

func (w *LineWriter) flushChunkLocked(size int) error {
	if w.failedBoundary > 0 {
		size = w.failedBoundary
	}
	if size <= 0 || size > len(w.pending) {
		return nil
	}
	text := w.pending[:size]
	if _, err := w.append(w.stream, w.now(), text); err != nil {
		w.failedBoundary = size
		return err
	}
	w.pending = w.pending[size:]
	w.failedBoundary = 0
	return nil
}

// flushReadyLocked emits every complete LF-terminated or max-byte chunk,
// leaving an unterminated partial line pending.
func (w *LineWriter) flushReadyLocked() error {
	for w.pending != "" {
		limit := len(w.pending)
		if limit > w.maxBytes {
			limit = w.maxBytes
		}
		size := 0
		if newline := indexByte(w.pending[:limit], '\n'); newline >= 0 {
			size = newline + 1
		} else if len(w.pending) >= w.maxBytes {
			size = w.maxBytes
		} else {
			return nil
		}
		if err := w.flushChunkLocked(size); err != nil {
			return err
		}
	}
	return nil
}

func (w *LineWriter) flushAllLocked() error {
	w.cancelTimerLocked()
	for w.pending != "" {
		size := w.failedBoundary
		if size == 0 {
			size = len(w.pending)
			if size > w.maxBytes {
				size = w.maxBytes
			}
			if newline := indexByte(w.pending[:size], '\n'); newline >= 0 {
				size = newline + 1
			}
		}
		if err := w.flushChunkLocked(size); err != nil {
			return err
		}
	}
	return nil
}

func (w *LineWriter) scheduleIdleLocked() {
	if w.idle <= 0 || w.pending == "" || w.closed {
		w.cancelTimerLocked()
		return
	}

	if w.timer != nil {
		w.timer.Stop()
	}
	w.timerGene++
	generation := w.timerGene
	w.timer = time.AfterFunc(w.idle, func() {
		w.idleFlush(generation)
	})
}

func (w *LineWriter) cancelTimerLocked() {
	w.timerGene++
	if w.timer != nil {
		w.timer.Stop()
		w.timer = nil
	}
}

func (w *LineWriter) idleFlush(generation uint64) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if generation != w.timerGene || w.closed || w.pending == "" {
		return
	}
	w.timer = nil
	w.timerGene++
	if err := w.flushAllLocked(); err != nil {
		// Keep the bytes for a later explicit Flush/Close or idle retry. There
		// is no error channel on a timer callback, so schedule the retry on the
		// normal idle interval rather than recursing in a busy loop.
		w.scheduleIdleLocked()
		return
	}
}

func indexByte(s string, needle byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == needle {
			return i
		}
	}
	return -1
}
