package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"

	"hum/internal/config"
	"hum/internal/daemon"
	"hum/internal/output"

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
		Version:        cfg.Version,
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
