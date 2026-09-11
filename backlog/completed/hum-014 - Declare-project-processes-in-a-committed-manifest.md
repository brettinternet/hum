---
id: HUM-014
title: Declare project processes in a canonical YAML manifest
status: Done
assignee: []
created_date: '2026-09-02 20:13'
updated_date: '2026-09-03 20:57'
labels:
  - cli
  - config
  - docs
milestone: m-1
dependencies:
  - HUM-010
modified_files:
  - internal/project/
  - internal/app/
  - internal/protocol/
  - internal/cli/
  - integration/
  - internal/testutil/
  - go.mod
  - go.sum
  - README.md
  - docs/design.md
priority: high
type: feature
ordinal: 1100
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: a project can commit one canonical, versioned YAML manifest of named development processes, so humans and agents can idempotently bring up precise project definitions, see their readiness at a glance, and wait for current-incarnation readiness without knowing, retyping, or reverse-engineering underlying commands.

Scope: one strict YAML document in `hum.yaml` at the nearest Git project root, with top-level `version: 1` and a `processes` mapping keyed by safe process name. Each entry has a non-empty `argv` string sequence and may have a root-relative `cwd` plus `ready: { match: <regex>, timeout: <duration> }`; the readiness timeout defaults to 30s. Reject unknown or duplicate keys, aliases and merge keys, multiple documents, unsupported versions, shell text, empty or non-string argv elements, invalid names/regexes/durations, absolute cwd values, lexical traversal, missing cwd directories at launch, and symlink-resolved cwd values outside the project root, with errors naming the file and entry. Do not accept JSON, TOML, `.yml`, or alternate manifest filenames. Implement parsing and source-tagged normalized process definitions in `internal/project`; HUM-016 extends the same model with non-manifest sources.

Add `hum start <name>... [--no-wait] [--timeout DURATION] [--json]` and `hum up [--no-wait] [--timeout DURATION] [--json]`. Both auto-start or version-replace the daemon exactly like `run`. `start` is an idempotent ensure-running operation: concurrent calls produce one child; an already-running manifest launch returns `already_running` without replacement; a stopped or never-started entry launches detached from the current manifest with the requesting client full environment and returns `started`; several names produce one result per name. `up` processes names lexically, leaves running manifest launches alone, attempts every stopped entry despite individual launch failures, and returns one stable result per declared name. A running ad hoc record whose name later becomes declared is a conflict, not `already_running`. Human launch output echoes the source and argv, as in `started web (manifest: bun run dev)`.

Every manifest launch records its readiness expression, launch cursor, and first matching cursor for the current incarnation as output arrives, even when no client is waiting. The readiness state survives output eviction, resets on relaunch, and applies only to the recorded expression. `start` and `up` wait for readiness by default; `--no-wait` returns after spawn. While waiting, every requested running incarnation with `ready` checks that state before subscribing to new output, including entries that were already running; neither eviction nor old incarnations can cause a false timeout or match. Entries without `ready` succeed immediately after spawn with the explicit human/JSON outcome `running_unverified`. CLI `--timeout` overrides the manifest timeout. `up` waits concurrently after launch and leaves successful processes running; aggregate exit precedence is error 1, exited-before-ready 3, timeout 2, otherwise 0, while JSON preserves every per-process outcome and source. `status` and `list` expose the same state without waiting through a `readiness` field: `ready` with `ready_cursor`, `starting` while a configured expression has not matched, or `running_unverified`; exited processes and ad hoc records omit it.

`run <name>` without argv uses the manifest command while retaining attached-run semantics; raw `run <name> -- <argv>` is rejected when the manifest declares that name and remains available only for undeclared ad hoc names. Without a daemon, `list` reads `hum.yaml` locally and reports every declaration as stopped with its source and argv without creating runtime state; with a daemon it merges declared and ad hoc records and shows argv for every entry. When no daemon is available and the requested name resolves to a definition, `status`, `logs`, `wait`, and `restart` fail with `Nothing is running in this project. Start it with hum start <name>.`; undefined names keep the HUM-006 message. `restart` prefers the current manifest argv/cwd/readiness definition and the requesting client current environment, returns the new launch cursor, and retains the existing output cursor sequence. Names are scoped to the existing nearest-Git-root rules. README.md and docs/design.md document that cwd does not activate shell hooks and projects needing deterministic MCP execution should encode environment activation in argv through a committed tool runner.

Non-goals: zero-config discovery (HUM-016), `down` (HUM-018), manifest generation (HUM-019), runtime settings in the manifest, shell-text commands, dependency ordering, ports or HTTP health checks, automatic crash restart/backoff, environment literals or files, or arbitrary-command MCP tools.

Modified-file contract: internal/project/, internal/app/, internal/protocol/, internal/cli/, integration/, internal/testutil/, go.mod, go.sum, README.md, docs/design.md.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `go test ./internal/project -run 'Manifest|Definition'` exits 0 and covers strict single-document `hum.yaml` version-1 parsing, normalized source-tagged definitions, deterministic name ordering, unknown/duplicate key and alias/merge rejection, non-string or empty argv rejection, invalid names/regexes/durations, root-relative cwd resolution, lexical and symlink escape rejection, and precise file/entry errors; fixture `hum.json`, `hum.toml`, and `hum.yml` files are ignored.
- [x] #2 `go test ./internal/cli -run 'TestManifestStart|TestManifestUp|TestManifestList|TestDeclaredNameCollision'` exits 0 and proves start/up auto-start the daemon, wait by default and return after spawn with `--no-wait`, start is idempotent under concurrent calls and accepts several names, up preserves running manifest launches and continues after partial failure, declared names reject raw run argv and conflicting ad hoc records, entries without readiness return `running_unverified`, CLI timeout overrides manifest/default timeout, human launch output echoes source and argv, stable source-bearing human/JSON results and aggregate exit precedence, list reports declarations with source and argv without starting a daemon, and the resolved-name unavailable-daemon message is exactly `Nothing is running in this project. Start it with hum start <name>.`
- [x] #3 `go test ./internal/app ./internal/protocol ./internal/cli -run 'TestManifestRestart|TestManifestReadinessRetention|TestReadinessField'` exits 0 and proves stopped entries and restart use current manifest argv/cwd plus requesting-client environment, cursors remain monotonic, a readiness match remains satisfied after its output is evicted, relaunch resets readiness, neither an old matching line nor old satisfied state can satisfy a new incarnation, and `status`/`list` report `readiness` as `starting`, `ready` with `ready_cursor`, or `running_unverified` and omit it for exited and ad hoc records.
- [x] #4 `go test ./integration -run TestManifestWorkflow -count=1` exits 0 and proves `up --json` from no daemon starts each YAML declaration once and waits for readiness by default, an already-running process remains ready after its matching line is evicted, `status --json` shows `ready` without waiting, a declared name cannot be occupied by raw run, verified and unverified outcomes are explicit, successful processes survive another entry failure, and `list --json` merges declared and ad hoc processes with source and argv.
- [x] #5 `go test -race ./internal/app ./internal/cli -run 'ManifestStart|ManifestUp'` exits 0 with concurrent start/up calls, output eviction, and relaunch, proving at most one running child and one incarnation-scoped readiness state per project/name.
- [x] #6 `go list -m all | rg 'yaml'` prints exactly one YAML parser module, proving the manifest has one implementation format.
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

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
- [x] Implement strict hum.yaml v1 parsing and normalized definitions
- [x] Add incarnation-scoped readiness state and protocol fields
- [x] Add start/up and integrate manifest-aware run/list/status/logs/wait/restart
- [x] Add focused unit, race, and integration coverage
- [x] Update README and design documentation
- [x] Run every acceptance command and task ci
- [x] Obtain independent verifier pass, commit, merge, and clean worktree
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
AC#1 — `mise exec go -- go test ./internal/project -run 'Manifest|Definition'` passed.
AC#2 — `mise exec go -- go test ./internal/cli -run 'TestManifestStart|TestManifestUp|TestManifestList|TestDeclaredNameCollision'` passed.
AC#3 — `mise exec go -- go test ./internal/app ./internal/protocol ./internal/cli -run 'TestManifestRestart|TestManifestReadinessRetention|TestReadinessField'` passed.
AC#4 — `mise exec go -- go test ./integration -run TestManifestWorkflow -count=1` passed.
AC#5 — `mise exec go -- go test -race ./internal/app ./internal/cli -run 'ManifestStart|ManifestUp'` passed.
AC#6 — `mise exec go -- go list -m all` passed and listed exactly one YAML parser module: `gopkg.in/yaml.v3 v3.0.1`.
DOD#1 — `task ci` passed after final changes: gofmt, vet, staticcheck, full tests, race suite, build, and built-binary smoke.
Review — independent adversarial review found protocol-version, nested-Git-root, and pre-registration readiness-eviction defects; all were fixed with focused regressions. A later cursor review confirmed zero-based restart marker behavior and corrected the overconstrained test.
Modified-file deviation — `internal/daemon/` is required to transport source/root/readiness and manifest restart definitions across the existing daemon boundary. `internal/output/` is required for synchronous append observation so a readiness match cannot be evicted before app registration. Both deviations directly satisfy AC#3/#5; no alternative inside the declared paths preserves the required daemon ownership and atomic output semantics.

Final corrections — rejected complete JSON payloads including UTF-8-BOM-prefixed JSON; rejected explicit 0s/-0s readiness durations; incumbent start/up now use the recorded incarnation readiness expression/state after manifest edits.
Independent verifier — PASS for AC#1 through AC#6 after final corrections.
Commit — `d2c2cf7 feat: add canonical process manifest`.
Final-commit gate — `task ci` passed on d2c2cf7: gofmt, vet, staticcheck, full tests, race suite, build, and built-binary smoke.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented strict canonical hum.yaml v1 definitions, idempotent start/up workflows, manifest-aware run/list/status/logs/wait/restart behavior, and durable incarnation-scoped readiness. Added focused parser, runtime, CLI, race, and end-to-end coverage plus README/design documentation. All six acceptance commands, final task ci on commit d2c2cf7, adversarial review fixes, and the independent verifier pass completed.
<!-- SECTION:FINAL_SUMMARY:END -->
