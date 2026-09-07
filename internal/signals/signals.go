// Package signals parses the portable signal names accepted by hum.
package signals

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"syscall"
)

// ErrInvalidSignal identifies an empty, malformed, unknown, or unsupported
// signal specification.
var ErrInvalidSignal = errors.New("invalid signal")

// Info is the canonical representation of one supported process signal.
type Info struct {
	Name   string
	Number int
	Signal syscall.Signal
}

// SignalInfo and Signal are descriptive aliases for Info.
type SignalInfo = Info
type Signal = Info

type signalDefinition struct {
	name   string
	number syscall.Signal
}

// The table deliberately contains only the signals that hum promises to
// expose. In particular, numeric real-time or otherwise unnamed signals are
// not accepted even when the host kernel supports them.
var definitions = []signalDefinition{
	{name: "SIGHUP", number: syscall.SIGHUP},
	{name: "SIGINT", number: syscall.SIGINT},
	{name: "SIGQUIT", number: syscall.SIGQUIT},
	{name: "SIGKILL", number: syscall.SIGKILL},
	{name: "SIGTERM", number: syscall.SIGTERM},
	{name: "SIGUSR1", number: syscall.SIGUSR1},
	{name: "SIGUSR2", number: syscall.SIGUSR2},
}

var (
	byName   = make(map[string]signalDefinition, len(definitions))
	byNumber = make(map[syscall.Signal]signalDefinition, len(definitions))
)

func init() {
	for _, definition := range definitions {
		byName[strings.TrimPrefix(definition.name, "SIG")] = definition
		byNumber[definition.number] = definition
	}
}

// Parse accepts a case-insensitive supported name with an optional SIG prefix,
// or a positive decimal number present in the supported named table. The
// returned name is always canonical and SIG-prefixed.
func Parse(value string) (Info, error) {
	if value == "" {
		return Info{}, invalid(value)
	}

	if isDecimal(value) {
		number, err := strconv.ParseUint(value, 10, 31)
		if err != nil || number == 0 {
			return Info{}, invalid(value)
		}
		definition, ok := byNumber[syscall.Signal(number)]
		if !ok {
			return Info{}, invalid(value)
		}
		return info(definition), nil
	}

	name := strings.ToUpper(value)
	name = strings.TrimPrefix(name, "SIG")
	if name == "" {
		return Info{}, invalid(value)
	}
	definition, ok := byName[name]
	if !ok {
		return Info{}, invalid(value)
	}
	return info(definition), nil
}

// ParseSignal is an explicit alias for Parse at call sites that prefer the
// operation's full name.
func ParseSignal(value string) (Info, error) { return Parse(value) }

func info(definition signalDefinition) Info {
	return Info{Name: definition.name, Number: int(definition.number), Signal: definition.number}
}

func invalid(value string) error {
	return fmt.Errorf("%w: %q", ErrInvalidSignal, value)
}

func isDecimal(value string) bool {
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return len(value) != 0
}
