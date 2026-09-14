---
id: HUM-113
title: >-
  Remove runtime conventional discovery; make hum.yaml or --file the only
  declaration source
status: To Do
assignee: []
created_date: '2026-09-14 23:16'
updated_date: '2026-09-14 23:17'
labels:
  - cli
  - mcp
  - config
  - json
  - contract
  - product-boundary
  - docs
milestone: m-1
dependencies: []
references:
  - decision-001
  - internal/project/resolver.go
modified_files:
  - internal/project/resolver.go
  - internal/project/resolver_test.go
  - internal/project/init.go
  - internal/project/init_test.go
  - internal/cli/manifest.go
  - internal/cli/manifest_test.go
  - internal/cli/commands.go
  - internal/cli/config.go
  - internal/cli/discovery_test.go
  - internal/cli/doctor.go
  - internal/cli/doctor_test.go
  - internal/cli/mcp_test.go
  - internal/cli/help_contract_test.go
  - internal/cli/man.go
  - internal/app/app.go
  - internal/app/app_test.go
  - internal/mcp/tools.go
  - internal/mcp/tools_test.go
  - integration/zero_config_test.go
  - integration/init_test.go
  - integration/doctor_test.go
  - README.md
  - hum.example.yaml
  - docs/design.md
  - docs/coding-agents.md
  - docs/cli-json-v1.md
  - internal/skill/SKILL.md
  - plugins/hum/skills/hum/SKILL.md
  - CHANGELOG.md
priority: medium
type: enhancement
ordinal: 85800
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: Hum has exactly two ways to name a process: an explicit manifest (hum.yaml or --file) or hum run NAME -- COMMAND. Without a manifest, up, start, restart, list, status, doctor, and MCP definition-resolving tools stop guessing a dev entrypoint from Mise, Task, Just, Make, package.json, Deno, Composer, bin/dev, or Mix and instead return one actionable error that points at hum init or hum run. hum init keeps read-only file detection to scaffold hum.yaml and never spawns a subprocess.

## Why

Discovery is roughly 2.6k lines including a subprocess path (mise tasks ls, task --list, just --dump) that runs before daemon contact, has bespoke Makefile/Elixir/JSONC parsers, produces a third source family (package_json, mise, task, ...) in JSON and MCP output, and cannot express readiness, cwd, dependencies, or restart policy, so agents that hit it must ask the user to run hum init anyway. It competes with pitchfork on convenience where Hum cannot win and dilutes the explicit-declaration contract that differentiates Hum. See decision-001.

## Contract

- Runtime resolution: project.ResolveDefinitions* return only manifest definitions. When hum.yaml is absent and no --file is given, launch and definition-listing commands fail with a single stable error (human text plus CLI JSON v1 error code manifest_missing) that names the resolved project root and suggests hum init or hum run NAME -- COMMAND. Runtime-only commands (logs, wait, input, signal, stop, remove, attach) and ad-hoc run are unchanged.
- list and status with no manifest still render retained daemon records for the scope; they no longer merge discovered declarations.
- doctor reports manifest absence as a single FAIL check with the same guidance, and drops discovery-specific checks.
- MCP start, up, restart, list return the same manifest_missing error object; no other tool schema changes.
- hum init: keep detection for the same nine sources but only through the existing read-only detectors (detect*ReadOnly / *WithReader); delete runDiscoveryCommand and every subprocess-backed detector. init output and template comments are unchanged.
- Runtime source values reduce to manifest:<path> and ad_hoc. Update docs/cli-json-v1.md accordingly; compatibility rules already permit clients to see fewer enum values.
- Remove README, design, coding-agents, SKILL.md, and hum.example.yaml text describing zero-config up; document the two declaration paths and the error.
- Land as one conventional commit with a ! breaking marker so git-cliff records the behavior change.

## Non-goals

Changing hum run; changing manifest syntax; adding new init sources; keeping a hidden opt-in for runtime discovery; touching project-root discovery (DiscoverProjectRoot is unrelated and stays).

Modified-file contract: internal/project/resolver.go, internal/project/resolver_test.go, internal/project/init.go, internal/project/init_test.go, internal/cli/manifest.go, internal/cli/manifest_test.go, internal/cli/commands.go, internal/cli/config.go, internal/cli/discovery_test.go, internal/cli/doctor.go, internal/cli/doctor_test.go, internal/cli/mcp_test.go, internal/cli/help_contract_test.go, internal/cli/man.go, internal/app/app.go, internal/app/app_test.go, internal/mcp/tools.go, internal/mcp/tools_test.go, integration/zero_config_test.go, integration/init_test.go, integration/doctor_test.go, README.md, hum.example.yaml, docs/design.md, docs/coding-agents.md, docs/cli-json-v1.md, internal/skill/SKILL.md, plugins/hum/skills/hum/SKILL.md, CHANGELOG.md.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — rg -n 'exec\.Command|runDiscoveryCommand|detectMise\(|detectTask\(|detectJust\(' internal/project exits 1 (no matches), and mise exec go -- go test ./internal/project -run '^TestInit' -count=1 -v exits 0 proving hum init still scaffolds from every read-only detector, keeps ambiguity/no-candidate templates, and spawns no subprocess.
- [ ] #2 AC2 — mise exec go -- go test ./internal/cli ./internal/mcp -run 'ManifestMissing' -count=1 -v exits 0 and proves up, start, restart, list, status, and doctor, plus MCP start/up/restart/list, return the manifest_missing error with the project root and hum init / hum run guidance in human, --json, and MCP forms, while list/status still render retained records for the scope.
- [ ] #3 AC3 — mise exec go -- go test ./integration -run '^TestZeroConfig|^TestInit' -count=1 -v exits 0 with TestZeroConfig* rewritten to assert the manifest_missing failure and exit 1 for hum up in a package.json-only project, and hum run continuing to work there.
- [ ] #4 AC4 — rg -n -i 'conventional|zero-config|package\.json|Justfile|Taskfile|bin/dev|Mix' README.md docs/design.md docs/coding-agents.md hum.example.yaml internal/skill/SKILL.md plugins/hum/skills/hum/SKILL.md exits 1 except for lines describing hum init scaffolding, and rg -n 'manifest_missing' docs/cli-json-v1.md README.md exits 0.
- [ ] #5 AC5 — task cli:check && task test exits 0, and git log -1 --format=%s on the landing commit matches ^[a-z]+(\([a-z-]+\))?!: confirming the breaking-change marker.
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
