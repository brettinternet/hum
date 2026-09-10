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
	daemonChildEnv      = "HUM_DAEMON_CHILD"
	daemonChildEnvValue = "1"
	// daemonStartupTimeout is dial/setup slack added after the complete
	// persisted-group reconciliation budget.
	daemonStartupTimeout   = 5 * time.Second
	daemonStartupPoll      = 10 * time.Millisecond
	daemonDialTimeout      = time.Second
	daemonTerminationGrace = 2 * time.Second
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

	startupBudget, err := daemon.StartupBudget(paths, cfg.StopGrace, daemonStartupTimeout)
	if err != nil {
		return 0, daemonStartupError(paths, err)
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

	pid, waitErr := waitForDaemon(ctx, paths, child, startupBudget)
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

func waitForDaemon(ctx context.Context, paths daemon.RuntimePaths, child *exec.Cmd, startupBudget time.Duration) (int, error) {
	waitCtx, cancel := context.WithTimeout(ctx, startupBudget)
	defer cancel()
	ticker := time.NewTicker(daemonStartupPoll)
	defer ticker.Stop()
	exited := reapDetachedChild(child)

	for {
		if pid, err := probeDaemon(waitCtx, paths); err == nil {
			if child != nil && child.Process != nil && pid != child.Process.Pid {
				// A concurrent starter may have won the runtime lock while
				// this child was still initializing. Retire the loser before
				// returning so it cannot resurrect after the winner later
				// shuts down.
				terminateDetachedChild(child, exited)
			}
			return pid, nil
		} else if isVersionMismatch(err) {
			terminateDetachedChild(child, exited)
			return 0, err
		}

		select {
		case <-exited.done:
			// The child gave up before publishing readiness, for example
			// because a live process owns the runtime or the runtime directory
			// was rejected. Waiting out the recovery budget would only delay
			// the same failure. A racing starter may still have won, so probe
			// once more before reporting the child's exit.
			if pid, err := probeDaemon(waitCtx, paths); err == nil {
				return pid, nil
			}
			return 0, daemonStartupError(paths, fmt.Errorf("detached daemon exited before readiness: %w", exited.status()))
		case <-waitCtx.Done():
			if err := ctx.Err(); err != nil {
				cancelDetachedChild(child, exited)
				return 0, err
			}
			terminateDetachedChild(child, exited)
			return 0, daemonStartupError(paths, waitCtx.Err())
		case <-ticker.C:
		}
	}
}

// reapedChild reports when the detached child exits. Reaping starts at launch
// so an early failure is observed immediately rather than after the complete
// startup budget, which scales with the number of recorded stale groups.
type reapedChild struct {
	done chan struct{}
	err  error
}

func reapDetachedChild(child *exec.Cmd) *reapedChild {
	reaped := &reapedChild{}
	if child == nil || child.Process == nil {
		// A nil channel never fires; callers only wait on it.
		return reaped
	}
	reaped.done = make(chan struct{})
	go func() {
		reaped.err = child.Wait()
		close(reaped.done)
	}()
	return reaped
}

// status is valid only after done is closed.
func (r *reapedChild) status() error {
	if r.err == nil {
		return errors.New("exit status 0")
	}
	return r.err
}

func (r *reapedChild) exited() bool {
	select {
	case <-r.done:
		return true
	default:
		return false
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

// cancelDetachedChild preserves prompt caller cancellation while allowing the
// daemon to finish its bounded reconciliation and then observe SIGTERM through
// its serve context. The launch-time reaper collects the cleanly exiting child.
func cancelDetachedChild(child *exec.Cmd, reaped *reapedChild) {
	if child == nil || child.Process == nil || child.Process.Pid <= 0 || reaped.exited() {
		return
	}
	_ = syscall.Kill(-child.Process.Pid, syscall.SIGTERM)
}

func terminateDetachedChild(child *exec.Cmd, reaped *reapedChild) {
	if child == nil || child.Process == nil || child.Process.Pid <= 0 {
		return
	}
	// The child was reaped on exit, so its process group may already belong
	// to an unrelated process; never signal a group whose leader is gone.
	if reaped.exited() {
		return
	}
	pid := child.Process.Pid
	// Setsid makes the child both a session leader and process-group leader.
	// TERM lets a daemon that reached its signal-aware serve path remove its
	// ownership artifacts. Fall back to KILL if startup itself is wedged.
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	select {
	case <-reaped.done:
	case <-time.After(daemonTerminationGrace):
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		<-reaped.done
	}
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
