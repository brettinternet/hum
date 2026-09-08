// Package orchestrate owns the manifest up scheduler and the lifecycle
// classifications shared by the CLI and MCP adapters.
package orchestrate

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// AutomaticRelaunchLimit is the daemon's bounded on-failure recovery limit.
	AutomaticRelaunchLimit = 5
	// DefaultReadinessTimeout is used when neither a command override nor a
	// definition timeout is present.
	DefaultReadinessTimeout = 30 * time.Second
)

const (
	ReadinessStarting          = "starting"
	ReadinessReady             = "ready"
	ReadinessRunningUnverified = "running_unverified"
)

const (
	WaitMatched  = "matched"
	WaitExited   = "exited"
	WaitTimedOut = "timed_out"
)

// IsActiveState reports whether a process group still owns its lifecycle slot.
func IsActiveState(state string) bool {
	return state == "running" || state == "descendants"
}

// Definition is the protocol-independent form of one resolved manifest
// definition. Adapters translate their project-specific definition types into
// this small model before invoking orchestration.
type Definition struct {
	Name    string
	Source  string
	Argv    []string
	Cwd     string
	Ready   *ReadinessConfig
	After   []string
	TTY     bool
	Restart string
}

// ReadinessConfig describes the output expression used by a definition.
type ReadinessConfig struct {
	Match   string
	Timeout time.Duration
}

// Readiness is the response-safe readiness state carried by a process
// snapshot.
type Readiness struct {
	State  string
	Cursor *uint64
	Time   time.Time
	Match  string
}

// SignalInfo identifies the canonical signal that terminated a process.
type SignalInfo struct {
	Name   string
	Number int
}

// Exit is the response-safe terminal process status used by the common
// snapshot model.
type Exit struct {
	Code   int
	Time   time.Time
	Error  string
	Signal *SignalInfo
}

// Process is the common process snapshot exchanged by adapters and the
// orchestrator. It intentionally contains no daemon or output-store handles.
type Process struct {
	Name         string
	Source       string
	Root         string
	TTY          bool
	PID          int
	PGID         int
	Cwd          string
	Argv         []string
	Start        time.Time
	LaunchCursor uint64
	NextCursor   *uint64
	State        string
	Exit         *Exit
	ExitCode     int
	ExitedAt     time.Time
	RestartCount int
	Followers    int
	Restart      string
	Relaunches   int
	NextLaunchAt *time.Time
	Readiness    *Readiness
}

// WaitRequest is the adapter-neutral readiness wait request.
type WaitRequest struct {
	Name    string
	Cwd     string
	Match   string
	Timeout time.Duration
}

// WaitResult is the adapter-neutral result returned by a readiness wait.
type WaitResult struct {
	Outcome string
	Cursor  uint64
	Exit    *Exit
}

// Result is the common launch result. Adapters render it into their stable
// response shape and map Error to their client-specific error representation.
type Result struct {
	Name          string
	Outcome       string
	Process       *Process
	Error         error
	BlockedBy     []string
	ExistingState string
	ChangedFields []string
	Guidance      string
}

// StartResult carries the initial result and process used by the up scheduler.
type StartResult struct {
	Result     Result
	Process    Process
	ObservedAt time.Time
	Already    bool
}

// EnsureOperations are the daemon seams needed to classify and launch one
// definition. IsNotFound and IsNameInUse retain the adapter's existing wire
// error handling without importing either client implementation here.
type EnsureOperations struct {
	Get         func(context.Context, string, string) (Process, error)
	Start       func(context.Context, StartRequest) (Process, error)
	IsNotFound  func(error) bool
	IsNameInUse func(error) bool
}

// StartRequest is the adapter-neutral direct launch request.
type StartRequest struct {
	Name    string
	Source  string
	Root    string
	Cwd     string
	Argv    []string
	Env     []string
	Ready   *ReadinessConfig
	TTY     bool
	Restart string
}

// UpOperations are the seams for one adapter's daemon and rendering model.
// Start is normally backed by Ensure. Readiness is normally backed by
// WaitForReadiness. The scheduler itself owns dependency ordering, gate
// evaluation, skipped classification coordination, and deterministic output.
type UpOperations struct {
	Start      func(context.Context, Definition) (StartResult, error)
	Readiness  func(context.Context, Definition, Process, string, time.Duration) (Result, error)
	Skipped    func(context.Context, Definition, []string) Result
	Get        func(context.Context, string, string) (Process, error)
	List       func(context.Context) ([]Process, error)
	OnProgress func(ProgressEvent)
}

// UpOptions controls one invocation of Up.
type UpOptions struct {
	Root        string
	Definitions []Definition
	Names       []string
	NoWait      bool
	TimeoutFor  func(Definition) (time.Duration, error)
}

// ProgressEvent is emitted once when a node settles its initial launch/skip
// classification and once more when a readiness wait settles.
type ProgressEvent struct {
	Definition Definition
	Result     Result
	Terminal   bool
}

// ErrorKind identifies errors that need a stable adapter-specific code.
type ErrorKind string

const (
	ErrorNameInUse   ErrorKind = "name_in_use"
	ErrorInputNotTTY ErrorKind = "input_not_tty"
)

// Error is a stable classification error. Its text intentionally matches the
// pre-shared CLI and MCP messages; adapters may map Kind to a wire code.
type Error struct {
	Kind ErrorKind
	Name string
}

func (e *Error) Error() string {
	if e == nil {
		return "orchestration error"
	}
	switch e.Kind {
	case ErrorNameInUse:
		return fmt.Sprintf("declared process %q is occupied by an ad-hoc launch", e.Name)
	case ErrorInputNotTTY:
		return fmt.Sprintf("declared process %q is running without a tty; stop it and rerun with tty: true", e.Name)
	default:
		return string(e.Kind)
	}
}

func (e *Error) KindValue() ErrorKind {
	if e == nil {
		return ""
	}
	return e.Kind
}

// ErrorKindOf returns the stable classification kind carried by err.
func ErrorKindOf(err error) ErrorKind {
	var classified *Error
	if errors.As(err, &classified) && classified != nil {
		return classified.Kind
	}
	return ""
}

// EffectiveRestart normalizes the empty policy to the daemon's never policy.
func EffectiveRestart(policy string) string {
	if policy == "" {
		return "never"
	}
	return policy
}

// IsManifestSource identifies manifest and conventional manifest source names.
func IsManifestSource(source string) bool {
	return source == "manifest" || source == "hum.yaml" || strings.HasPrefix(source, "manifest:") || strings.HasPrefix(source, "hum.yaml:")
}

// SameManifestSource treats all manifest source spellings as equivalent.
func SameManifestSource(left, right string) bool {
	return left == right || IsManifestSource(left) && IsManifestSource(right)
}

// CanonicalCwd resolves a definition or process cwd relative to root and
// follows existing symlinks in the same way both adapters historically did.
func CanonicalCwd(root, cwd string) string {
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

// NormalizeProcess copies mutable fields and applies the response defaults
// shared by CLI and MCP process snapshots.
func NormalizeProcess(process Process) Process {
	process.Argv = append([]string(nil), process.Argv...)
	if process.Source == "" {
		process.Source = "ad_hoc"
	}
	if !IsManifestSource(process.Source) {
		process.Restart = EffectiveRestart("never")
	} else {
		process.Restart = EffectiveRestart(process.Restart)
	}
	if process.NextCursor != nil {
		cursor := *process.NextCursor
		process.NextCursor = &cursor
	}
	if process.Exit != nil {
		exit := *process.Exit
		if exit.Signal != nil {
			signal := *exit.Signal
			exit.Signal = &signal
		}
		process.Exit = &exit
	}
	if process.NextLaunchAt != nil {
		next := *process.NextLaunchAt
		process.NextLaunchAt = &next
	}
	if process.Readiness != nil {
		readiness := *process.Readiness
		if readiness.Cursor != nil {
			cursor := *readiness.Cursor
			readiness.Cursor = &cursor
		}
		process.Readiness = &readiness
	}
	return process
}

func copyDefinition(definition Definition) Definition {
	definition.Argv = append([]string(nil), definition.Argv...)
	definition.After = append([]string(nil), definition.After...)
	if definition.Ready != nil {
		ready := *definition.Ready
		definition.Ready = &ready
	}
	return definition
}

func resultForDefinition(definition Definition, outcome string) Result {
	return Result{Name: definition.Name, Outcome: outcome}
}

// ResultForProcess creates the common result for a process outcome.
func ResultForProcess(definition Definition, process Process, outcome string) Result {
	process = NormalizeProcess(process)
	if process.State == "running" && process.Readiness == nil && definition.Ready == nil {
		process.Readiness = &Readiness{State: ReadinessRunningUnverified}
	}
	return Result{Name: definition.Name, Outcome: outcome, Process: &process}
}

// ErrorResult creates a stable error outcome for a definition.
func ErrorResult(definition Definition, err error) Result {
	return Result{Name: definition.Name, Outcome: "error", Error: err}
}

// DefinitionMatchesProcess reports whether a runtime record belongs to the
// same manifest source family as a definition.
func DefinitionMatchesProcess(definition Definition, process Process) bool {
	return SameManifestSource(process.Source, definition.Source)
}

func processReadinessMatch(process Process) (bool, string) {
	if process.Readiness == nil || process.Readiness.State == ReadinessRunningUnverified {
		return false, ""
	}
	return true, process.Readiness.Match
}

// DefinitionChangedFields returns sorted identity fields that differ between
// a current definition and a retained/running process snapshot.
func DefinitionChangedFields(root string, definition Definition, process Process) []string {
	changed := make([]string, 0, 5)
	if !slices.Equal(definition.Argv, process.Argv) {
		changed = append(changed, "argv")
	}
	if CanonicalCwd(root, definition.Cwd) != CanonicalCwd(root, process.Cwd) {
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
	if EffectiveRestart(definition.Restart) != EffectiveRestart(process.Restart) {
		changed = append(changed, "restart")
	}
	sort.Strings(changed)
	return changed
}

// ProcessSupportsDrift identifies running and recovery-capable records whose
// retained launch identity must not be silently replaced.
func ProcessSupportsDrift(process Process) bool {
	if IsActiveState(process.State) {
		return true
	}
	return process.State == "exited" && (process.NextLaunchAt != nil || EffectiveRestart(process.Restart) == "on-failure" && process.Relaunches >= AutomaticRelaunchLimit)
}

// DefinitionDriftResult classifies an unchanged runtime record whose launch
// identity no longer matches its current definition.
func DefinitionDriftResult(root string, definition Definition, process Process) Result {
	process = NormalizeProcess(process)
	return Result{
		Name: definition.Name, Outcome: "definition_drift", Process: &process,
		ChangedFields: DefinitionChangedFields(root, definition, process),
		Guidance:      fmt.Sprintf("hum restart %s", definition.Name),
	}
}

// RecoveryOutcome classifies a retained exited manifest process without
// sending a launch request or waiting for an automatic successor.
func RecoveryOutcome(definition Definition, process Process) (string, bool) {
	if process.State != "exited" || !DefinitionMatchesProcess(definition, process) {
		return "", false
	}
	if process.NextLaunchAt != nil {
		return "recovery_pending", true
	}
	if EffectiveRestart(process.Restart) == "on-failure" && process.Relaunches >= AutomaticRelaunchLimit {
		return "recovery_exhausted", true
	}
	return "", false
}

// RemovedDefinitionResults classifies manifest-sourced running and
// recovery-capable records absent from the current declarations.
func RemovedDefinitionResults(root string, definitions []Definition, processes []Process) []Result {
	declared := make(map[string]struct{}, len(definitions))
	for _, definition := range definitions {
		declared[definition.Name] = struct{}{}
	}
	results := make([]Result, 0)
	for _, process := range processes {
		if process.Name == "" || !IsManifestSource(process.Source) || !ProcessSupportsDrift(process) {
			continue
		}
		if _, ok := declared[process.Name]; ok {
			continue
		}
		if process.Root != "" && CanonicalCwd(root, process.Root) != CanonicalCwd(root, root) {
			continue
		}
		process = NormalizeProcess(process)
		results = append(results, Result{
			Name: process.Name, Outcome: "removed_definition", Process: &process,
			Guidance: fmt.Sprintf("hum stop %s or hum remove %s", process.Name, process.Name),
		})
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Name < results[j].Name })
	return results
}

// SkippedResult creates a skipped result and optionally carries the existing
// process snapshot without mutating its lifecycle.
func SkippedResult(ctx context.Context, root string, definition Definition, blockedBy []string, get func(context.Context, string, string) (Process, error)) Result {
	result := resultForDefinition(definition, "skipped")
	result.BlockedBy = append([]string(nil), blockedBy...)
	if get != nil {
		if current, err := get(ctx, definition.Name, root); err == nil {
			// A pre-launch log follower reserves an empty durable session. It is
			// observable state, but it is not an existing process and must not
			// change a dependency-blocked result from "not launched".
			if current.Source == "" && len(current.Argv) == 0 && current.PID == 0 && current.Start.IsZero() && current.LaunchCursor == 0 {
				return result
			}
			current = NormalizeProcess(current)
			result.Process = &current
			switch current.State {
			case "running", "stopped", "exited":
				result.ExistingState = current.State
			}
		}
	}
	return result
}

// ReadinessTimeout resolves a command override, a definition timeout, or the
// common default.
func ReadinessTimeout(override time.Duration, definition Definition) (time.Duration, error) {
	if override != 0 {
		if override <= 0 {
			return 0, errors.New("timeout must be positive")
		}
		return override, nil
	}
	if definition.Ready != nil && definition.Ready.Timeout > 0 {
		return definition.Ready.Timeout, nil
	}
	return DefaultReadinessTimeout, nil
}

func sameProcessIncarnation(observed, current Process) bool {
	return observed.PID == current.PID && observed.LaunchCursor == current.LaunchCursor
}

func readinessBeforeDeadline(readiness *Readiness, deadline time.Time) bool {
	if readiness == nil {
		return true
	}
	if !readiness.Time.IsZero() && readiness.Time.After(deadline) {
		return false
	}
	return !time.Now().After(deadline)
}

func durationTimeout(duration time.Duration) time.Duration {
	if duration <= 0 {
		return 0
	}
	return duration
}

// ReadinessOperations are the daemon seams needed by WaitForReadiness.
type ReadinessOperations struct {
	Get        func(context.Context, string, string) (Process, error)
	Wait       func(context.Context, WaitRequest) (WaitResult, error)
	IsNotFound func(error) bool
}

// WaitForReadiness waits on one process incarnation. Bounded subscription
// polls close the refresh-to-subscribe race without changing nil-after cursor
// semantics for first-launch output.
func WaitForReadiness(ctx context.Context, root string, definition Definition, process Process, initialOutcome string, timeout time.Duration, ops ReadinessOperations) (Result, error) {
	initialProcess := NormalizeProcess(process)
	result := ResultForProcess(definition, initialProcess, initialOutcome)
	deadline := time.Now().Add(durationTimeout(timeout))
	observed := initialProcess

	lookupRoot := process.Root
	if lookupRoot == "" {
		lookupRoot = root
	}
	getCurrent := func() (Process, error) {
		if ops.Get == nil {
			return Process{}, errors.New("readiness get operation is not configured")
		}
		return ops.Get(ctx, process.Name, lookupRoot)
	}
	refresh := func(outcome string) (Result, error) {
		current, err := getCurrent()
		if err != nil {
			if ops.IsNotFound != nil && ops.IsNotFound(err) {
				result.Outcome = outcome
				return result, nil
			}
			return result, err
		}
		return ResultForProcess(definition, current, outcome), nil
	}
	markExited := func() (Result, error) { return refresh("exited_before_ready") }
	markTimedOut := func() (Result, error) { return refresh("timed_out") }

	if process.State != "running" {
		return markExited()
	}
	if process.Readiness == nil {
		return result, nil
	}
	switch process.Readiness.State {
	case ReadinessReady:
		if !readinessBeforeDeadline(process.Readiness, deadline) {
			return markTimedOut()
		}
		return result, nil
	case ReadinessRunningUnverified:
		return result, nil
	case ReadinessStarting:
		// Continue below using the expression recorded on this incarnation.
	default:
		return result, nil
	}
	recordedMatch := process.Readiness.Match

	// Refresh the snapshot before subscribing so a match recorded between the
	// launch response and this call cannot be lost.
	if current, err := getCurrent(); err == nil {
		if !sameProcessIncarnation(observed, current) {
			return ResultForProcess(definition, current, "exited_before_ready"), nil
		}
		if current.State == "running" {
			process = current
			lookupRoot = process.Root
			if lookupRoot == "" {
				lookupRoot = root
			}
			result = ResultForProcess(definition, process, initialOutcome)
		}
	} else if ops.IsNotFound == nil || !ops.IsNotFound(err) {
		return result, err
	}
	if process.State != "running" {
		return markExited()
	}
	if process.Readiness == nil {
		return result, nil
	}
	switch process.Readiness.State {
	case ReadinessReady:
		if !readinessBeforeDeadline(process.Readiness, deadline) {
			return markTimedOut()
		}
		return result, nil
	case ReadinessRunningUnverified:
		return result, nil
	case ReadinessStarting:
		if process.Readiness.Match != recordedMatch {
			return result, nil
		}
	default:
		return result, nil
	}

	if ops.Wait == nil {
		return result, errors.New("readiness wait operation is not configured")
	}
	checkCurrent := func() (Result, bool, bool, error) {
		current, getErr := getCurrent()
		if getErr != nil {
			return result, false, false, getErr
		}
		if !sameProcessIncarnation(observed, current) {
			return ResultForProcess(definition, current, "exited_before_ready"), true, false, nil
		}
		if current.State != "running" {
			return ResultForProcess(definition, current, "exited_before_ready"), true, true, nil
		}
		if current.Readiness == nil {
			return ResultForProcess(definition, current, initialOutcome), true, true, nil
		}
		switch current.Readiness.State {
		case ReadinessReady:
			if !readinessBeforeDeadline(current.Readiness, deadline) {
				timedOut, _ := markTimedOut()
				return timedOut, true, false, nil
			}
			return ResultForProcess(definition, current, initialOutcome), true, true, nil
		case ReadinessRunningUnverified:
			return ResultForProcess(definition, current, initialOutcome), true, true, nil
		case ReadinessStarting:
			if current.Readiness.Match != recordedMatch {
				return ResultForProcess(definition, current, initialOutcome), true, true, nil
			}
		}
		return result, false, false, nil
	}

	for {
		remaining := time.Until(deadline)
		if remaining < time.Millisecond {
			return markTimedOut()
		}
		waitTimeout := min(remaining, 100*time.Millisecond)
		waitStarted := time.Now()
		waitResult, err := ops.Wait(ctx, WaitRequest{Name: process.Name, Cwd: lookupRoot, Match: recordedMatch, Timeout: waitTimeout})
		if err != nil {
			return result, err
		}

		switch waitResult.Outcome {
		case WaitExited:
			return markExited()
		case WaitMatched:
			// The readiness monitor and this client subscribe independently. Give
			// the monitor a bounded opportunity to publish durable state, but never
			// restart the original launch deadline after Wait returns.
			for {
				current, done, matchValid, getErr := checkCurrent()
				if getErr != nil {
					return result, getErr
				}
				if done {
					if current.Outcome == "exited_before_ready" && matchValid {
						// Preserve the observed launch snapshot while recording the
						// match that Wait saw before the process exited. This is the
						// launch-gate result expected by both adapters.
						matched := copyProcess(observed)
						readiness := &Readiness{State: ReadinessReady, Cursor: uint64Pointer(waitResult.Cursor), Match: recordedMatch}
						matched.Readiness = readiness
						return ResultForProcess(definition, matched, initialOutcome), nil
					}
					return current, nil
				}
				if time.Until(deadline) < time.Millisecond {
					return markTimedOut()
				}
				timer := time.NewTimer(time.Millisecond)
				select {
				case <-ctx.Done():
					timer.Stop()
					return result, ctx.Err()
				case <-timer.C:
				}
			}
		case WaitTimedOut:
			current, done, _, getErr := checkCurrent()
			if getErr != nil {
				return result, getErr
			}
			if done {
				return current, nil
			}
			if time.Until(deadline) < time.Millisecond || time.Since(waitStarted)+time.Millisecond < waitTimeout {
				return markTimedOut()
			}
		default:
			return result, fmt.Errorf("unknown readiness wait outcome %q", waitResult.Outcome)
		}
	}
}

func copyProcess(process Process) Process {
	return NormalizeProcess(process)
}

func uint64Pointer(value uint64) *uint64 {
	return &value
}

// LaunchOutcome returns the stable initial outcome for a successful ensure.
func LaunchOutcome(already bool, definition Definition) string {
	if already {
		return "already_running"
	}
	if definition.Ready == nil {
		return ReadinessRunningUnverified
	}
	return "started"
}

// ProgressWaitsForReadiness identifies results that receive a terminal
// progress transition in human CLI mode.
func ProgressWaitsForReadiness(definition Definition, result Result) bool {
	if definition.Ready == nil || (result.Outcome != "started" && result.Outcome != "already_running") {
		return false
	}
	if result.Process == nil {
		return false
	}
	if result.Process.State != "running" {
		return false
	}
	return result.Process.Readiness != nil && result.Process.Readiness.State == ReadinessStarting || result.Outcome == "started" && result.Process.Readiness == nil
}

// ResultSatisfiesGate reports whether a prerequisite may release its direct
// dependents.
func ResultSatisfiesGate(result Result) bool {
	return (result.Outcome == "started" || result.Outcome == "already_running") && result.Process != nil && result.Process.Readiness != nil && result.Process.Readiness.State == ReadinessReady
}

// DefinitionsHaveAfter reports whether any definition declares a dependency.
func DefinitionsHaveAfter(definitions []Definition) bool {
	for _, definition := range definitions {
		if len(definition.After) != 0 {
			return true
		}
	}
	return false
}

// Ensure performs the shared read/classify/start operation for one definition.
func Ensure(ctx context.Context, root string, definition Definition, env []string, preserveRecovery bool, ops EnsureOperations) StartResult {
	definition = copyDefinition(definition)
	lookupRoot := root
	makeError := func(err error) StartResult {
		return StartResult{Result: ErrorResult(definition, err)}
	}
	classifyRunning := func(current Process) StartResult {
		if !DefinitionMatchesProcess(definition, current) {
			return StartResult{Result: ErrorResult(definition, &Error{Kind: ErrorNameInUse, Name: definition.Name}), Process: current}
		}
		if changed := DefinitionChangedFields(root, definition, current); len(changed) != 0 {
			result := DefinitionDriftResult(root, definition, current)
			return StartResult{Result: result, Process: current, Already: true, ObservedAt: time.Now()}
		}
		if definition.TTY && !current.TTY {
			return StartResult{Result: ErrorResult(definition, &Error{Kind: ErrorInputNotTTY, Name: definition.Name}), Process: current}
		}
		result := ResultForProcess(definition, current, "already_running")
		return StartResult{Result: result, Process: current, Already: true, ObservedAt: time.Now()}
	}

	if ops.Get == nil {
		return makeError(errors.New("orchestration get operation is not configured"))
	}
	current, err := ops.Get(ctx, definition.Name, lookupRoot)
	if err == nil {
		if IsActiveState(current.State) {
			return classifyRunning(current)
		}
		if DefinitionMatchesProcess(definition, current) && ProcessSupportsDrift(current) {
			if changed := DefinitionChangedFields(root, definition, current); len(changed) != 0 {
				return StartResult{Result: DefinitionDriftResult(root, definition, current), Process: current, Already: true, ObservedAt: time.Now()}
			}
		}
		if preserveRecovery {
			if outcome, ok := RecoveryOutcome(definition, current); ok {
				return StartResult{Result: ResultForProcess(definition, current, outcome), Process: current, Already: true, ObservedAt: time.Now()}
			}
		}
	} else if ops.IsNotFound == nil || !ops.IsNotFound(err) {
		return makeError(err)
	}

	if ops.Start == nil {
		return makeError(errors.New("orchestration start operation is not configured"))
	}
	started, startErr := ops.Start(ctx, StartRequest{
		Name: definition.Name, Source: definition.Source, Root: root, Cwd: definition.Cwd,
		Argv: append([]string(nil), definition.Argv...), Env: append([]string(nil), env...),
		Ready: copyReadinessConfig(definition.Ready), TTY: definition.TTY, Restart: EffectiveRestart(definition.Restart),
	})
	if startErr == nil {
		observedAt := started.Start
		if observedAt.IsZero() {
			observedAt = time.Now()
		}
		return StartResult{Result: ResultForProcess(definition, started, LaunchOutcome(false, definition)), Process: started, ObservedAt: observedAt}
	}
	if ops.IsNameInUse == nil || !ops.IsNameInUse(startErr) {
		return makeError(startErr)
	}

	// A concurrent ensure may have won between Get and Start. Poll briefly so
	// callers converge on one already_running result rather than a duplicate.
	deadline := time.Now().Add(time.Second)
	for {
		current, getErr := ops.Get(ctx, definition.Name, lookupRoot)
		if getErr == nil && IsActiveState(current.State) {
			return classifyRunning(current)
		}
		if time.Now().After(deadline) {
			break
		}
		timer := time.NewTimer(5 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return makeError(ctx.Err())
		case <-timer.C:
		}
	}
	return makeError(startErr)
}

func copyReadinessConfig(config *ReadinessConfig) *ReadinessConfig {
	if config == nil {
		return nil
	}
	copy := *config
	return &copy
}

// OrchestrateUp executes the shared concurrent DAG scheduler and returns
// lexical results regardless of temporal launch completion order.
func OrchestrateUp(ctx context.Context, options UpOptions, ops UpOperations) ([]Result, error) {
	definitions := append([]Definition(nil), options.Definitions...)
	selected := make(map[string]struct{}, len(options.Names))
	if options.Names != nil {
		for _, name := range options.Names {
			selected[name] = struct{}{}
		}
	}
	filtered := make([]Definition, 0, len(definitions))
	for _, definition := range definitions {
		if options.Names != nil {
			if _, ok := selected[definition.Name]; !ok {
				continue
			}
		}
		filtered = append(filtered, copyDefinition(definition))
	}
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].Name < filtered[j].Name })
	if len(filtered) == 0 {
		if ops.List == nil {
			return []Result{}, nil
		}
		processes, err := ops.List(ctx)
		if err != nil {
			return nil, err
		}
		return RemovedDefinitionResults(options.Root, filtered, processes), nil
	}

	results := make([]Result, len(filtered))
	done := make([]bool, len(filtered))
	byName := make(map[string]int, len(filtered))
	for index, definition := range filtered {
		byName[definition.Name] = index
		results[index].Name = definition.Name
	}
	var mu sync.Mutex
	cond := sync.NewCond(&mu)
	wakeDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			mu.Lock()
			cond.Broadcast()
			mu.Unlock()
		case <-wakeDone:
		}
	}()
	defer close(wakeDone)

	var workers sync.WaitGroup
	for index, definition := range filtered {
		workers.Add(1)
		go func(index int, definition Definition) {
			defer workers.Done()
			mu.Lock()
			for {
				allDone := true
				for _, dependency := range definition.After {
					dependencyIndex, ok := byName[dependency]
					if !ok || !done[dependencyIndex] {
						allDone = false
						break
					}
				}
				if allDone || ctx.Err() != nil {
					break
				}
				cond.Wait()
			}
			if ctx.Err() != nil {
				result := ErrorResult(definition, ctx.Err())
				if ops.OnProgress != nil {
					ops.OnProgress(ProgressEvent{Definition: definition, Result: result})
				}
				results[index] = result
				done[index] = true
				cond.Broadcast()
				mu.Unlock()
				return
			}

			blocked := make([]string, 0, len(definition.After))
			for _, dependency := range definition.After {
				dependencyIndex := byName[dependency]
				if !ResultSatisfiesGate(results[dependencyIndex]) {
					blocked = append(blocked, dependency)
				}
			}
			if len(blocked) != 0 {
				sort.Strings(blocked)
				mu.Unlock()
				var skipped Result
				if ops.Skipped != nil {
					skipped = ops.Skipped(ctx, definition, blocked)
				} else {
					skipped = SkippedResult(ctx, "", definition, blocked, ops.Get)
				}
				if ops.OnProgress != nil {
					ops.OnProgress(ProgressEvent{Definition: definition, Result: skipped})
				}
				mu.Lock()
				results[index] = skipped
				done[index] = true
				cond.Broadcast()
				mu.Unlock()
				return
			}
			mu.Unlock()

			var started StartResult
			var startErr error
			if ops.Start == nil {
				startErr = errors.New("up start operation is not configured")
			} else {
				started, startErr = ops.Start(ctx, definition)
			}
			result := started.Result
			if result.Name == "" {
				result.Name = definition.Name
			}
			if startErr != nil {
				result = ErrorResult(definition, startErr)
			}
			if result.Process == nil && result.Error == nil && !isZeroProcess(started.Process) {
				process := NormalizeProcess(started.Process)
				result.Process = &process
			}
			if ops.OnProgress != nil {
				ops.OnProgress(ProgressEvent{Definition: definition, Result: result})
			}

			waitsForReadiness := ProgressWaitsForReadiness(definition, result)
			if startErr == nil && result.Error == nil && !options.NoWait && definition.Ready != nil && started.Process.State == "running" && (result.Outcome == "started" || result.Outcome == "already_running") {
				timeoutFor := options.TimeoutFor
				if timeoutFor == nil {
					timeoutFor = func(definition Definition) (time.Duration, error) { return ReadinessTimeout(0, definition) }
				}
				timeout, timeoutErr := timeoutFor(definition)
				if timeoutErr != nil {
					result = ErrorResult(definition, timeoutErr)
				} else {
					observedAt := started.ObservedAt
					if observedAt.IsZero() {
						observedAt = time.Now()
					}
					timeout -= time.Since(observedAt)
					if timeout < 0 {
						timeout = 0
					}
					if ops.Readiness == nil {
						result = ErrorResult(definition, errors.New("up readiness operation is not configured"))
					} else {
						var waitErr error
						result, waitErr = ops.Readiness(ctx, definition, started.Process, result.Outcome, timeout)
						if waitErr != nil {
							result = ErrorResult(definition, waitErr)
						}
					}
				}
			}
			if ops.OnProgress != nil && waitsForReadiness {
				ops.OnProgress(ProgressEvent{Definition: definition, Result: result, Terminal: true})
			}
			mu.Lock()
			results[index] = result
			done[index] = true
			cond.Broadcast()
			mu.Unlock()
		}(index, definition)
	}
	workers.Wait()

	if ops.List != nil {
		processes, err := ops.List(ctx)
		if err != nil {
			return nil, err
		}
		removed := RemovedDefinitionResults(options.Root, filtered, processes)
		results = append(results, removed...)
	}
	sort.SliceStable(results, func(i, j int) bool { return results[i].Name < results[j].Name })
	return results, nil
}

func isZeroProcess(process Process) bool {
	return process.Name == "" && process.Source == "" && process.Root == "" && process.Cwd == "" && process.PID == 0 && process.LaunchCursor == 0 && process.State == "" && len(process.Argv) == 0 && process.Readiness == nil
}
