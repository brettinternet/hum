//go:build !windows

package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"hum/internal/daemon"
)

func diagnoseRuntimePath(path string) (string, string) {
	info, err := os.Lstat(path)
	if err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return doctorFail, "runtime path exists but is not a private directory"
		}
		if stat, ok := info.Sys().(*syscall.Stat_t); !ok || int(stat.Uid) != os.Geteuid() {
			return doctorFail, "runtime directory is owned by another user"
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
