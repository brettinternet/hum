//go:build darwin

package process

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

const lsofOutputLimit = 1 << 20

type darwinMember struct {
	pid      int
	identity string
}

type lsofListener struct {
	pid      int
	identity string
	address  string
	port     uint16
}

func inspectGroupPorts(ctx context.Context, pgid, leaderPID int, leaderIdentity string) PortsResult {
	if ctx == nil {
		ctx = context.Background()
	}
	if pgid <= 0 || leaderPID <= 0 || leaderIdentity == "" {
		return emptyPorts(PortsUnavailable, "launch identity is unavailable")
	}
	members, diagnostics, denied, err := darwinGroupMembers(pgid)
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
	pids := make([]int, 0, len(members))
	for pid := range members {
		pids = append(pids, pid)
	}
	sort.Ints(pids)
	builder := make(portBuilder)
	for start := 0; start < len(pids); start += 64 {
		if err := ctx.Err(); err != nil {
			diagnostics = append(diagnostics, err.Error())
			break
		}
		end := start + 64
		if end > len(pids) {
			end = len(pids)
		}
		chunk := pids[start:end]
		arguments := []string{"-nP", "-a", "-p", joinPIDs(chunk), "-iTCP", "-sTCP:LISTEN", "-FpftdPin"}
		stdout := &cappedBuffer{limit: lsofOutputLimit}
		stderr := &cappedBuffer{limit: 64 * 1024}
		command := exec.CommandContext(ctx, "/usr/sbin/lsof", arguments...)
		command.Stdout, command.Stderr = stdout, stderr
		commandErr := command.Run()
		if stdout.exceeded || stderr.exceeded {
			diagnostics = append(diagnostics, "lsof output exceeded the 1 MiB inspection limit")
		}
		parsed, parseErr := parseLsofFields(stdout.Bytes())
		if parseErr != nil {
			diagnostics = append(diagnostics, parseErr.Error())
		}
		for _, endpoint := range parsed {
			if _, member := members[endpoint.pid]; !member {
				continue
			}
			builder.add("lsof:"+endpoint.identity, endpoint.address, endpoint.port, endpoint.pid)
		}
		if commandErr != nil {
			var exitError *exec.ExitError
			noMatches := errors.As(commandErr, &exitError) && exitError.ExitCode() == 1 && stdout.Len() == 0 && stderr.Len() == 0
			if noMatches {
				continue
			}
			message := strings.TrimSpace(stderr.String())
			if message == "" {
				message = commandErr.Error()
			}
			diagnostics = append(diagnostics, "lsof: "+message)
			denied = denied || isInspectionDenied(message)
			// An absolute path that does not exist fails with ENOENT, not
			// exec.ErrNotFound.
			if errors.Is(commandErr, exec.ErrNotFound) || errors.Is(commandErr, fs.ErrNotExist) {
				return PortsResult{State: PortsUnavailable, Listeners: builder.listeners(), Diagnostic: joinDiagnostics(diagnostics)}
			}
		}
	}
	listeners := builder.listeners()
	current, recheckDiagnostics, recheckDenied, membershipErr := darwinGroupMembers(pgid)
	if membershipErr != nil {
		return emptyPorts(PortsUnavailable, "recheck process group: "+membershipErr.Error())
	}
	diagnostics = append(diagnostics, recheckDiagnostics...)
	denied = denied || recheckDenied
	for pid, member := range members {
		if replacement, ok := current[pid]; !ok || replacement.identity != member.identity {
			diagnostics = append(diagnostics, fmt.Sprintf("pid %d left the process group during listener inspection", pid))
		}
	}
	for index := range listeners {
		listeners[index].PIDs = keepMatchingPIDs(listeners[index].PIDs, members, current)
	}
	listeners = compactPorts(listeners)
	state, diagnostic := finishPortInspection(ctx, listeners, diagnostics, denied)
	return PortsResult{State: state, Listeners: listeners, Diagnostic: diagnostic}
}

func isInspectionDenied(message string) bool {
	lower := strings.ToLower(message)
	return strings.Contains(lower, "permission denied") || strings.Contains(lower, "operation not permitted") || strings.Contains(lower, "not permitted")
}

func darwinGroupMembers(pgid int) (map[int]darwinMember, []string, bool, error) {
	members, err := unix.SysctlKinfoProcSlice("kern.proc.pgrp", pgid)
	if err != nil {
		if errors.Is(err, unix.ESRCH) {
			return map[int]darwinMember{}, nil, false, nil
		}
		return nil, nil, errors.Is(err, unix.EPERM), fmt.Errorf("read native process group %d: %w", pgid, err)
	}
	result := make(map[int]darwinMember)
	var diagnostics []string
	denied := false
	for _, member := range members {
		pid := int(member.Proc.P_pid)
		if pid <= 0 || member.Eproc.Pgid != int32(pgid) || member.Proc.P_stat == darwinZombieState {
			continue
		}
		identity, err := processStartIdentity(pid)
		if err != nil {
			if errors.Is(err, unix.EPERM) {
				denied = true
			}
			diagnostics = append(diagnostics, fmt.Sprintf("pid %d process identity: %v", pid, err))
			continue
		}
		result[pid] = darwinMember{pid: pid, identity: identity}
	}
	return result, diagnostics, denied, nil
}

func parseLsofFields(data []byte) ([]lsofListener, error) {
	var result []lsofListener
	var pid int
	var protocol, identity string
	var diagnostic string
	for lineNumber, line := range bytes.Split(data, []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		kind := line[0]
		value := string(line[1:])
		switch kind {
		case 'p':
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed <= 0 {
				diagnostic = fmt.Sprintf("malformed lsof pid field at line %d", lineNumber+1)
				pid = 0
				continue
			}
			pid, protocol, identity = parsed, "", ""
		case 'f':
			// Fields belong to one descriptor, not the entire process.
			protocol, identity = "", ""
		case 'P':
			protocol = value
		case 'd':
			// Darwin lsof exposes the socket's kernel identity in the
			// device-character field; TCP sockets have no inode field.
			identity = value
		case 'n':
			if pid == 0 || protocol != "TCP" || strings.Contains(value, "->") {
				continue
			}
			host, portText, err := net.SplitHostPort(value)
			if err != nil || host == "" {
				diagnostic = fmt.Sprintf("malformed lsof endpoint at line %d", lineNumber+1)
				continue
			}
			port, err := strconv.ParseUint(portText, 10, 16)
			if err != nil || port == 0 {
				diagnostic = fmt.Sprintf("malformed lsof port at line %d", lineNumber+1)
				continue
			}
			if identity == "" {
				diagnostic = fmt.Sprintf("lsof omitted socket identity at line %d", lineNumber+1)
				continue
			}
			result = append(result, lsofListener{pid: pid, identity: identity, address: host, port: uint16(port)})
		}
	}
	if diagnostic != "" {
		return result, errors.New(diagnostic)
	}
	return result, nil
}

func joinPIDs(pids []int) string {
	values := make([]string, len(pids))
	for index, pid := range pids {
		values[index] = strconv.Itoa(pid)
	}
	return strings.Join(values, ",")
}

type cappedBuffer struct {
	bytes.Buffer
	limit    int
	exceeded bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	remaining := b.limit - b.Len()
	if remaining <= 0 {
		b.exceeded = true
		return len(p), nil
	}
	if len(p) > remaining {
		_, _ = b.Buffer.Write(p[:remaining])
		b.exceeded = true
		return len(p), nil
	}
	return b.Buffer.Write(p)
}

func keepMatchingPIDs(pids []int, before, after map[int]darwinMember) []int {
	result := pids[:0]
	for _, pid := range pids {
		old, existed := before[pid]
		current, present := after[pid]
		if existed && present && old.identity == current.identity {
			result = append(result, pid)
		}
	}
	return result
}
