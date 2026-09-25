package protocol

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func FuzzDecoder(f *testing.F) {
	for _, input := range []string{
		"", `{"op":"hello","version":1}` + "\n", `{"op":"get"` + "\n",
		`{"op":"wat"}` + "\n", `{"name":"api"}` + "\n",
		"\n" + `{"op":"hello","version":1}` + "\n",
		strings.Repeat("x", 257) + "\n" + `{"op":"hello","version":1}` + "\n",
	} {
		f.Add([]byte(input))
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		// decodeRaw consumes the entire physical line on malformed and oversized
		// input, including a final line without LF (codec.go). The oversized-line
		// recovery is also exercised by TestTypedErrorsAndBoundedNDJSON.
		decoder := NewDecoder(bytes.NewReader(input), 256)
		for calls := 0; calls <= len(input); calls++ {
			_, err := decoder.DecodeRequest()
			if errors.Is(err, io.EOF) {
				return
			}
			if err != nil {
				var decodeErr *DecodeError
				if !errors.As(err, &decodeErr) {
					t.Fatalf("unexpected decode error after %d calls: %v", calls+1, err)
				}
			}
		}
		t.Fatalf("decoder did not reach EOF within %d calls", len(input)+1)
	})
}
