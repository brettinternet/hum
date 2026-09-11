package cli

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"hum/internal/app"
	"hum/internal/output"
	"hum/internal/protocol"
)

func TestCLIMachineOutputV1Contract(t *testing.T) {
	process := app.Process{
		Name: "api", Source: "manifest", Scope: "project", Root: "/project",
		Cwd: "/project", Argv: []string{"api"}, State: app.StateRunning,
		Start: time.Unix(1, 0).UTC(),
	}
	cases := []struct {
		name     string
		value    any
		required []string
	}{
		{name: "aggregate", value: processListJSON([]app.Process{process}, nil), required: []string{"schema_version", "processes"}},
		{name: "single process", value: statusJSONFor(process), required: []string{"schema_version", "name", "scope", "tty", "pid", "pgid", "cwd", "argv", "started_at", "state", "exit_status", "restart_count", "followers", "restart", "relaunches", "stop_grace", "stop_grace_inherited", "next_cursor"}},
		{name: "lifecycle result", value: stopResult{Name: "api", Status: "stopped"}, required: []string{"schema_version", "name", "status"}},
		{name: "bounded log", value: protocol.NewOutputResponse(outputJSON(output.ReadResult{Entries: []output.Entry{{Cursor: 1, Stream: output.Stdout, Time: time.Unix(2, 0).UTC(), Text: "ready\n"}}})), required: []string{"schema_version", "op", "ok", "entries"}},
		{name: "NDJSON stream", value: eventJSON("api", output.Event{Read: &output.ReadResult{Entries: []output.Entry{{Cursor: 2, Stream: output.Stdout, Time: time.Unix(3, 0).UTC(), Text: "running\n"}}}}), required: []string{"schema_version", "op", "type", "name", "entries"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			assertCLIMachineOutputV1(t, test.value, test.required...)
		})
	}

	t.Run("terminal error", func(t *testing.T) {
		var encoded bytes.Buffer
		if err := writeJSONError(&encoded, &protocol.WireError{Code: "usage", Message: "bad input"}); err != nil {
			t.Fatal(err)
		}
		assertCLIMachineOutputV1Document(t, encoded.Bytes(), "schema_version", "error")
	})
}

func assertCLIMachineOutputV1(t *testing.T, value any, required ...string) {
	t.Helper()
	var encoded bytes.Buffer
	if err := encodeJSON(&encoded, value); err != nil {
		t.Fatal(err)
	}
	assertCLIMachineOutputV1Document(t, encoded.Bytes(), required...)
}

func assertCLIMachineOutputV1Document(t *testing.T, encoded []byte, required ...string) {
	t.Helper()
	if len(encoded) == 0 || encoded[len(encoded)-1] != '\n' {
		t.Fatalf("machine output is not newline terminated: %q", encoded)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatalf("decode machine output: %v (%q)", err, encoded)
	}
	var version int
	if err := json.Unmarshal(document["schema_version"], &version); err != nil || version != 1 {
		t.Fatalf("schema_version = %d, %v; want 1 (%q)", version, err, encoded)
	}
	for _, field := range required {
		if _, ok := document[field]; !ok {
			t.Errorf("required field %q omitted from %q", field, encoded)
		}
	}
}
