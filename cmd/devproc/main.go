package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	appcli "devproc/internal/cli"
)

var (
	buildVersion = "dev"
	buildTime    = "unknown"

	outputWriter io.Writer = os.Stdout
	errorWriter  io.Writer = os.Stderr
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()

	if err := run(ctx, os.Args); err != nil {
		fmt.Fprintln(errorWriter, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	return appcli.NewRootCommand(buildVersion, buildTime, outputWriter, errorWriter).Run(ctx, args)
}
