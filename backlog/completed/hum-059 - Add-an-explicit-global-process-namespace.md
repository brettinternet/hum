---
id: HUM-059
title: Add an explicit --global process namespace
status: Done
assignee: []
created_date: '2026-09-09 16:18'
updated_date: '2026-09-09 20:28'
labels:
  - cli
  - daemon
  - mcp
milestone: m-4
dependencies:
  - HUM-058
modified_files:
  - internal/app/app.go
  - internal/app/app_test.go
  - internal/protocol/protocol.go
  - internal/protocol/protocol_test.go
  - internal/daemon/client.go
  - internal/daemon/server.go
  - internal/daemon/wire_protocol.go
  - internal/daemon/runtime.go
  - internal/daemon/daemon_test.go
  - internal/daemon/wire_protocol_test.go
  - internal/daemon/runtime_test.go
  - internal/cli/root.go
  - internal/cli/commands.go
  - internal/cli/config.go
  - internal/cli/render.go
  - internal/cli/completion.go
  - internal/cli/global_scope_test.go
  - internal/cli/completion_test.go
  - internal/cli/help_contract_test.go
  - internal/mcp/tools.go
  - internal/mcp/server.go
  - internal/mcp/tools_test.go
  - internal/mcp/server_test.go
  - cmd/hum/integration_test.go
  - integration/global_scope_test.go
  - README.md
  - docs/design.md
  - docs/coding-agents.md
  - internal/skill/SKILL.md
  - internal/skill/skill_test.go
priority: high
type: feature
ordinal: 36700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
**Problem.** Machine-wide helpers such as a proxy or tunnel have no home in hum. They land in whichever project the caller happened to be in, disappear from view in every other worktree, and collide with same-named project processes. HUM-058 makes project scope canonical and discoverable; this task adds the one scope that is not a directory.

**Outcome.** An explicit `--global` scope holds machine-wide ad-hoc sessions. It is selected deliberately, never inferred, and is visible from any directory through `list --all` or `--global`. Default commands stay current-project-only, so global sessions never leak into project views or lookups.

**Selection.**
- Canonical flag `--global`, interactive shorthand `-g`, with the same placement rules as `--project`/`-C`: before or after the subcommand, and before or after NAME for `run`.
- Mutually exclusive with `--project`/`-C`. `--global` with `list --all` is a usage error because `--all` already spans every scope. Conflicts fail before daemon contact with actionable guidance.
- Applies to run, start, down, list, status, attach, logs, wait, input, signal, restart, stop, and remove. Name completion under `--global` completes only global names.

**Semantics.**
- Global sessions are ad-hoc retained sessions only. `run` uses the lexical invocation directory as child cwd; supervision scope never changes a child's cwd.
- Under `--global`, `start` and `restart` never read a manifest; they act on retained global sessions and otherwise report not-found. `down` stops every running global session.
- `init` and `up` reject `--global` before daemon contact and point to `hum --global run NAME -- COMMAND`.
- `--global list` and nameless `--global status` show only global sessions. `list --all` includes global sessions under a `global` heading whose selector prefix is `hum --global`.
- `(scope, name)` remains the record key: equal names coexist in two worktrees and global, and records never move between scopes.
- Not-found guidance from HUM-058 gains global matches: `other_scopes` entries with `scope` `global` render as `hum --global logs NAME`.

**Wire protocol, runtime state, MCP.**
- Requests carry an explicit scope; legacy requests without one remain project-scoped. Global requests carry no project identity and never borrow the caller's directory.
- Runtime state persists global groups alongside canonical project groups; crash recovery restores them under the global key. Incompatible prior state produces the established actionable upgrade failure; never silently orphan, merge, or retarget a live record.
- MCP tools accept `scope` (`project` default, or `global`). `project_root` is required for project and rejected for global. `list` `all` includes global records. Existing project-root validation is unchanged.
- The `scope` JSON field introduced by HUM-058 gains the value `global`; `project_root` is omitted for global records.

**Documentation.** CLI help, README.md, docs/design.md, docs/coding-agents.md, MCP descriptions, and the embedded skill document `--global`/`-g`, placement and conflicts, global ad-hoc limits, global not-found guidance, and the `scope` `global` JSON value. Generated commands and JSON spell `--global`; `-g` appears once as shorthand. Examples: `hum -g run proxy -- caddy run`, `hum --global logs proxy`, `hum list --all`.

**Non-goals.** Declaring global processes in `hum.yaml`, moving processes between scopes, multi-scope mutation beyond `list --all`, silent fallback to global after a project miss, an environment variable for scope selection, or changing child cwd because the supervision scope is global.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `go test ./internal/app -run '^TestSupervisorGlobalScope$' -count=1 -v` exits 0 and prints PASS, proving equal names coexist across two projects and global, project list/get/output/wait/stop/remove cannot see a global record and global operations cannot see project records, global operations work independently of caller cwd, and a project not-found result for a name present globally carries a `global` `other_scopes` match.
- [x] #2 `go test ./internal/cli -run '^TestGlobalScopeSelection$' -count=1 -v` exits 0 and prints PASS, proving `--global` and `-g` work before or after run, start, down, list, status, attach, logs, wait, input, signal, restart, stop, and remove, and before or after NAME for run; a global ad-hoc run records the lexical invocation directory as cwd; `--global list` and nameless `--global status` show only global sessions; `--global` name completion lists only global names; `--global` with `--project`/`-C`, with `list --all`, with init, or with up fails before daemon contact with actionable guidance; `--global start`/`restart` on an unknown name reports not-found without reading a manifest; and `--global down` stops only global sessions.
- [x] #3 `go test ./internal/cli -run '^TestGlobalScopeDiscovery$' -count=1 -v` exits 0 and prints PASS, proving a global session is hidden from default project list and status, appears in `list --all` under a `global` heading with the `hum --global` selector prefix and JSON `scope` `global` without `project_root`, and a missing project name that exists globally yields `not_found` with a copyable `hum --global ...` command and no mutation.
- [x] #4 `go test ./internal/protocol ./internal/daemon ./internal/mcp -run GlobalScope -count=1 -v` exits 0 and prints PASS, proving the wire protocol carries explicit scope with a project-scoped legacy default; global requests carry no project identity; all-scope listing includes global records; MCP accepts `scope`, requires `project_root` for project and rejects it for global, and includes global records in `list` `all`; and conflicting selectors are rejected.
- [x] #5 `go test ./internal/daemon -run '^TestRuntimeStateGlobalScope$' -count=1 -v` exits 0 and prints PASS, proving global sessions survive daemon crash recovery under the global key, never collide with a project record of the same name, and incompatible prior state produces the established actionable upgrade failure.
- [x] #6 `go test ./internal/cli ./internal/mcp ./internal/skill -run 'GlobalScopeDocs|HelpContract|FlagAliases' -count=1 -v` exits 0 and prints PASS, proving CLI help, README.md, docs/design.md, docs/coding-agents.md, MCP descriptions, and the embedded skill document `--global` and `-g`, placement and conflicts, global ad-hoc limits, global not-found guidance, and the `scope` `global` JSON value with copyable examples, and that generated output uses the long `--global` flag.
- [x] #7 `task ci` exits 0.
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
AC#1 — PASS: go test ./internal/app -run ^TestSupervisorGlobalScope$ -count=1 -v exited 0 and printed PASS.
AC#2 — PASS: go test ./internal/cli -run ^TestGlobalScopeSelection$ -count=1 -v exited 0 and printed PASS.
AC#3 — PASS: go test ./internal/cli -run ^TestGlobalScopeDiscovery$ -count=1 -v exited 0 and printed PASS.
AC#4 — PASS: go test ./internal/protocol ./internal/daemon ./internal/mcp -run GlobalScope -count=1 -v exited 0 and printed PASS in all packages.
AC#5 — PASS: go test ./internal/daemon -run ^TestRuntimeStateGlobalScope$ -count=1 -v exited 0 and printed PASS.
AC#6 — PASS: go test ./internal/cli ./internal/mcp ./internal/skill -run GlobalScopeDocs|HelpContract|FlagAliases -count=1 -v exited 0 and printed PASS.
AC#7 — PASS: task ci exited 0 on rebased final implementation commit ae4e461 after checks, full tests, race tests, build, and smoke coverage.
Review — Independent reviewer findings were fixed. Final independent verifier returned PASS for AC#1–#7 and DoD#1–#6 before integration; post-rebase task ci also passed.
Modified-file deviations: internal/cli/init.go rejects global init before daemon contact; internal/cli/input.go routes global input without reading a manifest; internal/cli/mcp.go forwards scope through the CLI MCP adapter and documents it; internal/cli/flag_alias_test.go verifies -g on every command; internal/protocol/restart_policy_test.go updates the frozen protocol version to 17 after integration with HUM-060. These are direct HUM-059 surfaces omitted from the declared list. No tests were deleted, skipped, or weakened; no protected gate file changed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented explicit global process scope across CLI, daemon protocol/runtime recovery, supervisor, MCP, completion, rendering, guidance, and documentation. Added focused global-scope coverage and bumped the integrated private protocol to version 17. Implementation ae4e461 merged to main as d77e02c. task ci and independent verification passed.
<!-- SECTION:FINAL_SUMMARY:END -->
