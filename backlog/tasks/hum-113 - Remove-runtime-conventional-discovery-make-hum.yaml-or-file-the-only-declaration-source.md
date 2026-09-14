---
id: HUM-113
title: >-
  Remove runtime conventional discovery; make hum.yaml or --file the only
  declaration source
status: To Do
assignee: []
created_date: '2026-09-14 23:16'
updated_date: '2026-09-14 23:36'
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
  - internal/cli/mcp.go
  - internal/cli/init_test.go
  - internal/cli/json_errors_test.go
  - internal/mcp/server.go
  - internal/mcp/server_test.go
priority: medium
type: enhancement
ordinal: 85800
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: Hum has exactly two ways to name a process: an explicit manifest (hum.yaml or --file) or hum run NAME -- COMMAND. Without a manifest, up, start, restart, list, status, doctor, and MCP definition-resolving tools stop guessing a dev entrypoint from Mise, Task, Just, Make, package.json, Deno, Composer, bin/dev, or Mix and instead use only explicit declarations or already retained runtime records, with an actionable manifest_missing error when a declaration is required. hum init keeps read-only file detection to scaffold hum.yaml and never spawns a subprocess.

## Why

Runtime discovery includes a subprocess path (mise tasks ls, task --list, just --dump) that runs before daemon contact, has bespoke Makefile/Elixir/JSONC parsers, produces a third source family (package_json, mise, task, ...) in JSON and MCP output, and cannot express readiness, cwd, dependencies, or restart policy, so agents that hit it must ask the user to run hum init anyway. It competes with pitchfork on convenience where Hum cannot win and dilutes the explicit-declaration contract that differentiates Hum. See decision-001.

## Contract

- Runtime resolution: project.ResolveDefinitions* return only manifest definitions. When hum.yaml is absent and no --file is given, up and named start/restart without a retained record fail with a single stable error (human text plus CLI JSON v1 error code manifest_missing) that names the resolved project root and suggests hum init or hum run NAME -- COMMAND. Runtime-only commands (logs, wait, input, signal, stop, remove, attach) and ad-hoc run are unchanged.
- list, aggregate status, and MCP list with no manifest still render retained daemon records for the scope (or the existing successful empty-state/empty-array result); named status retains its existing not-found behavior. They never return manifest_missing merely because the default file is absent. Preserve named start/restart fallback for retained ad_hoc or removed-definition records; only unresolved names require the missing-manifest error. Explicit --file failures and malformed/unreadable default manifests remain authoritative errors, never hidden by runtime fallback. Global scope and --all remain unchanged.
- doctor reports manifest absence as one project.manifest FAIL check with the same guidance and project root, and drops discovery-specific checks. Keep its diagnostic report shape (ok/checks/summary, exit 1); do not replace doctor --json with a top-level terminal error. Other independent preflight checks still run read-only.
- MCP start/up/restart mirror the declaration-required versus retained-record rules above; list remains a read. Failures retain the existing MCP tool-error envelope with code manifest_missing, not the CLI JSON envelope; no unrelated tool schema changes.
- hum init: keep detection for the same nine sources but only through the existing read-only detectors (detect*ReadOnly / *WithReader); delete runDiscoveryCommand and every subprocess-backed detector. Keep the output shape, schema directive, and ambiguity/no-candidate template format. Candidate results follow the existing conservative read-only detectors; do not promise identical dynamic mise/task/just introspection results. Retain the Make/Elixir/JSONC parsers still needed by init.
- New runtime source values reduce to manifest:<path> and ad_hoc; init candidates may still use the nine detector source values and existing daemon records may retain legacy values. Update docs/cli-json-v1.md without claiming that its additive compatibility rules license removing source fields or changing their meaning.
- Remove README, design, coding-agents, SKILL.md, and hum.example.yaml text describing zero-config up; document the two declaration paths and the error.
- Land as one conventional commit with a ! breaking marker so git-cliff records the behavior change.

## Non-goals

Changing hum run or retained-record lifecycle control; changing manifest syntax; adding new init sources; keeping a hidden opt-in for runtime discovery; touching project-root discovery (DiscoverProjectRoot is unrelated and stays).

Modified-file contract: internal/project/resolver.go, internal/project/resolver_test.go, internal/project/init.go, internal/project/init_test.go, internal/cli/manifest.go, internal/cli/manifest_test.go, internal/cli/commands.go, internal/cli/config.go, internal/cli/discovery_test.go, internal/cli/doctor.go, internal/cli/doctor_test.go, internal/cli/mcp_test.go, internal/cli/help_contract_test.go, internal/cli/man.go, internal/app/app.go, internal/app/app_test.go, internal/mcp/tools.go, internal/mcp/tools_test.go, integration/zero_config_test.go, integration/init_test.go, integration/doctor_test.go, README.md, hum.example.yaml, docs/design.md, docs/coding-agents.md, docs/cli-json-v1.md, internal/skill/SKILL.md, plugins/hum/skills/hum/SKILL.md, CHANGELOG.md, internal/cli/mcp.go, internal/cli/init_test.go, internal/cli/json_errors_test.go, internal/mcp/server.go, internal/mcp/server_test.go.

Next action: encode the missing/default/explicit/invalid-manifest and retained/no-record matrix in CLI and MCP tests, then disconnect runtime resolution from the shared read-only init detectors. Move existing discovery coverage to init where it still applies; rewrite obsolete runtime expectations to assert no discovery rather than deleting or skipping tests. Add named cases for each acceptance claim; zero matching tests is not a pass.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `mise exec go -- go test ./internal/project -run "^TestInit|^TestResolve.*Manifest" -count=1 -v` exits 0 with RUN/PASS cases for all nine read-only init sources, conservative dynamic-config behavior, ambiguity/no-candidate templates, cancellation, and manifest-only runtime resolution. `python3 -c 'from pathlib import Path; import re; paths=[p for p in Path("internal/project").glob("*.go") if not p.name.endswith("_test.go")]; hits=[str(p) for p in paths if re.search(r"exec\.Command|runDiscoveryCommand|func detect(?:Mise|Task|Just)\(", p.read_text())]; assert not hits, hits'` exits 0; tests use sentinel executables to prove init launches no discovery subprocess.
- [ ] #2 AC2 — `mise exec go -- go test ./internal/cli ./internal/mcp -run "ManifestMissing|Doctor.*Manifest" -count=1 -v` exits 0 with RUN/PASS for the complete contract matrix: up/unresolved start/restart fail with root/guidance and manifest_missing in CLI human/JSON and MCP tool errors; retained start/restart still work; list/aggregate status/MCP list return retained or empty results; named status keeps not-found semantics; doctor retains its report envelope and one manifest FAIL check; invalid manifests and explicit --file failures never fall back; global/--all behavior is unchanged.
- [ ] #3 AC3 — mise exec go -- go test ./integration -run '^TestZeroConfig|^TestInit' -count=1 -v exits 0 with TestZeroConfig* rewritten to assert the manifest_missing failure and exit 1 for hum up in a package.json-only project, and hum run continuing to work there. Preserve coverage for all former runtime discovery sources by asserting they do not trigger introspection or launch; retain init tests rather than removing them.
- [ ] #4 AC4 — `rg -n -i "conventional|zero-config|package\.json|Justfile|Taskfile|bin/dev|Mix" README.md docs/design.md docs/coding-agents.md hum.example.yaml internal/skill/SKILL.md plugins/hum/skills/hum/SKILL.md` prints a review inventory: each occurrence must concern init scaffolding, explicit argv examples, ad_hoc run, or historical rationale, never a promise of runtime discovery. `for doc in docs/cli-json-v1.md README.md; do rg -n "manifest_missing" "$doc" || exit 1; done` exits 0. Review documents against the command matrix, including doctor framing, retained-record exceptions, and init/legacy source values; do not use grep exit 1 with informal exceptions as a gate.
- [ ] #5 AC5 — `task cli:check && task test` exits 0, and `git log -1 --format=%s | grep -Eq "^[a-z]+(\([a-z-]+\))?!:"` exits 0 on the implementation landing commit. This breaking marker applies to discovery removal, not to backlog-refinement commits.
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
