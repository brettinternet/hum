package signals

import (
	"errors"
	"testing"
)

func TestWindowsRejectsUnixSignals(t *testing.T) {
	for _, name := range []string{"SIGTERM", "KILL", "9", ""} {
		if _, err := Parse(name); !errors.Is(err, ErrUnsupportedSignal) {
			t.Errorf("Parse(%q) = %v, want ErrUnsupportedSignal", name, err)
		}
	}
}
