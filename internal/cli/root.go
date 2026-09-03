package cli

import (
	"context"
	"io"

	urfavecli "github.com/urfave/cli/v3"
)

// NewRootCommand builds the hum command with the supplied build metadata
// and output writers.
func NewRootCommand(version, buildTime string, writer, errWriter io.Writer) *urfavecli.Command {
	return &urfavecli.Command{
		Name:        "hum",
		Usage:       "A local development process supervisor",
		Description: "Supervise development processes locally from the command line.",
		Version:     version + " (built " + buildTime + ")",
		Writer:      writer,
		ErrWriter:   errWriter,
		Flags: []urfavecli.Flag{
			&urfavecli.StringFlag{Name: "runtime-dir", Usage: "runtime directory for the hum daemon"},
			&urfavecli.StringFlag{Name: "stop-grace", Usage: "grace period before killing a process"},
			&urfavecli.StringFlag{Name: "output-bytes", Usage: "retained output bytes per process"},
			&urfavecli.StringFlag{Name: "completed-records", Usage: "completed process records to retain"},
		},
		Commands: newCLICommands(version, buildTime, writer, errWriter),
		Action: func(ctx context.Context, cmd *urfavecli.Command) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			return urfavecli.ShowRootCommandHelp(cmd)
		},
	}
}
