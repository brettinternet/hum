package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"syscall"

	"hum/internal/config"
	"hum/internal/daemon"
	"hum/internal/output"
	"hum/internal/project"
	"hum/internal/protocol"

	urfavecli "github.com/urfave/cli/v3"
)

const (
	runUnavailableMessage      = "No hum daemon is running. Start it with hum serve --daemon."
	logsUnavailableMessage     = "Nothing is running. Start a process with hum run <name> -- <command>."
	stopUnavailableMessage     = "Nothing is running."
	shutdownUnavailableMessage = "No hum daemon is running."
)

type userFacingError string

func (e userFacingError) Error() string { return string(e) }

func newUserFacingError(message string) error { return userFacingError(message) }

type wrappedUserFacingError struct {
	cause   error
	message string
}

func (e wrappedUserFacingError) Error() string { return e.message }
func (e wrappedUserFacingError) Unwrap() error { return e.cause }

func wrapUserFacingError(cause error, message string) error {
	return wrappedUserFacingError{cause: cause, message: message}
}

// cliUsageError preserves the existing human-readable usage text while giving
// the JSON error boundary a stable classification. urfave/cli invokes
// OnUsageError before an action, so this marker also covers parse failures.
type cliUsageError struct{ cause error }

func (e cliUsageError) Error() string {
	if e.cause == nil {
		return ""
	}
	return e.cause.Error()
}

func (e cliUsageError) Unwrap() error { return e.cause }

func newCLIUsageError(cause error) error {
	if cause == nil {
		return nil
	}
	return cliUsageError{cause: cause}
}

// jsonHandledError carries the original error and exit code after the root
// boundary has emitted its JSON representation. Keeping the cause in the
// chain preserves errors.As/errors.Is for callers and tests.
type jsonHandledError struct{ cause error }

func (e *jsonHandledError) Error() string {
	if e == nil || e.cause == nil {
		return ""
	}
	return e.cause.Error()
}

func (e *jsonHandledError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func (e *jsonHandledError) ExitCode() int {
	if e == nil || e.cause == nil {
		return 1
	}
	var exitErr urfavecli.ExitCoder
	if errors.As(e.cause, &exitErr) {
		return exitErr.ExitCode()
	}
	return 1
}

func (*jsonHandledError) JSONErrorHandled() bool { return true }

// JSONErrorHandled reports whether err has already been rendered on stdout by
// the structured-error boundary. The main package uses this to suppress its
// legacy stderr diagnostic without importing the boundary implementation.
func JSONErrorHandled(err error) bool {
	if err == nil {
		return false
	}
	var handled interface{ JSONErrorHandled() bool }
	return errors.As(err, &handled) && handled.JSONErrorHandled()
}

const (
	jsonErrorUsage             protocol.ErrorCode = "usage"
	jsonErrorDaemonUnavailable protocol.ErrorCode = "daemon_unavailable"
	jsonErrorManifestInvalid   protocol.ErrorCode = "manifest_invalid"
	jsonErrorInternal          protocol.ErrorCode = "internal"
)

// classifyJSONError maps local CLI failures to the deliberately small public
// error vocabulary. A daemon-provided WireError is checked first so its
// protocol code is never replaced by a CLI classification.
func classifyJSONError(err error, usage bool) *protocol.WireError {
	if err == nil {
		return protocol.NewWireError(jsonErrorInternal, "command failed", nil)
	}
	var wire *daemon.WireError
	if errors.As(err, &wire) && wire != nil {
		copy := *wire
		if copy.Message == "" {
			copy.Message = err.Error()
		}
		return &copy
	}
	var active *daemon.ActiveProcessesError
	if errors.As(err, &active) {
		return protocol.NewWireError(protocol.ErrorActiveProcesses, err.Error(), nil)
	}
	if errors.Is(err, project.ErrConfiguration) || errors.Is(err, project.ErrAmbiguous) || errors.Is(err, project.ErrIntrospection) {
		return protocol.NewWireError(jsonErrorManifestInvalid, err.Error(), nil)
	}
	if usage || likelyCLIUsageError(err) {
		return protocol.NewWireError(jsonErrorUsage, err.Error(), nil)
	}
	if daemonUnavailable(err) || errors.Is(err, io.EOF) || likelyDaemonUnavailableMessage(err) {
		return protocol.NewWireError(jsonErrorDaemonUnavailable, err.Error(), nil)
	}
	message := err.Error()
	if message == "" {
		message = "command failed"
	}
	return protocol.NewWireError(jsonErrorInternal, message, nil)
}

// likelyCLIUsageError covers action-level validation that runs after flag
// parsing. Parse failures carry cliUsageError; this conservative fallback
// keeps existing command implementations unchanged while classifying their
// user-input diagnostics consistently.
func likelyDaemonUnavailableMessage(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{
		"no hum daemon is running",
		"nothing is running",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func likelyCLIUsageError(err error) bool {
	if err == nil {
		return false
	}
	var usageErr cliUsageError
	if errors.As(err, &usageErr) {
		return true
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{
		" requires ", " accepts ", " must ", " only supported ", " does not accept ",
		"unknown option", "duplicate", "regular expression", "valid duration", "negative",
		"--project requires", "--project directory", "--project path", "--tty requires", "before the command",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

// cliConfig resolves the command-edge values once. The config package remains
// independent of urfave/cli and receives only its typed input.
func cliConfig(cmd *urfavecli.Command, version, buildTime string) (config.Config, error) {
	env := os.Environ()
	input := config.Input{
		FlagRuntimeDir:       cmd.String("runtime-dir"),
		FlagStopGrace:        cmd.String("stop-grace"),
		FlagOutputBytes:      cmd.String("output-bytes"),
		FlagCompletedRecords: cmd.String("completed-records"),
		EnvRuntimeDir:        lookupEnv(env, "HUM_RUNTIME_DIR"),
		EnvXDGRuntimeDir:     lookupEnv(env, "XDG_RUNTIME_DIR"),
		EnvStopGrace:         lookupEnv(env, "HUM_STOP_GRACE"),
		EnvOutputBytes:       lookupEnv(env, "HUM_OUTPUT_BYTES"),
		EnvCompletedRecords:  lookupEnv(env, "HUM_COMPLETED_RECORDS"),
	}
	return config.New(config.BuildOpts{Version: version, BuildTime: buildTime}, input)
}

func lookupEnv(env []string, name string) string {
	prefix := name + "="
	var value string
	for _, item := range env {
		if len(item) >= len(prefix) && item[:len(prefix)] == prefix {
			value = item[len(prefix):]
		}
	}
	return value
}

func daemonConfig(cfg config.Config) (daemon.Config, error) {
	if cfg.OutputBytes < 0 || cfg.OutputBytes > int64(maxInt()) {
		return daemon.Config{}, fmt.Errorf("output bytes: value %d is too large", cfg.OutputBytes)
	}
	if cfg.ReadBytes < 0 || cfg.ReadBytes > int64(maxInt()) {
		return daemon.Config{}, fmt.Errorf("read bytes: value %d is too large", cfg.ReadBytes)
	}
	if cfg.MaxLineBytes < 0 || cfg.MaxLineBytes > int64(maxInt()) {
		return daemon.Config{}, fmt.Errorf("max line bytes: value %d is too large", cfg.MaxLineBytes)
	}
	return daemon.Config{
		RuntimeDir:     cfg.RuntimeDir,
		StopGrace:      cfg.StopGrace,
		CompletedLimit: cfg.CompletedRecords,
		MaxLineBytes:   int(cfg.MaxLineBytes),
		OutputLimits: output.Limits{
			RetainedBytes:      int(cfg.OutputBytes),
			DefaultReadEntries: cfg.ReadEntries,
			DefaultReadBytes:   int(cfg.ReadBytes),
		},
	}, nil
}

func maxInt() int {
	return int(^uint(0) >> 1)
}

func daemonClient(ctx context.Context, cfg config.Config) (*daemon.Client, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	return daemon.DialRuntime(ctx, daemon.NewRuntimePaths(cfg.RuntimeDir))
}

func daemonUnavailable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, net.ErrClosed) {
		return true
	}
	var opErr *net.OpError
	return errors.As(err, &opErr) && opErr.Op == "dial"
}

func isWireCode(err error, code string) bool {
	if err == nil {
		return false
	}
	var wire *daemon.WireError
	if !errors.As(err, &wire) || wire == nil {
		return false
	}
	return string(wire.Code) == code
}

func isNotFound(err error) bool {
	return isWireCode(err, "not_found")
}

// crossScopeNotFoundMessage turns daemon discovery metadata into copyable,
// explicit selectors. A local miss never changes the request's scope.
func crossScopeNotFoundError(err error, action string) error {
	if !isNotFound(err) {
		return err
	}
	return wrapUserFacingError(err, crossScopeNotFoundMessage(err, action))
}

func crossScopeNotFoundMessage(err error, action string) string {
	var wire *daemon.WireError
	if !errors.As(err, &wire) || wire == nil {
		return err.Error()
	}
	message := wire.Message
	var details map[string]any
	if value, ok := wire.Details.(map[string]any); ok {
		details = value
	}
	if details == nil {
		return message + ". Run hum list --all to see every scope."
	}
	matches, _ := details["other_scopes"].([]any)
	lines := make([]string, 0, len(matches))
	for _, value := range matches {
		match, ok := value.(map[string]any)
		if !ok {
			continue
		}
		scope, _ := match["scope"].(string)
		if scope == "global" {
			lines = append(lines, "hum --global "+action)
			continue
		}
		root, _ := match["project_root"].(string)
		if root != "" {
			lines = append(lines, fmt.Sprintf("hum --project %s %s", shellEscape(root), action))
		}
	}
	if len(lines) == 0 {
		return message + ". Run hum list --all to see every scope."
	}
	return message + ". Other scopes:\n  " + strings.Join(lines, "\n  ") + "\nRun hum list --all to see every scope."
}

func isNameInUse(err error) bool {
	return isWireCode(err, "name_in_use")
}

func isActiveProcesses(err error) bool {
	var active *daemon.ActiveProcessesError
	return errors.As(err, &active)
}

func activeProcessNames(err error) []string {
	var active *daemon.ActiveProcessesError
	if errors.As(err, &active) && active != nil {
		return append([]string(nil), active.Names...)
	}
	return nil
}

// activeProcessesShutdownMessage renders the "root: name" entries reported by
// ActiveProcessesError as a human-facing "name (root), ..." list, instead of
// a raw Go slice, for a shutdown refusal message.
func activeProcessesShutdownMessage(names []string) string {
	formatted := make([]string, 0, len(names))
	for _, entry := range names {
		root, name, ok := strings.Cut(entry, ": ")
		if !ok {
			formatted = append(formatted, entry)
			continue
		}
		formatted = append(formatted, fmt.Sprintf("%s (%s)", name, root))
	}
	return fmt.Sprintf("Active processes prevent daemon shutdown: %s. Stop them first or run hum shutdown --stop-processes.", strings.Join(formatted, ", "))
}
