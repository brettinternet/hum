---
id: HUM-023
title: Strip terminal control sequences from bounded output reads and matches
status: To Do
assignee: []
created_date: '2026-09-05 14:57'
labels:
  - output
  - daemon
  - cli
  - docs
milestone: m-2
dependencies:
  - HUM-022
modified_files:
  - internal/output/
  - internal/app/
  - internal/daemon/
  - internal/cli/
  - internal/mcp/
  - internal/skill/
  - internal/testutil/
  - integration/
  - plugins/hum/skills/hum/SKILL.md
  - README.md
  - docs/design.md
  - docs/coding-agents.md
priority: medium
type: feature
ordinal: 3400
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: bounded output reads and every output pattern match operate on text with terminal control sequences removed, so agents and humans reading `hum logs`, `hum wait --match`, MCP `logs`, and readiness `match` get clean lines from tty sessions (HUM-022) and from ordinary processes that emit colour under `FORCE_COLOR` or similar. Stored bytes, cursors, and the live follow/attached-run rendering stay raw. This is a stateless, per-entry strip. It is deliberately not the redraw collapsing or terminal emulation that HUM-022 declared out of scope, and it makes no byte-fidelity promise over JSON.

Scope, the transform: one pure function in internal/output, `StripTerminalControl(text string) string`, applied byte-wise and documented as the single definition of "stripped text". It removes: CSI sequences (ESC `[`, parameter bytes 0x30–0x3F, intermediate bytes 0x20–0x2F, one final byte 0x40–0x7E); OSC (ESC `]`) through BEL or ST (ESC `\`); DCS, SOS, PM, and APC (ESC `P`, `X`, `^`, `_`) through ST; ESC followed by zero or more intermediates 0x20–0x2F and one final byte 0x30–0x7E (charset designations such as ESC `(` `B`, and single-final sequences such as ESC `7`, ESC `=`, ESC `M`); and a `\r` that immediately precedes `\n`. An ESC whose sequence is not terminated within the entry is removed from the ESC to the end of the entry. Every other byte is preserved exactly: NUL, invalid UTF-8, tabs, backspace, BEL outside OSC, and lone `\r`. Text without ESC or `\r` is returned unchanged with no allocation. 8-bit C1 introducers are not recognised because they collide with UTF-8.

Scope, where it applies: (1) the daemon's bounded `output` operation strips stdout and stderr entry text before wire conversion, so CLI `logs`, `logs --json`, `logs --tail`, and MCP `logs` return stripped text; (2) every `Match` evaluation, in the ring read filter (bounded reads and `logs --follow --match`), `wait --match`, and the readiness tracker, tests the pattern against stripped text; (3) nothing else changes. Follow events delivered to `logs --follow` and attached `run` carry raw bytes so terminals render colour and redraws. The ring keeps storing raw bytes; `MaxBytes`, `MaxEntries`, `Tail`, eviction accounting, and cursors are computed on stored bytes exactly as today. System entries are never altered. No `--raw` flag or per-request option is added.

Semantics to document: a pattern written against raw escape bytes stops matching; patterns anchored at line start now match colourised output whose first byte was ESC. Because the strip is per entry, a sequence split across entries by the idle flush leaves its tail visible at the start of the next entry, and carriage-return redraw frames remain as separate segments; both are stated limitations, not bugs. docs/design.md changes "raw text" in the entry description to "raw stored text, stripped on bounded read and match", and docs/coding-agents.md plus both skills drop the HUM-022 guidance that bounded logs are not terminal-sanitized.

Non-goals: carriage-return, backspace, or cursor-movement redraw collapsing; alternate-screen or any screen-state tracking; terminal emulation; changing stored bytes or cursors; changing follow or attached `run` rendering; a `--raw` or opt-out flag; byte-exact invalid-UTF-8 over JSON; stripping C1 8-bit controls; stripping BEL or other C0 controls outside escape sequences; protocol version changes.

Modified-file contract: internal/output/, internal/app/, internal/daemon/, internal/cli/, internal/mcp/, internal/skill/, internal/testutil/, integration/, plugins/hum/skills/hum/SKILL.md, README.md, docs/design.md, docs/coding-agents.md. No go.mod or go.sum change.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `go test ./internal/output -run '^TestStripTerminalControl$' -count=1 -v` exits 0, prints `--- PASS: TestStripTerminalControl`, and proves the function removes SGR/colour, cursor-movement, and erase CSI sequences, OSC terminated by BEL and by ST, DCS/APC terminated by ST, charset designations (ESC ( B), single-final sequences (ESC 7, ESC =, ESC M), and `\r` before `\n`; removes an unterminated ESC sequence to the end of the entry; preserves NUL, invalid UTF-8, tabs, backspace, BEL outside OSC, and lone `\r` byte-for-byte; returns text without ESC or `\r` unchanged with zero allocations under `testing.AllocsPerRun`; and that ring read `Match` filters evaluate stripped text while stored entries and cursors stay raw.
- [ ] #2 AC2 — `go test ./internal/app -run '^TestMatchUsesStrippedText$' -count=1 -v` exits 0, prints `--- PASS: TestMatchUsesStrippedText`, and proves readiness `match` and `wait --match` fire on a colourised line whose first byte is ESC using a `^`-anchored pattern and on a CRLF-terminated line, a pattern containing a literal ESC no longer matches, the ready cursor equals the raw entry cursor, and stored entry text is unchanged.
- [ ] #3 AC3 — `go test ./internal/daemon -run '^TestOutputReadStripsTerminalControl$' -count=1 -v` exits 0, prints `--- PASS: TestOutputReadStripsTerminalControl`, and proves the bounded `output` operation returns stripped stdout and stderr text and untouched system entries; `MaxBytes`, `MaxEntries`, `Tail`, and returned cursors are computed on stored bytes; the follow stream returns the same entries raw; `logs --follow --match` filtering and `wait` matching use stripped text; and the protocol version is unchanged.
- [ ] #4 AC4 — `go test ./internal/cli -run '^TestLogsStripTerminalControl$' -count=1 -v` and `go test ./internal/mcp -run '^TestLogsStripTerminalControl$' -count=1 -v` both exit 0 and print the corresponding named PASS line, proving CLI `logs`, `logs --json`, `logs --tail`, and MCP `logs` return stripped text; `logs --follow` and attached `run` write raw bytes; `wait --match` succeeds against stripped text; and no `--raw` or opt-out flag exists on any command.
- [ ] #5 AC5 — `go test ./integration -run '^TestBoundedLogsStripTerminalControl$' -count=1 -v` exits 0, prints `--- PASS: TestBoundedLogsStripTerminalControl`, and proves with the built binary and a fixture that emits SGR colour, an OSC title, and CRLF line endings that `hum logs` and `hum logs --json` contain no ESC or `\r` bytes, a captured `hum logs --follow` contains the raw ESC bytes, and `hum wait --match '^ready'` returns matched on a line whose raw form begins with ESC.
- [ ] #6 AC6 — `go test ./internal/cli ./internal/skill -run '^TestTerminalControlDocs$' -count=1 -v` exits 0, prints both named PASS lines, and README.md, docs/design.md, docs/coding-agents.md, CLI help, the embedded skill, and `plugins/hum/skills/hum/SKILL.md` document that bounded reads and matches use stripped text, follow and attached run stay raw, stored bytes are unchanged, the per-entry limitations (split sequences, redraw frames), the absence of a `--raw` flag, and no longer say bounded logs are not terminal-sanitized.
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
