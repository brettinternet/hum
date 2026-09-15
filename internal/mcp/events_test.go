package mcp

import (
	"strings"
	"testing"
)

func TestEvents(t *testing.T) {
	var found bool
	for _, definition := range NewServer(Options{}).toolDefinitions() {
		if definition.Name == "events" {
			found = true
			if strings.Contains(strings.ToLower(definition.Description), "follow") == false {
				t.Fatal("events description does not document bounded semantics")
			}
		}
		if strings.Contains(definition.Name, "follow") {
			t.Fatalf("unbounded follow tool exposed: %s", definition.Name)
		}
	}
	if !found {
		t.Fatal("events tool missing")
	}
}
