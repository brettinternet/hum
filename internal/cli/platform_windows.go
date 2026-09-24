//go:build windows

package cli

import (
	"errors"
	"os"
	"os/signal"
)

func validateTTYRequest(tty bool) error {
	if tty {
		return errors.New("TTY mode is unsupported on Windows")
	}
	return nil
}

func ttySupported() bool { return false }

func registerFollowSignals(signals chan<- os.Signal) {
	signal.Notify(signals, os.Interrupt)
}

func registerRunBridgeSignals(signals chan<- os.Signal) {
	signal.Notify(signals, os.Interrupt)
}

func registerRunSignalsEarly() bool { return true }

func interruptStopsAttachedRun() bool { return true }

func registerTTYResizeSignal(chan<- os.Signal) {}
