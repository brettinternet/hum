---
id: HUM-118
title: Provide a private Windows daemon transport and recoverable runtime ownership
status: To Do
assignee: []
created_date: '2026-09-23 20:49'
updated_date: '2026-09-23 20:49'
labels:
  - daemon
  - security
  - architecture
milestone: m-5
dependencies:
  - HUM-117
modified_files:
  - internal/daemon/runtime.go
  - internal/daemon/client.go
  - internal/daemon/server.go
  - internal/daemon/*windows*.go
  - internal/daemon/*unix*.go
  - internal/daemon/*_test.go
  - internal/config/config.go
  - internal/config/*windows*.go
  - internal/config/*unix*.go
  - internal/config/*_test.go
  - go.mod
  - go.sum
priority: high
type: feature
ordinal: 2000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: one native Windows daemon owns the runtime and exposes the existing bounded JSON wire protocol to the same user only. Today internal/daemon/runtime.go binds a Unix socket, uses flock, Unix permissions, PID/PGID and start identity for stale recovery; internal/daemon/client.go and server.go dial Unix; internal/config/config.go defaults to temp plus os.Getuid. Scope: Windows-private IPC (named pipe with explicit current-user-only ACL or an equally secure equivalent), cross-process startup exclusion, Windows runtime directory, ACL and stale-owner recovery; preserve the existing wire protocol, single-owner invariant, refusal to displace an unverifiable live owner, and startup reconciliation of recorded children. Coordinate persisted group identity and crash cleanup with HUM-117: a Windows Job Object normally closes on daemon death, so distinguish already-dead children from uncertain ownership and never terminate an unverified reused PID. Preserve Unix paths and permissions. Validate same-user isolation and test concurrent startup. Non-goals: network-facing TCP endpoint, CLI autostart, ConPTY or release packaging. Read internal/daemon/runtime.go:345-412, :530-688 and :748-760, internal/daemon/client.go:121-151, internal/daemon/server.go listen path, internal/config/config.go:119-126; do not remove old Unix tests to obtain a green Windows gate.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — On Windows, `go test ./internal/daemon ./internal/config -count=1` exits 0; a real client can round-trip over the private transport and another simultaneous daemon cannot acquire the same runtime.
- [ ] #2 AC2 — On Windows, `go test ./internal/daemon -run "TestWindows" -count=1` exits 0; tests inspect transport/runtime ACLs to verify no broader access, and exercise stale-owner/crash recovery, PID-reuse or ownership-mismatch refusal, and recorded child reconciliation without killing unrelated processes.
- [ ] #3 AC3 — On macOS or Linux, `go test ./internal/daemon ./internal/config -count=1` exits 0; socket permission, locking, and existing stale-runtime recovery tests continue to pass.
- [ ] #4 AC4 — On macOS or Linux, `GOOS=windows GOARCH=amd64 go build ./internal/daemon ./internal/config` exits 0.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 An independent verifier pass returned PASS for every acceptance criterion
- [ ] #4 The diff touches only the paths declared in the task's modified-file list, or the deviation is justified in Implementation Notes
- [ ] #5 No test was deleted, skipped, or weakened
- [ ] #6 No protected gate file was modified unless the owner labelled this task tooling
<!-- DOD:END -->
