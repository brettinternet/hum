package output

import (
	"bytes"
	"errors"
	"sync"
	"testing"
	"time"
)

type capturedEntry struct {
	stream Stream
	at     time.Time
	text   string
}

func TestLineSplitting(t *testing.T) {
	fixedTime := time.Unix(42, 17)
	var mu sync.Mutex
	var got []capturedEntry
	appendFn := func(stream Stream, at time.Time, text string) (Cursor, error) {
		mu.Lock()
		got = append(got, capturedEntry{stream: stream, at: at, text: text})
		mu.Unlock()
		return Cursor(len(got) - 1), nil
	}

	writer, err := NewLineWriter(Stderr, 8, 0, func() time.Time { return fixedTime }, appendFn)
	if err != nil {
		t.Fatal(err)
	}
	input := []byte("one\r\ntwo\n")
	if n, err := writer.Write(input); err != nil || n != len(input) {
		t.Fatalf("CRLF Write = (%d, %v)", n, err)
	}
	invalid := []byte{'x', 0xff, 0x00, '\n'}
	if n, err := writer.Write(invalid); err != nil || n != len(invalid) {
		t.Fatalf("invalid UTF-8 Write = (%d, %v)", n, err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	entries := append([]capturedEntry(nil), got...)
	mu.Unlock()
	want := []string{"one\r\n", "two\n", string(invalid)}
	if len(entries) != len(want) {
		t.Fatalf("split entries = %#v, want %q", entries, want)
	}
	for i, entry := range entries {
		if entry.stream != Stderr || entry.at != fixedTime || entry.text != want[i] {
			t.Fatalf("entry %d = %#v, want stream/time/text %d/%v/%q", i, entry, Stderr, fixedTime, want[i])
		}
	}

	var chunks []string
	chunkWriter, err := NewLineWriter(Stdout, 4, 0, time.Now, func(_ Stream, _ time.Time, text string) (Cursor, error) {
		chunks = append(chunks, text)
		return Cursor(len(chunks) - 1), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := chunkWriter.Write([]byte("123456789")); err != nil {
		t.Fatal(err)
	}
	if err := chunkWriter.Flush(); err != nil {
		t.Fatal(err)
	}
	if want := []string{"1234", "5678", "9"}; !equalStrings(chunks, want) {
		t.Fatalf("max-byte chunks = %q, want %q", chunks, want)
	}

	idleReady := make(chan string, 1)
	idleWriter, err := NewLineWriter(System, 64, 10*time.Millisecond, time.Now, func(_ Stream, _ time.Time, text string) (Cursor, error) {
		idleReady <- text
		return 0, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := idleWriter.Write([]byte("partial")); err != nil {
		t.Fatal(err)
	}
	select {
	case text := <-idleReady:
		if text != "partial" {
			t.Fatalf("idle flush text = %q, want partial", text)
		}
	case <-time.After(time.Second):
		t.Fatal("partial line was not idle-flushed")
	}

	if err := idleWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := idleWriter.Close(); err != nil {
		t.Fatal("second Close: ", err)
	}
	if _, err := idleWriter.Write([]byte("later")); !errors.Is(err, ErrClosed) {
		t.Fatalf("Write after Close = %v, want ErrClosed", err)
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal("second writer Close: ", err)
	}
	if _, err := writer.Write([]byte("later")); !errors.Is(err, ErrClosed) {
		t.Fatalf("writer Write after Close = %v, want ErrClosed", err)
	}
}

func TestLineSplittingFlushRetryAfterAppendFailure(t *testing.T) {
	appendErr := errors.New("append failed")
	var got []string
	attempts := 0
	writer, err := NewLineWriter(Stdout, 64, 0, time.Now, func(_ Stream, _ time.Time, text string) (Cursor, error) {
		attempts++
		if attempts == 1 {
			return 0, appendErr
		}
		got = append(got, text)
		return Cursor(len(got) - 1), nil
	})
	if err != nil {
		t.Fatal(err)
	}

	input := []byte("a\nb\n")
	if n, err := writer.Write(input); n != len(input) || !errors.Is(err, appendErr) {
		t.Fatalf("failed Write = (%d, %v), want (%d, %v)", n, err, len(input), appendErr)
	}
	if writer.pending != string(input) {
		t.Fatalf("pending after failed Write = %q, want %q", writer.pending, input)
	}
	if err := writer.Flush(); err != nil {
		t.Fatal("retry Flush: ", err)
	}
	if want := []string{"a\n", "b\n"}; !equalStrings(got, want) {
		t.Fatalf("retry Flush entries = %q, want %q", got, want)
	}
}

func TestLineSplittingWriteRetryDrainsPendingChunks(t *testing.T) {
	appendErr := errors.New("append failed")
	var got []string
	attempts := 0
	writer, err := NewLineWriter(Stdout, 64, 0, time.Now, func(_ Stream, _ time.Time, text string) (Cursor, error) {
		attempts++
		if attempts == 1 {
			return 0, appendErr
		}
		got = append(got, text)
		return Cursor(len(got) - 1), nil
	})
	if err != nil {
		t.Fatal(err)
	}

	input := []byte("a\nb\n")
	if n, err := writer.Write(input); n != len(input) || !errors.Is(err, appendErr) {
		t.Fatalf("failed Write = (%d, %v), want (%d, %v)", n, err, len(input), appendErr)
	}

	later := []byte("later\n")
	if n, err := writer.Write(later); err != nil || n != len(later) {
		t.Fatalf("later Write = (%d, %v), want (%d, nil)", n, err, len(later))
	}
	if want := []string{"a\n", "b\n", "later\n"}; !equalStrings(got, want) {
		t.Fatalf("entries after Write retry = %q, want %q", got, want)
	}
}

func TestLineSplittingFlushPartialFailureBeforeLaterWrite(t *testing.T) {
	appendErr := errors.New("append failed")
	var got []string
	attempts := 0
	writer, err := NewLineWriter(Stdout, 64, 0, time.Now, func(_ Stream, _ time.Time, text string) (Cursor, error) {
		attempts++
		if attempts == 1 {
			return 0, appendErr
		}
		got = append(got, text)
		return Cursor(len(got) - 1), nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if n, err := writer.Write([]byte("partial")); err != nil || n != len("partial") {
		t.Fatalf("partial Write = (%d, %v), want (%d, nil)", n, err, len("partial"))
	}
	if err := writer.Flush(); !errors.Is(err, appendErr) {
		t.Fatalf("failed Flush = %v, want %v", err, appendErr)
	}

	later := []byte("later\n")
	if n, err := writer.Write(later); err != nil || n != len(later) {
		t.Fatalf("later Write = (%d, %v), want (%d, nil)", n, err, len(later))
	}
	if want := []string{"partial", "later\n"}; !equalStrings(got, want) {
		t.Fatalf("entries after partial Flush failure = %q, want %q", got, want)
	}
}

func TestLineSplittingIdleRetryAfterAppendFailure(t *testing.T) {
	appendErr := errors.New("append failed")
	type idleAttempt struct {
		at      time.Time
		attempt int
		text    string
		err     error
	}
	var got []string
	attempts := 0
	idle := 20 * time.Millisecond
	events := make(chan idleAttempt, 2)
	writer, err := NewLineWriter(Stdout, 64, idle, time.Now, func(_ Stream, at time.Time, text string) (Cursor, error) {
		attempts++
		if attempts == 1 {
			events <- idleAttempt{at: at, attempt: attempts, text: text, err: appendErr}
			return 0, appendErr
		}
		got = append(got, text)
		events <- idleAttempt{at: at, attempt: attempts, text: text}
		return Cursor(len(got) - 1), nil
	})
	if err != nil {
		t.Fatal(err)
	}

	input := []byte("partial")
	if n, err := writer.Write(input); err != nil || n != len(input) {
		t.Fatalf("partial Write = (%d, %v), want (%d, nil)", n, err, len(input))
	}

	var first idleAttempt
	select {
	case first = <-events:
	case <-time.After(time.Second):
		t.Fatal("first idle callback did not run")
	}
	if first.attempt != 1 || !errors.Is(first.err, appendErr) || first.text != string(input) {
		t.Fatalf("first idle callback = %#v, want failed attempt with %q", first, input)
	}

	var second idleAttempt
	select {
	case second = <-events:
	case <-time.After(time.Second):
		t.Fatal("idle retry callback did not run")
	}
	if second.attempt != 2 || second.err != nil || second.text != string(input) {
		t.Fatalf("second idle callback = %#v, want successful attempt with %q", second, input)
	}
	if elapsed := second.at.Sub(first.at); elapsed < idle {
		t.Fatalf("idle retry elapsed %s, want at least %s", elapsed, idle)
	}

	writer.mu.Lock()
	pending := writer.pending
	timer := writer.timer
	writer.mu.Unlock()
	if !equalStrings(got, []string{string(input)}) {
		t.Fatalf("idle retry entries = %q, want %q", got, []string{string(input)})
	}
	if pending != "" {
		t.Fatalf("pending after idle retry = %q, want empty", pending)
	}
	if timer != nil {
		t.Fatal("timer after successful idle retry is still scheduled")
	}
}

func TestLineSplittingIdlePartialFailureBeforeLaterWrite(t *testing.T) {
	appendErr := errors.New("append failed")
	var got []string
	attempts := 0
	writer, err := NewLineWriter(Stdout, 64, time.Hour, time.Now, func(_ Stream, _ time.Time, text string) (Cursor, error) {
		attempts++
		if attempts == 1 {
			return 0, appendErr
		}
		got = append(got, text)
		return Cursor(len(got) - 1), nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if n, err := writer.Write([]byte("partial")); err != nil || n != len("partial") {
		t.Fatalf("partial Write = (%d, %v), want (%d, nil)", n, err, len("partial"))
	}
	writer.mu.Lock()
	timer := writer.timer
	if timer == nil {
		writer.mu.Unlock()
		t.Fatal("timer after partial Write is nil")
	}
	timer.Stop()
	writer.timer = nil
	generation := writer.timerGene
	writer.mu.Unlock()
	writer.idleFlush(generation)
	writer.mu.Lock()
	timer = writer.timer
	writer.mu.Unlock()
	if timer == nil {
		t.Fatal("timer after failed idle callback is nil")
	}
	if attempts != 1 {
		t.Fatalf("idle callback attempts = %d, want 1", attempts)
	}

	later := []byte("later\n")
	if n, err := writer.Write(later); err != nil || n != len(later) {
		t.Fatalf("later Write = (%d, %v), want (%d, nil)", n, err, len(later))
	}
	writer.mu.Lock()
	timer = writer.timer
	writer.mu.Unlock()
	if timer != nil {
		t.Fatal("timer after later successful Write is still scheduled")
	}
	if want := []string{"partial", "later\n"}; !equalStrings(got, want) {
		t.Fatalf("entries after idle partial failure = %q, want %q", got, want)
	}
	if err := writer.Close(); err != nil {
		t.Fatal("Close: ", err)
	}
}

func TestLineSplittingCloseRetriesFailedFlush(t *testing.T) {
	appendErr := errors.New("append failed")
	var got []string
	attempts := 0
	writer, err := NewLineWriter(Stdout, 64, 0, time.Now, func(_ Stream, _ time.Time, text string) (Cursor, error) {
		attempts++
		if attempts <= 2 {
			return 0, appendErr
		}
		got = append(got, text)
		return Cursor(len(got) - 1), nil
	})
	if err != nil {
		t.Fatal(err)
	}

	input := []byte("a\nb\n")
	if n, err := writer.Write(input); n != len(input) || !errors.Is(err, appendErr) {
		t.Fatalf("failed Write = (%d, %v), want (%d, %v)", n, err, len(input), appendErr)
	}
	if err := writer.Close(); !errors.Is(err, appendErr) {
		t.Fatalf("failed Close = %v, want %v", err, appendErr)
	}
	if writer.pending != string(input) {
		t.Fatalf("pending after failed Close = %q, want %q", writer.pending, input)
	}
	if _, err := writer.Write([]byte("later")); !errors.Is(err, ErrClosed) {
		t.Fatalf("Write after failed Close = %v, want ErrClosed", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal("retry Close: ", err)
	}
	if want := []string{"a\n", "b\n"}; !equalStrings(got, want) {
		t.Fatalf("retry Close entries = %q, want %q", got, want)
	}
	if err := writer.Close(); err != nil {
		t.Fatal("second successful Close: ", err)
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if !bytes.Equal([]byte(got[i]), []byte(want[i])) {
			return false
		}
	}
	return true
}
