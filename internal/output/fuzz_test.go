package output

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func FuzzStripTerminalControl(f *testing.F) {
	for _, text := range []string{
		"plain text\n", "\x1b[31mred\x1b[0m\n", "before\x1b]0;title\aafter",
		"before\x1bPpayload\x1b\\after", "a\r\nb\rc", "prefix\x1b[31",
		"before\u009dtitle\u009cafter", "a\u0085b", string([]byte{'a', 0xff, 0, 0x9b, 'b'}),
	} {
		f.Add(text)
	}
	f.Fuzz(func(t *testing.T, text string) {
		got := StripTerminalControl(text)
		if strings.IndexByte(got, '\x1b') >= 0 || len(got) > len(text) {
			t.Fatalf("stripped %q to %q: ESC remains or bytes grew", text, got)
		}
		plain := strings.IndexByte(text, '\x1b') < 0 && !strings.Contains(text, "\r\n")
		for i := 0; plain && i+1 < len(text); i++ {
			if text[i] == 0xc2 && text[i+1] >= 0x80 && text[i+1] <= 0x9f {
				plain = false
			}
		}
		if plain && got != text {
			t.Fatalf("plain input %q changed to %q", text, got)
		}
	})
}

func FuzzLineWriter(f *testing.F) {
	for _, seed := range [][]byte{
		{}, []byte("one\r\ntwo\n"), []byte("123456789"), {'x', 0xff, 0, '\n'}, []byte("partial"),
	} {
		f.Add(seed, uint8(3), uint8(4))
	}
	f.Fuzz(func(t *testing.T, input []byte, chunkByte, limitByte uint8) {
		chunkSize := int(chunkByte%64) + 1
		maxLineBytes := int(limitByte%64) + 1
		var rebuilt bytes.Buffer
		partial := false
		writer, err := NewLineWriter(Stdout, maxLineBytes, 0, nil, func(_ Stream, _ time.Time, text string) (Cursor, error) {
			if len(text) == 0 || len(text) > maxLineBytes {
				t.Fatalf("entry length %d, bound %d", len(text), maxLineBytes)
			}
			// An entry ends at its only LF or at the byte bound; with idle
			// disabled, only Close emits a shorter unterminated partial line.
			if newline := strings.IndexByte(text, '\n'); newline >= 0 && newline != len(text)-1 {
				t.Fatalf("entry %q has LF before its end", text)
			}
			if partial {
				t.Fatalf("entry %q follows a partial-line entry", text)
			}
			partial = text[len(text)-1] != '\n' && len(text) < maxLineBytes
			rebuilt.WriteString(text)
			return 0, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		for start := 0; start < len(input); start += chunkSize {
			end := start + chunkSize
			if end > len(input) {
				end = len(input)
			}
			if n, err := writer.Write(input[start:end]); err != nil || n != end-start {
				t.Fatalf("Write accepted %d/%d bytes: %v", n, end-start, err)
			}
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(rebuilt.Bytes(), input) {
			t.Fatalf("entries rebuilt %q, want %q", rebuilt.Bytes(), input)
		}
	})
}
