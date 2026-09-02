---
id: DEVPROC-006
title: Deliver serve run list logs and stop
status: To Do
assignee: []
created_date: '2026-09-02 17:06'
labels:
  - cli
  - daemon
  - protocol
  - output
milestone: m-0
dependencies:
  - DEVPROC-005
modified_files:
  - cmd/devproc/
  - internal/cli/
  - internal/app/
  - internal/protocol/
priority: high
type: feature
ordinal: 600
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: humans and shell-capable agents can use the first complete CLI slice to start the daemon, supervise a named command, list processes, fetch bounded logs, and stop the full process tree.

Scope: urfave/cli commands `serve`, `run <name> -- <command> [args...]`, `list [--json]`, `logs <name> [--stream stdout|stderr|both] [--tail N] [--after-cursor N] [--limit-bytes N] [--grep REGEX] [--json]`, and `stop <name>`; exact argv forwarding; current cwd/project-root context; human-readable defaults; stable JSON; log response fields process, stream, cursor, next_cursor, truncated, running, and timestamped entries. Reject missing separators/argv, invalid names/cursors/regex/stream/limits, duplicate running names, and server-limit violations with clear errors.

Non-goals: `status`, `wait`, daemon auto-start, proactive logs, PTY, configuration files, or MCP.

Modified-file contract: cmd/devproc/, internal/cli/, internal/app/, internal/protocol/.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/cli -run TestVerticalSliceCommands` exits 0 and covers parsing, exact argv preservation, no-command help, human output, JSON output, and clear invalid-input errors.
- [ ] #2 With a temporary runtime directory, the built binary runs `serve`, `run api -- sh -c ...`, `list --json`, bounded `logs api --json`, and `stop api`; every command exits as documented and JSON decodes with the specified fields.
- [ ] #3 `logs` with no bounds returns at most 100 lines and 16 KiB, reports truncation, and its next_cursor retrieves only subsequent output with `--after-cursor`.
- [ ] #4 `go test ./internal/cli -run TestNoStatusOrWaitYet` confirms this vertical slice exposes neither incomplete command before its integration gate passes.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 An independent verifier pass returned PASS for every acceptance criterion
- [ ] #4 The diff touches only the paths declared in the task's modified-file list, or the deviation is justified in Implementation Notes
- [ ] #5 No test was deleted, skipped, or weakened
- [ ] #6 No protected gate file was modified unless the owner labelled this task tooling
- [ ] #7 Committed on main with the task ID in the commit subject and a Task: trailer
<!-- DOD:END -->
