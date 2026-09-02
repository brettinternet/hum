---
id: HUM-016
title: Start a conventional dev task without hum.yaml
status: To Do
assignee: []
created_date: '2026-09-02 20:42'
updated_date: '2026-09-02 20:45'
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

Scope: extend `internal/project` resolution so a present `hum.yaml` is always authoritative: valid YAML yields exactly its declarations, including an empty mapping; invalid YAML fails; neither case falls back to discovery. Only when the file is absent, inspect available project-level task runners through native machine-readable interfaces: exact local `dev` from `mise tasks --local --json` run at the project root and exact `dev` from `task --dir <root> --list-all --json`. Skip a runner whose executable is unavailable. Accept exactly one candidate and normalize it as `argv: [mise, run, dev]` or `argv: [task, dev]`; if both qualify, return an actionable ambiguity error.

Only when neither project task qualifies, inspect the root `package.json` for an exact `scripts.dev`. Select its runner from `packageManager` first; otherwise require exactly one lockfile family among Bun (`bun.lock` or `bun.lockb`), pnpm (`pnpm-lock.yaml`), Yarn (`yarn.lock`), and npm (`package-lock.json` or `npm-shrinkwrap.json`). Conflicting families are an error; no lockfile defaults to npm. Normalize the inferred process as name `dev`, root cwd, source `mise`, `task`, or `package_json`, and runner argv `[bun|pnpm|yarn|npm, run, dev]`, with no readiness expression. Never parse or execute the package script body directly.

The resolved definition powers `hum up`, `hum start dev`, `hum run dev` without argv, `hum restart dev`, and project-aware `hum list`. A discovered launch succeeds after spawn as `running_unverified`; it must never be reported ready. Malformed native output, ambiguity, and no candidate return typed, actionable errors and do not start a daemon.

Non-goals: inferring commands other than exact `dev`, scanning workspace packages or nested manifests, combining multiple inferred processes, inferring ports/readiness/dependencies, parsing human-formatted runner output or package-script shell text, or overriding an explicit `hum.yaml`.

Modified-file contract: internal/project/, internal/cli/, integration/, internal/testutil/, README.md, docs/design.md.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/project -run 'TestResolveExplicit|TestDiscoverProjectTask'` exits 0 and proves explicit YAML wins without fallback, empty explicit definitions stay empty, unavailable task-runner executables are skipped, exact project-root mise/task dev tasks normalize with stable source metadata, malformed native output fails, and dual matches return an actionable ambiguity error.
- [ ] #2 `go test ./internal/project -run TestDiscoverPackageDev` exits 0 and proves package discovery requires root scripts.dev, honors packageManager before lockfiles, recognizes each supported lockfile family, emits runner argv without parsing script text, defaults to npm only with no lockfile, and rejects conflicting families, malformed input, and workspace-only scripts.
- [ ] #3 `go test ./internal/cli -run 'TestDiscoveredUp|TestDiscoveredStart|TestDiscoveredList|TestDiscoveryErrors'` exits 0 and proves up/start/run-without-argv/restart/list consume the same resolved dev definition, report running_unverified plus its source, never report readiness, and do not start a daemon for resolution errors.
- [ ] #4 `go test ./integration -run TestZeroConfigDiscovery -count=1` exits 0 and proves a repository with one conventional dev task and no hum.yaml starts exactly once through hum up, is idempotent across start/run/restart/list, and exposes no underlying-command requirement to the caller.
- [ ] #5 `go test -race ./internal/project ./internal/cli -run 'Discover|Discovered'` exits 0 and preserves one deterministic resolved definition and one running child under concurrent zero-config requests.
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
