//go:build linux

package process

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type linuxMember struct {
	pid      int
	pgid     int
	identity string
	netns    string
	fdInodes map[string]struct{}
}

func inspectGroupPorts(ctx context.Context, pgid, leaderPID int, leaderIdentity string) PortsResult {
	if ctx == nil {
		ctx = context.Background()
	}
	if pgid <= 0 || leaderPID <= 0 || leaderIdentity == "" {
		return emptyPorts(PortsUnavailable, "launch identity is unavailable")
	}
	members, diagnostics, denied, err := linuxGroupMembers(ctx, pgid)
	if err != nil {
		if denied {
			return emptyPorts(PortsDenied, err.Error())
		}
		return emptyPorts(PortsUnavailable, err.Error())
	}
	if member, ok := members[leaderPID]; ok && member.identity != leaderIdentity {
		return emptyPorts(PortsUnavailable, "launch identity changed during listener inspection")
	}
	if len(members) == 0 {
		state, diagnostic := finishPortInspection(ctx, nil, diagnostics, denied)
		return emptyPorts(state, diagnostic)
	}
	builder := make(portBuilder)
	for _, member := range members {
		if err := ctx.Err(); err != nil {
			diagnostics = append(diagnostics, err.Error())
			break
		}
		member.fdInodes = make(map[string]struct{})
		fdDir := filepath.Join("/proc", strconv.Itoa(member.pid), "fd")
		fds, readErr := os.ReadDir(fdDir)
		if readErr != nil {
			if !errors.Is(readErr, os.ErrNotExist) {
				diagnostics = append(diagnostics, fmt.Sprintf("pid %d file descriptors: %v", member.pid, readErr))
				denied = denied || errors.Is(readErr, os.ErrPermission)
			}
			continue
		}
		for _, fd := range fds {
			if err := ctx.Err(); err != nil {
				break
			}
			target, linkErr := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if linkErr != nil {
				if !errors.Is(linkErr, os.ErrNotExist) {
					diagnostics = append(diagnostics, fmt.Sprintf("pid %d file descriptor: %v", member.pid, linkErr))
					denied = denied || errors.Is(linkErr, os.ErrPermission)
				}
				continue
			}
			if strings.HasPrefix(target, "socket:[") && strings.HasSuffix(target, "]") {
				member.fdInodes[strings.TrimSuffix(strings.TrimPrefix(target, "socket:["), "]")] = struct{}{}
			}
		}
		if len(member.fdInodes) == 0 {
			continue
		}
		for _, table := range []struct{ file, family string }{{"tcp", "tcp"}, {"tcp6", "tcp6"}} {
			data, tableErr := readProcTCPTable(filepath.Join("/proc", strconv.Itoa(member.pid), "net", table.file))
			if tableErr != nil {
				if !errors.Is(tableErr, os.ErrNotExist) {
					diagnostics = append(diagnostics, fmt.Sprintf("pid %d %s table: %v", member.pid, table.file, tableErr))
					denied = denied || errors.Is(tableErr, os.ErrPermission)
				}
				continue
			}
			rows, parseErr := parseProcTCPTable(data, table.family)
			if parseErr != nil {
				diagnostics = append(diagnostics, fmt.Sprintf("pid %d %s table: %v", member.pid, table.file, parseErr))
			}
			for inode, listener := range rows {
				if _, held := member.fdInodes[inode]; held {
					builder.add(member.netns+":"+inode, listener.address, listener.port, member.pid)
				}
			}
		}
	}
	listeners := builder.listeners()
	valid := make(map[int]struct{}, len(members))
	for pid, member := range members {
		current, state, verifyErr := linuxReadMember(pid)
		if verifyErr != nil || state == "Z" || current.identity != member.identity || current.pgid != pgid {
			if verifyErr != nil && !errors.Is(verifyErr, os.ErrNotExist) {
				diagnostics = append(diagnostics, fmt.Sprintf("pid %d membership recheck: %v", pid, verifyErr))
				denied = denied || errors.Is(verifyErr, os.ErrPermission)
			}
			continue
		}
		valid[pid] = struct{}{}
	}
	for index := range listeners {
		pids := listeners[index].PIDs[:0]
		for _, pid := range listeners[index].PIDs {
			if _, ok := valid[pid]; ok {
				pids = append(pids, pid)
			}
		}
		listeners[index].PIDs = pids
	}
	listeners = compactPorts(listeners)
	state, diagnostic := finishPortInspection(ctx, listeners, diagnostics, denied)
	return PortsResult{State: state, Listeners: listeners, Diagnostic: diagnostic}
}

func linuxGroupMembers(ctx context.Context, pgid int) (map[int]linuxMember, []string, bool, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, nil, errors.Is(err, os.ErrPermission), fmt.Errorf("read procfs: %w", err)
	}
	members := make(map[int]linuxMember)
	var diagnostics []string
	denied := false
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return members, append(diagnostics, err.Error()), denied, nil
		}
		pid, parseErr := strconv.Atoi(entry.Name())
		if parseErr != nil || pid <= 0 {
			continue
		}
		member, state, readErr := linuxReadMember(pid)
		if readErr != nil {
			if !errors.Is(readErr, os.ErrNotExist) {
				diagnostics = append(diagnostics, fmt.Sprintf("pid %d process identity: %v", pid, readErr))
				denied = denied || errors.Is(readErr, os.ErrPermission)
			}
			continue
		}
		if member.pgid != pgid || state == "Z" || state == "X" {
			continue
		}
		namespace, nsErr := os.Readlink(filepath.Join("/proc", entry.Name(), "ns", "net"))
		if nsErr != nil {
			if !errors.Is(nsErr, os.ErrNotExist) {
				diagnostics = append(diagnostics, fmt.Sprintf("pid %d network namespace: %v", pid, nsErr))
				denied = denied || errors.Is(nsErr, os.ErrPermission)
			}
			continue
		}
		member.netns = namespace
		members[pid] = member
	}
	return members, diagnostics, denied, nil
}

func linuxReadMember(pid int) (linuxMember, string, error) {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return linuxMember{}, "", err
	}
	closeParen := strings.LastIndexByte(string(data), ')')
	if closeParen < 0 || closeParen+1 >= len(data) {
		return linuxMember{}, "", errors.New("malformed procfs stat")
	}
	fields := strings.Fields(string(data[closeParen+1:]))
	if len(fields) <= 19 {
		return linuxMember{}, "", errors.New("procfs stat omitted start identity")
	}
	pgid, err := strconv.Atoi(fields[2])
	if err != nil {
		return linuxMember{}, "", errors.New("invalid process group id")
	}
	startTime, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil || startTime == 0 {
		return linuxMember{}, "", errors.New("invalid process start time")
	}
	boot, err := bootIdentity()
	if err != nil {
		return linuxMember{}, "", err
	}
	return linuxMember{pid: pid, pgid: pgid, identity: "procfs:" + boot + ":" + strconv.FormatUint(startTime, 10)}, fields[0], nil
}

type procListener struct {
	address string
	port    uint16
}

func readProcTCPTable(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	const limit = 16 << 20
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if len(data) > limit {
		return nil, errors.New("TCP table exceeded the 16 MiB inspection limit")
	}
	return data, err
}

func parseProcTCPTable(data []byte, family string) (map[string]procListener, error) {
	rows := make(map[string]procListener)
	var diagnostic string
	for lineNumber, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] == "sl" {
			continue
		}
		if len(fields) < 10 {
			diagnostic = fmt.Sprintf("malformed row %d", lineNumber+1)
			continue
		}
		if fields[3] != "0A" {
			continue
		}
		address, port, err := parseProcAddress(fields[1], family)
		if err != nil {
			diagnostic = fmt.Sprintf("malformed row %d: %v", lineNumber+1, err)
			continue
		}
		if _, err := strconv.ParseUint(fields[9], 10, 64); err != nil {
			diagnostic = fmt.Sprintf("malformed row %d: invalid socket inode", lineNumber+1)
			continue
		}
		rows[fields[9]] = procListener{address: address, port: port}
	}
	if diagnostic != "" {
		return rows, errors.New(diagnostic)
	}
	return rows, nil
}

func parseProcAddress(value, family string) (string, uint16, error) {
	addressHex, portHex, ok := strings.Cut(value, ":")
	if !ok || len(portHex) != 4 {
		return "", 0, errors.New("invalid local endpoint")
	}
	portValue, err := strconv.ParseUint(portHex, 16, 16)
	if err != nil {
		return "", 0, errors.New("invalid local port")
	}
	var address net.IP
	switch family {
	case "tcp":
		if len(addressHex) != 8 {
			return "", 0, errors.New("invalid IPv4 address")
		}
		bytes, err := decodeProcWords(addressHex)
		if err != nil {
			return "", 0, err
		}
		address = net.IP(bytes)
	case "tcp6":
		if len(addressHex) != 32 {
			return "", 0, errors.New("invalid IPv6 address")
		}
		bytes, err := decodeProcWords(addressHex)
		if err != nil {
			return "", 0, err
		}
		address = net.IP(bytes)
	default:
		return "", 0, errors.New("unknown address family")
	}
	return address.String(), uint16(portValue), nil
}

func decodeProcWords(value string) ([]byte, error) {
	decoded := make([]byte, len(value)/2)
	for word := 0; word < len(decoded); word += 4 {
		for index := 0; index < 4; index++ {
			byteValue, err := strconv.ParseUint(value[2*(word+index):2*(word+index+1)], 16, 8)
			if err != nil {
				return nil, errors.New("invalid address hex")
			}
			decoded[word+3-index] = byte(byteValue)
		}
	}
	return decoded, nil
}
