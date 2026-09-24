//go:build windows

package main

import (
	"context"
	"os"
	"os/signal"

	appcli "hum/internal/cli"
)

// signalContext cancels on Ctrl+C with appcli.ErrInterrupted as the cause, so
// commands that race the context against their own interrupt handler can still
// classify the cancellation as an interrupt.
func signalContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancelCause(context.Background())
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	go func() {
		select {
		case <-signals:
			cancel(appcli.ErrInterrupted)
		case <-ctx.Done():
		}
	}()
	return ctx, func() {
		signal.Stop(signals)
		cancel(nil)
	}
}
