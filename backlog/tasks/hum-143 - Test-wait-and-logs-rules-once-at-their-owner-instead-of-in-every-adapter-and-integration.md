---
id: HUM-143
title: >-
  Test wait and logs rules once at their owner instead of in every adapter and
  integration
status: To Do
assignee: []
created_date: '2026-09-24 22:54'
updated_date: '2026-09-24 22:54'
labels:
  - cli
  - mcp
  - integration
  - architecture
dependencies:
  - HUM-142
modified_files:
  - integration/wait_test.go
  - integration/terminal_control_test.go
  - internal/cli/list_logs_test.go
  - internal/mcp/tools_test.go
  - internal/app/app_test.go
  - internal/output/ring_test.go
  - internal/output/terminal_control_test.go
priority: low
type: task
ordinal: 27000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: finish what HUM-131 started. HUM-131 moved `up` scheduling rules to one owner and listed "logs, wait, status, and restart duplicates" as non-goals (backlog/tasks/hum-131*.md). A read-only survey on 2026-09-24 at commit 5f0ed4c found the remaining real duplication is wait and logs match-context. Status/list, restart, restart-policy, stop grace, signal/terminal state, readiness, and events tests at each layer assert different responsibilities. Leave them alone.

Layer rule, as in HUM-131: a rule is tested where it is computed, with fakes. CLI and MCP tests keep only what the adapter adds: flag or argument validation, request forwarding, human and JSON rendering, MCP structuredContent and isError, and exit codes. Integration keeps one built-binary path per user-visible behavior to prove the wiring.

Owner exception (2026-09-24): this task may delete or shrink only the tests in the table below, and only after Implementation Notes map each removed assertion to the owner test that still asserts it. If an assertion has no owner, add it to the owner first, following that test pattern. Its Definition of Done replaces the default no-deletion rule with a scoped one.

| Behavior | Owner (keep) | Duplicates to shrink |
|---|---|---|
| wait matching, cursor, exit, and pre-launch semantics | internal/app/app_test.go Wait* tests from :3381 (buffered/cursor zero :3381, new match with follower :3422, exit wakeup :3470, pre-launch/timeout/concurrent :3554), WaitPreLaunch* and WaitProcessObserved; internal/daemon/daemon_test.go wait bridge from :1818; CLI surface in internal/cli/wait_test.go (TestWaitCLIRequestOptions :22, TestWaitCLIOutputsAndExitCodes :89 on a stub daemon, TestWaitCLIValidation :245) | integration/wait_test.go TestWait (:25, ~275 lines, 5 subtests). Keep one built-binary subtest that matches buffered output and reports the cursor, plus the exit-code-3 subtest (:75) as the only real-binary proof of that exit code. Remove the no-match-exit (:106), timeout-cursor (:140), and pre-launch (:172) subtests. Pre-launch is also covered by integration TestWaitBeforeStart (integration/durable_session_test.go:160) and internal/cli TestWaitCLIPreLaunchSessionTimesOut (internal/cli/wait_test.go:387). Before removing the pre-launch subtest, check that the daemon-autostart assertion (it waits for runtime.paths.Ready) is covered by integration TestAutomaticStartup. |
| logs match and context windows | internal/output/ring_test.go TestReadMatchContext (:122) and TestReadMatchContextBounds (:223) | internal/cli/list_logs_test.go TestLogsMatchContext (:626): keep validation (negative or missing context, errors before daemon contact) and one request-forwarding check; drop repeated context-window happy paths. internal/mcp/tools_test.go TestLogsMatchContext (:1969): keep schema/argument validation and forwarded Match/Context fields; drop repeated window assertions. Keep integration/logs_test.go TestLogsMatchContext (:78) unchanged; it is the multi-page cursor proof. |
| terminal-control stripping in bounded logs | internal/output/terminal_control_test.go TestStripTerminalControl (:9) | integration/terminal_control_test.go TestBoundedLogsStripTerminalControl (:15): keep one bounded-output assertion and the raw follow replay and live assertions. Drop repeated human/JSON/MCP bounded checks only when internal/cli/terminal_control_test.go (:15) and internal/mcp/terminal_control_test.go (:12) assert that surface. |

Procedure:
1. Record baselines: `go test ./integration ./internal/cli ./internal/mcp -count=1` wall times, and `go test ./internal/app ./internal/output ./internal/daemon ./internal/cli ./internal/mcp -count=1 -coverpkg=./internal/app,./internal/output,./internal/daemon,./internal/cli,./internal/mcp -coverprofile=/tmp/hum-wait-cover.out && go tool cover -func=/tmp/hum-wait-cover.out | tail -1`.
2. For each row, classify every assertion in each duplicate as rule (owner computes it) or surface (adapter-specific). Map rules to owner tests, adding any that are missing.
3. Shrink the duplicates to their surface assertions.

Non-goals: status, restart, readiness, stop grace, signal, and events tests; production code; other integration tests.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `go test ./internal/app ./internal/output ./internal/daemon ./internal/cli ./internal/mcp ./integration -count=1` exits 0.
- [ ] #2 AC2 — the coverage command in procedure step 1 reports total coverage no more than 0.5 points below the baseline recorded in Implementation Notes.
- [ ] #3 AC3 — `git diff --numstat main -- integration/wait_test.go integration/terminal_control_test.go internal/cli/list_logs_test.go internal/mcp/tools_test.go` shows a combined net reduction of at least 120 lines.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 An independent verifier pass returned PASS for every acceptance criterion
- [ ] #4 The diff touches only the paths declared in the task modified-file list, or the deviation is justified in Implementation Notes
- [ ] #5 Only tests in the description table were deleted or shrunk, and Implementation Notes map every removed assertion to the owner test that now holds it
- [ ] #6 No protected gate file was modified unless the owner labelled this task tooling
<!-- DOD:END -->
