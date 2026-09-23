---
id: HUM-104
title: Accept an explicit manifest in MCP definition-resolving tools
status: Done
assignee: []
created_date: '2026-09-12 01:34'
updated_date: '2026-09-12 03:30'
labels: []
dependencies:
  - HUM-103
modified_files:
  - internal/cli/mcp.go
  - internal/cli/mcp_test.go
  - internal/mcp/tools.go
  - internal/mcp/tools_test.go
  - integration/mcp_test.go
  - docs/coding-agents.md
priority: medium
type: enhancement
ordinal: 76800
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: MCP clients gain parity with HUM-103 when a tool actually resolves or merges project definitions, by passing an optional `manifest` path.

Scope:
- Add an optional `manifest` string input to `start`, `up`, `restart`, and `list`. `status` and `logs` address existing runtime records and therefore do not accept it.
- Relative paths resolve from the required `project_root`; absolute paths must remain within it. Omission retains `hum.yaml`/conventional discovery. Explicit selection loads exactly that file, with the HUM-103 missing, non-regular, directory, symlink-escape, and outside-root rejection before daemon contact.
- `start` and `restart` use definitions from the selected file but preserve the existing retained-record fallback when the requested name is not declared. `up` starts only selected declarations and reports retained manifest records absent from that file through the existing removed-definition behavior. `list` merges selected stopped declarations with all retained records in the project; retained records win by name.
- Reject `manifest` on `down`, `status`, `logs`, `wait`, `input`, `stop`, `remove`, and `signal`, and whenever `scope` is `global`, with the existing `invalid_request` tool-error shape.
- Extend the resolver boundary implemented by `internal/cli/mcp.go`; keep path validation and definition loading in the HUM-103 project/CLI loader rather than duplicating filesystem resolution in `internal/mcp/tools.go`.
- Preserve the project-root namespace, project-root-relative child cwd, environment inheritance, bounded responses, private daemon protocol, and stable `manifest:<project-root-relative path>` source.
- Update MCP tool schemas/descriptions and docs/coding-agents.md.

Non-goals: aggregate MCP logs; stopped-declaration support in MCP status; per-manifest namespaces; manifests outside `project_root`; daemon protocol changes; CLI behavior.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 AC1 — `go test ./internal/cli ./internal/mcp -run "Test.*ManifestSelection" -count=1 -v` exits 0 and prints PASS for resolver-adapter and tool-schema/handler coverage of omitted and explicit `manifest` on start/up/restart/list, project-root-relative and absolute-inside-root resolution, exact-file/no-discovery behavior, rejection before daemon contact, retained-record fallback/precedence, removed-definition results, stable source identity, and unchanged bounded response contracts.
- [x] #2 AC2 — `go test ./integration -run TestMCPAlternateManifest -count=1 -v` exits 0 and prints PASS after one real daemon serves default-manifest and alternate-manifest definitions in one project namespace, list merges the selected declarations with retained records, and runtime-only/global calls reject `manifest` as `invalid_request`.
- [x] #3 AC3 — `task cli:check && task test` exits 0 after MCP schemas, tool descriptions, and docs/coding-agents.md document `manifest` resolution, retained-record behavior, and the exact tools/scopes that accept or reject it.
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
AC#1 evidence: `go test ./internal/cli ./internal/mcp -run "Test.*ManifestSelection" -count=1 -v` exited 0 and printed PASS; coverage includes omitted/explicit selection, path handling, exact-file discovery suppression, pre-daemon rejection, retained fallback/precedence, removed definitions, stable source identity, and bounded responses.
AC#2 evidence: `go test ./integration -run TestMCPAlternateManifest -count=1 -v` exited 0 and printed PASS against a real daemon.
AC#3 evidence: `task cli:check && task test` exited 0.
Delivery evidence: commit fe73796 (`feat(mcp): add manifest selection`) fast-forward merged to main. `task check:staged` passed before commit. `task ci` passed on final commit, including security, vet, staticcheck, full tests, race tests, build, manpage, and smoke checks. Independent verifier returned PASS for AC1, AC2, and AC3 with no material residual defects. Diff is limited to the six declared modified files; no test was deleted, skipped, or weakened; no protected gate file was modified.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented explicit MCP manifest selection for start, up, restart, and list, including loader delegation, retained-record semantics, schemas, documentation, and comprehensive unit/integration coverage. Merged commit fe73796 to main and passed the full CI gate.
<!-- SECTION:FINAL_SUMMARY:END -->
