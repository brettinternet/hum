package process

import (
	"context"
	"testing"
)

// Inspection failures must not be rendered as a successful empty snapshot.
func TestPortsInspectionOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name        string
		listeners   []Port
		diagnostics []string
		denied      bool
		cancelled   bool
		want        string
	}{
		{name: "empty", want: PortsAvailable},
		{name: "denied", diagnostics: []string{"permission denied"}, denied: true, want: PortsDenied},
		{name: "partial", listeners: []Port{{Port: 8080}}, diagnostics: []string{"permission denied"}, denied: true, want: PortsPartial},
		{name: "cancelled", cancelled: true, want: PortsUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancelled {
				cancel()
			}
			state, diagnostic := finishPortInspection(ctx, tc.listeners, tc.diagnostics, tc.denied)
			if state != tc.want || (diagnostic == "") != (tc.want == PortsAvailable) {
				t.Fatalf("state=%s diagnostic=%q, want %s", state, diagnostic, tc.want)
			}
		})
	}
}
