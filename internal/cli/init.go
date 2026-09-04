package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"hum/internal/project"

	urfavecli "github.com/urfave/cli/v3"
)

const initNextCommand = "hum up"

type initCandidateJSON struct {
	Name   string   `json:"name"`
	Source string   `json:"source"`
	Argv   []string `json:"argv"`
}

type initJSON struct {
	Path        string              `json:"path"`
	Outcome     project.InitOutcome `json:"outcome"`
	NextCommand string              `json:"next_command"`
	Candidates  []initCandidateJSON `json:"candidates"`
}

func initCommand(ctx context.Context, cmd *urfavecli.Command, writer io.Writer) error {
	if err := requireNoArgs(cmd, "init"); err != nil {
		return err
	}
	if err := nonNilContext(ctx).Err(); err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("current directory: %w", err)
	}

	result, err := project.InitManifest(cwd)
	if err != nil {
		var existsErr *project.ManifestExistsError
		if !errors.As(err, &existsErr) {
			return err
		}
		if result.Path == "" {
			result.Path = existsErr.Path
		}
		result.Outcome = project.InitOutcomeExists
		if err := renderInitResult(writer, result, cmd.Bool("json")); err != nil {
			return err
		}
		return urfavecli.Exit("", 1)
	}
	if err := renderInitResult(writer, result, cmd.Bool("json")); err != nil {
		return err
	}
	return nil
}

func renderInitResult(writer io.Writer, result project.InitResult, jsonOutput bool) error {
	if jsonOutput {
		return encodeJSON(writer, initJSONFor(result))
	}
	return renderInitHuman(writer, result)
}

func initJSONFor(result project.InitResult) initJSON {
	candidates := make([]initCandidateJSON, len(result.Candidates))
	for index, definition := range result.Candidates {
		argv := append([]string(nil), definition.Argv...)
		if argv == nil {
			argv = []string{}
		}
		candidates[index] = initCandidateJSON{
			Name:   definition.Name,
			Source: definition.Source,
			Argv:   argv,
		}
	}
	if candidates == nil {
		candidates = []initCandidateJSON{}
	}
	return initJSON{
		Path:        result.Path,
		Outcome:     result.Outcome,
		NextCommand: initNextCommand,
		Candidates:  candidates,
	}
}

func renderInitHuman(writer io.Writer, result project.InitResult) error {
	if _, err := fmt.Fprintf(writer, "path: %s\noutcome: %s\nnext_command: %s\n", result.Path, result.Outcome, initNextCommand); err != nil {
		return err
	}
	for _, definition := range result.Candidates {
		if _, err := fmt.Fprintf(writer, "candidate: %s source=%s argv=%s\n", definition.Name, definition.Source, shellJoin(definition.Argv)); err != nil {
			return err
		}
	}
	return nil
}
