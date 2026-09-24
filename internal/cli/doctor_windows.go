//go:build windows

package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"hum/internal/daemon"
)

func diagnoseRuntimePath(path string) (string, string) {
	info, err := os.Lstat(path)
	if err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return doctorFail, "runtime path exists but is not a private directory"
		}
		if err := checkDoctorRuntimeACL(path); err != nil {
			return doctorFail, "runtime directory ACL is not private"
		}
		if !probeDoctorDirectory(path) {
			return doctorFail, "runtime directory is not writable"
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
			if !info.IsDir() || !probeDoctorDirectory(ancestor) {
				return doctorFail, "runtime directory cannot be created beneath its existing parent"
			}
			return doctorInfo, "runtime directory is absent; its existing parent is usable"
		}
		if !os.IsNotExist(statErr) {
			return doctorFail, "runtime path parent cannot be inspected"
		}
	}
}

func checkDoctorRuntimeACL(path string) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	if sd == nil {
		return errors.New("missing security descriptor")
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return err
	}
	if owner == nil || !owner.Equals(user.User.Sid) {
		return errors.New("owner is not the current user")
	}
	acl, present, err := sd.DACL()
	if err != nil {
		return err
	}
	if !present || acl == nil || acl.AceCount == 0 {
		return errors.New("missing private DACL")
	}
	for index := uint32(0); index < uint32(acl.AceCount); index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(acl, index, &ace); err != nil {
			return err
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return errors.New("unexpected ACL entry")
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if !sid.Equals(user.User.Sid) {
			return errors.New("ACL grants another user access")
		}
	}
	return nil
}

func diagnoseDaemon(ctx context.Context, paths daemon.RuntimePaths, add func(string, string, string, map[string]any)) {
	dialCtx, cancel := context.WithTimeout(ctx, 750*time.Millisecond)
	defer cancel()
	client, err := daemon.DialRuntime(dialCtx, paths)
	if client != nil {
		defer client.Close()
	}
	if err == nil {
		add("daemon", doctorPass, "existing daemon is reachable and protocol-compatible", map[string]any{"pipe": paths.Socket})
		return
	}
	var mismatch *daemon.VersionMismatchError
	if errors.As(err, &mismatch) {
		add("daemon", doctorFail, "existing daemon uses an incompatible protocol", map[string]any{"pipe": paths.Socket})
		return
	}
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, windows.ERROR_PATH_NOT_FOUND) || errors.Is(err, os.ErrNotExist) {
		add("daemon", doctorInfo, "daemon pipe is absent; launch commands will start it", map[string]any{"pipe": paths.Socket})
		return
	}
	add("daemon", doctorFail, "existing daemon pipe is unreachable", map[string]any{"pipe": paths.Socket})
}

func probeDoctorDirectory(path string) bool {
	probe, err := os.CreateTemp(path, ".hum-doctor-*")
	if err != nil {
		return false
	}
	name := probe.Name()
	closeErr := probe.Close()
	removeErr := os.Remove(name)
	return closeErr == nil && removeErr == nil
}
