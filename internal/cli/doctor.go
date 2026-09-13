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
	"time"

	"hum/internal/config"
	"hum/internal/daemon"
	processpkg "hum/internal/process"
	"hum/internal/project"
	"hum/internal/protocol"

	urfavecli "github.com/urfave/cli/v3"
	"golang.org/x/sys/unix"
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
	case "darwin", "linux":
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
		add("project.discovery", doctorFail, selectionErr.Error(), nil)
	} else if err := ctx.Err(); err != nil {
		add("project.discovery", doctorFail, "project inspection canceled", nil)
	} else {
		manifest, declarationPresent, selectionErr = loadDoctorManifest(ctx, selection)
		if selectionErr != nil {
			add("project.discovery", doctorFail, selectionErr.Error(), nil)
		} else if !declarationPresent {
			add("project.discovery", doctorFail, "no manifest or supported conventional process was found", map[string]any{"project_root": selection.root})
		} else {
			manifest.selector = selection.selector
			add("project.discovery", doctorPass, fmt.Sprintf("resolved %d process definition(s)", len(manifest.defs)), map[string]any{"project_root": manifest.root, "manifest": manifestDisplayName(manifest)})
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
		byName := make(map[string]project.Definition, len(definitions))
		for _, definition := range definitions {
			byName[definition.Name] = definition
		}
		return manifestState{root: selection.manifest.Root, defs: definitions, byName: byName, display: selection.manifest.Relative}, true, nil
	}
	definitions, err := project.ResolveDefinitionsReadOnly(ctx, selection.root)
	if err != nil {
		var noCandidate *project.NoCandidateError
		if !errors.As(err, &noCandidate) {
			return manifestState{}, false, err
		}
		return manifestState{root: selection.root, defs: []project.Definition{}, byName: make(map[string]project.Definition)}, false, nil
	}
	byName := make(map[string]project.Definition, len(definitions))
	for _, definition := range definitions {
		byName[definition.Name] = definition
	}
	return manifestState{root: selection.root, defs: definitions, byName: byName}, true, nil
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

func diagnoseRuntimePath(path string) (string, string) {
	info, err := os.Lstat(path)
	if err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return doctorFail, "runtime path exists but is not a private directory"
		}
		if info.Mode().Perm()&0o022 != 0 {
			return doctorFail, "runtime directory permits group or other writes"
		}
		probe, err := os.CreateTemp(path, ".hum-doctor-*")
		if err != nil {
			return doctorFail, "runtime directory is not writable"
		}
		name := probe.Name()
		closeErr := probe.Close()
		removeErr := os.Remove(name)
		if closeErr != nil || removeErr != nil {
			return doctorFail, "runtime writability probe could not be cleaned up"
		}
		return doctorPass, "runtime directory is usable"
	}
	if !os.IsNotExist(err) {
		return doctorFail, "runtime path cannot be inspected"
	}
	ancestor := filepath.Clean(path)
	for {
		if _, lstatErr := os.Lstat(ancestor); lstatErr == nil {
			return doctorFail, "runtime path contains an unusable filesystem entry"
		} else if !os.IsNotExist(lstatErr) {
			return doctorFail, "runtime path component cannot be inspected"
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return doctorFail, "runtime directory has no usable parent"
		}
		ancestor = parent
		info, statErr := os.Stat(ancestor)
		if statErr == nil {
			if !info.IsDir() || unix.Access(ancestor, unix.W_OK|unix.X_OK) != nil {
				return doctorFail, "runtime directory cannot be created beneath its existing parent"
			}
			return doctorInfo, "runtime directory is absent; its existing parent is usable"
		}
		if !os.IsNotExist(statErr) {
			return doctorFail, "runtime path parent cannot be inspected"
		}
	}
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

func diagnoseDaemon(ctx context.Context, paths daemon.RuntimePaths, add func(string, string, string, map[string]any)) {
	info, err := os.Lstat(paths.Socket)
	if os.IsNotExist(err) {
		add("daemon", doctorInfo, "daemon socket is absent; launch commands will start it", map[string]any{"socket": paths.Socket})
		return
	}
	if err != nil {
		add("daemon", doctorFail, "daemon socket cannot be inspected", map[string]any{"socket": paths.Socket})
		return
	}
	if info.Mode()&os.ModeSocket == 0 {
		add("daemon", doctorFail, "daemon socket path is not a socket", map[string]any{"socket": paths.Socket})
		return
	}
	dialCtx, cancel := context.WithTimeout(ctx, 750*time.Millisecond)
	defer cancel()
	client, err := daemon.DialRuntime(dialCtx, paths)
	if client != nil {
		defer client.Close()
	}
	if err == nil {
		add("daemon", doctorPass, "existing daemon is reachable and protocol-compatible", map[string]any{"socket": paths.Socket})
		return
	}
	var mismatch *daemon.VersionMismatchError
	if errors.As(err, &mismatch) {
		add("daemon", doctorFail, "existing daemon uses an incompatible protocol", map[string]any{"socket": paths.Socket})
		return
	}
	add("daemon", doctorFail, "existing daemon socket is unreachable", map[string]any{"socket": paths.Socket})
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
