//go:build windows

package cli

import (
	"os"
	"os/signal"
)

func validateTTYRequest(bool) error { return nil }

func ttySupported() bool { return true }

func registerFollowSignals(signals chan<- os.Signal) {
	signal.Notify(signals, os.Interrupt)
}

func registerRunBridgeSignals(signals chan<- os.Signal) {
	signal.Notify(signals, os.Interrupt)
}

func registerRunSignalsEarly() bool { return true }

func interruptStopsAttachedRun() bool { return true }

func registerTTYResizeSignal(chan<- os.Signal) {}
