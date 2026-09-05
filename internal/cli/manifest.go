package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
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

func manifestProcess(definition project.Definition, root string) app.Process {
	return app.Process{
		Name:   definition.Name,
		Source: definition.Source,
		Root:   root,
		TTY:    definition.TTY,
		Cwd:    definition.Cwd,
		Argv:   append([]string(nil), definition.Argv...),
		State:  app.State("stopped"),
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
	Name         string   `json:"name"`
	Outcome      string   `json:"outcome"`
	Source       string   `json:"source"`
	Argv         []string `json:"argv"`
	PID          *int     `json:"pid,omitempty"`
	LaunchCursor *uint64  `json:"launch_cursor,omitempty"`
	Readiness    string   `json:"readiness,omitempty"`
	ReadyCursor  *uint64  `json:"ready_cursor,omitempty"`
	Error        string   `json:"error,omitempty"`
}
type manifestWaitItem struct {
	index      int
	definition project.Definition
	process    app.Process
	initial    string
	timeout    time.Duration
}

func undefinedManifestDefinition(name string) project.Definition {
	return project.Definition{Name: name, Source: "manifest", Argv: []string{}}
}

func newManifestLaunchResult(definition project.Definition, outcome string) manifestLaunchResult {
	return manifestLaunchResult{
		Name:    definition.Name,
		Outcome: outcome,
		Source:  definition.Source,
		Argv:    append([]string(nil), definition.Argv...),
	}
}

func manifestLaunchResultFor(definition project.Definition, process app.Process, outcome string) manifestLaunchResult {
	result := manifestLaunchResult{
		Name:    process.Name,
		Outcome: outcome,
		Source:  process.Source,
		Argv:    append([]string(nil), process.Argv...),
	}
	if process.PID > 0 {
		pid := process.PID
		result.PID = &pid
	}
	cursor := uint64(process.LaunchCursor)
	result.LaunchCursor = &cursor
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
	return process.Source == definition.Source
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

func manifestReadinessResult(client *daemon.Client, ctx context.Context, cwd string, definition project.Definition, process app.Process, initialOutcome string, timeout time.Duration) (manifestLaunchResult, error) {
	result := manifestLaunchResultFor(definition, process, initialOutcome)
	if process.State != app.StateRunning {
		result.Outcome = "exited_before_ready"
		result.Readiness = ""
		return result, nil
	}
	if process.Readiness == nil {
		return result, nil
	}
	switch process.Readiness.State {
	case app.ReadinessReady, app.ReadinessRunningUnverified:
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
		result.Outcome = "exited_before_ready"
		result.Readiness = ""
		return result, nil
	}
	if process.Readiness == nil {
		return result, nil
	}
	switch process.Readiness.State {
	case app.ReadinessReady, app.ReadinessRunningUnverified:
		return result, nil
	case app.ReadinessStarting:
		if process.Readiness.Match != recordedMatch {
			return result, nil
		}
	default:
		return result, nil
	}

	milliseconds, err := manifestTimeoutMS(timeout)
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

	checkCurrent := func() (manifestLaunchResult, bool, error) {
		current, getErr := client.Get(ctx, daemon.GetRequest{Name: definition.Name, Cwd: lookupCwd})
		if getErr != nil {
			return result, false, getErr
		}
		if current.State != app.StateRunning {
			terminal := manifestLaunchResultFor(definition, current, "exited_before_ready")
			terminal.Readiness = ""
			return terminal, true, nil
		}
		if current.Readiness == nil {
			return manifestLaunchResultFor(definition, current, initialOutcome), true, nil
		}
		switch current.Readiness.State {
		case app.ReadinessReady:
			// A ready state belongs to the expression recorded for the
			// current incarnation. A different match means another
			// incarnation won the race; report that snapshot without
			// waiting on the stale expression.
			return manifestLaunchResultFor(definition, current, initialOutcome), true, nil
		case app.ReadinessRunningUnverified:
			return manifestLaunchResultFor(definition, current, initialOutcome), true, nil
		case app.ReadinessStarting:
			if current.Readiness.Match != recordedMatch {
				return manifestLaunchResultFor(definition, current, initialOutcome), true, nil
			}
		}
		return result, false, nil
	}

	switch waitResult.Outcome {
	case app.WaitMatched:
		// The readiness monitor and this client subscribe independently. Give
		// the monitor a bounded opportunity to publish its durable state, then
		// use that state (rather than a retained entry) for the result.
		deadline := time.Now().Add(timeout)
		for {
			current, done, getErr := checkCurrent()
			if getErr != nil {
				return result, getErr
			}
			if done {
				if current.Outcome == "exited_before_ready" {
					// Wait observed the readiness match before the process exited,
					// even if reconciliation hid terminal readiness before Get.
					result.Readiness = app.ReadinessReady
					cursor := uint64(waitResult.Cursor)
					result.ReadyCursor = &cursor
					return result, nil
				}
				return current, nil
			}
			if time.Now().After(deadline) {
				result.Outcome = "timed_out"
				return result, nil
			}
			timer := time.NewTimer(time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return result, ctx.Err()
			case <-timer.C:
			}
		}
	case app.WaitExited:
		result.Outcome = "exited_before_ready"
		result.Readiness = ""
		return result, nil
	case app.WaitTimedOut:
		// A match can be consumed and evicted immediately before Wait's
		// subscription observes it. Re-check the daemon's incarnation-local
		// readiness state before reporting a timeout.
		current, done, getErr := checkCurrent()
		if getErr != nil {
			return result, getErr
		}
		if done {
			return current, nil
		}
		result.Outcome = "timed_out"
		return result, nil
	default:
		return result, fmt.Errorf("unknown readiness wait outcome %q", waitResult.Outcome)
	}
}

func ensureManifestStart(ctx context.Context, client *daemon.Client, cwd, root string, definition project.Definition, env []string) (manifestLaunchResult, app.Process, bool, error) {
	lookupCwd := root
	if lookupCwd == "" {
		lookupCwd = cwd
	}
	current, err := client.Get(ctx, daemon.GetRequest{Name: definition.Name, Cwd: lookupCwd})
	if err == nil {
		if current.State == app.StateRunning {
			if ttyErr := manifestTTYUpgradeError(definition, current); ttyErr != nil {
				return manifestLaunchError(definition, ttyErr), current, false, nil
			}
			if !definitionMatchesProcess(definition, current) {
				return manifestLaunchError(definition, fmt.Errorf("declared process %q is occupied by an ad-hoc launch", definition.Name)), current, false, nil
			}
			return manifestLaunchResultFor(definition, current, "already_running"), current, true, nil
		}
	} else if !isNotFound(err) {
		return manifestLaunchError(definition, err), app.Process{}, false, nil
	}

	process, startErr := client.Start(ctx, daemon.StartRequest{
		Name:   definition.Name,
		Source: definition.Source,
		Root:   root,
		Cwd:    definition.Cwd,
		Argv:   append([]string(nil), definition.Argv...),
		Env:    append([]string(nil), env...),
		Ready:  readinessConfig(definition),
		TTY:    definition.TTY,
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
			if ttyErr := manifestTTYUpgradeError(definition, current); ttyErr != nil {
				return manifestLaunchError(definition, ttyErr), current, false, nil
			}
			if definitionMatchesProcess(definition, current) {
				return manifestLaunchResultFor(definition, current, "already_running"), current, true, nil
			}
			collision := manifestLaunchError(definition, fmt.Errorf("declared process %q is occupied by an ad-hoc launch", definition.Name))
			return collision, current, false, nil
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
		if result.Outcome == "error" {
			return urfavecli.Exit("", 1)
		}
	}
	for _, result := range results {
		if result.Outcome == "exited_before_ready" {
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
