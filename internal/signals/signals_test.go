package signals

import (
	"errors"
	"strconv"
	"syscall"
	"testing"
)

func TestParse(t *testing.T) {
	cases := []struct {
		input  string
		name   string
		number int
		signal syscall.Signal
	}{
		{input: "hup", name: "SIGHUP", number: int(syscall.SIGHUP), signal: syscall.SIGHUP},
		{input: "SIGHUP", name: "SIGHUP", number: int(syscall.SIGHUP), signal: syscall.SIGHUP},
		{input: "sigTerm", name: "SIGTERM", number: int(syscall.SIGTERM), signal: syscall.SIGTERM},
		{input: "9", name: "SIGKILL", number: int(syscall.SIGKILL), signal: syscall.SIGKILL},
		{input: strconv.Itoa(int(syscall.SIGUSR1)), name: "SIGUSR1", number: int(syscall.SIGUSR1), signal: syscall.SIGUSR1},
		{input: strconv.Itoa(int(syscall.SIGUSR2)), name: "SIGUSR2", number: int(syscall.SIGUSR2), signal: syscall.SIGUSR2},
	}
	for _, test := range cases {
		t.Run(test.input, func(t *testing.T) {
			got, err := Parse(test.input)
			if err != nil {
				t.Fatal(err)
			}
			if got.Name != test.name || got.Number != test.number || got.Signal != test.signal {
				t.Fatalf("Parse(%q) = %#v, want name=%q number=%d signal=%v", test.input, got, test.name, test.number, test.signal)
			}
		})
	}

	for _, input := range []string{"", "0", "-1", "+1", "01x", "SIG", "SIGALRM", "13", "999999999999999999999999"} {
		t.Run("reject "+input, func(t *testing.T) {
			if _, err := Parse(input); !errors.Is(err, ErrInvalidSignal) {
				t.Fatalf("Parse(%q) error = %v, want ErrInvalidSignal", input, err)
			}
		})
	}
}
