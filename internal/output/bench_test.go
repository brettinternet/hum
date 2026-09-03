package output

import (
	"context"
	"testing"
	"time"
)

func BenchmarkAppend(b *testing.B) {
	const retention = 128
	r, err := newRing(Limits{RetainedBytes: retention, DefaultReadEntries: 1, DefaultReadBytes: retention})
	if err != nil {
		b.Fatal(err)
	}
	text := "benchmark output line with a stable payload\n"
	at := time.Unix(0, 0)
	b.ReportAllocs()
	b.SetBytes(int64(len(text)))
	b.ResetTimer()
	for range b.N {
		if _, err := r.append(Stdout, at, text); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()

	entryBound := retention / len(text)
	if r.bytes > retention {
		b.Fatalf("benchmark retained %d bytes, want <= %d", r.bytes, retention)
	}
	if r.count > entryBound {
		b.Fatalf("benchmark retained %d entries, want <= %d", r.count, entryBound)
	}
	if len(r.entries) > entryBound {
		b.Fatalf("benchmark retained storage for %d entries, want <= %d", len(r.entries), entryBound)
	}
	if r.next != Cursor(b.N) {
		b.Fatalf("benchmark advanced cursor to %d, want %d", r.next, b.N)
	}
}

func BenchmarkRead(b *testing.B) {
	r, err := newRing(Limits{RetainedBytes: 1 << 20, DefaultReadEntries: 100, DefaultReadBytes: 16 << 10})
	if err != nil {
		b.Fatal(err)
	}
	for i := range 100 {
		if _, err := r.append(Stdout, time.Unix(int64(i), 0), "benchmark output line with a stable payload\n"); err != nil {
			b.Fatal(err)
		}
	}
	opts := ReadOptions{MaxEntries: 100, MaxBytes: 16 << 10}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		result, err := r.read(opts)
		if err != nil {
			b.Fatal(err)
		}
		if len(result.Entries) == 0 {
			b.Fatal("benchmark read returned no entries")
		}
	}
}

func BenchmarkReadTailLarge(b *testing.B) {
	const (
		source = 4096
		tail   = source / 2
	)
	r, err := newRing(Limits{RetainedBytes: source, DefaultReadEntries: 100, DefaultReadBytes: source})
	if err != nil {
		b.Fatal(err)
	}
	for range source {
		if _, err := r.append(Stdout, time.Time{}, "x"); err != nil {
			b.Fatal(err)
		}
	}
	opts := ReadOptions{Tail: tail, MaxEntries: source, MaxBytes: source}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		result, err := r.read(opts)
		if err != nil {
			b.Fatal(err)
		}
		if len(result.Entries) != tail {
			b.Fatalf("benchmark tail read returned %d entries, want %d", len(result.Entries), tail)
		}
	}
}

func BenchmarkFollowerNotification(b *testing.B) {
	const retention = 128
	store, err := NewStore(Limits{RetainedBytes: retention, DefaultReadEntries: 1, DefaultReadBytes: retention})
	if err != nil {
		b.Fatal(err)
	}
	text := "benchmark output line with a stable payload\n"
	sub := store.Subscribe(ReadOptions{MaxEntries: 1, MaxBytes: retention})
	ctx := context.Background()
	b.ReportAllocs()
	b.SetBytes(int64(len(text)))
	b.ResetTimer()
	for i := range b.N {
		if _, err := store.Append(Stdout, time.Unix(int64(i), 0), text); err != nil {
			b.Fatal(err)
		}
		event, err := sub.Next(ctx)
		if err != nil {
			b.Fatal(err)
		}
		if event.Read == nil || len(event.Read.Entries) != 1 {
			b.Fatal("follower notification did not deliver appended output")
		}
	}
	b.StopTimer()

	entryBound := retention / len(text)
	if store.ring.bytes > retention {
		b.Fatalf("benchmark retained %d bytes, want <= %d", store.ring.bytes, retention)
	}
	if store.ring.count > entryBound {
		b.Fatalf("benchmark retained %d entries, want <= %d", store.ring.count, entryBound)
	}
	if len(store.ring.entries) > entryBound {
		b.Fatalf("benchmark retained storage for %d entries, want <= %d", len(store.ring.entries), entryBound)
	}
	if store.ring.next != Cursor(b.N) {
		b.Fatalf("benchmark advanced cursor to %d, want %d", store.ring.next, b.N)
	}
}
