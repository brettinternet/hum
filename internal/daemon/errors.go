package daemon

import (
	"errors"
	"fmt"

	"hum/internal/protocol"
)

// VersionMismatchError identifies the protocol version advertised by a client
// and the version implemented by this daemon. Shutdown remains permitted after
type VersionMismatchError struct {
	ClientVersion int
	DaemonVersion int
	Message       string
}

func (e *VersionMismatchError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("daemon protocol version mismatch: client %d, daemon %d; run hum shutdown", e.ClientVersion, e.DaemonVersion)
}

var ErrVersionMismatch = errors.New("daemon protocol version mismatch")

func (e *VersionMismatchError) Unwrap() error { return ErrVersionMismatch }

func (e *VersionMismatchError) As(target any) bool {
	if destination, ok := target.(**protocol.WireError); ok {
		*destination = protocol.NewWireError(protocol.ErrorVersionMismatch, e.Error(), protocol.VersionMismatchDetails{Client: e.ClientVersion, Daemon: e.DaemonVersion})
		return true
	}
	return false
}

type ActiveProcessesError struct {
	Names []string
}

func (e *ActiveProcessesError) Error() string {
	if len(e.Names) == 0 {
		return "active supervised processes prevent daemon shutdown"
	}
	return fmt.Sprintf("active supervised processes prevent daemon shutdown: %v", e.Names)
}

var ErrActiveProcesses = errors.New("active supervised processes prevent daemon shutdown")

func (e *ActiveProcessesError) Unwrap() error { return ErrActiveProcesses }

// MalformedRequestError identifies invalid NDJSON request syntax or required
// fields. It is safe to send its message to a local client.
type MalformedRequestError struct{ Err error }

func (e *MalformedRequestError) Error() string {
	if e == nil || e.Err == nil {
		return "malformed daemon request"
	}
	return fmt.Sprintf("malformed daemon request: %v", e.Err)
}

var ErrMalformedRequest = errors.New("malformed daemon request")

func (e *MalformedRequestError) Unwrap() error { return ErrMalformedRequest }

// RequestTooLargeError rejects a wire line before JSON decoding can allocate
// unbounded memory.
type RequestTooLargeError struct{ Limit int }

func (e *RequestTooLargeError) Error() string {
	return fmt.Sprintf("daemon request exceeds %d bytes", e.Limit)
}

var ErrRequestTooLarge = errors.New("daemon request is too large")

func (e *RequestTooLargeError) Unwrap() error { return ErrRequestTooLarge }

// WireError is the shared protocol typed error representation.
type WireError = protocol.WireError
