package protocol

import (
	"bytes"
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

func TestPortLookupRequestAndResponseRoundTrip(t *testing.T) {
	request := Request{Op: OpPortLookup, PortLookup: &PortLookupRequest{Port: 3000}}
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(encoded), `{"op":"port_lookup","port":3000}`; got != want {
		t.Fatalf("port lookup request = %s, want %s", got, want)
	}
	decoded, err := NewDecoder(bytes.NewReader(append(encoded, '\n'))).DecodeRequest()
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Op != OpPortLookup || decoded.PortLookup == nil || decoded.PortLookup.Port != 3000 {
		t.Fatalf("decoded port lookup request = %#v", decoded)
	}

	response := PortLookupResponse{
		Op: OpPortLookup, OK: true,
		Processes:  []Process{{Name: "web", Scope: ScopeProject, Root: "/work/b", State: StateRunning, Restart: RestartNever, Ports: &PortInspection{State: "available", Listeners: []PortEndpoint{{Transport: "tcp", Address: "127.0.0.1", Port: 3000, PIDs: []int{42}}}}}},
		Incomplete: []PortLookupIncomplete{{Name: "uncertain", Scope: ScopeGlobal, ProjectRoot: "", State: "denied", Diagnostic: "permission denied"}},
	}
	var wire bytes.Buffer
	if err := NewEncoder(&wire).EncodeResponse(response); err != nil {
		t.Fatal(err)
	}
	decodedResponse, err := NewDecoder(&wire).DecodeResponse()
	if err != nil {
		t.Fatal(err)
	}
	if decodedResponse.PortLookup == nil || !reflect.DeepEqual(decodedResponse.PortLookup.Processes, response.Processes) || !reflect.DeepEqual(decodedResponse.PortLookup.Incomplete, response.Incomplete) {
		t.Fatalf("decoded port lookup response = %#v, want %#v", decodedResponse.PortLookup, response)
	}
}
