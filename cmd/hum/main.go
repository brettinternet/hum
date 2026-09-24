package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	urfavecli "github.com/urfave/cli/v3"
	appcli "hum/internal/cli"
)

var (
	buildVersion = "dev"
	buildCommit  = "unknown"
	buildTime    = "unknown"

	outputWriter io.Writer = os.Stdout
	errorWriter  io.Writer = os.Stderr
)

func main() {
	ctx, stop := signalContext()
	defer stop()

	if err := run(ctx, os.Args); err != nil {
		if err.Error() != "" && !appcli.JSONErrorHandled(err) {
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
	root := appcli.NewRootCommandWithCommit(buildVersion, buildCommit, buildTime, outputWriter, errorWriter)
	appcli.SetInvocationArgs(root, args)
	return root.Run(ctx, args)
}
