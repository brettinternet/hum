---
id: HUM-147
title: Make interactive TTY attach display and detach reliably
status: To Do
assignee: []
created_date: '2026-09-25 20:53'
updated_date: '2026-09-25 20:57'
labels:
  - cli
  - tty
dependencies: []
modified_files:
  - internal/cli/tty.go
  - internal/cli/render.go
  - internal/cli/commands.go
  - internal/cli/tty_test.go
  - internal/cli/render_test.go
  - integration/tty_interactive_test.go
  - examples/interactive/README.md
priority: medium
type: bug
ordinal: 31000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: the interactive greeter renders at the correct column in an attached terminal and Ctrl-] detaches reliably, including from Herdr. Repro (macOS, Herdr, 2026-09-25): hum up -F ./hum.yaml -d; hum attach greeter; type Bob and Enter. Each output line starts farther right and wraps because hum puts the local terminal in raw mode while forwarding bare LF output. Pressing Ctrl-] appeared to echo ^] rather than detach; investigate whether Herdr sends byte 0x1d, the CLI misses it, or terminal mode/input ownership changes. Preserve raw child output and single-owner input semantics. The separate "name? greeter launched" seen in hum logs is not foreign stdin: greeter launched is a system event adjacent to an unterminated prompt; hum input greeter --text with a newline intentionally sends the one-shot answer. Scope: attached TTY rendering, detach handling, and example guidance distinguishing replayed system events, echoed input, and actual child stdin. Non-goals: changing the greeter loop, feeding stdin from hum up or hum logs, changing the raw retained-output/JSON contract, or redesigning all log streams. Modified-file contract: internal/cli/tty.go, internal/cli/render.go, internal/cli/commands.go, internal/cli/tty_test.go, internal/cli/render_test.go, integration/tty_interactive_test.go, examples/interactive/README.md (touch only the files needed; document any justified deviation).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — go test ./internal/cli -run "^(TestTTYAttachDisplay|TestTTYAttachDetach)$" -count=1 -v exits 0 and prints RUN/PASS for both tests. New tests cover CRLF versus LF-only rendering in raw local-terminal mode (including unterminated prompts and no duplicated carriage returns) and consuming byte 0x1d without forwarding it to the child while restoring local terminal settings.
- [ ] #2 AC2 — go test ./integration -run "^TestTTYInteractiveSession$" -count=1 -v exits 0. Extend this existing PTY test to assert successive greeter-style replies start at column zero and Ctrl-] exits attachment without stopping the supervised child; no skipped or weakened assertions.
- [ ] #3 AC3 — from examples/interactive, hum -F ./hum.yaml up -d followed by hum attach greeter allows Bob plus Enter to display hello, Bob followed by a fresh name? aligned at column zero; Ctrl-] in Herdr returns to the shell while hum -F ./hum.yaml status greeter still reports running. If Herdr intercepts Ctrl-] before the CLI receives it, record the observed bytes/behavior and document a working detach path; do not claim an unverified CLI fix.
- [ ] #4 AC4 — from examples/interactive, hum -F ./hum.yaml logs greeter --stream stdout omits greeter launched and hum -F ./hum.yaml logs greeter --stream system shows the launch event; both commands exit 0. examples/interactive/README.md explains the distinction from child stdin and the input/attach commands. Retained JSON output remains raw (no display-only CR insertion).
- [ ] #5 AC5 — go test ./internal/cli -run "^TestLogsCursorAfterPartialLine$" -count=1 -v exits 0 and prints RUN/PASS. Add a human-rendering test where the last child entry is an unterminated name? prompt: next cursor: N appears on a separate line as CLI metadata, not as apparent child output; --json retains the original prompt bytes and the same cursor N. From examples/interactive, hum -F ./hum.yaml logs greeter --tail 1 prints the prompt and next cursor on distinct lines.
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
