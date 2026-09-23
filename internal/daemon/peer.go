package daemon

import (
	"errors"
	"fmt"
	"net"
	"os"
	"sync/atomic"
)

// runtimeUserOverride replaces the effective user ID in tests that simulate
// another local user. It is atomic because server goroutines read it.
var runtimeUserOverride atomic.Pointer[int]

// runtimeUser returns the only user allowed to own the runtime directory or to
// be either end of a daemon connection.
func runtimeUser() int {
	if uid := runtimeUserOverride.Load(); uid != nil {
		return *uid
	}
	return os.Geteuid()
}

// verifyPeer requires the other end of a daemon connection to run as the
// runtime user. Directory and socket modes remain the primary boundary; peer
// credentials also refuse a daemon that another user planted at the runtime
// path and a client that reached an operator-managed directory.
func verifyPeer(conn net.Conn) error {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return errors.New("daemon connection is not a Unix socket")
	}
	raw, err := unixConn.SyscallConn()
	if err != nil {
		return fmt.Errorf("read peer credentials: %w", err)
	}
	uid, credErr := 0, error(nil)
	if err := raw.Control(func(fd uintptr) { uid, credErr = peerUID(int(fd)) }); err != nil {
		return fmt.Errorf("read peer credentials: %w", err)
	}
	if credErr != nil {
		return fmt.Errorf("read peer credentials: %w", credErr)
	}
	if want := runtimeUser(); uid != want {
		return fmt.Errorf("peer uid %d is not the current user (uid %d)", uid, want)
	}
	return nil
}
