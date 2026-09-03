package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	urfavecli "github.com/urfave/cli/v3"
	appcli "hum/internal/cli"
)

var (
	buildVersion = "dev"
	buildTime    = "unknown"

	outputWriter io.Writer = os.Stdout
	errorWriter  io.Writer = os.Stderr
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGHUP)
	defer stop()

	if err := run(ctx, os.Args); err != nil {
		if err.Error() != "" {
			fmt.Fprintln(errorWriter, err)
		}
		os.Exit(exitCode(err))
	}
}

func exitCode(err error) int {
	var exitErr urfavecli.ExitCoder
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return 1
}

func run(ctx context.Context, args []string) error {
	return appcli.NewRootCommand(buildVersion, buildTime, outputWriter, errorWriter).Run(ctx, args)
}
