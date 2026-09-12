---
id: HUM-104
title: Accept an explicit manifest in MCP definition-resolving tools
status: To Do
assignee: []
created_date: '2026-09-12 01:34'
updated_date: '2026-09-12 01:34'
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
Outcome: MCP clients gain parity with the CLI `--file/-F` selector from HUM-103 by passing an optional `manifest` path to the project tools that resolve or merge definitions.

Scope:
- Add an optional `manifest` string input only to `start`, `up`, `restart`, `list`, `status`, and `logs`. Relative paths resolve from the required `project_root`; absolute paths must remain within it. Omission retains `hum.yaml`/discovery behavior. Explicit selection loads only that file with the same rejection rules as the CLI (missing, non-regular, directory, symlink-escaped, outside-root) before daemon contact.
- Runtime-only tools (`down`, `stop`, `remove`, `signal`, `wait`, `input`) and global scope do not accept `manifest`; the input is rejected with the existing structured error shape.
- Reuse the path-aware loader from HUM-103 through `internal/cli/mcp.go`; do not duplicate resolution in `internal/mcp/tools.go`.
- Preserve project-root namespace, environment inheritance, bounded responses, structured error behavior, and the private daemon protocol. Sources remain `manifest:<relative path>`.
- Update MCP tool descriptions and docs/coding-agents.md.

Non-goals: per-manifest namespaces; manifests outside `project_root`; changing daemon protocol; CLI changes.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `go test ./internal/mcp -run 'Test.*ManifestSelection' -count=1 -v` exits 0 and prints PASS for omitted and explicit `manifest` on start/up/restart/list/status/logs, project-root-relative resolution, exact-file/no-discovery behavior, outside-root and symlink-escape rejection before daemon contact, rejection on runtime-only tools and global scope, and unchanged project namespace and bounded response contracts.
- [ ] #2 AC2 — `go test ./integration -run TestMCPAlternateManifest -count=1 -v` exits 0 and prints PASS after one real daemon serves a CLI-started `hum.yaml` record and an MCP `up` with `manifest: hum.dev.yaml` in the same project namespace.
- [ ] #3 AC3 — `task cli:check && task test` exits 0 after tool descriptions and docs/coding-agents.md document the `manifest` input, its resolution rules, and the tools that reject it.
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
