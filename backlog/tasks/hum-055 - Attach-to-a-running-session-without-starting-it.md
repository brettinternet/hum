---
id: HUM-055
title: Attach to a running session without starting it
status: To Do
assignee: []
created_date: '2026-09-06 19:09'
labels:
  - cli
  - tty
  - integration
milestone: m-4
dependencies: []
modified_files:
  - internal/cli/commands.go
  - internal/cli/render.go
  - internal/cli/serve_run_test.go
  - internal/cli/run_args_test.go
  - internal/cli/surface_test.go
  - README.md
  - docs/design.md
priority: medium
type: enhancement
ordinal: 32700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: a human-facing `hum attach NAME` command joins an already-running supervision session without starting, restarting, or otherwise changing process lifecycle. It provides the explicit connect semantic needed by shells and terminal UIs such as Herdr while `hum run` retains its existing start-or-attach behavior.

Scope: reuse the existing attached-session stream and TTY input lease. A running TTY target receives raw input and resize forwarding under the existing single-owner rules; a running non-TTY target follows output without gaining input. Add `--tail N` to bound retained replay before live output, with `--tail 0` suppressing replay. Missing and stopped targets return an actionable error and remain unchanged. Keep this human-facing: MCP continues to use bounded logs, wait, and one-shot input.

Why now: `hum run NAME` can launch a stopped declaration using the caller environment, so an external UI cannot safely label it Attach. Overmind demonstrates that explicit, lifecycle-independent attachment is a useful operator primitive.

Non-goals: launching or restarting; command argv; one-shot command execution or exit-code propagation; a Herdr dependency or pane management; tmux integration; a new MCP tool; changes to retained-output storage.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/cli -run '^TestAttachRunningSession$' -count=1 -v` exits 0 and prints PASS, proving attach joins a running TTY session, forwards raw input and resize under the existing exclusive lease, detaches without stopping the child, and gives a second attachment output without input ownership.
- [ ] #2 `go test ./internal/cli -run '^TestAttachNeverStartsSession$' -count=1 -v` exits 0 and prints PASS, proving missing and stopped names return actionable errors without launching, restarting, or changing the retained record.
- [ ] #3 `go test ./internal/cli -run '^TestAttachTail$' -count=1 -v` exits 0 and prints PASS for exact final-N retained replay in source order, `--tail 0` suppressing replay while preserving live output, non-negative validation before daemon contact, and no retention mutation.
- [ ] #4 `go test ./internal/cli -run '^TestAttachSurface$' -count=1 -v` exits 0 and prints PASS for command help and documentation that distinguish read-only log following, non-starting attach, and start-or-attach run, including copy-pasteable examples for an external terminal UI.
- [ ] #5 `task ci` exits 0.
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
