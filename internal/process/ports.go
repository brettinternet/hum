package process

import (
	"context"
	"sort"
	"strings"
)

const (
	PortsAvailable   = "available"
	PortsPartial     = "partial"
	PortsDenied      = "denied"
	PortsUnavailable = "unavailable"
)

// Port is one TCP listening socket observed in an owned process group.
type Port struct {
	Transport string `json:"transport"`
	Address   string `json:"address"`
	Port      uint16 `json:"port"`
	PIDs      []int  `json:"pids"`
}

// PortsResult is the opt-in, bounded result of one listener snapshot.
type PortsResult struct {
	State      string `json:"state"`
	Listeners  []Port `json:"listeners"`
	Diagnostic string `json:"diagnostic,omitempty"`
}

// PortInspector is implemented by supervised children with a platform-native
// way to enumerate members of their owned process group.
type PortInspector interface {
	InspectPorts(context.Context) PortsResult
}

type portKey struct {
	identity string
	address  string
	port     uint16
}

type portBuilder map[portKey]map[int]struct{}

func (b portBuilder) add(identity, address string, port uint16, pid int) {
	key := portKey{identity: identity, address: address, port: port}
	if b[key] == nil {
		b[key] = make(map[int]struct{})
	}
	b[key][pid] = struct{}{}
}

func (b portBuilder) listeners() []Port {
	result := make([]Port, 0, len(b))
	for key, pids := range b {
		entry := Port{Transport: "tcp", Address: key.address, Port: key.port, PIDs: make([]int, 0, len(pids))}
		for pid := range pids {
			entry.PIDs = append(entry.PIDs, pid)
		}
		sort.Ints(entry.PIDs)
		result = append(result, entry)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Address != result[j].Address {
			return result[i].Address < result[j].Address
		}
		if result[i].Port != result[j].Port {
			return result[i].Port < result[j].Port
		}
		for index := 0; index < len(result[i].PIDs) && index < len(result[j].PIDs); index++ {
			if result[i].PIDs[index] != result[j].PIDs[index] {
				return result[i].PIDs[index] < result[j].PIDs[index]
			}
		}
		return len(result[i].PIDs) < len(result[j].PIDs)
	})
	return result
}

func emptyPorts(state, diagnostic string) PortsResult {
	return PortsResult{State: state, Listeners: []Port{}, Diagnostic: diagnostic}
}

func compactPorts(items []Port) []Port {
	result := items[:0]
	for _, item := range items {
		if len(item.PIDs) != 0 {
			result = append(result, item)
		}
	}
	return result
}

func joinDiagnostics(items []string) string {
	var result strings.Builder
	for _, item := range items {
		if item == "" {
			continue
		}
		if result.Len() != 0 {
			result.WriteString("; ")
		}
		if result.Len()+len(item) > 512 {
			remaining := 512 - result.Len()
			if remaining > 0 {
				result.WriteString(item[:remaining])
			}
			result.WriteString("...")
			break
		}
		result.WriteString(item)
	}
	return result.String()
}

func finishPortInspection(ctx context.Context, listeners []Port, diagnostics []string, denied bool) (string, string) {
	diagnostic := joinDiagnostics(diagnostics)
	state := PortsAvailable
	if diagnostic != "" {
		state = PortsPartial
		if len(listeners) == 0 && denied {
			state = PortsDenied
		}
	}
	if err := ctx.Err(); err != nil {
		diagnostics = append(diagnostics, err.Error())
		diagnostic = joinDiagnostics(diagnostics)
		if len(listeners) == 0 {
			state = PortsUnavailable
		} else {
			state = PortsPartial
		}
	}
	return state, diagnostic
}

func isInspectionDenied(message string) bool {
	lower := strings.ToLower(message)
	return strings.Contains(lower, "permission denied") || strings.Contains(lower, "operation not permitted") || strings.Contains(lower, "not permitted")
}
