package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"hum/internal/app"
	"hum/internal/daemon"
	"hum/internal/orchestrate"
	"hum/internal/output"
	processpkg "hum/internal/process"
	"hum/internal/project"
	"hum/internal/protocol"

	urfavecli "github.com/urfave/cli/v3"
)

type manifestState struct {
	root     string
	defs     []project.Definition
	byName   map[string]project.Definition
	selector string
}

func loadManifest(cwd string) (manifestState, error) {
	root, err := app.DiscoverProjectRoot(cwd)
	if err != nil {
		return manifestState{}, err
	}
	defs, err := project.ResolveDefinitions(root)
	if err != nil {
		return manifestState{}, err
	}
	byName := make(map[string]project.Definition, len(defs))
	for _, definition := range defs {
		byName[definition.Name] = definition
	}
	return manifestState{root: root, defs: defs, byName: byName}, nil
}

// loadManifestOrEmpty preserves the ad-hoc command path when no conventional
// or explicit definition exists. Any other resolution failure remains
// authoritative and is returned before a daemon is contacted.
func loadManifestOrEmpty(cwd string) (manifestState, error) {
	manifest, err := loadManifest(cwd)
	if err == nil {
		return manifest, nil
	}
	var noCandidate *project.NoCandidateError
	if !errors.As(err, &noCandidate) {
		return manifestState{}, err
	}
	root := noCandidate.Root
	if root == "" {
		root, err = app.DiscoverProjectRoot(cwd)
		if err != nil {
			return manifestState{}, err
		}
	}
	return manifestState{
		root:   root,
		defs:   []project.Definition{},
		byName: make(map[string]project.Definition),
	}, nil
}

func readinessConfig(definition project.Definition) *protocol.ReadinessConfig {
	if definition.Ready == nil {
		return nil
	}
	return &protocol.ReadinessConfig{Match: definition.Ready.Match, Timeout: definition.Ready.Timeout}
}

func restartPolicy(definition project.Definition) string {
	if definition.Restart == "" {
		return string(project.RestartNever)
	}
	return string(definition.Restart)
}

func protocolRestartPolicy(definition project.Definition) string {
	return restartPolicy(definition)
}

func effectiveAppRestart(policy app.RestartPolicy) app.RestartPolicy {
	if policy == "" {
		return app.RestartNever
	}
	return policy
}

func isManifestSource(source string) bool {
	return source == "manifest" || source == "hum.yaml" || strings.HasPrefix(source, "manifest:") || strings.HasPrefix(source, "hum.yaml:")
}

func effectiveProcessRestart(process app.Process) app.RestartPolicy {
	if !isManifestSource(process.Source) {
		return app.RestartNever
	}
	return effectiveAppRestart(process.Restart)
}

func manifestProcess(definition project.Definition, root string) app.Process {
	return app.Process{
		Name:    definition.Name,
		Source:  definition.Source,
		Root:    root,
		TTY:     definition.TTY,
		Cwd:     definition.Cwd,
		Argv:    append([]string(nil), definition.Argv...),
		State:   app.State("stopped"),
		Restart: app.RestartPolicy(restartPolicy(definition)),
	}
}

func mergeManifestProcesses(manifest manifestState, running []app.Process) []app.Process {
	result := append([]app.Process(nil), running...)
	seen := make(map[string]struct{}, len(result))
	for _, process := range result {
		if process.Root == manifest.root {
			seen[process.Name] = struct{}{}
		}
	}
	for _, definition := range manifest.defs {
		if _, ok := seen[definition.Name]; ok {
			continue
		}
		result = append(result, manifestProcess(definition, manifest.root))
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Root != result[j].Root {
			return result[i].Root < result[j].Root
		}
		if result[i].Name != result[j].Name {
			return result[i].Name < result[j].Name
		}
		return result[i].Start.Before(result[j].Start)
	})
	return result
}

// manifestLaunchResult is the stable one-line result emitted by start and up.
// Error is deliberately a string so every result remains easy to consume as
// one NDJSON object without exposing daemon internals.
type manifestLaunchResult struct {
	Name                string     `json:"name"`
	ProjectSelector     string     `json:"-"`
	Outcome             string     `json:"outcome"`
	Source              string     `json:"source"`
	Argv                []string   `json:"argv"`
	State               string     `json:"state,omitempty"`
	PID                 *int       `json:"pid,omitempty"`
	LaunchCursor        *uint64    `json:"launch_cursor,omitempty"`
	Readiness           string     `json:"readiness,omitempty"`
	ReadinessMatch      string     `json:"readiness_match,omitempty"`
	ReadinessConfigured bool       `json:"-"`
	ReadyCursor         *uint64    `json:"ready_cursor,omitempty"`
	BlockedBy           []string   `json:"blocked_by,omitempty"`
	ExistingState       string     `json:"existing_state,omitempty"`
	ChangedFields       []string   `json:"changed_fields,omitempty"`
	Guidance            string     `json:"guidance,omitempty"`
	Restart             string     `json:"restart"`
	Relaunches          int        `json:"relaunches"`
	NextLaunchAt        *time.Time `json:"next_launch_at,omitempty"`
	Error               string     `json:"error,omitempty"`
	ExitCode            *int       `json:"exit_code,omitempty"`
}

// MarshalJSON keeps a configured empty readiness matcher visible. The matcher
// is omitted for processes without readiness, but an empty regular expression
// is a valid configured value and must remain distinguishable from omission.
func (result manifestLaunchResult) MarshalJSON() ([]byte, error) {
	type plainManifestLaunchResult manifestLaunchResult
	if !result.ReadinessConfigured || result.ReadinessMatch != "" {
		return json.Marshal(plainManifestLaunchResult(result))
	}
	match := result.ReadinessMatch
	return json.Marshal(struct {
		plainManifestLaunchResult
		ReadinessMatch *string `json:"readiness_match"`
	}{
		plainManifestLaunchResult: plainManifestLaunchResult(result),
		ReadinessMatch:            &match,
	})
}

func undefinedManifestDefinition(name string) project.Definition {
	return project.Definition{Name: name, Source: "manifest", Argv: []string{}, After: []string{}}
}

func manifestResultWithSelector(result manifestLaunchResult, selector string) manifestLaunchResult {
	result.ProjectSelector = selector
	if selector != "" && result.Guidance != "" {
		result.Guidance = strings.ReplaceAll(result.Guidance, "hum ", "hum "+selector+" ")
	}
	return result
}

func newManifestLaunchResult(definition project.Definition, outcome string) manifestLaunchResult {
	return manifestLaunchResult{
		Name:    definition.Name,
		Outcome: outcome,
		Source:  definition.Source,
		Argv:    append([]string(nil), definition.Argv...),
		Restart: restartPolicy(definition),
	}
}

func manifestLaunchSkipped(ctx context.Context, client *daemon.Client, root string, definition project.Definition, blockedBy []string) manifestLaunchResult {
	shared := orchestrate.SkippedResult(ctx, root, cliOrchestrateDefinition(definition), blockedBy, func(ctx context.Context, name, lookupRoot string) (orchestrate.Process, error) {
		current, err := client.Get(ctx, daemon.GetRequest{Name: name, Cwd: lookupRoot})
		return cliOrchestrateProcess(current), err
	})
	return cliManifestLaunchResult(definition, shared)
}

func manifestLaunchResultFor(definition project.Definition, process app.Process, outcome string) manifestLaunchResult {
	result := manifestLaunchResult{
		Name:         process.Name,
		Outcome:      outcome,
		Source:       process.Source,
		Argv:         append([]string(nil), process.Argv...),
		State:        string(process.State),
		Restart:      string(effectiveProcessRestart(process)),
		Relaunches:   process.Relaunches,
		NextLaunchAt: process.NextLaunchAt,
	}
	if process.PID > 0 {
		pid := process.PID
		result.PID = &pid
	}
	if process.State == app.StateExited {
		exitCode := process.ExitCode
		result.ExitCode = &exitCode
	}
	cursor := uint64(process.LaunchCursor)
	result.LaunchCursor = &cursor
	if process.Readiness != nil && process.Readiness.State != app.ReadinessRunningUnverified {
		result.ReadinessMatch = process.Readiness.Match
		result.ReadinessConfigured = true
	}
	if process.State != app.StateRunning {
		return result
	}
	if process.Readiness == nil {
		result.Readiness = app.ReadinessRunningUnverified
		return result
	}
	result.Readiness = process.Readiness.State
	if process.Readiness.Cursor != nil {
		readyCursor := uint64(*process.Readiness.Cursor)
		result.ReadyCursor = &readyCursor
	}
	return result
}

func manifestLaunchError(definition project.Definition, err error) manifestLaunchResult {
	result := newManifestLaunchResult(definition, "error")
	if err != nil {
		result.Error = err.Error()
	}
	return result
}

func cliOrchestrateDefinition(definition project.Definition) orchestrate.Definition {
	shared := orchestrate.Definition{
		Name: definition.Name, Source: definition.Source, Argv: append([]string(nil), definition.Argv...),
		Cwd: definition.Cwd, After: append([]string(nil), definition.After...), TTY: definition.TTY,
		Restart: restartPolicy(definition),
	}
	if definition.Ready != nil {
		shared.Ready = &orchestrate.ReadinessConfig{Match: definition.Ready.Match, Timeout: definition.Ready.Timeout}
	}
	return shared
}

func cliOrchestrateProcess(process app.Process) orchestrate.Process {
	shared := orchestrate.Process{
		Name: process.Name, Source: process.Source, Root: process.Root, TTY: process.TTY,
		PID: process.PID, PGID: process.PGID, Cwd: process.Cwd, Argv: append([]string(nil), process.Argv...),
		Start: process.Start, LaunchCursor: uint64(process.LaunchCursor), State: string(process.State),
		ExitCode: process.ExitCode, ExitedAt: process.ExitedAt, RestartCount: process.RestartCount,
		Followers: process.Followers, Restart: string(process.Restart), Relaunches: process.Relaunches,
		NextLaunchAt: process.NextLaunchAt,
	}
	if process.NextCursor != 0 {
		cursor := uint64(process.NextCursor)
		shared.NextCursor = &cursor
	}
	if process.Exit != nil {
		shared.Exit = &orchestrate.Exit{Code: process.Exit.ExitCode, Time: process.Exit.ExitedAt}
		if process.Exit.Err != nil {
			shared.Exit.Error = process.Exit.Err.Error()
		}
	}
	if process.Readiness != nil {
		readiness := &orchestrate.Readiness{State: process.Readiness.State, Time: process.Readiness.Time, Match: process.Readiness.Match}
		if process.Readiness.Cursor != nil {
			cursor := uint64(*process.Readiness.Cursor)
			readiness.Cursor = &cursor
		}
		shared.Readiness = readiness
	}
	return shared
}

func cliAppProcess(process orchestrate.Process) app.Process {
	process = orchestrate.NormalizeProcess(process)
	result := app.Process{
		Name: process.Name, Source: process.Source, Root: process.Root, TTY: process.TTY,
		PID: process.PID, PGID: process.PGID, Cwd: process.Cwd, Argv: append([]string(nil), process.Argv...),
		Start: process.Start, LaunchCursor: output.Cursor(process.LaunchCursor), State: app.State(process.State),
		ExitCode: process.ExitCode, ExitedAt: process.ExitedAt, RestartCount: process.RestartCount,
		Followers: process.Followers, Restart: app.RestartPolicy(process.Restart), Relaunches: process.Relaunches,
		NextLaunchAt: process.NextLaunchAt,
	}
	if process.NextCursor != nil {
		result.NextCursor = output.Cursor(*process.NextCursor)
	}
	if process.Exit != nil {
		exit := &processpkg.Result{ExitCode: process.Exit.Code, ExitedAt: process.Exit.Time}
		if process.Exit.Error != "" {
			exit.Err = errors.New(process.Exit.Error)
		}
		result.Exit = exit
	}
	if process.Readiness != nil {
		readiness := &app.Readiness{State: process.Readiness.State, Time: process.Readiness.Time, Match: process.Readiness.Match}
		if process.Readiness.Cursor != nil {
			cursor := output.Cursor(*process.Readiness.Cursor)
			readiness.Cursor = &cursor
		}
		result.Readiness = readiness
	}
	return result
}

func cliManifestLaunchResult(definition project.Definition, shared orchestrate.Result) manifestLaunchResult {
	var result manifestLaunchResult
	if shared.Process != nil {
		result = manifestLaunchResultFor(definition, cliAppProcess(*shared.Process), shared.Outcome)
	} else {
		result = newManifestLaunchResult(definition, shared.Outcome)
	}
	result.BlockedBy = append([]string(nil), shared.BlockedBy...)
	result.ExistingState = shared.ExistingState
	result.ChangedFields = append([]string(nil), shared.ChangedFields...)
	result.Guidance = shared.Guidance
	if shared.Error != nil {
		result.Error = shared.Error.Error()
	}
	return result
}

func cliSharedLaunchResult(definition project.Definition, result manifestLaunchResult, process *app.Process) orchestrate.Result {
	shared := orchestrate.Result{
		Name: result.Name, Outcome: result.Outcome,
		BlockedBy: append([]string(nil), result.BlockedBy...), ExistingState: result.ExistingState,
		ChangedFields: append([]string(nil), result.ChangedFields...), Guidance: result.Guidance,
	}
	if shared.Name == "" {
		shared.Name = definition.Name
	}
	if result.Error != "" {
		shared.Error = errors.New(result.Error)
	}
	var snapshot app.Process
	hasSnapshot := process != nil && (process.Name != "" || process.Source != "" || process.State != "" || process.Cwd != "" || process.PID != 0 || process.LaunchCursor != 0 || len(process.Argv) != 0)
	if hasSnapshot {
		snapshot = *process
	}
	if result.State != "" || result.PID != nil || result.LaunchCursor != nil || result.Readiness != "" || result.ReadinessConfigured {
		hasSnapshot = true
		if result.Name != "" {
			snapshot.Name = result.Name
		}
		if result.Source != "" {
			snapshot.Source = result.Source
		}
		if result.Argv != nil {
			snapshot.Argv = append([]string(nil), result.Argv...)
		}
		if result.State != "" {
			snapshot.State = app.State(result.State)
			snapshot.PID = 0
			if result.PID != nil {
				snapshot.PID = *result.PID
			}
			snapshot.Exit = nil
			snapshot.ExitCode = 0
			if result.ExitCode != nil {
				snapshot.ExitCode = *result.ExitCode
			}
			snapshot.Relaunches = result.Relaunches
			snapshot.NextLaunchAt = result.NextLaunchAt
		}
		if result.LaunchCursor != nil {
			snapshot.LaunchCursor = output.Cursor(*result.LaunchCursor)
		}
		if result.Restart != "" {
			snapshot.Restart = app.RestartPolicy(result.Restart)
		}
		if result.Readiness != "" || result.ReadinessConfigured {
			snapshot.Readiness = nil
			if result.ReadinessConfigured || result.Readiness == app.ReadinessStarting || result.Readiness == app.ReadinessReady {
				readiness := &app.Readiness{State: result.Readiness, Match: result.ReadinessMatch}
				if result.ReadyCursor != nil {
					cursor := output.Cursor(*result.ReadyCursor)
					readiness.Cursor = &cursor
				}
				snapshot.Readiness = readiness
			}
		}
	}
	if hasSnapshot && shared.Error == nil {
		converted := cliOrchestrateProcess(snapshot)
		shared.Process = &converted
	}
	return shared
}

func cliReadinessResult(client *daemon.Client, ctx context.Context, cwd string, definition project.Definition, process app.Process, initialOutcome string, timeout time.Duration) (manifestLaunchResult, error) {
	shared, err := orchestrate.WaitForReadiness(ctx, cwd, cliOrchestrateDefinition(definition), cliOrchestrateProcess(process), initialOutcome, timeout, orchestrate.ReadinessOperations{
		Get: func(ctx context.Context, name, lookupCwd string) (orchestrate.Process, error) {
			current, err := client.Get(ctx, daemon.GetRequest{Name: name, Cwd: lookupCwd})
			return cliOrchestrateProcess(current), err
		},
		Wait: func(ctx context.Context, request orchestrate.WaitRequest) (orchestrate.WaitResult, error) {
			milliseconds, err := manifestTimeoutMS(request.Timeout)
			if err != nil {
				return orchestrate.WaitResult{}, err
			}
			waited, err := client.Wait(ctx, daemon.WaitRequest{Name: request.Name, Cwd: request.Cwd, Match: request.Match, TimeoutMS: milliseconds})
			result := orchestrate.WaitResult{Outcome: string(waited.Outcome), Cursor: uint64(waited.Cursor)}
			if waited.Exit != nil {
				result.Exit = &orchestrate.Exit{Code: waited.Exit.ExitCode, Time: waited.Exit.ExitedAt}
				if waited.Exit.Err != nil {
					result.Exit.Error = waited.Exit.Err.Error()
				}
			}
			return result, err
		},
		IsNotFound: isNotFound,
	})
	if err != nil {
		return manifestLaunchResult{}, err
	}
	return cliManifestLaunchResult(definition, shared), nil
}

// manifestProgressDriftDetail renders the changed_fields and restart
// guidance already carried on a definition_drift result into the single
// stderr progress detail up prints for it, e.g.
// "definition_drift (argv, cwd); run hum restart db" instead of the bare
// outcome name.
func manifestProgressDriftDetail(result manifestLaunchResult) string {
	return fmt.Sprintf("definition_drift (%s); run %s", strings.Join(result.ChangedFields, ", "), result.Guidance)
}

func removedManifestResults(ctx context.Context, client *daemon.Client, manifest manifestState) ([]manifestLaunchResult, error) {
	processes, err := client.List(ctx, daemon.ListRequest{Op: protocol.OpList, Cwd: manifest.root, IncludeCompleted: true})
	if err != nil {
		return nil, err
	}
	definitions := make([]orchestrate.Definition, 0, len(manifest.defs))
	for _, definition := range manifest.defs {
		definitions = append(definitions, cliOrchestrateDefinition(definition))
	}
	sharedProcesses := make([]orchestrate.Process, 0, len(processes))
	for _, process := range processes {
		sharedProcesses = append(sharedProcesses, cliOrchestrateProcess(process))
	}
	sharedResults := orchestrate.RemovedDefinitionResults(manifest.root, definitions, sharedProcesses)
	results := make([]manifestLaunchResult, 0, len(sharedResults))
	for _, shared := range sharedResults {
		definition := undefinedManifestDefinition(shared.Name)
		if shared.Process != nil {
			definition.Source = shared.Process.Source
		}
		results = append(results, cliManifestLaunchResult(definition, shared))
	}
	return results, nil
}

func manifestTTYUpgradeError(definition project.Definition, process app.Process) error {
	if definition.TTY && !process.TTY {
		return fmt.Errorf("declared process %q is running without a tty; stop it and rerun with tty: true", definition.Name)
	}
	return nil
}

func manifestUnavailableMessage(definition project.Definition, selectors ...string) error {
	selector := ""
	if len(selectors) != 0 {
		selector = selectors[0]
	}
	return newUserFacingError(fmt.Sprintf("Nothing is running in this project. Start it with %s.", projectCommand(selector, "start "+definition.Name)))
}

func parseManifestTimeout(cmd *urfavecli.Command, definition project.Definition) (time.Duration, error) {
	if cmd.IsSet("timeout") {
		parsed, err := time.ParseDuration(cmd.String("timeout"))
		if err != nil {
			return 0, fmt.Errorf("timeout must be a valid duration: %w", err)
		}
		if parsed <= 0 {
			return 0, errors.New("timeout must be positive")
		}
		if parsed < time.Millisecond {
			return 0, errors.New("timeout must be at least 1ms")
		}
	}
	if definition.Ready == nil || definition.Ready.Timeout <= 0 {
		return defaultWaitTimeout, nil
	}
	return definition.Ready.Timeout, nil
}

func manifestTimeoutMS(timeout time.Duration) (int64, error) {
	milliseconds := timeout / time.Millisecond
	if milliseconds <= 0 {
		milliseconds = 1
	}
	if milliseconds > (1<<63 - 1) {
		return 0, errors.New("timeout is too large")
	}
	return int64(milliseconds), nil
}

func ensureManifestStart(ctx context.Context, client *daemon.Client, cwd, root string, definition project.Definition, env []string, preserveRecovery bool) (manifestLaunchResult, app.Process, bool, error) {
	lookupRoot := root
	if lookupRoot == "" {
		lookupRoot = cwd
	}
	shared := orchestrate.Ensure(ctx, lookupRoot, cliOrchestrateDefinition(definition), env, preserveRecovery, orchestrate.EnsureOperations{
		Get: func(ctx context.Context, name, root string) (orchestrate.Process, error) {
			current, err := client.Get(ctx, daemon.GetRequest{Name: name, Cwd: root})
			return cliOrchestrateProcess(current), err
		},
		Start: func(ctx context.Context, request orchestrate.StartRequest) (orchestrate.Process, error) {
			var ready *protocol.ReadinessConfig
			if request.Ready != nil {
				ready = &protocol.ReadinessConfig{Match: request.Ready.Match, Timeout: request.Ready.Timeout}
			}
			current, err := client.Start(ctx, daemon.StartRequest{
				Name: request.Name, Source: request.Source, Root: request.Root, Cwd: request.Cwd,
				Argv: append([]string(nil), request.Argv...), Env: append([]string(nil), request.Env...),
				Ready: ready, TTY: request.TTY, Restart: request.Restart,
			})
			return cliOrchestrateProcess(current), err
		},
		IsNotFound: isNotFound,
		IsNameInUse: func(err error) bool {
			return isNameInUse(err) || errors.Is(err, app.ErrNameInUse)
		},
	})
	result := cliManifestLaunchResult(definition, shared.Result)
	process := app.Process{}
	if shared.Result.Process != nil {
		process = cliAppProcess(*shared.Result.Process)
	} else if shared.Process.Name != "" || shared.Process.State != "" {
		process = cliAppProcess(shared.Process)
	}
	return result, process, shared.Already, nil
}
func manifestTimeoutOverride(cmd *urfavecli.Command) (time.Duration, error) {
	if !cmd.IsSet("timeout") {
		return 0, nil
	}
	parsed, err := time.ParseDuration(cmd.String("timeout"))
	if err != nil {
		return 0, fmt.Errorf("timeout must be a valid duration: %w", err)
	}
	if parsed <= 0 {
		return 0, errors.New("timeout must be positive")
	}
	if parsed < time.Millisecond {
		return 0, errors.New("timeout must be at least 1ms")
	}
	return parsed, nil
}
func aggregateManifestExit(results []manifestLaunchResult) error {
	for _, result := range results {
		if result.Outcome == "error" || result.Outcome == "definition_drift" {
			return urfavecli.Exit("", 1)
		}
	}
	for _, result := range results {
		if result.Outcome == "exited_before_ready" || result.Outcome == "recovery_pending" || result.Outcome == "recovery_exhausted" {
			return urfavecli.Exit("", 3)
		}
	}
	for _, result := range results {
		if result.Outcome == "timed_out" {
			return urfavecli.Exit("", 2)
		}
	}
	return nil
}

func manifestProcessEnv() []string {
	return append([]string(nil), os.Environ()...)
}

// manifestResultJSON returns the stable, response-safe launch object. Keep
// argv present even for an error so each NDJSON line has the same shape.
func manifestResultJSON(result manifestLaunchResult) manifestLaunchResult {
	if result.Argv == nil {
		result.Argv = []string{}
	}
	return result
}

func processReadinessFields(process app.Process) (string, *protocol.Cursor) {
	if process.State != app.StateRunning || process.Source == "" || process.Source == "ad_hoc" {
		return "", nil
	}
	if process.Readiness == nil {
		return app.ReadinessRunningUnverified, nil
	}
	var cursor *protocol.Cursor
	if process.Readiness.Cursor != nil {
		value := protocol.Cursor(*process.Readiness.Cursor)
		cursor = &value
	}
	return process.Readiness.State, cursor
}
