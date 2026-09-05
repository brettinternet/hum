package protocol

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const (
	// DefaultMaxLineBytes is the conservative maximum encoded message size.
	DefaultMaxLineBytes = 1 << 20
	// MaxLineBytesDefault is a descriptive alias for DefaultMaxLineBytes.
	MaxLineBytesDefault = DefaultMaxLineBytes
)

// Decoder reads one bounded JSON value per newline-delimited message. It does
// not use json.Decoder directly because json.Decoder permits multiple values
// without line boundaries and can buffer beyond the protocol limit.
type Decoder struct {
	reader *bufio.Reader
	limit  int
}

// NewDecoder constructs a bounded decoder. With no limit it uses
// DefaultMaxLineBytes; a non-positive limit also selects that safe default.
func NewDecoder(r io.Reader, maxLineBytes ...int) *Decoder {
	limit := DefaultMaxLineBytes
	if len(maxLineBytes) != 0 && maxLineBytes[0] > 0 {
		limit = maxLineBytes[0]
	}
	return &Decoder{reader: bufio.NewReader(r), limit: limit}
}

// NewDecoderWithLimit constructs a decoder and rejects an invalid bound.
func NewDecoderWithLimit(r io.Reader, maxLineBytes int) (*Decoder, error) {
	if maxLineBytes <= 0 {
		return nil, fmt.Errorf("protocol: maximum line bytes must be positive")
	}
	return &Decoder{reader: bufio.NewReader(r), limit: maxLineBytes}, nil
}

// MaxLineBytes returns the decoder's content-byte limit. The terminating
// newline is not counted.
func (d *Decoder) MaxLineBytes() int {
	if d == nil {
		return 0
	}
	return d.limit
}

// Decode reads exactly one line and unmarshals it into v. A final line may omit
// its newline, but two JSON values on one line are rejected.
func (d *Decoder) Decode(v any) error {
	raw, err := d.decodeRaw()
	if err != nil {
		return err
	}
	if v == nil {
		return &DecodeError{Kind: DecodeMalformed, Err: errors.New("nil decode target")}
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return &DecodeError{Kind: DecodeMalformed, Err: err}
	}
	return nil
}

// DecodeRequest reads, validates, and dispatches one request by its op field.
func (d *Decoder) DecodeRequest() (Request, error) {
	raw, err := d.decodeRaw()
	if err != nil {
		return Request{}, err
	}
	var header struct {
		Op *Operation `json:"op"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return Request{}, &DecodeError{Kind: DecodeMalformed, Err: err}
	}
	if header.Op == nil || *header.Op == "" {
		return Request{}, &DecodeError{Kind: DecodeUnknownOperation, Err: MissingOperationError{}}
	}
	if !IsKnown(*header.Op) {
		return Request{}, &DecodeError{Kind: DecodeUnknownOperation, Err: &UnknownOperationError{Operation: *header.Op}}
	}
	var request Request
	if err := json.Unmarshal(raw, &request); err != nil {
		var unknown *UnknownOperationError
		if errors.As(err, &unknown) {
			return Request{}, &DecodeError{Kind: DecodeUnknownOperation, Err: err}
		}
		return Request{}, &DecodeError{Kind: DecodeMalformed, Err: err}
	}
	return request, nil
}

// DecodeMessage is an alias for DecodeRequest used by dynamic dispatchers.
func (d *Decoder) DecodeMessage() (Request, error) { return d.DecodeRequest() }

func (d *Decoder) decodeRaw() ([]byte, error) {
	if d == nil || d.reader == nil || d.limit <= 0 {
		return nil, &DecodeError{Kind: DecodeMalformed, Err: errors.New("decoder is not initialized")}
	}

	var line []byte
	contentBytes := 0
	for {
		part, err := d.reader.ReadSlice('\n')
		if len(part) != 0 {
			partContent := len(part)
			if part[partContent-1] == '\n' {
				partContent--
			}
			contentBytes += partContent
			if contentBytes <= d.limit {
				line = append(line, part...)
			}
		}

		if contentBytes > d.limit {
			// Keep consuming the remainder of this physical line before returning
			// so a caller can safely continue with the next request.
			if err == bufio.ErrBufferFull {
				continue
			}
			return nil, &DecodeError{Kind: DecodeOversized, Limit: d.limit, Err: &OversizedError{Size: contentBytes, Limit: d.limit}}
		}

		if err == bufio.ErrBufferFull {
			continue
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, &DecodeError{Kind: DecodeMalformed, Err: err}
		}
		if len(part) == 0 && errors.Is(err, io.EOF) {
			if len(line) == 0 {
				return nil, io.EOF
			}
		}
		break
	}

	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return nil, &DecodeError{Kind: DecodeMalformed, Err: errors.New("empty line")}
	}
	if !json.Valid(line) {
		return nil, &DecodeError{Kind: DecodeMalformed, Err: ErrMalformed}
	}
	return line, nil
}

// Request is the dynamically dispatched request union. Raw contains the exact
// JSON object received; one operation-specific pointer is populated for known
// operations. Payload is an alias view of Raw for callers using envelope
// terminology.
type Request struct {
	Op      Operation       `json:"op"`
	ID      string          `json:"id,omitempty"`
	Version int             `json:"version,omitempty"`
	Payload json.RawMessage `json:"-"`
	Raw     json.RawMessage `json:"-"`

	Hello        *HelloRequest        `json:"-"`
	Start        *StartRequest        `json:"-"`
	List         *ListRequest         `json:"-"`
	Get          *GetRequest          `json:"-"`
	Output       *OutputRequest       `json:"-"`
	Follow       *FollowRequest       `json:"-"`
	Wait         *WaitRequest         `json:"-"`
	Signal       *SignalRequest       `json:"-"`
	Stop         *StopRequest         `json:"-"`
	Restart      *RestartRequest      `json:"-"`
	Remove       *RemoveRequest       `json:"-"`
	Shutdown     *ShutdownRequest     `json:"-"`
	InputAttach  *InputAttachRequest  `json:"-"`
	InputRelease *InputReleaseRequest `json:"-"`
	InputWrite   *InputWriteRequest   `json:"-"`
	InputResize  *InputResizeRequest  `json:"-"`
}

// UnmarshalJSON dispatches a flattened request object into its operation DTO.
func (r *Request) UnmarshalJSON(data []byte) error {
	var header struct {
		Op Operation `json:"op"`
		ID string    `json:"id"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return err
	}
	if header.Op == "" {
		return MissingOperationError{}
	}
	if !IsKnown(header.Op) {
		return &UnknownOperationError{Operation: header.Op}
	}

	r.Op, r.ID = header.Op, header.ID
	r.Raw = append(r.Raw[:0], data...)
	r.Payload = append(r.Payload[:0], data...)
	r.Version = 0
	r.Hello, r.Start, r.List, r.Get, r.Output, r.Follow, r.Wait, r.Signal, r.Stop, r.Restart, r.Remove, r.Shutdown = nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil
	r.InputAttach, r.InputRelease, r.InputWrite, r.InputResize = nil, nil, nil, nil

	var err error
	switch header.Op {
	case OpHello:
		r.Hello = new(HelloRequest)
		err = json.Unmarshal(data, r.Hello)
		r.Version = r.Hello.Version
	case OpStart:
		r.Start = new(StartRequest)
		err = json.Unmarshal(data, r.Start)
	case OpList:
		r.List = new(ListRequest)
		err = json.Unmarshal(data, r.List)
	case OpGet:
		r.Get = new(GetRequest)
		err = json.Unmarshal(data, r.Get)
	case OpOutput:
		r.Output = new(OutputRequest)
		err = json.Unmarshal(data, r.Output)
	case OpFollow:
		r.Follow = new(FollowRequest)
		err = json.Unmarshal(data, r.Follow)
	case OpWait:
		r.Wait = new(WaitRequest)
		err = json.Unmarshal(data, r.Wait)
	case OpSignal:
		r.Signal = new(SignalRequest)
		err = json.Unmarshal(data, r.Signal)
	case OpStop:
		r.Stop = new(StopRequest)
		err = json.Unmarshal(data, r.Stop)
	case OpRestart:
		r.Restart = new(RestartRequest)
		err = json.Unmarshal(data, r.Restart)
	case OpRemove:
		r.Remove = new(RemoveRequest)
		err = json.Unmarshal(data, r.Remove)
	case OpShutdown:
		r.Shutdown = new(ShutdownRequest)
		err = json.Unmarshal(data, r.Shutdown)
	case OpInputAttach:
		r.InputAttach = new(InputAttachRequest)
		err = json.Unmarshal(data, r.InputAttach)
	case OpInputRelease:
		r.InputRelease = new(InputReleaseRequest)
		err = json.Unmarshal(data, r.InputRelease)
	case OpInputWrite:
		r.InputWrite = new(InputWriteRequest)
		err = json.Unmarshal(data, r.InputWrite)
	case OpInputResize:
		r.InputResize = new(InputResizeRequest)
		err = json.Unmarshal(data, r.InputResize)
	}
	return err
}

// Decode decodes Raw into an operation-specific value. It is useful when a
// dispatcher keeps Request as its only envelope value.
func (r Request) Decode(v any) error {
	if len(r.Raw) == 0 {
		return &DecodeError{Kind: DecodeMalformed, Err: errors.New("request has no raw payload")}
	}
	if v == nil {
		return &DecodeError{Kind: DecodeMalformed, Err: errors.New("nil decode target")}
	}
	if err := json.Unmarshal(r.Raw, v); err != nil {
		return &DecodeError{Kind: DecodeMalformed, Err: err}
	}
	return nil
}

// MarshalJSON writes Raw when available, or marshals the populated operation
// DTO. This keeps Request a convenient client-side envelope without introducing
// a second payload object on the wire.
func (r Request) MarshalJSON() ([]byte, error) {
	if len(r.Raw) != 0 {
		return append([]byte(nil), r.Raw...), nil
	}
	if r.Hello != nil {
		return json.Marshal(r.Hello)
	}
	if r.Start != nil {
		return json.Marshal(r.Start)
	}
	if r.List != nil {
		return json.Marshal(r.List)
	}
	if r.Get != nil {
		return json.Marshal(r.Get)
	}
	if r.Output != nil {
		return json.Marshal(r.Output)
	}
	if r.Follow != nil {
		return json.Marshal(r.Follow)
	}
	if r.Wait != nil {
		return json.Marshal(r.Wait)
	}
	if r.Signal != nil {
		return json.Marshal(r.Signal)
	}
	if r.Stop != nil {
		return json.Marshal(r.Stop)
	}
	if r.Restart != nil {
		return json.Marshal(r.Restart)
	}
	if r.Remove != nil {
		return json.Marshal(r.Remove)
	}
	if r.Shutdown != nil {
		return json.Marshal(r.Shutdown)
	}
	if r.InputAttach != nil {
		return json.Marshal(r.InputAttach)
	}
	if r.InputRelease != nil {
		return json.Marshal(r.InputRelease)
	}
	if r.InputWrite != nil {
		return json.Marshal(r.InputWrite)
	}
	if r.InputResize != nil {
		return json.Marshal(r.InputResize)
	}
	if r.Op == "" {
		return nil, MissingOperationError{}
	}
	if !IsKnown(r.Op) {
		return nil, &UnknownOperationError{Operation: r.Op}
	}
	return json.Marshal(struct {
		Op Operation `json:"op"`
		ID string    `json:"id,omitempty"`
	}{Op: r.Op, ID: r.ID})
}

// Encoder writes one bounded JSON message per line.
type Encoder struct {
	writer io.Writer
	limit  int
}

// NewEncoder constructs a bounded encoder. With no limit it uses
// DefaultMaxLineBytes; a non-positive limit also selects that safe default.
func NewEncoder(w io.Writer, maxLineBytes ...int) *Encoder {
	limit := DefaultMaxLineBytes
	if len(maxLineBytes) != 0 && maxLineBytes[0] > 0 {
		limit = maxLineBytes[0]
	}
	return &Encoder{writer: w, limit: limit}
}

// NewEncoderWithLimit constructs an encoder and rejects an invalid bound.
func NewEncoderWithLimit(w io.Writer, maxLineBytes int) (*Encoder, error) {
	if maxLineBytes <= 0 {
		return nil, fmt.Errorf("protocol: maximum line bytes must be positive")
	}
	return &Encoder{writer: w, limit: maxLineBytes}, nil
}

// MaxLineBytes returns the encoder's content-byte limit. The terminating
// newline is not counted.
func (e *Encoder) MaxLineBytes() int {
	if e == nil {
		return 0
	}
	return e.limit
}

// Encode marshals v, checks its line bound, and writes exactly one newline.
func (e *Encoder) Encode(v any) error {
	if e == nil || e.writer == nil || e.limit <= 0 {
		return errors.New("protocol: encoder is not initialized")
	}
	line, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return e.encodeLine(line)
}

// EncodeRequest encodes a request DTO. StartRequest.Env is allowed here; it is
// the only protocol operation that carries an environment.
func (e *Encoder) EncodeRequest(v any) error { return e.Encode(v) }

// EncodeResponse encodes a response or stream event and rejects any JSON
// object key named env, including nested details, before writing it. If a
// typed error response is oversized, retry with a compact typed error that
// preserves the error code.
func (e *Encoder) EncodeResponse(v any) error {
	if e == nil || e.writer == nil || e.limit <= 0 {
		return errors.New("protocol: encoder is not initialized")
	}
	line, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if containsJSONKey(line, "env") {
		return ErrResponseEnvironment
	}
	if len(line) > e.limit {
		if compact, ok := compactErrorResponse(v); ok {
			compactLine, compactErr := json.Marshal(compact)
			if compactErr != nil {
				return compactErr
			}
			if len(compactLine) <= e.limit {
				return e.encodeLine(compactLine)
			}
		}
	}
	return e.encodeLine(line)
}

func compactErrorResponse(v any) (ErrorResponse, bool) {
	var response ErrorResponse
	switch typed := v.(type) {
	case ErrorResponse:
		response = typed
	case *ErrorResponse:
		if typed == nil {
			return ErrorResponse{}, false
		}
		response = *typed
	default:
		return ErrorResponse{}, false
	}
	if response.Error == nil {
		return ErrorResponse{}, false
	}
	if response.Error.Code == ErrorUnknownOperation && !IsKnown(response.Op) {
		// An unknown operation cannot be echoed safely when it is itself
		// unbounded; an empty op is the stable response shape for it.
		response.Op = ""
	}
	response.Error = compactWireError(response.Error)
	return response, true
}

func compactWireError(err *WireError) *WireError {
	if err == nil {
		return nil
	}
	var message string
	switch err.Code {
	case ErrorMalformed:
		message = ErrMalformed.Error()
	case ErrorOversized:
		message = ErrOversized.Error()
	case ErrorUnknownOperation:
		message = ErrUnknownOperation.Error()
	default:
		message = "protocol error"
	}
	return NewWireError(err.Code, message, nil)
}

// EncodeLine is an alias for Encode.
func (e *Encoder) EncodeLine(v any) error { return e.Encode(v) }

func (e *Encoder) encodeLine(line []byte) error {
	if len(line) == 0 || line[0] != '{' {
		return &MalformedError{Err: errors.New("message must be a JSON object")}
	}
	if len(line) > e.limit {
		return &OversizedError{Size: len(line), Limit: e.limit}
	}
	message := append(line, '\n')
	n, err := e.writer.Write(message)
	if err != nil {
		return err
	}
	if n != len(message) {
		return io.ErrShortWrite
	}
	return nil
}

// MarshalLine returns one bounded NDJSON line without writing it.
func MarshalLine(v any, maxLineBytes ...int) ([]byte, error) {
	limit := DefaultMaxLineBytes
	if len(maxLineBytes) != 0 && maxLineBytes[0] > 0 {
		limit = maxLineBytes[0]
	}
	line, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if len(line) == 0 || line[0] != '{' {
		return nil, &MalformedError{Err: errors.New("message must be a JSON object")}
	}
	if len(line) > limit {
		return nil, &OversizedError{Size: len(line), Limit: limit}
	}
	return append(line, '\n'), nil
}

func containsJSONKey(raw []byte, key string) bool {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return false
	}
	return containsJSONKeyValue(value, key)
}

func containsJSONKeyValue(value any, key string) bool {
	switch typed := value.(type) {
	case map[string]any:
		if _, ok := typed[key]; ok {
			return true
		}
		for _, child := range typed {
			if containsJSONKeyValue(child, key) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if containsJSONKeyValue(child, key) {
				return true
			}
		}
	}
	return false
}
