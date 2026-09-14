---
id: HUM-116
title: Prefer a private .hum.yaml over the shared hum.yaml manifest
status: To Do
assignee: []
created_date: '2026-09-14 23:39'
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
  - internal/cli/mcp_test.go
  - internal/cli/man.go
  - internal/cli/man_test.go
  - internal/cli/help_contract_test.go
  - internal/mcp/tools_test.go
  - integration/init_test.go
  - integration/doctor_test.go
  - README.md
  - docs/design.md
  - docs/coding-agents.md
  - docs/cli-json-v1.md
  - internal/skill/SKILL.md
  - plugins/hum/skills/hum/SKILL.md
priority: medium
type: enhancement
ordinal: 88800
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: Hum automatically recognizes `.hum.yaml` as the canonical private project manifest, with deterministic precedence `--file PATH` > `.hum.yaml` > `hum.yaml`. Each file is a complete manifest: Hum never merges declarations. This lets users globally ignore `.hum.yaml` while projects can continue committing `hum.yaml`.

## Why

Developers sometimes need machine-specific commands, environment files, or local service choices that should not be committed. `--file` supports complete alternate manifests but is cumbersome as a permanent per-repository convention. A standard private filename gives those repositories a zero-flag workflow without renaming or breaking the established committed `hum.yaml`.

## Contract

- Default CLI and MCP project resolution use `.hum.yaml` when it exists and otherwise use `hum.yaml`. Explicit `--file`/MCP `manifest` selection always wins and continues loading exactly the selected file.
- When both defaults exist, `.hum.yaml` wholly shadows `hum.yaml`; declarations and top-level settings are not merged. A malformed, unreadable, non-regular, or unsafe `.hum.yaml` is authoritative and must fail rather than falling back to `hum.yaml`.
- Human and machine-readable discovery surfaces expose the selected manifest path/source so shadowing is diagnosable. `hum list` process sources identify `.hum.yaml`, and `hum doctor` reports the active default plus the shadowed `hum.yaml` when both exist. Drift and retained-record behavior use the effective manifest exactly as they do for an explicit alternate manifest.
- `hum init` still creates `hum.yaml` when neither default exists. If `.hum.yaml` is active, init treats it as the existing manifest; normal init leaves it unchanged and `--force` replaces that active regular file atomically without modifying `hum.yaml`.
- Existing repositories containing only `hum.yaml` retain byte-for-byte-compatible selection and behavior. `.hum.yaml` follows the same root containment, file-type, parsing, environment, cwd, and symlink safety rules as every other manifest.
- Document `.hum.yaml` as suitable for repository or global Git ignore rules, while warning that ignored configuration is not shared with collaborators or CI.

## Non-goals

Merging or layering manifests; automatically editing `.gitignore` or a global Git excludes file; introducing user-home configuration; changing explicit alternate-manifest semantics; changing manifest syntax; inferring whether either file is tracked by Git.

Modified-file contract: internal/project/manifest.go, internal/project/manifest_test.go, internal/project/resolver.go, internal/project/resolver_test.go, internal/project/init.go, internal/project/init_test.go, internal/cli/manifest.go, internal/cli/manifest_test.go, internal/cli/init.go, internal/cli/init_test.go, internal/cli/doctor.go, internal/cli/doctor_test.go, internal/cli/mcp_test.go, internal/cli/man.go, internal/cli/man_test.go, internal/cli/help_contract_test.go, internal/mcp/tools_test.go, integration/init_test.go, integration/doctor_test.go, README.md, docs/design.md, docs/coding-agents.md, docs/cli-json-v1.md, internal/skill/SKILL.md, plugins/hum/skills/hum/SKILL.md.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `mise exec go -- go test ./internal/project -run "PrivateManifest|DefaultManifest|Resolve.*Manifest" -count=1 -v` exits 0 with RUN/PASS cases proving `--file` selection is exact, `.hum.yaml` wins over `hum.yaml`, either default works alone, no merge occurs, and invalid/unreadable/non-regular/unsafe `.hum.yaml` errors never fall back.
- [ ] #2 AC2 — `mise exec go -- go test ./internal/cli ./internal/mcp -run "PrivateManifest|ManifestPrecedence|Doctor.*Manifest" -count=1 -v` exits 0 with RUN/PASS cases proving CLI and MCP share the precedence, list/source output identifies `.hum.yaml`, doctor identifies the active and shadowed defaults, explicit selection wins, and effective-manifest drift/retained-record behavior is preserved.
- [ ] #3 AC3 — `mise exec go -- go test ./internal/project ./internal/cli ./integration -run "Init.*PrivateManifest|PrivateManifest.*Init" -count=1 -v` exits 0 with RUN/PASS cases proving init creates `hum.yaml` when neither file exists, refuses an active `.hum.yaml` unchanged without force, atomically replaces the active regular `.hum.yaml` with force, and never modifies a shadowed `hum.yaml`.
- [ ] #4 AC4 — `for doc in README.md docs/design.md docs/coding-agents.md; do rg -F ".hum.yaml" "$doc" || exit 1; done` exits 0, and review confirms each document states `--file` > `.hum.yaml` > `hum.yaml`, complete replacement rather than merging, invalid-private fail-closed behavior, and the repository/global-ignore trade-off.
- [ ] #5 AC5 — `task cli:check && task test` exits 0 with all existing `hum.yaml` and explicit alternate-manifest tests still passing, proving compatibility for repositories that do not add `.hum.yaml`.
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
