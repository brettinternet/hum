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
		Name:  "hum",
		Usage: "A local development process supervisor",
		Description: "The ordinary workflow is hum run NAME -- COMMAND [ARGS...]. run automatically starts a detached daemon when needed and stays attached by default; add --detach to return immediately. " +
			"hum serve runs the daemon in the foreground, while hum serve --daemon runs it detached. " +
			"Read/control commands (excluding explicit hum serve) do not start an empty daemon: list, status, logs, wait, restart, stop, and shutdown inspect or control existing work; " +
			"When nothing is running, status, logs, wait, and restart point to hum run <name> -- <command>, while stop and shutdown report that there is no work to do. " +
			"Stopping named processes and shutting down the daemon are separate operations.",
		Version:   version + " (built " + buildTime + ")",
		Writer:    writer,
		ErrWriter: errWriter,
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
