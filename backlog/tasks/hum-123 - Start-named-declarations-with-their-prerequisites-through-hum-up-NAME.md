---
id: HUM-123
title: Start named declarations with their prerequisites through hum up NAME
status: To Do
assignee: []
created_date: '2026-09-23 21:21'
updated_date: '2026-09-23 22:24'
labels:
  - cli
  - mcp
  - process
milestone: m-3
dependencies:
  - HUM-131
modified_files:
  - internal/orchestrate/orchestrate.go
  - internal/orchestrate/*_test.go
  - internal/cli/commands.go
  - internal/cli/manifest.go
  - internal/cli/root.go
  - internal/cli/completion.go
  - internal/cli/*_test.go
  - internal/mcp/tools.go
  - internal/mcp/*_test.go
  - integration/*_test.go
  - README.md
  - docs/design.md
  - docs/coding-agents.md
  - internal/skill/SKILL.md
  - plugins/hum/skills/hum/SKILL.md
priority: high
type: feature
ordinal: 5000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: `hum up NAME...` and MCP `up` with `names` start the named manifest declarations plus their transitive `after` prerequisites through the existing scheduler, and report results only for that subgraph. Today `hum up` always converges every declaration (`upCommand` in internal/cli/commands.go calls `requireNoArgs`), and `hum start NAME` intentionally never adds prerequisites, so there is no way to bring up one service and what it needs in a larger stack. pitchfork `start api` does this; it is the most common request once a stack has more than a few services.

Scope:
- internal/orchestrate owns one selection function returning the transitive-prerequisite closure of the requested names from the resolved definitions, in declaration order, deduplicated. Undeclared or ad-hoc-only names fail together in one error naming every unknown name. CLI and MCP both use it; no adapter-local graph walk.
- CLI `hum up [NAME...]`. Zero names keeps current behavior exactly. Named up applies the existing readiness, skip cascade, drift, recovery, `--detach`, `--no-wait`, `--timeout`, `--full`, `--json`, exit-code, and Ctrl+C-during-startup rules to the selected subgraph only. Interactive follow shows only selected processes. `--no-wait` rejection applies when the selected subgraph declares `after`. Unknown names are rejected before daemon creation or contact with exit 1 (structured JSON error on stdout with `--json`). Removed-definition warnings are emitted only for unnamed `up`.
- MCP `up` gains optional `names` (array of unique non-empty strings, at least one item when present) in its closed input schema; omission keeps current behavior.
- Shell completion offers declared names at `up` NAME positions.
- Update README (Start processes), docs/design.md command semantics (replace the statement that only `start NAME...` takes names), docs/coding-agents.md, and both skill copies.

Non-goals: changing `hum start` (it still never pulls prerequisites); `down NAME...` or dependent-inclusive selection; manifest groups or profiles; new result fields.

Implementation context (commit 465b774):
- CLI path: upCommand internal/cli/commands.go:2922 calls requireNoArgs (:2924) and builds `names` from every definition (:2958). The --no-wait/after rejection at :2962 checks the whole manifest through manifestHasAfter (:3423). It calls manifestLaunchCommandWithStateMode (:3145; `ordered` is true for up, and start passes false at :3142), then manifestUpSchedule (:3565), then manifestUpScheduleWithOps (:3587), then orchestrate.OrchestrateUp (:3594). The same `names` also feed the interactive follow session, startUpLogFollow (:2991). The command block is at commands.go:142-158; UsageText and ArgsUsage must gain `[NAME...]`.
- MCP path: Server.up internal/mcp/tools.go:1304 calls OrchestrateUp at :1351 without Names. upSchema is built from waitProps at :431-434. commonInput already has `Names []string` (:535, used by events). validateToolInputFields (:883) has no `array` case, so minItems, uniqueItems, and item type and minLength are not enforced today. Add an array case there; it will also enforce events' existing `maxItems: 2000`.
- Completion: completionNamePosition internal/cli/completion.go:120 lists the commands that take names; add `up`. Candidates come from completionProcessNames (:321).
- Docs to change: docs/design.md:391 says `start NAME...` never adds prerequisites. Keep that sentence and add the named `up` semantics next to it. In README.md's "Hum and pitchfork" section, remove `up NAME` from the gaps: the table row that lists `pitchfork start api` and the sentence "The planned `hum up NAME...` will start...".

Pitfalls, verified in code:
- OrchestrateUp keeps only the definitions named in UpOptions.Names (orchestrate.go:958-975). A worker whose `after` names a definition outside that set never finds it in `byName` (:1017) and waits on the condition variable until the context is cancelled. Always pass the prerequisite closure, never raw user names. Make OrchestrateUp return an error for a dependency missing from its input, and cover that case in TestSelectWithPrerequisites or a sibling test.
- OrchestrateUp computes removed_definition warnings against the filtered set (:984, :1136). A named up would therefore report every running, declared, but unselected process as removed_definition. Emit removed-definition results only when Names is nil.
- For named up, apply the --no-wait rejection to the selected closure only, not to manifest.defs.

Test templates:
- orchestrate: TestOrchestrateUp "DAG ordering concurrent roots and classifications" (internal/orchestrate/orchestrate_test.go:236).
- CLI: TestUpOrdersByAfter (internal/cli/manifest_test.go:1285), with writeManifestCLITestFile (:30), manifestCLILaunchResults (:318), and manifestCLIExitCode (:334).
- MCP: TestUpOrdersByAfter (internal/mcp/tools_test.go:1565), with newTestServer (:374), args (:399), and fakeClient (:196).
- integration: TestUpOrderedStack (integration/manifest_test.go:659).
- completion: internal/cli/completion_test.go.
HUM-131 moves scheduler-rule assertions into internal/orchestrate first. Follow its split: test the rule in orchestrate, and test only the surface in the CLI and MCP.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `go test ./internal/orchestrate -run "^TestSelectWithPrerequisites$" -count=1 -v` exits 0 and prints its PASS line; cases cover a chain, a diamond, multiple names sharing prerequisites (deduplicated), declaration order preserved, a name without `after` selecting only itself, and several unknown names reported in one error.
- [ ] #2 AC2 — `go test ./internal/cli -run "^TestUpNamedSelectsPrerequisites$" -count=1 -v` exits 0; with a manifest db <- api <- web plus independent worker, `hum up api --json` launches db then api only and returns exactly those two results; a repeat reports both `already_running`; `hum up nope` exits 1 without daemon contact; `hum up` with no names still launches all four.
- [ ] #3 AC3 — `go test ./internal/mcp -run "^TestUpNames$" -count=1 -v` exits 0; `names` selects the same subgraph as the CLI; an empty array, duplicate names, and non-string items are rejected by input validation before any daemon call; unknown names return an error before any start request.
- [ ] #4 AC4 — `go test ./integration -run "^TestUpNamedStack$" -count=1 -v` exits 0; against the built binary, `hum up api --detach` in a db <- api <- web chain leaves exactly db and api running according to `hum status --json`.
- [ ] #5 AC5 — `go test ./internal/cli -run "Completion" -count=1 -v` exits 0 and includes a case where completion at the `hum up` NAME position returns the declared process names.
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
