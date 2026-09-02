---
id: DEVPROC-014
title: Declare and ensure project processes from a committed manifest
status: To Do
assignee: []
created_date: '2026-09-02 20:13'
updated_date: '2026-09-02 20:29'
labels:
  - cli
  - config
  - docs
milestone: m-1
dependencies:
  - DEVPROC-010
modified_files:
  - internal/manifest/
  - internal/app/
  - internal/protocol/
  - internal/cli/
  - integration/
  - internal/testutil/
  - README.md
  - docs/design.md
priority: high
type: feature
ordinal: 1100
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: a project commits one versioned manifest of named development processes, so humans and agents can idempotently bring up the project and wait for current-incarnation readiness without knowing, retyping, or reverse-engineering underlying commands.

Scope: strict JSON in `hum.json` at the nearest Git project root, with top-level `version: 1` and a `processes` object keyed by safe process name. Each entry has a non-empty `argv` string array and may have a root-relative `cwd` plus `ready: { "match": "<regex>", "timeout": "<duration>" }`; the readiness timeout defaults to 30s. Reject unknown fields, duplicate keys, unsupported versions, shell text, empty argv/elements, invalid names/regexes/durations, absolute cwd values, lexical traversal, missing cwd directories at launch, and symlink-resolved cwd values outside the project root, with errors naming the file and entry.

Add `hum start <name> [--wait] [--timeout DURATION] [--json]` and `hum up [--wait] [--timeout DURATION] [--json]`. Both auto-start or version-replace the daemon exactly like `run`. `start` is an idempotent ensure-running operation: concurrent calls produce one child; an already-running manifest launch returns `already_running` without replacement; a stopped or never-started entry launches detached from the current manifest with the requesting client's full environment and returns `started`. `up` processes names lexically, leaves running manifest launches alone, attempts every stopped entry despite individual launch failures, and returns one stable result per declared name. A running ad hoc record whose name later becomes declared is a conflict, not `already_running`.

Every manifest launch records its readiness expression, launch cursor, and first matching cursor for the current incarnation as output arrives, even when no client is waiting. The readiness state survives output eviction, resets on relaunch, and applies only to the recorded expression. With `--wait`, every requested running incarnation with `ready` checks that state before subscribing to new output, including entries that were already running; neither eviction nor old incarnations can cause a false timeout or match. Entries without `ready` succeed after spawn. CLI `--timeout` overrides the manifest timeout. `up --wait` waits concurrently after launch and leaves successful processes running; aggregate exit precedence is error 1, exited-before-ready 3, timeout 2, otherwise 0, while JSON preserves every per-process outcome.

`run <name>` without argv uses the manifest command while retaining attached-run semantics; raw `run <name> -- <argv>` is rejected when the manifest declares that name and remains available only for undeclared ad hoc names. Without a daemon, `list` reads the manifest locally and reports every declaration as stopped without creating runtime state; with a daemon it merges declared and ad hoc records. `restart` prefers the current manifest argv/cwd/readiness definition and the requesting client's current environment, returns the new launch cursor, and retains the existing output cursor sequence. Names are scoped to the existing nearest-Git-root rules. README.md and docs/design.md document that cwd does not activate shell hooks and projects needing deterministic MCP execution should encode environment activation in argv through a committed tool runner.

Non-goals: runtime settings in the manifest, shell-text commands, inferred package-manager scripts, dependency ordering, ports or HTTP health checks, automatic crash restart/backoff, environment literals or files, or arbitrary-command MCP tools.

Modified-file contract: internal/manifest/, internal/app/, internal/protocol/, internal/cli/, integration/, internal/testutil/, README.md, docs/design.md.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/manifest` exits 0 and covers strict version-1 parsing, deterministic name ordering, non-empty argv arrays, unknown/duplicate field rejection, invalid names/regexes/durations, root-relative cwd resolution, lexical and symlink escape rejection, and precise errors naming `hum.json` and the entry.
- [ ] #2 `go test ./internal/cli -run 'TestManifestStart|TestManifestUp|TestManifestList|TestDeclaredNameCollision'` exits 0 and proves start/up auto-start the daemon, start is idempotent under concurrent calls, up preserves running manifest launches and continues after partial failure, declared names reject raw run argv and conflicting ad hoc records, CLI timeout overrides manifest/default timeout, stable human/JSON results and aggregate exit precedence, and list reports declarations without starting a daemon.
- [ ] #3 `go test ./internal/app ./internal/protocol ./internal/cli -run 'TestManifestRestart|TestManifestReadinessRetention'` exits 0 and proves stopped entries and restart use current manifest argv/cwd plus requesting-client environment, cursors remain monotonic, a readiness match remains satisfied after its output is evicted, relaunch resets readiness, and neither an old matching line nor old satisfied state can satisfy a new incarnation.
- [ ] #4 `go test ./integration -run TestManifestWorkflow -count=1` exits 0 and proves `up --wait --json` from no daemon starts each declaration once, an already-running process remains ready after its matching line is evicted, a declared name cannot be occupied by raw run, every readiness outcome is reported, successful processes survive another entry's failure, and `list --json` merges declared and ad hoc processes.
- [ ] #5 `go test -race ./internal/app ./internal/cli -run 'ManifestStart|ManifestUp'` exits 0 with concurrent start/up calls, output eviction, and relaunch, proving at most one running child and one incarnation-scoped readiness state per project/name.
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
