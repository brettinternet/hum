package mcp

import (
	"context"
	"errors"
	"regexp"

	"hum/internal/protocol"
)

func (s *Server) events(ctx context.Context, input commonInput) (any, error) {
	if input.Tail == 0 {
		input.Tail = 50
	}
	if input.Tail < 1 || input.Tail > 2000 {
		return nil, &ToolError{Code: "invalid_request", Message: "tail must be between 1 and 2000"}
	}
	if input.Match != "" {
		if _, err := regexp.Compile(input.Match); err != nil {
			return nil, &ToolError{Code: "invalid_request", Message: "match must be a valid regular expression"}
		}
	}
	client, err := s.client(ctx, false)
	if err != nil {
		if client != nil {
			_ = client.Close()
			client = nil
		}
		if s.opts.EventReader == nil {
			return nil, mapError(err)
		}
	}
	if client != nil {
		defer client.Close()
	}
	var after *protocol.Cursor
	if input.AfterCursor != nil {
		value := protocol.Cursor(*input.AfterCursor)
		after = &value
	}
	request := protocol.EventsRequest{Scope: input.Scope, Root: input.ProjectRoot, Cwd: input.ProjectRoot, Names: append([]string(nil), input.Names...), SinceUnixNano: input.SinceUnixNano, Kinds: append([]protocol.EventKind(nil), input.Kinds...), Failed: input.Failed, Match: input.Match, Tail: input.Tail, AfterCursor: after, MaxBytes: input.MaxBytes}
	var response protocol.EventsResponse
	if client == nil {
		response, err = s.opts.EventReader(ctx, request)
	} else if events, ok := client.(eventsClient); ok {
		response, err = events.Events(ctx, request)
		var wire *protocol.WireError
		if err != nil && s.opts.EventReader != nil && errors.As(err, &wire) && wire.Code == protocol.ErrorUnknownOperation {
			response, err = s.opts.EventReader(ctx, request)
		}
	} else {
		return nil, &ToolError{Code: "unavailable", Message: "daemon does not support event history"}
	}
	if err != nil {
		return nil, mapError(err)
	}
	return map[string]any{
		"events":      response.Events,
		"next_cursor": response.NextCursor,
		"truncated":   response.Truncated,
		"has_more":    response.HasMore,
	}, nil
}
