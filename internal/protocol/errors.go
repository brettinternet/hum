package protocol

import (
	"errors"
	"fmt"
)

var (
	// ErrMalformed identifies invalid JSON or an invalid NDJSON line.
	ErrMalformed = errors.New("protocol message is malformed")
	// ErrOversized identifies a line beyond the configured maximum.
	ErrOversized = errors.New("protocol message exceeds maximum line size")
	// ErrUnknownOperation identifies an operation the protocol does not know.
	ErrUnknownOperation = errors.New("protocol operation is unknown")
	// ErrMissingOperation identifies a message without its required op field.
	ErrMissingOperation = errors.New("protocol message operation is missing")
	// ErrResponseEnvironment identifies an attempted response environment leak.
	ErrResponseEnvironment = errors.New("protocol response contains an environment")
)

// DecodeErrorKind identifies the class of decoder failure.
type DecodeErrorKind string

const (
	// DecodeMalformed is invalid JSON, an empty line, or trailing data.
	DecodeMalformed DecodeErrorKind = "malformed"
	// DecodeOversized is a line larger than the decoder limit.
	DecodeOversized DecodeErrorKind = "oversized"
	// DecodeUnknownOperation is an unrecognized or missing operation.
	DecodeUnknownOperation DecodeErrorKind = "unknown_operation"
)

const (
	// maxUnknownOperationPreviewBytes bounds operation text in local errors.
	maxUnknownOperationPreviewBytes = 32
	// maxUnknownOperationErrorBytes bounds the complete local diagnostic.
	maxUnknownOperationErrorBytes = 256
)

// DecodeError reports a bounded NDJSON decoding failure.
type DecodeError struct {
	Kind  DecodeErrorKind
	Limit int
	Err   error
}

// Error describes the decoder failure without exposing the raw line.
func (e *DecodeError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err != nil {
		return fmt.Sprintf("protocol %s message: %v", e.Kind, e.Err)
	}
	return fmt.Sprintf("protocol %s message", e.Kind)
}

// Unwrap returns the underlying sentinel or JSON error.
func (e *DecodeError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Is lets errors.Is classify malformed and oversized decoder errors.
func (e *DecodeError) Is(target error) bool {
	if e == nil {
		return target == nil
	}
	switch e.Kind {
	case DecodeMalformed:
		return target == ErrMalformed
	case DecodeOversized:
		return target == ErrOversized
	case DecodeUnknownOperation:
		return target == ErrUnknownOperation || target == ErrMissingOperation
	default:
		return false
	}
}

// UnknownOperationError identifies an unrecognized operation value.
type UnknownOperationError struct {
	Operation Operation
}

// Error returns a bounded unknown-operation diagnostic. The operation remains
// available through the typed Operation field, while the local text contains
// only a short preview.
func (e *UnknownOperationError) Error() string {
	if e == nil || e.Operation == "" {
		return ErrUnknownOperation.Error()
	}
	operation := e.Operation
	truncated := false
	if len(operation) > maxUnknownOperationPreviewBytes {
		operation = operation[:maxUnknownOperationPreviewBytes]
		truncated = true
	}
	message := fmt.Sprintf("%s: %q", ErrUnknownOperation, operation)
	if truncated {
		message += "..."
	}
	if len(message) > maxUnknownOperationErrorBytes {
		return ErrUnknownOperation.Error()
	}
	return message
}

// Unwrap supports errors.Is(err, ErrUnknownOperation).
func (e *UnknownOperationError) Unwrap() error { return ErrUnknownOperation }

// MissingOperationError identifies a message without an operation marker.
type MissingOperationError struct{}

// Error returns the missing-operation error text.
func (MissingOperationError) Error() string { return ErrMissingOperation.Error() }

// Unwrap supports errors.Is(err, ErrMissingOperation).
func (MissingOperationError) Unwrap() error { return ErrMissingOperation }

// OversizedError identifies an encoded or decoded line beyond a limit.
type OversizedError struct {
	Size  int
	Limit int
}

// Error returns the bounded-size error text.
func (e *OversizedError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%s: %d bytes exceeds %d-byte limit", ErrOversized, e.Size, e.Limit)
}

// Unwrap supports errors.Is(err, ErrOversized).
func (e *OversizedError) Unwrap() error { return ErrOversized }

// MalformedError identifies a malformed message independent of a decoder.
type MalformedError struct {
	Err error
}

// Error returns the malformed-message text.
func (e *MalformedError) Error() string {
	if e == nil || e.Err == nil {
		return ErrMalformed.Error()
	}
	return fmt.Sprintf("%s: %v", ErrMalformed, e.Err)
}

// Unwrap supports errors.Is(err, ErrMalformed).
func (e *MalformedError) Unwrap() error {
	if e == nil || e.Err == nil {
		return ErrMalformed
	}
	return errors.Join(ErrMalformed, e.Err)
}

// ErrorCodeForDecode maps a decoder error to its wire error code.
func ErrorCodeForDecode(err error) ErrorCode {
	if errors.Is(err, ErrOversized) {
		return ErrorOversized
	}
	if errors.Is(err, ErrUnknownOperation) || errors.Is(err, ErrMissingOperation) {
		return ErrorUnknownOperation
	}
	return ErrorMalformed
}

// WireErrorForDecode converts a decoder failure into a response-safe wire
// error. Decoder details are intentionally reduced to stable bounded text;
// the raw input line and operation value are never included in Details.
func WireErrorForDecode(err error) *WireError {
	if err == nil {
		return nil
	}
	code := ErrorCodeForDecode(err)
	var message string
	switch code {
	case ErrorOversized:
		message = ErrOversized.Error()
	case ErrorUnknownOperation:
		message = ErrUnknownOperation.Error()
	default:
		message = ErrMalformed.Error()
	}
	return NewWireError(code, message, nil)
}
