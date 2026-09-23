---
id: HUM-127
title: Make CLI help and the README opening plain task guidance
status: To Do
assignee: []
created_date: '2026-09-23 21:22'
updated_date: '2026-09-23 21:54'
labels:
  - cli
  - docs
milestone: m-4
dependencies:
  - HUM-130
modified_files:
  - internal/cli/commands.go
  - internal/cli/root.go
  - internal/cli/*_test.go
  - cmd/hum/*_test.go
  - internal/skill/*_test.go
  - README.md
  - docs/design.md
priority: medium
type: docs
ordinal: 10000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: a first-time user can read `hum --help`, any `hum COMMAND --help`, and the top of README.md and know what each command is for and how to use it, while exact semantics stay in docs/design.md. Today help descriptions pack specification prose into semicolon-chained sentences, for example `hum start --help`: "Idempotently ensure named sessions are running from .hum.yaml when present, otherwise hum.yaml; without it, unresolved names return manifest_missing. start never pulls in prerequisites and waits for readiness unless --no-wait; ready.exec uses exact argv with no shell, probes are immediate-first and serial, retain bounded diagnostics, and gate startup—not liveness; see docs/design.md." Several descriptions point at docs/design.md, which an installed binary does not ship, and `hum mcp --help` still says there are twelve tools although `events` makes thirteen. The README opens with "bounded, exact-argv process interface". Colleagues comparing Hum with pitchfork give up here, before features matter.

Scope:
- Every visible command description: at most two plain sentences (at most 240 characters) saying what the command does and when to use it, then the existing optional `Exit codes:` sentence, then 1 to 3 examples. No snake_case wire identifiers (for example `manifest_missing`, `running_unverified`) and no `docs/` paths in help prose.
- Fix the `hum mcp` tool count and list.
- README from the title through Quickstart: plain description of what Hum does and who it is for, keeping the diagram, demos, install, and quickstart. Sections after Quickstart are out of scope.
- Specification wording removed from help that is not already in docs/design.md moves there. HUM-130 (a dependency) removes the tests that pin help wording. If a test still pins wording you change, stop and record it in Implementation Notes instead of deleting it.
- Tighten TestHelpContract to enforce the rules above.

Non-goals: flag names, aliases, or behavior; JSON output; MCP tool descriptions (HUM-126); README sections after Quickstart; restructuring docs/design.md.

Implementation context (commit 465b774):
- Command blocks in internal/cli/commands.go: version :38, serve :52, doctor :66, init :80, skill :96, run :107, start :125, up :142, down :160, list :174, events :191, status :214, attach :229, logs :244, wait :267, input :285, restart :302, signal :319, stop :335, remove :350, shutdown :366. Root help is at internal/cli/root.go:417.
- `hum mcp`: internal/cli/mcp.go:26 defines a description that already says thirteen tools, but commands.go:35 overwrites it with a second string that says twelve. Keep one short description. docs/design.md:895 also says twelve.
- Contract tests: TestHelpContract (internal/cli/help_contract_test.go:15), whose sentence and example counting helpers are in the same file; TestHelpExitCodes (:181), which requires the exact `Exit codes:` sentence for start, up, wait, and restart; TestHelpWordBudgets (internal/cli/surface_test.go:147). Fold the word budgets into the new 240-character rule rather than keeping two budgets.
- The man page (task cli:man, .taskfiles/cli.yaml:33, using cmd/hum-man) renders from the same command tree into dist/hum.1, which is not committed.
- HUM-130 adds TestDocsCoverEveryTool, which fails if `hum mcp --help` omits a tool.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `go test ./internal/cli -run "^TestHelpContract$" -count=1 -v` exits 0 after it is tightened to enforce, for every visible command, at most two sentences and 240 characters of prose before any `Exit codes:` sentence, 1 to 3 examples, no snake_case identifiers, and no `docs/` paths.
- [ ] #2 AC2 — `go test ./internal/cli ./internal/skill ./internal/mcp ./cmd/hum -count=1` exits 0, including the HUM-130 checks that docs reference only real commands, flags, and tools.
- [ ] #3 AC3 — `go run ./cmd/hum mcp --help` names all 13 MCP tools, including `events`.
- [ ] #4 AC4 — `sed -n "1,12p" README.md | rg -i "bounded|exact-argv"` prints nothing and exits 1, and README.md still contains the install and Quickstart sections (`rg -c "^## (Install|Quickstart)$" README.md` prints 2).
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
