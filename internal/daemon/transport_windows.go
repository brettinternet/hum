//go:build windows

package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

// The client retains its connected handle so security checks cannot race a
// pipe-name replacement between a path lookup and the wire handshake.
type securePipeConn struct {
	io.ReadWriteCloser
	handle    windows.Handle
	path      string
	deadlines interface {
		SetReadDeadline(time.Time) error
		SetWriteDeadline(time.Time) error
	}
}
type pipeAddr string

func (a pipeAddr) Network() string             { return "pipe" }
func (a pipeAddr) String() string              { return string(a) }
func (c *securePipeConn) LocalAddr() net.Addr  { return pipeAddr(c.path) }
func (c *securePipeConn) RemoteAddr() net.Addr { return pipeAddr(c.path) }
func (c *securePipeConn) SetDeadline(t time.Time) error {
	if err := c.SetReadDeadline(t); err != nil {
		return err
	}
	return c.SetWriteDeadline(t)
}
func (c *securePipeConn) SetReadDeadline(t time.Time) error  { return c.deadlines.SetReadDeadline(t) }
func (c *securePipeConn) SetWriteDeadline(t time.Time) error { return c.deadlines.SetWriteDeadline(t) }

func dialRuntime(ctx context.Context, path string) (net.Conn, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		h, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE|windows.READ_CONTROL,
			windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OVERLAPPED, 0)
		if err == nil {
			sd, sdErr := windows.GetSecurityInfo(h, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
			if sdErr == nil {
				sdErr = checkPrivateDescriptor(sd)
			}
			if sdErr != nil {
				_ = windows.CloseHandle(h)
				return nil, fmt.Errorf("refusing daemon pipe: %w", sdErr)
			}
			file, fileErr := winio.NewOpenFile(h)
			if fileErr != nil {
				_ = windows.CloseHandle(h)
				return nil, fileErr
			}
			deadlines, ok := file.(interface {
				SetReadDeadline(time.Time) error
				SetWriteDeadline(time.Time) error
			})
			if !ok {
				_ = file.Close()
				return nil, errors.New("daemon pipe does not support deadlines")
			}
			return &securePipeConn{ReadWriteCloser: file, handle: h, path: path, deadlines: deadlines}, nil
		}
		// An absent endpoint is not a transient busy listener. Return it so
		// callers can autostart (or report an absent daemon) without requiring
		// an explicit deadline on every read-only request.
		if !errors.Is(err, windows.ERROR_PIPE_BUSY) {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}
