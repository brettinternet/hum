package protocol

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestPortsProtocolRoundTripAndOptInRequest(t *testing.T) {
	request := NewGetRequest("api", "/work")
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"ports"`) {
		t.Fatalf("default get request includes port inspection: %s", encoded)
	}
	request.Ports = true
	encoded, err = json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var decodedRequest GetRequest
	if err := json.Unmarshal(encoded, &decodedRequest); err != nil {
		t.Fatal(err)
	}
	if decodedRequest.Op != OpGet || decodedRequest.Name != "api" || !decodedRequest.Ports {
		t.Fatalf("decoded get request = %#v", decodedRequest)
	}

	process := Process{
		Name: "api", Root: "/work", Cwd: "/work", State: StateRunning,
		Ports: &PortInspection{
			State: "partial", Diagnostic: "member exited during snapshot",
			Listeners: []PortEndpoint{{Transport: "tcp", Address: "::1", Port: 8080, PIDs: []int{41, 42}}},
		},
	}
	encoded, err = json.Marshal(process)
	if err != nil {
		t.Fatal(err)
	}
	var decodedProcess Process
	if err := json.Unmarshal(encoded, &decodedProcess); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decodedProcess.Ports, process.Ports) {
		t.Fatalf("decoded port inspection = %#v, want %#v", decodedProcess.Ports, process.Ports)
	}
}
