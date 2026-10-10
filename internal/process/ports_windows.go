//go:build windows

package process

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	windowsAFInet       = 2
	windowsAFInet6      = 23
	tcpTableOwnerListen = 3
	windowsTCPv4RowSize = 24
	windowsTCPv6RowSize = 56
)

var iphlpapi = windows.NewLazySystemDLL("iphlpapi.dll")

func (c *Child) InspectPorts(ctx context.Context) PortsResult {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if c == nil {
		return emptyPorts(PortsUnavailable, "process job is unavailable")
	}
	c.mu.Lock()
	if c.groupEnded || c.ownedJobHandle == 0 {
		c.mu.Unlock()
		return emptyPorts(PortsUnavailable, "process job is no longer available")
	}
	job := c.ownedJobHandle
	beforeIDs, err := jobProcessIDs(job)
	c.mu.Unlock()
	if err != nil {
		state := PortsUnavailable
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			state = PortsDenied
		}
		return emptyPorts(state, fmt.Sprintf("read owned job membership: %v", err))
	}
	before := make(map[int]string, len(beforeIDs))
	// This API identifies the binding owner, not every holder of an inherited
	// socket. Even an empty table cannot establish complete holder coverage.
	diagnostics := []string{"Windows reports binding-owner PIDs only; inherited socket holders and socket identity are unavailable"}
	denied := false
	for _, rawPID := range beforeIDs {
		pid := int(rawPID)
		identity, identityErr := ProcessStartIdentity(pid)
		if identityErr != nil {
			diagnostics = append(diagnostics, fmt.Sprintf("pid %d process identity: %v", pid, identityErr))
			denied = denied || errors.Is(identityErr, windows.ERROR_ACCESS_DENIED)
			continue
		}
		before[pid] = identity
	}
	if identity, ok := before[c.pid]; ok && identity != c.startIdentity {
		return emptyPorts(PortsUnavailable, "launch identity changed during listener inspection")
	}
	builder := make(portBuilder)
	for _, family := range []int{windowsAFInet, windowsAFInet6} {
		if err := ctx.Err(); err != nil {
			diagnostics = append(diagnostics, err.Error())
			break
		}
		rows, tableErr := windowsTCPListeners(family)
		if tableErr != nil {
			diagnostics = append(diagnostics, tableErr.Error())
			denied = denied || errors.Is(tableErr, windows.ERROR_ACCESS_DENIED)
			continue
		}
		for index, row := range rows {
			if _, owned := before[row.pid]; !owned {
				continue
			}
			// No socket identifier is exposed. Keep each table row separate;
			// equal endpoints do not prove that two owners share a socket.
			builder.add(fmt.Sprintf("%d:%d", family, index), row.address, row.port, row.pid)
		}
	}
	listeners := builder.listeners()
	c.mu.Lock()
	if c.groupEnded || c.ownedJobHandle == 0 {
		c.mu.Unlock()
		return emptyPorts(PortsUnavailable, "process job ended during listener inspection")
	}
	currentIDs, membershipErr := jobProcessIDs(c.ownedJobHandle)
	c.mu.Unlock()
	current := make(map[int]string, len(currentIDs))
	if membershipErr != nil {
		state := PortsUnavailable
		if errors.Is(membershipErr, windows.ERROR_ACCESS_DENIED) {
			state = PortsDenied
		}
		// Never retain owners whose membership could not be reconfirmed.
		return emptyPorts(state, fmt.Sprintf("recheck owned job membership: %v", membershipErr))
	} else {
		for _, rawPID := range currentIDs {
			pid := int(rawPID)
			identity, identityErr := ProcessStartIdentity(pid)
			if identityErr != nil {
				diagnostics = append(diagnostics, fmt.Sprintf("pid %d membership recheck: %v", pid, identityErr))
				denied = denied || errors.Is(identityErr, windows.ERROR_ACCESS_DENIED)
				continue
			}
			current[pid] = identity
		}
		for index := range listeners {
			pids := listeners[index].PIDs[:0]
			for _, pid := range listeners[index].PIDs {
				if before[pid] == current[pid] {
					pids = append(pids, pid)
				}
			}
			listeners[index].PIDs = pids
		}
		listeners = compactPorts(listeners)
	}
	state, diagnostic := finishPortInspection(ctx, listeners, diagnostics, denied)
	return PortsResult{State: state, Listeners: listeners, Diagnostic: diagnostic}
}

type windowsListener struct {
	pid     int
	address string
	port    uint16
}

func windowsTCPListeners(family int) ([]windowsListener, error) {
	procedure := iphlpapi.NewProc("GetExtendedTcpTable")
	if err := procedure.Find(); err != nil {
		return nil, fmt.Errorf("load GetExtendedTcpTable: %w", err)
	}
	for attempt := 0; attempt < 3; attempt++ {
		var size uint32
		result, _, _ := procedure.Call(0, uintptr(unsafe.Pointer(&size)), 0, uintptr(family), tcpTableOwnerListen, 0)
		if result != uintptr(syscall.Errno(122)) && result != 0 {
			return nil, fmt.Errorf("GetExtendedTcpTable family %d: %w", family, syscall.Errno(result))
		}
		if size == 0 {
			return []windowsListener{}, nil
		}
		if size > 16<<20 {
			return nil, errors.New("GetExtendedTcpTable exceeded the 16 MiB inspection limit")
		}
		buffer := make([]byte, size)
		result, _, _ = procedure.Call(uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&size)), 0, uintptr(family), tcpTableOwnerListen, 0)
		if result == uintptr(syscall.Errno(122)) {
			continue
		}
		if result != 0 {
			return nil, fmt.Errorf("GetExtendedTcpTable family %d: %w", family, syscall.Errno(result))
		}
		if len(buffer) < 4 {
			return nil, errors.New("GetExtendedTcpTable returned a truncated table")
		}
		count := int(windowsLittleEndianUint32(buffer[:4]))
		rowSize := windowsTCPv4RowSize
		if family == windowsAFInet6 {
			rowSize = windowsTCPv6RowSize
		}
		if count > (len(buffer)-4)/rowSize {
			return nil, errors.New("GetExtendedTcpTable returned an invalid row count")
		}
		rows := make([]windowsListener, 0, count)
		for index := 0; index < count; index++ {
			row := buffer[4+index*rowSize : 4+(index+1)*rowSize]
			var address string
			var portOffset, pidOffset int
			if family == windowsAFInet {
				address = net.IPv4(row[4], row[5], row[6], row[7]).String()
				portOffset, pidOffset = 8, 20
			} else {
				address = net.IP(row[:16]).String()
				scope := windowsLittleEndianUint32(row[16:20])
				if scope != 0 {
					address += "%" + strconv.FormatUint(uint64(scope), 10)
				}
				portOffset, pidOffset = 20, 52
			}
			port := uint16(row[portOffset])<<8 | uint16(row[portOffset+1])
			pid := int(windowsLittleEndianUint32(row[pidOffset : pidOffset+4]))
			if pid > 0 && port > 0 {
				rows = append(rows, windowsListener{pid: pid, address: address, port: port})
			}
		}
		return rows, nil
	}
	return nil, errors.New("GetExtendedTcpTable changed repeatedly during snapshot")
}

func windowsLittleEndianUint32(value []byte) uint32 {
	return uint32(value[0]) | uint32(value[1])<<8 | uint32(value[2])<<16 | uint32(value[3])<<24
}
