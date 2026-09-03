package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"hum/internal/config"
	"hum/internal/daemon"
)

const (
	daemonChildEnv       = "HUM_DAEMON_CHILD"
	daemonChildEnvValue  = "1"
	daemonStartupTimeout = 5 * time.Second
	daemonStartupPoll    = 10 * time.Millisecond
	daemonDialTimeout    = 100 * time.Millisecond
)

func isDaemonChild() bool {
	return os.Getenv(daemonChildEnv) == daemonChildEnvValue
}
func boundedDaemonDial(ctx context.Context, paths daemon.RuntimePaths) (*daemon.Client, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	dialCtx, cancel := context.WithTimeout(ctx, daemonDialTimeout)
	defer cancel()
	return daemon.DialRuntime(dialCtx, paths)
}

func startupDaemonUnavailable(ctx context.Context, err error) bool {
	if daemonUnavailable(err) {
		return true
	}
	return errors.Is(err, context.DeadlineExceeded) && (ctx == nil || ctx.Err() == nil)
}

// ensureDaemon returns the PID of a daemon that completed its protocol
// handshake. It first reuses a compatible daemon, then starts exactly one
// detached child and waits for that child (or a racing winner) to become ready.
func ensureDaemon(ctx context.Context, cfg config.Config) (int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	paths := daemon.NewRuntimePaths(cfg.RuntimeDir)
	client, err := boundedDaemonDial(ctx, paths)
	if err == nil {
		defer client.Close()
		return readDaemonPID(paths)
	}
	var mismatch *daemon.VersionMismatchError
	if !startupDaemonUnavailable(ctx, err) {
		if errors.As(err, &mismatch) {
			if client != nil {
				_ = client.Close()
			}
			return 0, err
		}
		return 0, err
	}

	executable, err := os.Executable()
	if err != nil {
		return 0, fmt.Errorf("locate hum executable: %w", err)
	}
	child := exec.Command(executable, "serve")
	child.Env = daemonChildEnvironment(cfg)
	// A nil stream is connected to the null device by os/exec. In particular,
	// no terminal or caller pipe can keep the detached daemon attached.
	child.Stdin = nil
	child.Stdout = nil
	child.Stderr = nil
	child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := child.Start(); err != nil {
		return 0, daemonStartupError(paths, fmt.Errorf("start detached daemon: %w", err))
	}

	pid, waitErr := waitForDaemon(ctx, paths, child)
	if waitErr != nil {
		return 0, waitErr
	}
	return pid, nil
}

func daemonChildEnvironment(cfg config.Config) []string {
	env := append([]string(nil), os.Environ()...)
	env = replaceEnv(env, daemonChildEnv, daemonChildEnvValue)
	// Global flags are resolved before the child is launched, while the child
	// receives only the required `serve` argument. Carry their resolved values
	// through the normal HUM_* configuration boundary.
	env = replaceEnv(env, "HUM_RUNTIME_DIR", cfg.RuntimeDir)
	env = replaceEnv(env, "HUM_STOP_GRACE", cfg.StopGrace.String())
	env = replaceEnv(env, "HUM_OUTPUT_BYTES", strconv.FormatInt(cfg.OutputBytes, 10))
	env = replaceEnv(env, "HUM_COMPLETED_RECORDS", strconv.Itoa(cfg.CompletedRecords))
	return env
}

func replaceEnv(env []string, name, value string) []string {
	prefix := name + "="
	result := make([]string, 0, len(env)+1)
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			continue
		}
		result = append(result, item)
	}
	return append(result, prefix+value)
}

func waitForDaemon(ctx context.Context, paths daemon.RuntimePaths, child *exec.Cmd) (int, error) {
	waitCtx, cancel := context.WithTimeout(ctx, daemonStartupTimeout)
	defer cancel()
	ticker := time.NewTicker(daemonStartupPoll)
	defer ticker.Stop()

	for {
		if pid, err := probeDaemon(waitCtx, paths); err == nil {
			if child != nil && child.Process != nil {
				if pid != child.Process.Pid {
					// A concurrent starter may have won the runtime lock while
					// this child was still initializing. Retire and reap the
					// loser before returning so it cannot resurrect after the
					// winner later shuts down.
					terminateDetachedChild(child)
				} else {
					// The child owns the ready daemon. Reap it only after
					// readiness so its PID cannot be reused during startup.
					go func() { _ = child.Wait() }()
				}
			}
			return pid, nil
		} else if isVersionMismatch(err) {
			terminateDetachedChild(child)
			return 0, err
		}

		select {
		case <-waitCtx.Done():
			terminateDetachedChild(child)
			if err := waitCtx.Err(); err != nil {
				if errors.Is(err, context.DeadlineExceeded) {
					return 0, daemonStartupError(paths, err)
				}
				return 0, err
			}
		case <-ticker.C:
		}
	}
}

func probeDaemon(parent context.Context, paths daemon.RuntimePaths) (int, error) {
	ctx, cancel := context.WithTimeout(parent, daemonDialTimeout)
	defer cancel()
	client, err := daemon.DialRuntime(ctx, paths)
	if err != nil {
		return 0, err
	}
	defer client.Close()
	return readDaemonPID(paths)
}

func readDaemonPID(paths daemon.RuntimePaths) (int, error) {
	data, err := os.ReadFile(paths.PID)
	if err != nil {
		return 0, fmt.Errorf("read daemon pid: %w", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		if err == nil {
			err = errors.New("pid must be positive")
		}
		return 0, fmt.Errorf("read daemon pid: %w", err)
	}
	return pid, nil
}

func daemonStartupError(paths daemon.RuntimePaths, err error) error {
	return fmt.Errorf("daemon startup failed; see %s: %w", paths.Log, err)
}

func isVersionMismatch(err error) bool {
	var mismatch *daemon.VersionMismatchError
	return errors.As(err, &mismatch)
}

func terminateDetachedChild(child *exec.Cmd) {
	if child == nil || child.Process == nil {
		return
	}
	pid := child.Process.Pid
	if pid <= 0 {
		return
	}
	// Setsid makes the child both a session leader and process-group leader.
	// Kill the whole group before synchronously reaping the child. Keeping the
	// child unreaped until this point prevents its PID from being reused.
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	_ = child.Wait()
}

func runDaemonClient(ctx context.Context, cfg config.Config) (*daemon.Client, error) {
	client, err := boundedDaemonDial(ctx, daemon.NewRuntimePaths(cfg.RuntimeDir))
	if err == nil {
		return client, nil
	}
	if startupDaemonUnavailable(ctx, err) {
		if _, startErr := ensureDaemon(ctx, cfg); startErr != nil {
			return nil, startErr
		}
		return boundedDaemonDial(ctx, daemon.NewRuntimePaths(cfg.RuntimeDir))
	}

	var mismatch *daemon.VersionMismatchError
	if client == nil || !errors.As(err, &mismatch) || mismatch == nil {
		return nil, err
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), daemonDialTimeout)
	shutdownErr := client.Shutdown(shutdownCtx, daemon.ShutdownRequest{Force: false})
	cancel()
	_ = client.Close()
	if shutdownErr != nil {
		var active *daemon.ActiveProcessesError
		if errors.As(shutdownErr, &active) {
			names := strings.Join(active.Names, ", ")
			if names == "" {
				names = "managed processes"
			}
			return nil, fmt.Errorf("%w: daemon version %d has active processes (%s); run hum shutdown --stop-processes", shutdownErr, mismatch.DaemonVersion, names)
		}
		// Another client may have retired the same idle daemon first. Treat
		// the resulting disconnected shutdown as a successful hand-off.
		if !startupDaemonUnavailable(context.Background(), shutdownErr) {
			return nil, shutdownErr
		}
	}
	if _, startErr := ensureDaemon(ctx, cfg); startErr != nil {
		return nil, startErr
	}
	return boundedDaemonDial(ctx, daemon.NewRuntimePaths(cfg.RuntimeDir))
}
