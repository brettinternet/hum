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
	identity string
	netns    string
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
	// TCP tables are per network namespace; read them once per namespace
	// rather than once per member.
	tables := make(map[string]map[string]procListener)
	for _, member := range members {
		if ctx.Err() != nil {
			break
		}
		inodes, fdDiagnostics, fdDenied := linuxSocketInodes(ctx, member.pid)
		diagnostics = append(diagnostics, fdDiagnostics...)
		denied = denied || fdDenied
		if len(inodes) == 0 {
			continue
		}
		rows, cached := tables[member.netns]
		if !cached {
			var read, tableDenied bool
			var tableDiagnostics []string
			rows, read, tableDiagnostics, tableDenied = linuxListenRows(member.pid)
			diagnostics = append(diagnostics, tableDiagnostics...)
			denied = denied || tableDenied
			if read {
				tables[member.netns] = rows
			}
		}
		for inode := range inodes {
			if listener, ok := rows[inode]; ok {
				builder.add(member.netns+":"+inode, listener.address, listener.port, member.pid)
			}
		}
	}
	listeners := builder.listeners()
	valid := make(map[int]struct{}, len(members))
	for pid, member := range members {
		current, verifyErr := readProcStat(pid)
		if verifyErr != nil || !linuxLiveMember(current, pgid) || current.identity != member.identity {
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

func linuxLiveMember(stat procStat, pgid int) bool {
	return stat.pgid == pgid && stat.state != "Z" && stat.state != "X"
}

// linuxSocketInodes returns the socket inodes held by pid's descriptors.
func linuxSocketInodes(ctx context.Context, pid int) (map[string]struct{}, []string, bool) {
	fdDir := filepath.Join("/proc", strconv.Itoa(pid), "fd")
	fds, err := os.ReadDir(fdDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, false
		}
		return nil, []string{fmt.Sprintf("pid %d file descriptors: %v", pid, err)}, errors.Is(err, os.ErrPermission)
	}
	inodes := make(map[string]struct{})
	var diagnostics []string
	denied := false
	for _, fd := range fds {
		if ctx.Err() != nil {
			break
		}
		target, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				diagnostics = append(diagnostics, fmt.Sprintf("pid %d file descriptor: %v", pid, err))
				denied = denied || errors.Is(err, os.ErrPermission)
			}
			continue
		}
		if inode, ok := strings.CutPrefix(target, "socket:["); ok && strings.HasSuffix(inode, "]") {
			inodes[strings.TrimSuffix(inode, "]")] = struct{}{}
		}
	}
	return inodes, diagnostics, denied
}

// linuxListenRows reads LISTEN rows from pid's network namespace. read reports
// whether at least one table was observed, so a member that exits mid-read does
// not hide the namespace's listeners from other members.
func linuxListenRows(pid int) (map[string]procListener, bool, []string, bool) {
	rows := make(map[string]procListener)
	read := false
	var diagnostics []string
	denied := false
	for _, table := range []string{"tcp", "tcp6"} {
		data, err := readProcTCPTable(filepath.Join("/proc", strconv.Itoa(pid), "net", table))
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				diagnostics = append(diagnostics, fmt.Sprintf("pid %d %s table: %v", pid, table, err))
				denied = denied || errors.Is(err, os.ErrPermission)
			}
			continue
		}
		read = true
		parsed, err := parseProcTCPTable(data, table)
		if err != nil {
			diagnostics = append(diagnostics, fmt.Sprintf("pid %d %s table: %v", pid, table, err))
		}
		for inode, listener := range parsed {
			rows[inode] = listener
		}
	}
	return rows, read, diagnostics, denied
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
		if ctx.Err() != nil {
			return members, diagnostics, denied, nil
		}
		pid, parseErr := strconv.Atoi(entry.Name())
		if parseErr != nil || pid <= 0 {
			continue
		}
		stat, readErr := readProcStat(pid)
		if readErr != nil {
			if !errors.Is(readErr, os.ErrNotExist) {
				diagnostics = append(diagnostics, fmt.Sprintf("pid %d process identity: %v", pid, readErr))
				denied = denied || errors.Is(readErr, os.ErrPermission)
			}
			continue
		}
		if !linuxLiveMember(stat, pgid) {
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
		members[pid] = linuxMember{pid: pid, identity: stat.identity, netns: namespace}
	}
	return members, diagnostics, denied, nil
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
