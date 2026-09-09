package cli

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"

	urfavecli "github.com/urfave/cli/v3"

	"hum/internal/daemon"
	"hum/internal/project"
	"hum/internal/protocol"
)

// inputResult is the stable one-shot success shape. It intentionally contains
// only the target name, acknowledged byte count, and selected launch cursor.
type inputResult struct {
	Name         string          `json:"name"`
	Bytes        int             `json:"bytes"`
	LaunchCursor protocol.Cursor `json:"launch_cursor"`
}

func inputCommand(ctx context.Context, cmd *urfavecli.Command, version, buildTime string, writer io.Writer) error {
	args := cmd.Args().Slice()
	name := ""
	if len(args) == 1 {
		name = args[0]
	}
	if len(args) == 0 {
		return inputCommandError(cmd, writer, name, inputInvalidRequestError("input requires exactly one process name"))
	}
	if len(args) != 1 {
		return inputCommandError(cmd, writer, name, inputInvalidRequestError("input accepts exactly one process name"))
	}
	if strings.TrimSpace(name) == "" {
		return inputCommandError(cmd, writer, name, inputInvalidRequestError("input process name must not be empty"))
	}

	data, err := inputPayload(cmd)
	if err != nil {
		return inputCommandError(cmd, writer, name, err)
	}
	ctx = nonNilContext(ctx)
	if err := ctx.Err(); err != nil {
		return inputCommandError(cmd, writer, name, err)
	}
	selection, err := selectedProjectDirectory(cmd)
	if err != nil {
		return inputCommandError(cmd, writer, name, err)
	}
	cwd := selection.cwd
	selector := selection.selector
	manifest := manifestState{byName: make(map[string]project.Definition)}
	if selection.scope != "global" {
		manifest, err = loadManifestOrEmpty(cwd)
		if err != nil {
			return inputCommandError(cmd, writer, name, err)
		}
	}
	cfg, err := cliConfig(cmd, version, buildTime)
	if err != nil {
		return inputCommandError(cmd, writer, name, err)
	}
	client, err := daemonClient(ctx, cfg)
	if err != nil {
		if client != nil {
			_ = client.Close()
		}
		if daemonUnavailable(err) && !cmd.Bool("json") {
			err = protocol.NewWireError(protocol.ErrorCode("unavailable"), runUnavailableMessage, nil)
		}
		return inputCommandError(cmd, writer, name, err)
	}
	defer client.Close()

	definition, declared := manifest.byName[name]
	process, err := client.Get(ctx, daemon.GetRequest{Name: name, Scope: selection.scope, Cwd: manifest.root})
	if err != nil {
		if !isNotFound(err) {
			return inputCommandError(cmd, writer, name, err)
		}
		if declared && !definition.TTY {
			return inputCommandError(cmd, writer, name, inputNotTTYError(name, definition.TTY, selector))
		}
		if declared {
			return inputCommandError(cmd, writer, name, inputSessionNotRunningError(name, selector))
		}
		return inputCommandError(cmd, writer, name, inputNotFoundError(name, selector))
	}
	if declared && !definition.TTY {
		return inputCommandError(cmd, writer, name, inputNotTTYError(name, false, selector))
	}
	if !process.TTY {
		return inputCommandError(cmd, writer, name, inputNotTTYError(name, declared && definition.TTY, selector))
	}

	root := process.Root
	if root == "" {
		root = manifest.root
	}
	inputCwd := process.Cwd
	if inputCwd == "" {
		inputCwd = manifest.root
	}
	result, err := client.Input(ctx, daemon.InputRequest{Name: name, Scope: selection.scope, Cwd: inputCwd, Root: root, Data: data})
	if err != nil {
		var notRunning *daemon.SessionNotRunningError
		if errors.As(err, &notRunning) {
			err = inputSessionNotRunningError(name, selector)
		}
		return inputCommandError(cmd, writer, name, err)
	}
	if cmd.Bool("json") {
		return encodeJSON(writer, inputResult{Name: name, Bytes: result.Bytes, LaunchCursor: result.LaunchCursor})
	}
	_, err = fmt.Fprintf(writer, "wrote %d bytes to %s at launch cursor %d\n", result.Bytes, name, result.LaunchCursor)
	return err
}

func inputPayload(cmd *urfavecli.Command) ([]byte, error) {
	textSet := cmd.IsSet("text")
	base64Set := cmd.IsSet("base64")
	if textSet == base64Set {
		return nil, inputInvalidRequestError("input requires exactly one of --text or --base64")
	}
	if textSet {
		value := cmd.String("text")
		if value == "" {
			return nil, inputInvalidRequestError("--text must not be empty")
		}
		data := []byte(value)
		if len(data) > protocol.MaxInputBytes {
			return nil, protocol.NewWireError(protocol.ErrorInputTooLarge, fmt.Sprintf("input payload exceeds %d bytes", protocol.MaxInputBytes), nil)
		}
		return data, nil
	}

	value := cmd.String("base64")
	data, err := decodeStrictInputBase64(value)
	if err != nil {
		return nil, err
	}
	if len(data) > protocol.MaxInputBytes {
		return nil, protocol.NewWireError(protocol.ErrorInputTooLarge, fmt.Sprintf("input payload exceeds %d bytes", protocol.MaxInputBytes), nil)
	}
	return data, nil
}

func decodeStrictInputBase64(value string) ([]byte, error) {
	if value == "" {
		return nil, inputInvalidRequestError("--base64 must not be empty")
	}
	for _, runeValue := range value {
		if unicode.IsSpace(runeValue) {
			return nil, inputInvalidRequestError("--base64 must not contain whitespace")
		}
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(value)
	if err != nil {
		return nil, inputInvalidRequestError("--base64 must be standard padded base64")
	}
	if len(decoded) == 0 {
		return nil, inputInvalidRequestError("--base64 must decode to at least one byte")
	}
	return decoded, nil
}

func inputInvalidRequestError(message string) error {
	return protocol.NewWireError(protocol.ErrorInvalidRequest, message, nil)
}

func inputNotFoundError(name string, selectors ...string) error {
	selector := ""
	if len(selectors) != 0 {
		selector = selectors[0]
	}
	return protocol.NewWireError(protocol.ErrorNotFound,
		fmt.Sprintf("process %q was not found; use %s for a resolved name or %s", name, projectCommand(selector, "start "+name), projectCommand(selector, "run "+name+" -- COMMAND")), nil)
}

func inputSessionNotRunningError(name string, selectors ...string) error {
	selector := ""
	if len(selectors) != 0 {
		selector = selectors[0]
	}
	return protocol.NewWireError(protocol.ErrorCode("session_not_running"),
		fmt.Sprintf("session %q is not running; start it with %s", name, projectCommand(selector, "start "+name)), nil)
}

func inputNotTTYError(name string, declaredTTY bool, selectors ...string) error {
	selector := ""
	if len(selectors) != 0 {
		selector = selectors[0]
	}
	message := fmt.Sprintf("process %q is not a tty; set tty: true in hum.yaml or launch it with %s", name, projectCommand(selector, "run "+name+" --tty -- COMMAND"))
	if declaredTTY {
		if selector == "" {
			message = fmt.Sprintf("process %q is running without a tty; stop it and rerun with tty: true or --tty", name)
		} else {
			message = fmt.Sprintf("process %q is running without a tty; stop it with %s and rerun with tty: true or --tty", name, projectCommand(selector, "stop "+name))
		}
	}
	return protocol.NewWireError(protocol.ErrorInputNotTTY, message, nil)
}

func inputCommandError(_ *urfavecli.Command, _ io.Writer, _ string, err error) error {
	return err
}
