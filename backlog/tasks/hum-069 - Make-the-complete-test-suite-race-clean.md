---
id: HUM-069
title: Make the complete test suite race-clean
status: To Do
assignee: []
created_date: '2026-09-10 01:51'
updated_date: '2026-09-10 01:58'
labels: []
dependencies: []
modified_files:
  - internal/cli/tty.go
  - internal/cli/tty_test.go
  - internal/cli/serve_run_test.go
  - cmd/hum/integration_test.go
  - internal/testutil/harness.go
  - Taskfile.dist.yaml
  - .github/workflows/ci.yaml
priority: high
type: bug
ordinal: 45700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: `go test -race ./...` passes locally and CI runs that complete race surface. Evidence: the full race run detects unsynchronized `exec.Cmd` reads in `cmd/hum/integration_test.go` and `internal/cli/serve_run_test.go`, plus a production race between `ttyInput.start` reading `os.File.Fd` and the forwarding goroutine closing that file. The current `task race` excludes CLI, integration, MCP, protocol, and command packages, so CI misses these failures. Scope: fix the TTY ownership race, make process-test harnesses synchronize completion state, and expand the race task to all packages with any necessary deterministic test timeouts. Non-goals: do not skip race-sensitive tests, serialize the entire suite to hide races, or weaken lifecycle assertions.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `mise exec go -- go test -race ./... -count=1` exits 0 with no `WARNING: DATA RACE` output.
- [ ] #2 `task race` exits 0 and its logged Go package pattern is `./...`, covering command, CLI, MCP, protocol, and integration packages.
- [ ] #3 `task ci` exits 0 after running the expanded race target.
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
