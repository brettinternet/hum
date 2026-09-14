---
id: HUM-116
title: Prefer a private .hum.yaml over the shared hum.yaml manifest
status: To Do
assignee: []
created_date: '2026-09-14 23:39'
updated_date: '2026-09-14 23:50'
labels:
  - cli
  - mcp
  - config
  - docs
milestone: m-1
dependencies:
  - HUM-113
references:
  - internal/project/resolver.go
  - internal/project/manifest.go
  - internal/project/init.go
  - internal/cli/mcp.go
  - internal/daemon/server.go
modified_files:
  - internal/project/manifest.go
  - internal/project/manifest_test.go
  - internal/project/resolver.go
  - internal/project/resolver_test.go
  - internal/project/init.go
  - internal/project/init_test.go
  - internal/cli/manifest.go
  - internal/cli/manifest_test.go
  - internal/cli/init.go
  - internal/cli/init_test.go
  - internal/cli/doctor.go
  - internal/cli/doctor_test.go
  - internal/cli/root.go
  - internal/cli/root_test.go
  - internal/cli/commands.go
  - internal/cli/man.go
  - internal/cli/man_test.go
  - internal/cli/mcp_test.go
  - internal/cli/help_contract_test.go
  - internal/cli/discovery_test.go
  - internal/mcp/tools.go
  - internal/mcp/tools_test.go
  - integration/init_test.go
  - integration/doctor_test.go
  - integration/manifest_test.go
  - README.md
  - docs/design.md
  - docs/coding-agents.md
  - internal/skill/SKILL.md
  - plugins/hum/skills/hum/SKILL.md
priority: medium
type: enhancement
ordinal: 88800
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: Hum recognizes `.hum.yaml` as the private project manifest with deterministic precedence `--file PATH` > `.hum.yaml` > `hum.yaml`. Each file is a complete manifest; Hum never merges declarations. Users can globally ignore `.hum.yaml` while projects keep committing `hum.yaml`.

## Why

Developers sometimes need machine-specific commands, environment files, or local service choices that should not be committed. `--file` supports complete alternate manifests but is cumbersome as a permanent per-repository convention. A standard private filename gives those repositories a zero-flag workflow without renaming or breaking the committed `hum.yaml`.

## Contract

- Default selection lives in one place in `internal/project`: `ResolveDefinitions*`, `ResolveDefinitionsReadOnly`, and `LoadDefinitions` all select `.hum.yaml` when it exists at the project root and otherwise `hum.yaml`. The CLI, MCP (`internal/cli/mcp.go` resolver), and daemon stop-grace lookup (`internal/daemon/server.go` calls `project.LoadDefinitions`) therefore share the precedence without daemon changes. Explicit `--file`/MCP `manifest` selection always wins and loads exactly the selected file.
- Source identity: an active `.hum.yaml` yields `Source: "manifest:.hum.yaml"` and display name `.hum.yaml` (the same shape an explicit `--file .hum.yaml` produces); an active `hum.yaml` keeps today's `manifest` source and `hum.yaml` display byte-for-byte. `IsManifestSource`/`SameManifestSource` already treat both as manifest sources, so switching between the two defaults is definition drift, not removal.
- When both defaults exist, `.hum.yaml` wholly shadows `hum.yaml`; declarations and top-level settings are not merged. A malformed, unreadable, non-regular, or unsafe `.hum.yaml` is authoritative and fails with a `ConfigurationError{Source: ".hum.yaml"}` rather than falling back to `hum.yaml`. The default `.hum.yaml` passes through the same regular-file, root-containment, and symlink validation as `ResolveManifestPath`.
- Diagnosability: `hum list` process sources and `ConfigurationError` messages name `.hum.yaml`. `hum doctor` `project.discovery` details report `manifest` (active display name, already emitted) and add `shadowed_manifest: "hum.yaml"` only when both defaults exist; the check stays `PASS`. Drift and retained-record behavior use the effective manifest exactly as for an explicit alternate manifest.
- `hum init` creates `hum.yaml` when neither default exists. When `.hum.yaml` exists it is the existing manifest: plain init reports `exists` with the `.hum.yaml` path and changes nothing; `--force` atomically replaces that regular `.hum.yaml` (refusing symlinks/non-regular files as today) and never touches `hum.yaml`. Error and help text say "manifest" or name the actual path instead of hard-coding `hum.yaml`.
- Help, man page, MCP tool descriptions, skills, and docs state the precedence, the no-merge rule, the fail-closed rule for an invalid `.hum.yaml`, and that `.hum.yaml` suits repository or global Git ignore rules while ignored configuration is not shared with collaborators or CI.
- Repositories containing only `hum.yaml` retain identical selection, source identity, output, and errors.

## Non-goals

Merging or layering manifests; editing `.gitignore` or a global excludes file; user-home configuration; changing explicit alternate-manifest semantics; changing manifest syntax; inferring whether either file is tracked by Git; daemon protocol changes.

## Modified-file contract

Code: internal/project/manifest.go, internal/project/resolver.go, internal/project/init.go, internal/cli/manifest.go, internal/cli/init.go, internal/cli/doctor.go, internal/cli/root.go, internal/cli/commands.go, internal/cli/man.go, internal/mcp/tools.go (descriptions only).
Tests: the `_test.go` siblings of those files plus internal/cli/mcp_test.go, internal/cli/help_contract_test.go, internal/cli/discovery_test.go, internal/mcp/tools_test.go, integration/init_test.go, integration/doctor_test.go, integration/manifest_test.go.
Docs: README.md, docs/design.md, docs/coding-agents.md, internal/skill/SKILL.md, plugins/hum/skills/hum/SKILL.md.
Existing tests may be updated only where asserted help/description text changes; none may be removed or weakened.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `mise exec go -- go test ./internal/project -run "TestPrivateManifest" -count=1 -v` exits 0 and prints PASS for subtests named `PrivateWins`, `PrivateAlone`, `SharedAlone`, `NoMerge`, `InvalidPrivateNoFallback`, `UnreadablePrivateNoFallback`, `SymlinkPrivateNoFallback`, `DirectoryPrivateNoFallback`, and `ExplicitFileWins`, covering `ResolveDefinitionsContext`, `ResolveDefinitionsReadOnly`, and `LoadDefinitions`; private-manifest definitions carry `Source == "manifest:.hum.yaml"` and the shared-only case still yields `Source == "manifest"`.
- [ ] #2 AC2 — `mise exec go -- go test ./internal/cli -run "TestPrivateManifest|TestDoctorPrivateManifest" -count=1 -v` exits 0 with PASS cases proving: `hum list --json` in a root with both defaults reports only the `.hum.yaml` declarations with `source` `manifest:.hum.yaml`; `hum list -F hum.yaml` selects the shared file; `hum doctor --json` emits `project.discovery` `PASS` with `details.manifest == ".hum.yaml"` and `details.shadowed_manifest == "hum.yaml"`, and omits `shadowed_manifest` when only one default exists; an invalid `.hum.yaml` beside a valid `hum.yaml` fails `list` with a `manifest_invalid` error naming `.hum.yaml`; and `mcpResolver.ResolveManifest` with an empty `manifest` returns the `.hum.yaml` definitions.
- [ ] #3 AC3 — `mise exec go -- go test ./internal/project ./internal/cli ./integration -run "TestInitPrivateManifest" -count=1 -v` exits 0 with PASS cases proving: init writes `hum.yaml` when neither default exists; with `.hum.yaml` present, plain init exits 1, reports outcome `exists` with the `.hum.yaml` path, and leaves both files byte-identical; `init --force` replaces the regular `.hum.yaml` and leaves `hum.yaml` byte-identical; `init --force` refuses a symlinked `.hum.yaml`.
- [ ] #4 AC4 — `rg -lF ".hum.yaml" README.md docs/design.md docs/coding-agents.md internal/skill/SKILL.md plugins/hum/skills/hum/SKILL.md | wc -l` prints 5; `rg -qF shadowed_manifest docs/design.md`, `mise exec go -- go run ./cmd/hum man | rg -qF ".hum.yaml"`, and `mise exec go -- go run ./cmd/hum --help | rg -qF ".hum.yaml"` each exit 0; review confirms README.md and docs/design.md each state the precedence `--file`, then `.hum.yaml`, then `hum.yaml`; complete replacement rather than merging; fail-closed on an invalid `.hum.yaml`; and the Git-ignore trade-off.
- [ ] #5 AC5 — `task cli:check && task test` exits 0 with no test removed, skipped, or weakened; `git diff --stat main -- "internal/project/*_test.go" "internal/cli/*_test.go" "integration/*_test.go"` shows only additions plus help/description string updates, proving repositories with only `hum.yaml` keep existing behavior.
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
