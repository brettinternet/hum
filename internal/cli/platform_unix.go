//go:build !windows

package cli

import (
	"os"
	"os/signal"
	"syscall"
)

func validateTTYRequest(bool) error { return nil }

func ttySupported() bool { return true }

func registerFollowSignals(signals chan<- os.Signal) {
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
}

func registerRunBridgeSignals(signals chan<- os.Signal) {
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGHUP)
}

func registerRunSignalsEarly() bool { return false }

func interruptStopsAttachedRun() bool { return false }

func registerTTYResizeSignal(signals chan<- os.Signal) {
	signal.Notify(signals, syscall.SIGWINCH)
}
