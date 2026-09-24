---
id: HUM-145
title: Document where each kind of test belongs so agents stop duplicating them
status: To Do
assignee: []
created_date: '2026-09-24 22:55'
updated_date: '2026-09-24 22:58'
labels:
  - docs
dependencies:
  - HUM-138
  - HUM-140
  - HUM-142
modified_files:
  - docs/development.md
  - AGENTS.md
priority: medium
type: docs
ordinal: 29000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: docs/development.md gets a short "Tests" section. Agents and contributors will know where a new test belongs before they write it, and the duplication that HUM-131, HUM-139, HUM-142, and HUM-143 remove stops coming back. Almost every test here is written by an agent working one task at a time. With no written rule, each task re-tested its feature at every layer and copied its own helpers. The suite is now about 56k test lines against 36k source lines, with 733 top-level tests.

Add a `## Tests` section after `## Checks` in docs/development.md, at most about 40 lines, in the same plain style as the rest of the file. It must state these rules, each tied to a real example path:
1. Test a rule once, where it is computed, with fakes. Examples: internal/orchestrate for `up`/`down` scheduling (TestOrchestrateUp, fakes UpOperations and EnsureOperations); internal/app for supervisor, wait, and readiness semantics; internal/output for ring, match, and terminal-control rules; internal/protocol for wire shapes.
2. Adapter tests (internal/cli, internal/mcp) cover only what the adapter adds: argument and flag validation, request forwarding, human and JSON rendering, MCP structuredContent and isError, and exit codes. Use a stub daemon or fake client, not real children. Point at waitCLIStubDaemon (internal/cli/wait_test.go), manifestCLIRecoveryStubDaemon (internal/cli/manifest_test.go), and newTestServer/fakeClient (internal/mcp/tools_test.go).
3. integration/ runs the built binary. Add one test per user-visible behavior, to prove the wiring, not every rule variant. Every integration test calls t.Parallel() and builds its own runtime with lifecycleNewRuntime or testutil.RuntimeDir.
4. Generic waits and process helpers live in internal/testutil (WaitForFile, WaitForOutput, WaitUntil, WaitForPathGone, WaitForProcessGroupGone, Run, Start). Add to testutil rather than writing a file-local copy. Do not prefix helpers with task IDs.
5. Poll for a condition, never sleep a fixed time, unless elapsed time is the behavior under test. When a test waits on stop grace or another timeout that is not under test, shorten it through configuration (HUM_STOP_GRACE, app.Options).
6. Performance guarantees are asserted with counters (for example TestEventHistoryAppendCost), not wall-clock time. Timing measurements go in Benchmark functions.
7. Code that parses bytes hum does not control gets a fuzz target (HUM-144) in addition to example tests.
8. Structural doc and help checks (TestHelpContract, TestDocsReferenceRealCommandsAndFlags, TestDocsCoverEveryCommand, TestDocsCoverEveryTool) replace phrase or prose assertions. Do not assert help or doc wording.

Also add one line to AGENTS.md under Tooling: "Before adding a test, follow the placement rules in docs/development.md#tests."

Check every named helper and test against the tree when you write the section. This task depends on HUM-138, HUM-140, and HUM-142 so that the parallel, smoke, and helper statements are true. If HUM-144 has not merged, write rule 7 without the task reference.

Non-goals: code or test changes; a separate testing document.

Unrelated failures: if `task ci` or a package run fails in a test this task did not touch, rerun that package once. If the rerun passes, record both runs in Implementation Notes and continue; if it fails again, stop and report it. Do not fix unrelated tests in this task.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `rg -n "^## Tests$" docs/development.md` exits 0, and the section between it and the next `## ` heading is at most 45 lines (`awk "/^## Tests$/{f=1;next} /^## /{f=0} f" docs/development.md | wc -l` prints at most 45).
- [ ] #2 AC2 — every Go identifier the section names exists: for each name, `rg -n "func (\([^)]*\) )?<name>\b|type <name>\b" internal integration` exits 0. Implementation Notes list each name and its result.
- [ ] #3 AC3 — `rg -n "docs/development.md#tests" AGENTS.md` exits 0.
- [ ] #4 AC4 — `go test ./internal/cli -run "^(TestDocsReferenceRealCommandsAndFlags|TestDocsCoverEveryCommand)$" -count=1` exits 0.
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
