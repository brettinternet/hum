---
id: HUM-131
title: Test up scheduling rules once in orchestrate instead of in every adapter
status: To Do
assignee: []
created_date: '2026-09-23 21:53'
updated_date: '2026-09-23 21:53'
labels:
  - cli
  - mcp
  - architecture
milestone: m-3
dependencies: []
modified_files:
  - internal/orchestrate/*_test.go
  - internal/cli/manifest_test.go
  - internal/mcp/tools_test.go
priority: medium
type: task
ordinal: 4000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: each rule `hum up` applies is tested once, with fakes, in internal/orchestrate, where the rule is computed. The rules are dependency order, skips with blocked_by and existing_state, definition drift, the drifted readiness gate, removed_definition warnings, crash-recovery outcomes, and executable readiness outcomes. CLI and MCP tests keep only what each adapter adds: argument and field validation, human and JSON rendering, MCP structuredContent and isError, and exit codes. Today each rule is tested three or four times. The CLI copies start real child processes through an in-process daemon, and internal/cli takes about 31s. internal/orchestrate's own tests cover only 63.1% of its statements, so most real rule coverage sits in the adapter copies.

Owner exception (2026-09-23): this task may delete or shrink the CLI and MCP tests in the table below, but only after an internal/orchestrate test asserts the same rule. It may not remove anything else. Its Definition of Done replaces the default no-deletion rule with a scoped one.

Duplicates (commit 465b774, function lines in parentheses):
| Rule | internal/cli | internal/mcp | internal/orchestrate today |
|---|---|---|---|
| order by after | TestUpOrdersByAfter manifest_test.go:1285 (45) | TestUpOrdersByAfter tools_test.go:1565 (82) | TestOrchestrateUp "DAG ordering concurrent roots and classifications" orchestrate_test.go:236 |
| skipped with existing state | TestUpReportsBlockedExistingState manifest_test.go:1331 (67) | tools_test.go:1648 (53) | partial: "pre-launch follower" case orchestrate_test.go:318 |
| manifest runtime drift | TestUpReportsManifestRuntimeDrift manifest_test.go:2319 (97) | tools_test.go:811 (66) | "definition drift removed definitions and recovery" orchestrate_test.go:328; TestReadinessDriftAllMethodPairs :133 |
| drifted readiness gate | TestUpRejectsDriftedReadinessGate manifest_test.go:2417 (42) | tools_test.go:878 (24) | none |
| removed definitions | TestUpReportsRemovedManifestSessions manifest_test.go:2460 (52) | tools_test.go:903 (39) | orchestrate_test.go:328 |
| crash recovery | TestUpPreservesCrashRecovery manifest_test.go:756 (82) | tools_test.go:744 (66) | orchestrate_test.go:328 |
| executable readiness | TestExecutableReadiness manifest_test.go:499 (90) | tools_test.go:1341 (108) | TestExecutableReadiness orchestrate_test.go:28 |
Keep TestUpAdapterParity in both packages (internal/cli/manifest_test.go:439, internal/mcp/tools_test.go:943). It is the adapter-surface parity check. Leave the end-to-end copies unchanged: integration/manifest_test.go TestExecutableReadiness :409, TestUpReportsManifestRuntimeDrift :557, and TestUpOrderedStack :659; integration/relaunch_test.go TestUpPreservesPendingRecovery :207; integration/mcp_test.go TestUpAdapterParityAcrossSurfaces :407.

Rule functions in internal/orchestrate/orchestrate.go: OrchestrateUp :958, SkippedResult :511, DefinitionDriftResult :457, DefinitionChangedFields :390, RecoveryOutcome :468, RemovedDefinitionResults :483, WaitForReadiness :579, Ensure :855, and ResultSatisfiesGate :840. Test fakes are UpOperations (:188), EnsureOperations (:163), and ReadinessOperations (:570).

Procedure:
1. Record both baselines in Implementation Notes. First, `go test ./internal/orchestrate -count=1 -cover` (63.1% on 2026-09-23). Second, `go test ./internal/orchestrate ./internal/cli ./internal/mcp -count=1 -coverpkg=./internal/orchestrate,./internal/cli,./internal/mcp -coverprofile=/tmp/hum-up-cover.out`, then `go tool cover -func=/tmp/hum-up-cover.out | tail -1` (81.5% total). Also record `go test ./internal/cli -count=1` wall time.
2. For each table row, classify every assertion in the CLI and MCP copies. It is a rule if a function above computes it. It is surface if it covers rendering, JSON or MCP field names, exit codes, or validation before daemon contact.
3. Add each rule assertion that internal/orchestrate lacks to a table-driven orchestrate test built on the fakes, following TestOrchestrateUp.
4. Shrink each adapter copy to its surface assertions. Merge them into as few tests as stay readable, for example one CLI and one MCP test that render skipped, drift, removed, and recovery results from a single fake run. When an assertion only concerns rendering, the CLI test uses a stub daemon, following manifestCLIRecoveryStubDaemon (internal/cli/manifest_test.go:345), instead of real children. MCP tests use newTestServer (internal/mcp/tools_test.go:374), args (:399), and fakeClient (:196).
5. If a rule turns out to be computed in an adapter rather than in internal/orchestrate, keep its adapter test and note it. Moving that logic is a separate task.

Non-goals: logs, wait, status, and restart duplicates; integration tests; production code changes.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `go test ./internal/orchestrate -count=1 -cover` exits 0 and reports at least 75.0% of statements (63.1% on 2026-09-23).
- [ ] #2 AC2 — `go test ./internal/orchestrate ./internal/cli ./internal/mcp -count=1 -coverpkg=./internal/orchestrate,./internal/cli,./internal/mcp -coverprofile=/tmp/hum-up-cover.out && go tool cover -func=/tmp/hum-up-cover.out | tail -1` exits 0 and reports total coverage no more than 0.5 points below the baseline recorded in Implementation Notes (81.5% on 2026-09-23).
- [ ] #3 AC3 — `go test ./internal/orchestrate ./internal/cli ./internal/mcp ./integration -count=1` exits 0.
- [ ] #4 AC4 — `git diff --numstat main -- internal/cli/manifest_test.go internal/mcp/tools_test.go` shows a combined net reduction of at least 400 lines.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 An independent verifier pass returned PASS for every acceptance criterion
- [ ] #4 The diff touches only the paths declared in the task's modified-file list, or the deviation is justified in Implementation Notes
- [ ] #5 No protected gate file was modified unless the owner labelled this task tooling
- [ ] #6 Only adapter tests in the duplicate table were deleted or shrunk, and Implementation Notes map every removed assertion to the internal/orchestrate test that now owns it
<!-- DOD:END -->
