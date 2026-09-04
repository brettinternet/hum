package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	urfavecli "github.com/urfave/cli/v3"
)

// NewRootCommand builds the hum command with the supplied build metadata
// and output writers.
func NewRootCommand(version, buildTime string, writer, errWriter io.Writer) *urfavecli.Command {
	root := &urfavecli.Command{
		Name:  "hum",
		Usage: "A local development process supervisor",
		Description: "The ordinary workflow is hum run NAME -- COMMAND [ARGS...]. run automatically starts a detached daemon when needed and stays attached by default; add --detach to return immediately. " +
			"Manifest projects use hum start NAME and hum up to ensure declared processes are running, waiting for readiness unless --no-wait is set. " +
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
	if err := validateCLICommandTree(root); err != nil {
		panic(err)
	}
	return root
}

func validateCLICommandTree(root *urfavecli.Command) error {
	if root == nil {
		return nil
	}
	return validateCLICommand(root, nil, true, true, nil)
}

func validateCLICommand(cmd *urfavecli.Command, inherited []urfavecli.Flag, helpVisible, isRoot bool, path []string) error {
	path = append(path, cmd.Name)
	flags := append([]urfavecli.Flag(nil), inherited...)
	ownFlags := cliCommandFlags(cmd)
	if helpVisible && !cmd.HideHelp && urfavecli.HelpFlag != nil {
		ownFlags = append(ownFlags, urfavecli.HelpFlag)
	}
	if isRoot && !cmd.HideVersion && cmd.Version != "" && urfavecli.VersionFlag != nil {
		ownFlags = append(ownFlags, urfavecli.VersionFlag)
	}
	flags = append(flags, ownFlags...)
	if err := validateCLIFlags(strings.Join(path, " "), flags); err != nil {
		return err
	}

	nextInherited := append([]urfavecli.Flag(nil), inherited...)
	for _, flag := range ownFlags {
		if cliFlagIsPersistent(flag) {
			nextInherited = append(nextInherited, flag)
		}
	}
	nextHelpVisible := helpVisible && !cmd.HideHelp
	for _, child := range cmd.Commands {
		if err := validateCLICommand(child, nextInherited, nextHelpVisible, false, path); err != nil {
			return err
		}
	}
	return nil
}

func validateCLIFlags(commandName string, flags []urfavecli.Flag) error {
	seen := make(map[string]urfavecli.Flag)
	for _, flag := range flags {
		if flag == nil {
			return fmt.Errorf("nil flag in command %q", commandName)
		}
		for _, name := range flag.Names() {
			if previous, ok := seen[name]; ok {
				return fmt.Errorf("flag name %q is used by both %q and %q in command %q", name, previous.Names()[0], flag.Names()[0], commandName)
			}
			seen[name] = flag
		}
	}
	return nil
}

func cliFlagIsPersistent(flag urfavecli.Flag) bool {
	localFlag, ok := flag.(urfavecli.LocalFlag)
	return !ok || !localFlag.IsLocal()
}

func cliCommandFlags(cmd *urfavecli.Command) []urfavecli.Flag {
	flags := append([]urfavecli.Flag(nil), cmd.Flags...)
	for _, group := range cmd.MutuallyExclusiveFlags {
		for _, option := range group.Flags {
			flags = append(flags, option...)
		}
	}
	return flags
}
