---
id: HUM-143
title: >-
  Test wait and logs rules once at their owner instead of in every adapter and
  integration
status: Done
assignee: []
created_date: '2026-09-24 22:54'
updated_date: '2026-09-25 18:52'
labels:
  - cli
  - mcp
  - integration
  - architecture
  - reviewed
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

Stop rules: default to keeping. If an assertion cannot be clearly classified as rule or surface, or its owner test is not obvious within the files named in the table, keep it and list it in Implementation Notes. Work only on the three table rows. Do not look for more duplicates.

Unrelated failures: if `task ci` or a package run fails in a test this task did not touch, rerun that package once. If the rerun passes, record both runs in Implementation Notes and continue; if it fails again, stop and report it. Do not fix unrelated tests in this task.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 AC1 — `go test ./internal/app ./internal/output ./internal/daemon ./internal/cli ./internal/mcp ./integration -count=1` exits 0.
- [x] #2 AC2 — the coverage command in procedure step 1 reports total coverage no more than 0.5 points below the baseline recorded in Implementation Notes.
- [x] #3 AC3 — `git diff --numstat main -- integration/wait_test.go integration/terminal_control_test.go internal/cli/list_logs_test.go internal/mcp/tools_test.go` shows a combined net reduction of at least 80 lines.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 task ci passes on the final commit
- [x] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [x] #3 An independent verifier pass returned PASS for every acceptance criterion
- [x] #4 The diff touches only the paths declared in the task modified-file list, or the deviation is justified in Implementation Notes
- [x] #5 Only tests in the description table were deleted or shrunk, and Implementation Notes map every removed assertion to the owner test that now holds it
- [x] #6 No protected gate file was modified unless the owner labelled this task tooling
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Map each removable rule assertion to its owner tests and retain adapter and binary-surface assertions.
2. Shrink only the named duplicate tests, adding owner assertions if needed.
3. Run focused acceptance checks, independent verification, and task ci; commit, merge, and finalize the task.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Baseline in hum-143-tests at main 280fed4: go test ./integration ./internal/cli ./internal/mcp -count=1 PASS (integration 9.782s, cli 26.213s, mcp 8.238s; wall real 28.36s). Coverage: go test ./internal/app ./internal/output ./internal/daemon ./internal/cli ./internal/mcp -count=1 -coverpkg=./internal/app,./internal/output,./internal/daemon,./internal/cli,./internal/mcp -coverprofile=/tmp/hum-wait-cover.out && go tool cover -func=/tmp/hum-wait-cover.out | tail -1 PASS total 81.3% (wall real 34.73s).

Assertion map before deletion (all within the three table rows): integration/wait_test.go no-match exit/liveness and cursor advance -> internal/app/TestWaitExitWakeupWithAndWithoutMatch, TestWaitExplicitCursorReplaysTerminalIncarnationExit; human/JSON and success code -> internal/cli/TestWaitCLIOutputsAndExitCodes. Timed-out outcome, consumed cursor and no stderr/code 2 -> internal/app/TestWaitPreLaunchTimeoutCursorCancellationAndConcurrentWaiters and internal/cli/TestWaitCLIOutputsAndExitCodes; unrelated stop cleanup belongs to integration lifecycle, no wait rule lost. Pre-launch timeout -> internal/app/TestWaitPreLaunchWithoutLaunchTimesOut, internal/cli/TestWaitCLIPreLaunchSessionTimesOut; early-start match -> integration/TestWaitBeforeStart; daemon ready/autostart -> integration/TestAutomaticStartup (both attached and detached assert Ready). integration/wait_test.go retained buffered/follower and exit-code-3 built-binary paths; internal/daemon/TestWaitDaemonBridge retains wait bridge. internal/cli/list_logs_test.go single/aggregate/zero-context entry-window assertions -> internal/output/TestReadMatchContext (before/after, zero context, stream) and TestReadMatchContextBounds (paging); built-binary multi-page path remains integration/TestLogsMatchContext. CLI validation/no contact remains; one single-process match/context request path remains to prove forwarding. internal/mcp/tools_test.go fake output equality is adapter passthrough, not an owner rule; schema, request fields and invalid requests stay (simplify only fake window entries). integration/terminal_control_test.go JSON and MCP bounded entry/control assertions -> internal/cli/TestLogsStripTerminalControl (human/JSON) and internal/mcp/TestLogsStripTerminalControl (output passthrough and metadata) plus internal/output/TestStripTerminalControl (actual stripping and raw ring entries); retain human bounded binary path and raw follow replay/live. MCP list check only served the removed MCP bounded branch; integration list wiring is covered elsewhere.

Unrelated package failure: first AC1 run failed internal/daemon TestWaitDaemonBridge/exit_before_match (timed_out instead of exited); as instructed reran go test ./internal/daemon -count=1 once, PASS (6.119s). No unrelated test modified; continuing.

AC3 — git diff --numstat main -- integration/wait_test.go integration/terminal_control_test.go internal/cli/list_logs_test.go internal/mcp/tools_test.go: 11 insertions, 167 deletions, net reduction 156 lines (PASS, >=80). Focused: go test ./internal/cli ./internal/mcp ./integration -run "^(TestLogsMatchContext|TestWait|TestBoundedLogsStripTerminalControl|TestLogsStripTerminalControl)$" -count=1 PASS; git diff --check PASS; task check:staged PASS. Worktree commit 597ea83 (test: consolidate wait and logs coverage), branch hum-143-tests, path .worktrees/hum-143-tests; not merged. AC1 blocked: go test ./internal/app ./internal/output ./internal/daemon ./internal/cli ./internal/mcp ./integration -count=1 failed twice in untouched internal/daemon/TestWaitDaemonBridge/exit_before_match (timed_out instead of exited); second run also failed untouched TestFollowAcrossOrdinaryStartReplacement (context deadline exceeded). Mandatory standalone go test ./internal/daemon -count=1 rerun after first failure passed. Per unrelated-failure stop rule no more tests run; AC2 coverage-after, task ci, and independent verifier remain pending. Next: diagnose/load-isolate daemon timing failure outside this task or obtain an explicit exception to stop rule, then rerun AC1/AC2 and task ci on commit 597ea83, get independent verifier PASS, merge to main, finalize item and clean up worktree. Do not merge without acceptance.

Resumption (2026-09-25): cherry-picked 597ea83 onto fresh worktree hum-143-verify at main 96c17d2, yielding code commit 7fff465; earlier concurrent daemon timing failures did not reproduce. This continuation did not change unrelated tests. AC1 — go test ./internal/app ./internal/output ./internal/daemon ./internal/cli ./internal/mcp ./integration -count=1: PASS all six packages on 7fff465; independent verifier also reran and PASS. AC2 — go test ./internal/app ./internal/output ./internal/daemon ./internal/cli ./internal/mcp -count=1 -coverpkg=./internal/app,./internal/output,./internal/daemon,./internal/cli,./internal/mcp -coverprofile=/tmp/hum-wait-cover.out && go tool cover -func=/tmp/hum-wait-cover.out | tail -1: PASS 81.1% against 81.3% baseline (0.2 points below); independent verifier PASS. AC3 — git diff --numstat main -- integration/wait_test.go integration/terminal_control_test.go internal/cli/list_logs_test.go internal/mcp/tools_test.go before merge: PASS 11 added, 167 deleted, net -156 lines; independent verifier PASS. task ci: PASS on final code commit 7fff465 (security, vet, staticcheck, all tests, race, installer and smoke). Independent verifier: PASS AC1/AC2/AC3; mapped removed assertions to owner tests; no actionable review findings. git diff --check main...7fff465: PASS. Only four declared test paths changed, no protected gate files touched. Fast-forward merged 7fff465 into main. Previous notes recording AC1 blocker and pending verification are superseded by these passing results. Remaining step: mark task Done, commit backlog update, remove the hum-143-verify worktree. The pre-existing hum-143-tests worktree is not owned by this session and is left untouched.

Final delivery: main code commit 7fff465; provider completion commit 3224a7c. Worktrunk removed session-owned hum-143-verify checkout and branch after confirming clean state and workspace pane; post-remove hook closed its Herdr workspace. No implementation blocker or next step. Pre-existing hum-143-tests worktree is not session-owned; left intact.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Consolidated wait, logs-context, and terminal-control tests under their owners; merged code commit 7fff465 to main. AC1 tests and task ci pass; AC2 81.1% vs 81.3% baseline; AC3 net -156 lines. Independent verifier passed all acceptance criteria with no findings.
<!-- SECTION:FINAL_SUMMARY:END -->
