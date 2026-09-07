package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"hum/internal/project"

	urfavecli "github.com/urfave/cli/v3"
)

const initNextCommand = "hum up"

func initNextCommandFor(selector string) string {
	return projectCommand(selector, "up")
}

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
	selection, err := selectedProjectDirectory(cmd)
	if err != nil {
		return err
	}

	result, err := project.InitManifest(selection.cwd, cmd.Bool("force"))
	if err != nil {
		var existsErr *project.ManifestExistsError
		if !errors.As(err, &existsErr) {
			return err
		}
		if result.Path == "" {
			result.Path = existsErr.Path
		}
		result.Outcome = project.InitOutcomeExists
		if !cmd.Bool("json") {
			return newUserFacingError(fmt.Sprintf("hum.yaml already exists at %s; edit it, or remove it before running %s again", result.Path, projectCommand(selection.selector, "init")))
		}
		if err := renderInitResult(writer, result, cmd.Bool("json"), selection.selector); err != nil {
			return err
		}
		return urfavecli.Exit("", 1)
	}
	if err := renderInitResult(writer, result, cmd.Bool("json"), selection.selector); err != nil {
		return err
	}
	return nil
}

func renderInitResult(writer io.Writer, result project.InitResult, jsonOutput bool, selectors ...string) error {
	selector := ""
	if len(selectors) != 0 {
		selector = selectors[0]
	}
	if jsonOutput {
		return encodeJSON(writer, initJSONFor(result, selector))
	}
	return renderInitHuman(writer, result, selector)
}

func initJSONFor(result project.InitResult, selectors ...string) initJSON {
	selector := ""
	if len(selectors) != 0 {
		selector = selectors[0]
	}
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
	return initJSON{
		Path:        result.Path,
		Outcome:     result.Outcome,
		NextCommand: initNextCommandFor(selector),
		Candidates:  candidates,
	}
}

func renderInitHuman(writer io.Writer, result project.InitResult, selectors ...string) error {
	selector := ""
	if len(selectors) != 0 {
		selector = selectors[0]
	}
	if _, err := fmt.Fprintf(writer, "path: %s\noutcome: %s\nnext_command: %s\n", result.Path, result.Outcome, initNextCommandFor(selector)); err != nil {
		return err
	}
	for _, definition := range result.Candidates {
		if _, err := fmt.Fprintf(writer, "candidate: %s source=%s argv=%s\n", definition.Name, definition.Source, shellJoin(definition.Argv)); err != nil {
			return err
		}
	}
	return nil
}
