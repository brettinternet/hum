//go:build windows

package daemon

import (
	"errors"
	"fmt"
	"net"
	"sync/atomic"
	"unsafe"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

// Tests can substitute the expected user without ever changing the security
// descriptor used when creating the pipe and directory.
var runtimeSIDOverride atomic.Pointer[string]

func currentSID() (*windows.SID, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	return user.User.Sid, nil
}
func expectedSID() (*windows.SID, error) {
	if override := runtimeSIDOverride.Load(); override != nil {
		return windows.StringToSid(*override)
	}
	return currentSID()
}
func privateSDDL() string {
	sid, err := currentSID()
	if err != nil {
		return "D:P"
	} // Invalid/empty descriptor must fail closed on creation.
	return "O:" + sid.String() + "D:P(A;;GA;;;" + sid.String() + ")"
}

// Refuse both foreign owners and any allow ACE for another identity. A
// protected, current-user-only DACL is required for every trusted artifact.
func checkPrivateACL(path string) error {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	return checkPrivateDescriptor(sd)
}
func checkPrivateDescriptor(sd *windows.SECURITY_DESCRIPTOR) error {
	want, err := expectedSID()
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
	if owner == nil || !owner.Equals(want) {
		return errors.New("owner is not the current user")
	}
	acl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	if acl == nil || acl.AceCount == 0 {
		return errors.New("missing private DACL")
	}
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(acl, i, &ace); err != nil {
			return err
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return fmt.Errorf("unexpected ACE type %d", ace.Header.AceType)
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if !sid.Equals(want) {
			return fmt.Errorf("ACE %d grants another user access", i)
		}
	}
	return nil
}

// Client connections verify the descriptor on their connected handle. Server
// connections are admitted only by the first-instance pipe's explicit,
// current-user-only ACL (not by a caller-provided path or claimed identity).
func verifyPeer(conn net.Conn) error {
	if client, ok := conn.(*securePipeConn); ok {
		sd, err := windows.GetSecurityInfo(client.handle, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
		if err != nil {
			return err
		}
		return checkPrivateDescriptor(sd)
	}
	if _, ok := conn.(winio.PipeConn); !ok {
		return errors.New("daemon connection is not a named pipe")
	}
	return nil
}
