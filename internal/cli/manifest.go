package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"hum/internal/app"
	"hum/internal/daemon"
	"hum/internal/project"
	"hum/internal/protocol"

	urfavecli "github.com/urfave/cli/v3"
)

type manifestState struct {
	root   string
	defs   []project.Definition
	byName map[string]project.Definition
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

func sameManifestSource(left, right string) bool {
	return left == right || isManifestSource(left) && isManifestSource(right)
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
	result := newManifestLaunchResult(definition, "skipped")
	if current, err := client.Get(ctx, daemon.GetRequest{Name: definition.Name, Cwd: root}); err == nil {
		result = manifestLaunchResultFor(definition, current, "skipped")
		if current.State == app.StateRunning {
			result.ExistingState = "running"
		} else {
			result.ExistingState = "exited"
		}
	}
	result.BlockedBy = append([]string(nil), blockedBy...)
	return result
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

func definitionMatchesProcess(definition project.Definition, process app.Process) bool {
	return sameManifestSource(process.Source, definition.Source)
}

func canonicalManifestCwd(root, cwd string) string {
	if cwd == "" {
		cwd = root
	}
	if !filepath.IsAbs(cwd) {
		cwd = filepath.Join(root, cwd)
	}
	absolute, err := filepath.Abs(cwd)
	if err != nil {
		absolute = filepath.Clean(cwd)
	}
	if resolved, err := filepath.EvalSymlinks(absolute); err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(absolute)
}

func processReadinessMatch(process app.Process) (bool, string) {
	if process.Readiness == nil || process.Readiness.State == app.ReadinessRunningUnverified {
		return false, ""
	}
	return true, process.Readiness.Match
}

func manifestChangedFields(root string, definition project.Definition, process app.Process) []string {
	changed := make([]string, 0, 5)
	if !slices.Equal(definition.Argv, process.Argv) {
		changed = append(changed, "argv")
	}
	if canonicalManifestCwd(root, definition.Cwd) != canonicalManifestCwd(root, process.Cwd) {
		changed = append(changed, "cwd")
	}
	definitionReady, definitionMatch := false, ""
	if definition.Ready != nil {
		definitionReady, definitionMatch = true, definition.Ready.Match
	}
	processReady, processMatch := processReadinessMatch(process)
	if definitionReady != processReady || definitionMatch != processMatch {
		changed = append(changed, "readiness_match")
	}
	if definition.TTY != process.TTY {
		changed = append(changed, "tty")
	}
	if restartPolicy(definition) != string(effectiveProcessRestart(process)) {
		changed = append(changed, "restart")
	}
	sort.Strings(changed)
	return changed
}

func manifestDefinitionDriftResult(root string, definition project.Definition, process app.Process) manifestLaunchResult {
	result := manifestLaunchResultFor(definition, process, "definition_drift")
	result.ChangedFields = manifestChangedFields(root, definition, process)
	result.Guidance = fmt.Sprintf("hum restart %s", definition.Name)
	return result
}

func manifestProcessSupportsDrift(process app.Process) bool {
	if process.State == app.StateRunning {
		return true
	}
	return process.State == app.StateExited && (process.NextLaunchAt != nil || effectiveProcessRestart(process) == app.RestartOnFailure && process.Relaunches >= manifestAutomaticRelaunchLimit)
}

func manifestRemovedProcessEligible(process app.Process) bool {
	return manifestProcessSupportsDrift(process)
}

func removedManifestResults(ctx context.Context, client *daemon.Client, manifest manifestState) ([]manifestLaunchResult, error) {
	processes, err := client.List(ctx, daemon.ListRequest{Op: protocol.OpList, Cwd: manifest.root, IncludeCompleted: true})
	if err != nil {
		return nil, err
	}
	results := make([]manifestLaunchResult, 0)
	for _, process := range processes {
		if process.Name == "" || !isManifestSource(process.Source) || !manifestRemovedProcessEligible(process) {
			continue
		}
		if _, ok := manifest.byName[process.Name]; ok {
			continue
		}
		if process.Root != "" && canonicalManifestCwd(manifest.root, process.Root) != canonicalManifestCwd(manifest.root, manifest.root) {
			continue
		}
		result := manifestLaunchResultFor(undefinedManifestDefinition(process.Name), process, "removed_definition")
		result.Guidance = fmt.Sprintf("hum stop %s or hum remove %s", process.Name, process.Name)
		results = append(results, result)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Name < results[j].Name })
	return results, nil
}

func manifestTTYUpgradeError(definition project.Definition, process app.Process) error {
	if definition.TTY && !process.TTY {
		return fmt.Errorf("declared process %q is running without a tty; stop it and rerun with tty: true", definition.Name)
	}
	return nil
}

func manifestUnavailableMessage(definition project.Definition) error {
	return newUserFacingError(fmt.Sprintf("Nothing is running in this project. Start it with hum start %s.", definition.Name))
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

func manifestSameIncarnation(observed, current app.Process) bool {
	return observed.PID == current.PID && observed.LaunchCursor == current.LaunchCursor
}

func manifestReadinessBeforeDeadline(readiness *app.Readiness, deadline time.Time) bool {
	if readiness == nil {
		return true
	}
	if !readiness.Time.IsZero() && readiness.Time.After(deadline) {
		return false
	}
	return !time.Now().After(deadline)
}

func manifestReadinessResult(client *daemon.Client, ctx context.Context, cwd string, definition project.Definition, process app.Process, initialOutcome string, timeout time.Duration) (manifestLaunchResult, error) {
	result := manifestLaunchResultFor(definition, process, initialOutcome)
	deadline := time.Now().Add(timeout)
	observed := process
	markExited := func() (manifestLaunchResult, error) {
		result.Outcome = "exited_before_ready"
		result.Readiness = ""
		return result, nil
	}
	markTimedOut := func() (manifestLaunchResult, error) {
		result.Outcome = "timed_out"
		return result, nil
	}
	if process.State != app.StateRunning {
		return markExited()
	}
	if process.Readiness == nil {
		return result, nil
	}
	switch process.Readiness.State {
	case app.ReadinessReady:
		if !manifestReadinessBeforeDeadline(process.Readiness, deadline) {
			return markTimedOut()
		}
		return result, nil
	case app.ReadinessRunningUnverified:
		return result, nil
	case app.ReadinessStarting:
		// Continue below using the expression recorded on this incarnation.
	default:
		return result, nil
	}
	recordedMatch := process.Readiness.Match

	// Start returns a snapshot taken before the child can necessarily emit its
	// first line. Refresh it before subscribing so a readiness match that was
	// recorded by the daemon in that interval is not lost to output eviction.
	// If the child already exited, Wait must order its retained output and exit.
	lookupCwd := process.Root
	if lookupCwd == "" {
		lookupCwd = cwd
	}
	if current, getErr := client.Get(ctx, daemon.GetRequest{Name: definition.Name, Cwd: lookupCwd}); getErr == nil {
		if !manifestSameIncarnation(observed, current) {
			return markExited()
		}
		if current.State == app.StateRunning {
			process = current
			lookupCwd = process.Root
			if lookupCwd == "" {
				lookupCwd = cwd
			}
			result = manifestLaunchResultFor(definition, process, initialOutcome)
		}
	} else if !isNotFound(getErr) {
		return result, getErr
	}
	if process.State != app.StateRunning {
		return markExited()
	}
	if process.Readiness == nil {
		return result, nil
	}
	switch process.Readiness.State {
	case app.ReadinessReady:
		if !manifestReadinessBeforeDeadline(process.Readiness, deadline) {
			return markTimedOut()
		}
		return result, nil
	case app.ReadinessRunningUnverified:
		return result, nil
	case app.ReadinessStarting:
		if process.Readiness.Match != recordedMatch {
			return result, nil
		}
	default:
		return result, nil
	}

	remaining := time.Until(deadline)
	if remaining < time.Millisecond {
		return markTimedOut()
	}
	milliseconds, err := manifestTimeoutMS(remaining)
	if err != nil {
		return result, err
	}
	waitResult, err := client.Wait(ctx, daemon.WaitRequest{
		Name:      definition.Name,
		Cwd:       lookupCwd,
		Match:     recordedMatch,
		TimeoutMS: milliseconds,
	})
	if err != nil {
		return result, err
	}

	checkCurrent := func() (manifestLaunchResult, bool, bool, error) {
		current, getErr := client.Get(ctx, daemon.GetRequest{Name: definition.Name, Cwd: lookupCwd})
		if getErr != nil {
			return result, false, false, getErr
		}
		if !manifestSameIncarnation(observed, current) {
			terminal, _ := markExited()
			return terminal, true, false, nil
		}
		if current.State != app.StateRunning {
			terminal := manifestLaunchResultFor(definition, current, "exited_before_ready")
			terminal.Readiness = ""
			return terminal, true, true, nil
		}
		if current.Readiness == nil {
			return manifestLaunchResultFor(definition, current, initialOutcome), true, true, nil
		}
		switch current.Readiness.State {
		case app.ReadinessReady:
			if !manifestReadinessBeforeDeadline(current.Readiness, deadline) {
				timedOut, _ := markTimedOut()
				return timedOut, true, false, nil
			}
			return manifestLaunchResultFor(definition, current, initialOutcome), true, true, nil
		case app.ReadinessRunningUnverified:
			return manifestLaunchResultFor(definition, current, initialOutcome), true, true, nil
		case app.ReadinessStarting:
			if current.Readiness.Match != recordedMatch {
				return manifestLaunchResultFor(definition, current, initialOutcome), true, true, nil
			}
		}
		return result, false, false, nil
	}

	switch waitResult.Outcome {
	case app.WaitMatched:
		// The readiness monitor and this client subscribe independently. Give
		// the monitor a bounded opportunity to publish its durable state, but
		// never restart the original launch's deadline after Wait returns.
		for {
			current, done, matchValid, getErr := checkCurrent()
			if getErr != nil {
				return result, getErr
			}
			if done {
				if current.Outcome == "exited_before_ready" && matchValid {
					// Wait observed the readiness match before the process exited,
					// even if reconciliation hid terminal readiness before Get.
					result.Readiness = app.ReadinessReady
					cursor := uint64(waitResult.Cursor)
					result.ReadyCursor = &cursor
					return result, nil
				}
				return current, nil
			}
			remaining = time.Until(deadline)
			if remaining < time.Millisecond {
				return markTimedOut()
			}
			timerDuration := time.Millisecond
			if remaining < timerDuration {
				timerDuration = remaining
			}
			timer := time.NewTimer(timerDuration)
			select {
			case <-ctx.Done():
				timer.Stop()
				return result, ctx.Err()
			case <-timer.C:
			}
		}
	case app.WaitExited:
		return markExited()
	case app.WaitTimedOut:
		// A match can be consumed and evicted immediately before Wait's
		// subscription observes it. Re-check the daemon's incarnation-local
		// readiness state before reporting a timeout.
		current, done, _, getErr := checkCurrent()
		if getErr != nil {
			return result, getErr
		}
		if done {
			return current, nil
		}
		return markTimedOut()
	default:
		return result, fmt.Errorf("unknown readiness wait outcome %q", waitResult.Outcome)
	}
}

const manifestAutomaticRelaunchLimit = 5

func manifestRecoveryOutcome(definition project.Definition, process app.Process) (string, bool) {
	if process.State != app.StateExited || !definitionMatchesProcess(definition, process) {
		return "", false
	}
	if process.NextLaunchAt != nil {
		return "recovery_pending", true
	}
	if effectiveProcessRestart(process) == app.RestartOnFailure && process.Relaunches >= manifestAutomaticRelaunchLimit {
		return "recovery_exhausted", true
	}
	return "", false
}

func ensureManifestStart(ctx context.Context, client *daemon.Client, cwd, root string, definition project.Definition, env []string, preserveRecovery bool) (manifestLaunchResult, app.Process, bool, error) {
	lookupCwd := root
	if lookupCwd == "" {
		lookupCwd = cwd
	}
	current, err := client.Get(ctx, daemon.GetRequest{Name: definition.Name, Cwd: lookupCwd})
	if err == nil {
		if current.State == app.StateRunning {
			if !definitionMatchesProcess(definition, current) {
				return manifestLaunchError(definition, fmt.Errorf("declared process %q is occupied by an ad-hoc launch", definition.Name)), current, false, nil
			}
			if changed := manifestChangedFields(root, definition, current); len(changed) != 0 {
				return manifestDefinitionDriftResult(root, definition, current), current, true, nil
			}
			if ttyErr := manifestTTYUpgradeError(definition, current); ttyErr != nil {
				return manifestLaunchError(definition, ttyErr), current, false, nil
			}
			return manifestLaunchResultFor(definition, current, "already_running"), current, true, nil
		}
		if definitionMatchesProcess(definition, current) && manifestProcessSupportsDrift(current) {
			if changed := manifestChangedFields(root, definition, current); len(changed) != 0 {
				return manifestDefinitionDriftResult(root, definition, current), current, true, nil
			}
		}
		if preserveRecovery {
			if outcome, ok := manifestRecoveryOutcome(definition, current); ok {
				return manifestLaunchResultFor(definition, current, outcome), current, true, nil
			}
		}
	} else if !isNotFound(err) {
		return manifestLaunchError(definition, err), app.Process{}, false, nil
	}

	process, startErr := client.Start(ctx, daemon.StartRequest{
		Name:    definition.Name,
		Source:  definition.Source,
		Root:    root,
		Cwd:     definition.Cwd,
		Argv:    append([]string(nil), definition.Argv...),
		Env:     append([]string(nil), env...),
		Ready:   readinessConfig(definition),
		TTY:     definition.TTY,
		Restart: protocolRestartPolicy(definition),
	})
	if startErr == nil {
		outcome := "started"
		if definition.Ready == nil {
			outcome = "running_unverified"
		}
		return manifestLaunchResultFor(definition, process, outcome), process, false, nil
	}
	if !isNameInUse(startErr) && !errors.Is(startErr, app.ErrNameInUse) {
		return manifestLaunchError(definition, startErr), app.Process{}, false, nil
	}

	// A concurrent ensure may have won while this call was between Get and
	// Start. Poll briefly for its published record so both callers converge on
	// one already_running result instead of reporting a transient duplicate.
	deadline := time.Now().Add(time.Second)
	for {
		current, getErr := client.Get(ctx, daemon.GetRequest{Name: definition.Name, Cwd: lookupCwd})
		if getErr == nil && current.State == app.StateRunning {
			if !definitionMatchesProcess(definition, current) {
				collision := manifestLaunchError(definition, fmt.Errorf("declared process %q is occupied by an ad-hoc launch", definition.Name))
				return collision, current, false, nil
			}
			if changed := manifestChangedFields(root, definition, current); len(changed) != 0 {
				return manifestDefinitionDriftResult(root, definition, current), current, true, nil
			}
			if ttyErr := manifestTTYUpgradeError(definition, current); ttyErr != nil {
				return manifestLaunchError(definition, ttyErr), current, false, nil
			}
			return manifestLaunchResultFor(definition, current, "already_running"), current, true, nil
		}
		if time.Now().After(deadline) {
			break
		}
		timer := time.NewTimer(5 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return manifestLaunchError(definition, ctx.Err()), app.Process{}, false, nil
		case <-timer.C:
		}
	}
	return manifestLaunchError(definition, startErr), app.Process{}, false, nil
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
