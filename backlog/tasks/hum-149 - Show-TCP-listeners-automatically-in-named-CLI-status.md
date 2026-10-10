---
id: HUM-149
title: Show TCP listeners automatically in named status
status: Done
assignee: []
created_date: '2026-10-10 15:32'
updated_date: '2026-10-10 15:54'
labels: []
dependencies: []
ordinal: 33000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: status for one running process always includes its observed TCP listeners, on both the CLI (`hum status NAME`, human and --json) and the MCP status tool with `name`. The HUM-148 opt-ins (`--ports` and MCP `ports: true`) are removed so there is a single rule: named status of a running process includes listeners. Hum has no users, so no backward compatibility, deprecation path, or breaking-change marker is needed.

Scope:
- CLI: remove the `--ports` flag and its single-name validation. Named `hum status NAME` always requests the existing listener snapshot. Help and examples teach `hum status NAME`.
- MCP: remove the `ports` input from the status tool schema and input struct. Named status always requests the snapshot; aggregate MCP status is unchanged.
- Daemon/protocol: keep the internal `GetRequest.Ports` field and the daemon dispatch unchanged. About ten other internal Get callers (wait, logs, send, restart polling, etc.) must keep skipping inspection.
- Bounded latency: add one inspection deadline in `internal/app` `GetPortsScoped` (about 2s, a named constant) so macOS lsof, which today stops only when the caller cancels, cannot stall named status. When the deadline expires, return `partial` (with any valid listeners) or `unavailable` plus a diagnostic, never a command error. Do not change platform inspectors.
- Human output: replace the `ports_state:` / `ports_diagnostic:` / `port: transport=... address=... port=... pids=...` lines with one compact line, `listening: 127.0.0.1:5173 (pid 4182), [::]:9229 (pids 4182,4190)`. Bracket IPv6 addresses; print `listening: none` for a successful empty result. For denied, partial, or unavailable, print the state and diagnostic, e.g. `listening: unavailable (lsof not found)`; for partial, print the listeners followed by `(partial: DIAGNOSTIC)`. The transport is omitted because it is always tcp.
- JSON: the existing `ports` object (state, diagnostic, listeners with transport/address/port/pids) is unchanged in shape and is present on every running named snapshot. This is an additive cli-json-v1 change, so no schema_version bump.
- Missing, stopped, exited, and unlaunched named status keep current semantics, do not inspect, and omit `ports`. Aggregate `hum status` and `list` never inspect.
- Docs: append a dated revision to decision-001 replacing "opt-in" with "automatic for named status of a running process". Update README.md, docs/design.md, docs/cli-json-v1.md, and docs/coding-agents.md to remove `--ports`/`ports: true` and keep the group-scope, escaped-process, container, snapshot-race, and empty-versus-unavailable caveats.

Non-goals: backward compatibility for `--ports` or MCP `ports` (no alias, migration note, or rejection tests), aggregate or cross-worktree port lookup (DRAFT-005), listener readiness (DRAFT-004), configuration to disable inspection, inspector/platform redesign, polling, port allocation, URL inference, container port forwarding, and JSON schema shape changes.

Modified-file contract: internal/cli/ (commands, render, and focused tests including status_ports_test.go and flag_alias_test.go); internal/mcp/ (tools.go and ports_test.go); internal/app/ (the GetPortsScoped deadline and its test); integration/status_ports_test.go; backlog/decisions/decision-001 (append-only revision); README.md, docs/design.md, docs/cli-json-v1.md, docs/coding-agents.md. internal/protocol and internal/daemon are not expected to change. Leave the untracked draft-005 untouched (it mentions `--ports`); report it instead. Record a justification before expanding scope.

Testing: implement first; follow docs/development.md#tests. Update existing opt-in tests to the new contract rather than adding parallel ones. Regressions to catch: named CLI/MCP status silently omitting listeners; aggregate status or internal Get callers inspecting sockets; a stalled inspector blocking status past the deadline (use a controlled blocking fake, not timing thresholds where avoidable); inspection failure becoming a command error; stopped/missing semantics changing; ownership and unrelated-listener exclusion still asserted.

Dependency: follows completed HUM-148. Next action: remove the flag in internal/cli/commands.go (status command around L215 and L1092-1151) and the MCP input in internal/mcp/tools.go (L561, L1157, L1626), then add the app-layer deadline.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 mise exec go -- go test ./internal/cli -run "Ports|Status|FlagAlias" -count=1 -v exits 0 with named tests executed: named human and JSON status include listeners without any flag; human output uses the single `listening:` line for available, empty, partial, denied, and unavailable; aggregate status does not inspect; missing/stopped/unlaunched output is unchanged and omits ports; failed inspection does not fail the command.
- [x] #2 mise exec go -- go test ./internal/mcp -run "Ports|Status|Schema" -count=1 -v exits 0 with named tests executed: named MCP status includes ports without any input; aggregate MCP status and the non-status internal Get callers send Ports=false.
- [x] #3 mise exec go -- go test ./internal/app ./internal/daemon -run Ports -count=1 -v exits 0 with named tests executed, including a controlled blocking inspector proving GetPortsScoped returns partial/unavailable with a deadline diagnostic instead of blocking or erroring; existing cancellation, identity-change, failure-state, and bypass tests still pass.
- [x] #4 mise exec go -- go test ./integration -run "^TestStatusPorts$" -count=1 -v exits 0 using `status NAME --json`; it reports the child ephemeral TCP listener with the child PID and excludes the unrelated listener.
- [x] #5 task cli:build && bin/hum status --help exits 0; help shows no --ports and its examples teach `hum status NAME`. rg -n -e '--ports' -e 'ports: true' -e 'ports:true' README.md docs internal integration exits 1 (no matches anywhere, tests included). decision-001 has a new dated revision describing automatic named inspection. mise exec go -- go test ./internal/cli ./internal/mcp -run "TestHelpContract|TestDocs" -count=1 -v exits 0 with named tests executed.
- [x] #6 task cli:test exits 0 with no skipped, deleted, or weakened regression checks; opt-in tests are converted to the automatic contract and keep their ownership, exclusion, and failure-state assertions.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 task ci passes on the final commit
- [x] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [x] #3 An independent verifier pass returned PASS for every acceptance criterion
- [x] #4 The diff touches only the paths declared in the task's modified-file list, or the deviation is justified in Implementation Notes
- [x] #5 No test was deleted, skipped, or weakened
- [x] #6 No protected gate file was modified unless the owner labelled this task tooling
<!-- DOD:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Scope clarification: internal/mcp/tools_test.go needs three existing direct status callers updated for the removed boolean parameter; no assertion changes. The existing CLI stub in internal/cli/manifest_test.go gains an optional Get observer so adapter tests can assert automatic inspection without spawning real children. Backlog CLI has no decision edit operation, so the declared decision-001 append-only revision uses a direct edit (task state remains CLI-owned). MCP has no aggregate status call: status requires name; its existing aggregate tool is list, which remains unchanged.

AC#1: mise exec go -- go test ./internal/cli -run "Ports|Status|FlagAlias" -count=1 -v — PASS. Automatic named human/JSON requests, all inspection outcomes, aggregate bypass, and terminal/missing/unlaunched status checked.
AC#2: mise exec go -- go test ./internal/mcp -run "Ports|Status|Schema" -count=1 -v — PASS. Named status automatic, aggregate list and non-status metadata operations bypass inspection.
AC#3: mise exec go -- go test ./internal/app ./internal/daemon -run Ports -count=1 -v — PASS. Controlled blocking fake returns on deadline; partial listeners retained and empty timeout unavailable; cancellation, identity changes, and bypass preserved.
AC#4: mise exec go -- go test ./integration -run "^TestStatusPorts$" -count=1 -v — PASS. Real child listener/PID ownership and unrelated-listener exclusion preserved; aggregate status omits ports.
AC#5: task cli:build && bin/hum status --help — PASS; rg -n -e "--ports" -e "ports: true" -e "ports:true" README.md docs internal integration — exit 1, no matches; mise exec go -- go test ./internal/cli ./internal/mcp -run "TestHelpContract|TestDocs" -count=1 -v — PASS. Decision revision dated 2026-10-10 appended.
AC#6: task cli:test — PASS, all Go packages and 12 Herdr Python tests. Existing opt-in checks converted to automatic contract; no regression test deleted/skipped/weakened.
Independent verifier 9fbc7ccf returned PASS for AC#1–6; no concrete in-scope defects or residual risks. This was the single general review pass. task ci passed before commit (security, checks, tests, race, smoke); affected LSP diagnostics clean. No protected gate files changed.
User selected release v0.17.1. Next: commit implementation, rerun task ci on that commit, push main, wait for its CI run to start, then tag that exact commit and push v0.17.1. Keep task In Progress until release delivery completes. Both pre-existing untracked drafts remain untouched.

Delivery completed: implementation commit 7102d47d20b0bdc332a97ec8190aa684b6d78ba6 pushed to main; task ci passed on that final implementation commit before push. GitHub CI run 38065196633 succeeded. Tag v0.17.1 points to that exact commit; release run 38065233192 succeeded and published at 2026-10-10T15:53:55Z with five platform archives and checksums.txt. Release URL: https://github.com/brettinternet/hum/releases/tag/v0.17.1.
The authorized release workflow generated CHANGELOG.md in 2dbf3b7, now fast-forwarded locally; this release-generated delivery artifact is outside the implementation file contract by design. Independent verifier PASS for all ACs remains valid; no implementation changed after review.
No remaining blocker or implementation step. Task claim released by Done status. Both pre-existing untracked drafts (DRAFT-004 and DRAFT-005) remain untouched. Final bookkeeping step: commit this closure record, rerun task ci on the closure commit, and push main.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Named CLI and MCP status automatically report TCP listeners with compact human output and a two-second best-effort deadline. Aggregate/internal reads remain inspection-free. All acceptance checks and independent verification passed. Published v0.17.1 from 7102d47; CI and release workflows succeeded.
<!-- SECTION:FINAL_SUMMARY:END -->
