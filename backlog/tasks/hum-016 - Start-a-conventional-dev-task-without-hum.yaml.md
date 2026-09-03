---
id: HUM-016
title: Discover a conventional dev entrypoint without hum.yaml
status: To Do
assignee: []
created_date: '2026-09-02 20:42'
updated_date: '2026-09-03 03:18'
labels:
  - cli
  - config
  - docs
milestone: m-1
dependencies:
  - HUM-014
modified_files:
  - internal/project/
  - internal/cli/
  - integration/
  - internal/testutil/
  - README.md
  - docs/design.md
priority: high
type: feature
ordinal: 1150
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: when a project has no `hum.yaml`, a human or agent can run `hum up` and get one deterministic, source-labelled conventional development process without first authoring configuration or retyping its underlying command.

Scope: extend `internal/project` resolution so a present `hum.yaml` is always authoritative: valid YAML yields exactly its declarations, including an empty mapping; invalid YAML fails; neither case falls back to discovery. Only when the file is absent, inspect every supported root-level declaration without launching a development process, collect candidates, accept exactly one, and return an actionable ambiguity error listing every source when several qualify. Do not hide lower-priority candidates behind precedence.

Recognize these bounded conventions: an exact local Mise task `dev` through `mise tasks --local --json`; an exact Task task `dev` through `task --dir <root> --list-all --json`; an exact public Just recipe `dev` through Just JSON dump for the root-resolved justfile; a literal non-pattern `dev` target in the root Makefile family, parsed conservatively without executing Make; an exact root `package.json` `scripts.dev`; an exact root `deno.json` or `deno.jsonc` `tasks.dev`; an exact root `composer.json` `scripts.dev`; an executable root `bin/dev`; and, for a root `mix.exs`, an available `phx.server` Mix task confirmed by Mix introspection. Skip command-backed detectors when their runner is unavailable. Malformed files or native machine-readable output for a present candidate source fail with a source-specific configuration error rather than falling through.

Normalize every candidate as the single name `dev`, root cwd, no readiness expression, stable source metadata, and exact argv: `[mise, run, dev]`, `[task, dev]`, `[just, dev]`, `[make, dev]`, the package runner `[bun|pnpm|yarn|npm, run, dev]`, `[deno, task, dev]`, `[composer, run-script, dev]`, `[./bin/dev]`, or `[mix, phx.server]`. Package runner selection honors `packageManager` first, otherwise requires exactly one Bun, pnpm, Yarn, or npm lockfile family and defaults to npm only when none exists. Never parse or execute package, Composer, Deno, Just, Make, or Mix task bodies during Hum resolution, and never probe by starting likely commands.

The resolved definition powers `hum up`, `hum start dev`, `hum run dev` without argv, `hum restart dev`, and project-aware `hum list`. A discovered launch succeeds immediately after spawn as `running_unverified` under the default wait; it must never be reported ready, and `status`/`list` show `readiness: running_unverified`. Launch output and `list` echo the source and argv, as in `started dev (package_json: bun run dev)`, so the first `hum up` in a repository is never a surprise. Ambiguity, malformed discovery input, and no candidate return typed, actionable errors and do not start a daemon. The no-candidate error names `hum.yaml` and the supported root-level conventions; HUM-019 later adds `hum init` to those errors.

Non-goals: language-level guesses such as bare `go run`, `cargo run`, framework launch commands other than confirmed `mix phx.server`, Docker Compose inference, scanning workspace packages or nested manifests, combining several inferred processes, inferring ports/readiness/dependencies, executing candidate commands to see which succeeds, manifest generation (HUM-019), or overriding an explicit `hum.yaml`.

Modified-file contract: internal/project/, internal/cli/, integration/, internal/testutil/, README.md, docs/design.md.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/project -run TestResolveExplicit -count=1` exits 0 and proves valid, empty, and invalid `hum.yaml` are authoritative and never fall back to discovery.
- [ ] #2 `go test ./internal/project -run TestDiscoverTaskRunnerDev -count=1` exits 0 and proves exact root Mise, Task, Just, and conservative literal Make `dev` declarations normalize to their documented argv and source metadata without executing recipe bodies; unavailable command-backed runners are skipped and malformed introspection fails visibly.
- [ ] #3 `go test ./internal/project -run TestDiscoverEcosystemDev -count=1` exits 0 and proves exact package, Deno, and Composer `dev` entries, executable `bin/dev`, and confirmed Mix `phx.server` normalize to documented argv; package-manager selection honors `packageManager`, recognizes every supported lockfile family, rejects conflicts, and no detector starts a development command.
- [ ] #4 `go test ./internal/project -run TestDiscoveryAmbiguity -count=1` exits 0 and proves all supported sources are collected before selection, exactly one candidate succeeds, multiple candidates fail while naming every source, and no candidate fails while naming `hum.yaml` plus the supported conventions.
- [ ] #5 `go test ./integration -run TestZeroConfigDiscovery -count=1` exits 0 across representative task-runner, package, Mix/Phoenix, and executable `bin/dev` fixtures and proves one unambiguous root entrypoint starts exactly once, remains idempotent across start/run/restart/list, and exposes no underlying-command requirement to the caller.
- [ ] #6 `go test ./internal/cli -run 'TestDiscoveredUp|TestDiscoveredStart|TestDiscoveredList|TestDiscoveryErrors' -count=1` exits 0 and proves up/start/run-without-argv/restart/list consume the same inferred `dev` definition, report `running_unverified` immediately under the default wait, echo source and argv on launch and in list, never report readiness, and do not start a daemon for resolution errors.
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
