package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"hum/internal/config"
	"hum/internal/daemon"
	processpkg "hum/internal/process"
	"hum/internal/project"
	"hum/internal/protocol"

	urfavecli "github.com/urfave/cli/v3"
)

const (
	doctorPass = "PASS"
	doctorWarn = "WARN"
	doctorFail = "FAIL"
	doctorInfo = "INFO"
)

type doctorCheck struct {
	Name    string         `json:"name"`
	Status  string         `json:"status"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

type doctorSummary struct {
	Pass int `json:"pass"`
	Warn int `json:"warn"`
	Fail int `json:"fail"`
	Info int `json:"info"`
}

type doctorResult struct {
	OK      bool          `json:"ok"`
	Checks  []doctorCheck `json:"checks"`
	Summary doctorSummary `json:"summary"`
}

func doctorCommand(ctx context.Context, cmd *urfavecli.Command, version, buildTime string, writer io.Writer) error {
	if err := requireNoArgs(cmd, "doctor"); err != nil {
		return err
	}
	if cmd.Bool("global") || cmd.IsSet("global") || rawScopeFlag(cmd, "global", "g") {
		return newCLIUsageError(errors.New("hum doctor does not accept --global; select one filesystem project"))
	}
	ctx = nonNilContext(ctx)
	checks := make([]doctorCheck, 0, 12)
	add := func(name, status, message string, details map[string]any) {
		checks = append(checks, doctorCheck{Name: name, Status: status, Message: message, Details: details})
	}

	switch runtime.GOOS {
	case "darwin", "linux", "windows":
		add("platform", doctorPass, "supported operating system", map[string]any{"os": runtime.GOOS})
	default:
		add("platform", doctorFail, "unsupported operating system", map[string]any{"os": runtime.GOOS})
	}

	input := cliConfigInput(cmd, os.Environ())
	cfg := diagnoseDoctorConfig(version, buildTime, input, add)
	paths := daemon.NewRuntimePaths(cfg.RuntimeDir)
	status, message := diagnoseRuntimePath(paths.Dir)
	add("runtime.path", status, message, map[string]any{"path": paths.Dir})

	selection, selectionErr := selectedProjectDirectory(cmd)
	var (
		manifest           manifestState
		declarationPresent bool
	)
	if selectionErr != nil {
		add("project.manifest", doctorFail, selectionErr.Error(), nil)
	} else if err := ctx.Err(); err != nil {
		add("project.manifest", doctorFail, "project inspection canceled", nil)
	} else {
		manifest, declarationPresent, selectionErr = loadDoctorManifest(ctx, selection)
		if selectionErr != nil {
			add("project.manifest", doctorFail, selectionErr.Error(), map[string]any{"project_root": selection.root})
		} else if !declarationPresent {
			add("project.manifest", doctorFail, (&project.ManifestMissingError{Root: selection.root}).Error(), map[string]any{"project_root": selection.root})
		} else {
			manifest.selector = selection.selector
			details := map[string]any{"project_root": manifest.root, "manifest": manifestDisplayName(manifest)}
			if manifest.shadowedManifest != "" {
				details["shadowed_manifest"] = manifest.shadowedManifest
			}
			add("project.manifest", doctorPass, fmt.Sprintf("resolved %d process definition(s)", len(manifest.defs)), details)
			if err := prepareManifestEnvironments(&manifest, nil, os.Environ(), true); err != nil {
				add("environment.composition", doctorFail, err.Error(), nil)
			} else if !doctorEnvironmentsFitProtocol(manifest) {
				add("environment.composition", doctorFail, "a composed process request exceeds the protocol limit", nil)
			} else {
				add("environment.composition", doctorPass, fmt.Sprintf("composed %d process environment(s)", len(manifest.defs)), map[string]any{"processes": len(manifest.defs)})
				diagnoseExecutables(ctx, manifest, add)
			}
		}
	}

	diagnoseDaemon(ctx, paths, add)
	result := newDoctorResult(checks)
	var writeErr error
	if cmd.Bool("json") {
		writeErr = encodeJSON(writer, result)
	} else {
		writeErr = writeDoctorHuman(writer, result)
	}
	if writeErr != nil {
		return writeErr
	}
	if !result.OK {
		return urfavecli.Exit("", 1)
	}
	return nil
}

func loadDoctorManifest(ctx context.Context, selection projectSelection) (manifestState, bool, error) {
	if selection.hasManifest {
		definitions, err := project.ResolveExplicitDefinitionsReadOnly(selection.manifest)
		if err != nil {
			return manifestState{}, false, &project.ConfigurationError{Source: selection.manifest.Relative, Err: err}
		}
		manifest := newManifestState(selection.manifest.Root, definitions)
		manifest.display = selection.manifest.Relative
		return manifest, true, nil
	}
	defaultSelection, present, selectionErr := project.DefaultManifestSelection(selection.root)
	if selectionErr != nil {
		return manifestState{}, false, selectionErr
	}
	definitions, err := project.ResolveDefinitionsContext(ctx, selection.root)
	if err != nil {
		if errors.Is(err, project.ErrManifestMissing) {
			return newManifestState(selection.root, []project.Definition{}), false, nil
		}
		return manifestState{}, false, err
	}
	manifest := newManifestState(selection.root, definitions)
	manifest.display = defaultSelection.Relative
	if present && defaultSelection.Relative == ".hum.yaml" {
		if _, sharedErr := os.Lstat(filepath.Join(selection.root, "hum.yaml")); sharedErr == nil {
			manifest.shadowedManifest = "hum.yaml"
		}
	}
	return manifest, true, nil
}

func diagnoseDoctorConfig(version, buildTime string, input config.Input, add func(string, string, string, map[string]any)) config.Config {
	build := config.BuildOpts{Version: version, BuildTime: buildTime}
	baseInput := config.Input{FlagRuntimeDir: input.FlagRuntimeDir, EnvRuntimeDir: input.EnvRuntimeDir, EnvXDGRuntimeDir: input.EnvXDGRuntimeDir}
	cfg, _ := config.New(build, baseInput)
	add("config.runtime_dir", doctorPass, "runtime directory resolved", map[string]any{"path": daemon.NewRuntimePaths(cfg.RuntimeDir).Dir})

	stop, err := config.New(build, config.Input{FlagStopGrace: input.FlagStopGrace, EnvStopGrace: input.EnvStopGrace})
	if err != nil {
		add("config.stop_grace", doctorFail, "stop grace is invalid", nil)
	} else {
		cfg.StopGrace = stop.StopGrace
		status, message := doctorPass, "stop grace is valid"
		if cfg.StopGrace == 0 {
			status, message = doctorWarn, "stop grace is zero; processes are killed immediately after the termination check"
		}
		add("config.stop_grace", status, message, map[string]any{"duration": cfg.StopGrace.String()})
	}
	output, err := config.New(build, config.Input{FlagOutputBytes: input.FlagOutputBytes, EnvOutputBytes: input.EnvOutputBytes})
	if err != nil {
		add("config.output_bytes", doctorFail, "output byte limit is invalid", nil)
	} else {
		cfg.OutputBytes = output.OutputBytes
		add("config.output_bytes", doctorPass, "output byte limit is valid", map[string]any{"bytes": cfg.OutputBytes})
	}
	completed, err := config.New(build, config.Input{FlagCompletedRecords: input.FlagCompletedRecords, EnvCompletedRecords: input.EnvCompletedRecords})
	if err != nil {
		add("config.completed_records", doctorFail, "completed record limit is invalid", nil)
	} else {
		cfg.CompletedRecords = completed.CompletedRecords
		add("config.completed_records", doctorPass, "completed record limit is valid", map[string]any{"records": cfg.CompletedRecords})
	}
	return cfg
}

func doctorEnvironmentsFitProtocol(manifest manifestState) bool {
	for _, definition := range manifest.defs {
		request := protocol.StartRequest{
			Op: protocol.OpStart, Name: definition.Name, Root: manifest.root,
			Argv: definition.Argv, Cwd: definition.Cwd, Env: manifestEnvironment(manifest, definition),
			Source: definition.Source, Ready: readinessConfig(definition), TTY: definition.TTY,
			Restart: protocolRestartPolicy(definition), StopGrace: definition.StopGrace,
		}
		if _, err := protocol.MarshalLine(request); err != nil {
			return false
		}
	}
	return true
}

func diagnoseExecutables(ctx context.Context, manifest manifestState, add func(string, string, string, map[string]any)) {
	for _, definition := range manifest.defs {
		if err := ctx.Err(); err != nil {
			add("inspection.canceled", doctorFail, "executable inspection canceled", nil)
			return
		}
		env := manifestEnvironment(manifest, definition)
		if definition.TTY && !ttySupported() {
			add("process.tty", doctorFail, "TTY mode is unsupported on this operating system", map[string]any{"process": definition.Name})
		}
		if len(definition.Argv) == 0 || definition.Argv[0] == "" {
			add("process.executable", doctorFail, "process command is empty", map[string]any{"process": definition.Name})
		} else if _, err := processpkg.ResolveExecutable(definition.Argv[0], env, definition.Cwd); err != nil {
			add("process.executable", doctorFail, "process executable is unavailable", map[string]any{"process": definition.Name})
		} else {
			add("process.executable", doctorPass, "process executable is available", map[string]any{"process": definition.Name})
		}
		if definition.Ready == nil || len(definition.Ready.Exec) == 0 {
			continue
		}
		if definition.Ready.Exec[0] == "" {
			add("ready.executable", doctorFail, "readiness executable is empty", map[string]any{"process": definition.Name})
		} else if _, err := processpkg.ResolveExecutable(definition.Ready.Exec[0], env, definition.Cwd); err != nil {
			add("ready.executable", doctorFail, "readiness executable is unavailable", map[string]any{"process": definition.Name})
		} else {
			add("ready.executable", doctorPass, "readiness executable is available", map[string]any{"process": definition.Name})
		}
	}
}

func newDoctorResult(checks []doctorCheck) doctorResult {
	result := doctorResult{OK: true, Checks: checks}
	for _, check := range checks {
		switch check.Status {
		case doctorPass:
			result.Summary.Pass++
		case doctorWarn:
			result.Summary.Warn++
		case doctorFail:
			result.Summary.Fail++
			result.OK = false
		case doctorInfo:
			result.Summary.Info++
		}
	}
	return result
}

func writeDoctorHuman(writer io.Writer, result doctorResult) error {
	return writeDoctorHumanWithPolicy(writer, result, colorPolicyForWriter(writer))
}

func writeDoctorHumanWithPolicy(writer io.Writer, result doctorResult, colors colorPolicy) error {
	for _, check := range result.Checks {
		status := colors.apply(doctorStatusStyle(check.Status), check.Status)
		if _, err := fmt.Fprintf(writer, "%s %-28s %s\n", status, check.Name, check.Message); err != nil {
			return err
		}
	}
	parts := []string{
		fmt.Sprintf("%d pass", result.Summary.Pass),
		fmt.Sprintf("%d warn", result.Summary.Warn),
		fmt.Sprintf("%d fail", result.Summary.Fail),
		fmt.Sprintf("%d info", result.Summary.Info),
	}
	_, err := fmt.Fprintf(writer, "Summary: %s\n", strings.Join(parts, ", "))
	return err
}

func doctorStatusStyle(status string) ansiStyle {
	switch status {
	case doctorPass:
		return ansiGreen
	case doctorWarn:
		return ansiYellow
	case doctorFail:
		return ansiRed
	case doctorInfo:
		return ansiCyan
	default:
		return ""
	}
}
