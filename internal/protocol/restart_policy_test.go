package protocol

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestRestartPolicyProtocol(t *testing.T) {
	if Version != 11 {
		t.Fatalf("protocol version = %d, want immutable-since protocol version 11", Version)
	}

	start := NewStartRequest("api", []string{"server"}, "/project", nil)
	encoded, err := json.Marshal(start)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(encoded); !strings.Contains(got, `"restart":"never"`) {
		t.Fatalf("default start request = %s, want restart=never", got)
	}
	start.Restart = RestartOnFailure
	encoded, err = json.Marshal(start)
	if err != nil {
		t.Fatal(err)
	}
	var decodedStart StartRequest
	if err := json.Unmarshal(encoded, &decodedStart); err != nil {
		t.Fatal(err)
	}
	if decodedStart.Restart != RestartOnFailure {
		t.Fatalf("start restart = %q, want %q", decodedStart.Restart, RestartOnFailure)
	}

	restart := RestartRequest{Op: OpRestart, Name: "api", Cwd: "/project", Update: true, Source: "manifest", Restart: RestartOnFailure}
	encoded, err = json.Marshal(restart)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"restart":"on-failure"`) {
		t.Fatalf("definition update request = %s, want restart policy", encoded)
	}
	var decodedRestart RestartRequest
	if err := json.Unmarshal(encoded, &decodedRestart); err != nil {
		t.Fatal(err)
	}
	if decodedRestart.Restart != RestartOnFailure || !decodedRestart.Update {
		t.Fatalf("restart request = %#v", decodedRestart)
	}

	next := time.Date(2026, time.September, 5, 18, 0, 1, 0, time.UTC)
	process := Process{Name: "api", Source: "manifest", State: "exited", Restart: RestartOnFailure, Relaunches: 5, NextLaunchAt: &next}
	encoded, err = json.Marshal(process)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"restart":"on-failure"`, `"relaunches":5`, `"next_launch_at":"2026-09-05T18:00:01Z"`} {
		if !strings.Contains(string(encoded), field) {
			t.Fatalf("process snapshot = %s, missing %s", encoded, field)
		}
	}
	var decodedProcess Process
	if err := json.Unmarshal(encoded, &decodedProcess); err != nil {
		t.Fatal(err)
	}
	if decodedProcess.Restart != RestartOnFailure || decodedProcess.Relaunches != 5 || decodedProcess.NextLaunchAt == nil || !decodedProcess.NextLaunchAt.Equal(next) {
		t.Fatalf("decoded process = %#v", decodedProcess)
	}

	var legacy Process
	if err := json.Unmarshal([]byte(`{"name":"old","state":"exited","followers":0}`), &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.Restart != RestartNever || legacy.Relaunches != 0 || legacy.NextLaunchAt != nil {
		t.Fatalf("legacy process defaults = %#v", legacy)
	}
}
