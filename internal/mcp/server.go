package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
)

const (
	mcpProtocolVersion = "2025-06-18"
	maxMessageBytes    = 4 << 20
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type callToolParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type textContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type callToolResult struct {
	Content           []textContent `json:"content"`
	StructuredContent any           `json:"structuredContent,omitempty"`
	IsError           bool          `json:"isError,omitempty"`
}

// Serve runs the MCP server over newline-delimited JSON-RPC until EOF or cancellation.
func (s *Server) Serve(ctx context.Context, reader io.Reader, writer io.Writer) error {
	if reader == nil || writer == nil {
		return errors.New("MCP stdio reader and writer are required")
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), maxMessageBytes)
	encoder := json.NewEncoder(writer)
	var writeMu sync.Mutex
	writeResponse := func(response rpcResponse) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return encoder.Encode(response)
	}
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line := append([]byte(nil), scanner.Bytes()...)
		var request rpcRequest
		if err := json.Unmarshal(line, &request); err != nil {
			if err := writeResponse(rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{Code: -32700, Message: "parse error"}}); err != nil {
				return err
			}
			continue
		}
		if request.JSONRPC != "2.0" || request.Method == "" {
			if len(request.ID) != 0 {
				if err := writeResponse(rpcResponse{JSONRPC: "2.0", ID: request.ID, Error: &rpcError{Code: -32600, Message: "invalid request"}}); err != nil {
					return err
				}
			}
			continue
		}
		if len(request.ID) == 0 {
			// notifications/initialized, notifications/cancelled, and unknown notifications have no response.
			continue
		}
		result, rpcErr := s.handleRequest(ctx, request)
		if err := writeResponse(rpcResponse{JSONRPC: "2.0", ID: request.ID, Result: result, Error: rpcErr}); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return fmt.Errorf("MCP message exceeds %d bytes", maxMessageBytes)
		}
		return err
	}
	return nil
}

func (s *Server) handleRequest(ctx context.Context, request rpcRequest) (any, *rpcError) {
	switch request.Method {
	case "initialize":
		version := "dev"
		if s != nil && s.opts.Version != "" {
			version = s.opts.Version
		}
		return map[string]any{
			"protocolVersion": mcpProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]string{"name": "hum", "version": version},
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": s.toolDefinitions()}, nil
	case "tools/call":
		var params callToolParams
		if err := json.Unmarshal(request.Params, &params); err != nil || params.Name == "" {
			return nil, &rpcError{Code: -32602, Message: "invalid tools/call params"}
		}
		if len(params.Arguments) == 0 || string(params.Arguments) == "null" {
			params.Arguments = json.RawMessage("{}")
		}
		value, err := s.callTool(ctx, params.Name, params.Arguments)
		if err != nil {
			mapped := mapError(err)
			text, _ := json.Marshal(mapped)
			return callToolResult{Content: []textContent{{Type: "text", Text: string(text)}}, StructuredContent: mapped, IsError: true}, nil
		}
		text, err := json.Marshal(value)
		if err != nil {
			return nil, &rpcError{Code: -32603, Message: "failed to encode tool result"}
		}
		return callToolResult{Content: []textContent{{Type: "text", Text: string(text)}}, StructuredContent: value}, nil
	default:
		return nil, &rpcError{Code: -32601, Message: "method not found"}
	}
}
