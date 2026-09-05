---
id: HUM-026
title: Order up launches by declared readiness dependencies
status: To Do
assignee: []
created_date: '2026-09-05 15:14'
labels:
  - config
  - cli
  - docs
milestone: m-3
dependencies: []
modified_files:
  - internal/project/
  - internal/app/
  - internal/cli/
  - internal/mcp/
  - internal/skill/
  - internal/testutil/
  - integration/
  - plugins/hum/skills/hum/SKILL.md
  - README.md
  - docs/design.md
  - docs/coding-agents.md
priority: medium
type: feature
ordinal: 3700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: a declared process can list the processes that must be `ready` before it launches, so `hum up` brings a database, a queue, and a web server up in the right order with one command instead of a script of `start` calls and `wait --match` checks. Processes without `after` keep launching concurrently exactly as today.

Why now: a typical stack is `db` then `api` then `web`. Today `up` launches everything at once and each dependent must tolerate its dependency being absent, or the operator must sequence starts by hand. Agents in particular end up encoding the ordering in prompts. Readiness already exists per process; ordering is the missing use of it.

Scope, manifest: `after` is an optional per-process list of declared process names in `hum.yaml`. Each name must exist in the same manifest, must not be the entry itself, and must declare `ready`, because a definition without `ready` is never reported ready and would block forever. Cycles, unknown names, self-reference, a dependency without `ready`, and non-list or non-string values are errors with file and entry context. Definitions carry `After []string`. `init` templates include a commented `after: [db]` example.

Scope, up: ordering is resolved and enforced by the client `up` in CLI and MCP, consistent with the rule that clients resolve definitions and the daemon receives exact launch requests; the daemon and protocol are unchanged. `up` launches every definition whose `after` list is empty immediately, and launches each remaining definition as soon as every listed dependency reports `ready` within its own readiness wait. `--timeout` applies to each process's own readiness wait as today, so total wall time may exceed it. If a dependency finishes with a request error, early exit, or readiness timeout, each definition waiting on it is not launched and reports outcome `skipped` with `blocked_by` naming that dependency; skipping cascades. `up` still attempts every launchable entry and leaves successful children running. Exit-code precedence becomes request error (1), early exit (3), timeout (2), skipped (4), success (0). Per-name results are emitted as they complete, so output order follows completion rather than declaration. `up --no-wait` fails before any launch when a definition declares `after`, with a message that ordering requires waiting for readiness. A dependency that is already `ready` when `up` runs (from an earlier `start`) satisfies the gate without relaunch, matching the idempotent `up` behavior today.

Scope, other commands: `start <name>` launches only the named processes and does not launch or wait for their dependencies; it stays the explicit single-process tool. `restart`, `stop`, `down`, and `remove` are unchanged, and `down` keeps stopping concurrently without reverse ordering. `list --json` and `status --json` do not expose `after`; it is a manifest-only launch-ordering concern.

Docs: docs/design.md removes "dependencies" from the manifest exclusions, documents `after` and the `skipped` outcome, and updates the `up` exit-code precedence. README.md shows a three-process example. docs/coding-agents.md, the embedded skill, and `plugins/hum/skills/hum/SKILL.md` tell agents to prefer `up` over sequencing `start` calls when `after` is declared.

Non-goals: dependencies on ad hoc or discovered definitions; `start` pulling in dependencies; reverse-order `down`; restarting dependents when a dependency restarts; port, HTTP, or command health checks as gates; daemon-side dependency knowledge or protocol changes; per-dependency timeouts; Windows.

Modified-file contract: internal/project/, internal/app/, internal/cli/, internal/mcp/, internal/skill/, internal/testutil/, integration/, plugins/hum/skills/hum/SKILL.md, README.md, docs/design.md, docs/coding-agents.md. No protocol, go.mod, or go.sum change.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `go test ./internal/project -run "^TestAfterManifest$" -count=1 -v` exits 0 and prints `--- PASS: TestAfterManifest`. It proves `after` parses into `After []string`, defaults to empty, and rejects unknown names, self-reference, cycles of length 2 and 3, a dependency without `ready`, and non-list or non-string values, each with file and entry context; and the `init` template includes a commented `after` example that still parses.
- [ ] #2 AC2 — `go test ./internal/cli -run "^TestUpOrdersByAfter$" -count=1 -v` and `go test ./internal/mcp -run "^TestUpOrdersByAfter$" -count=1 -v` both exit 0 and print the corresponding named PASS line. Against a fake daemon client they prove root definitions launch immediately, a dependent launches only after every listed dependency reports `ready`, a dependency that is already `ready` satisfies the gate without relaunch, a dependency request error, early exit, or timeout yields `skipped` with `blocked_by` for each waiting dependent and cascades, `--timeout` applies per process, exit-code precedence is request error 1, early exit 3, timeout 2, skipped 4, success 0, results are emitted on completion, `up --no-wait` fails before any launch when `after` is declared, and `start <name>` launches only the named process.
- [ ] #3 AC3 — `go test ./integration -run "^TestUpOrderedStack$" -count=1 -v` exits 0 and prints `--- PASS: TestUpOrderedStack`. With the built binary and a manifest of `db`, `api` (`after: [db]`), and `web` (`after: [api]`) whose fixtures print a readiness line after a delay and record their start time, it proves each process launches only after its dependency was ready, `hum up` exits 0 with three success results, a second `hum up` is idempotent, and a run where `db` exits before readiness returns exit 3 with `api` and `web` reported `skipped` and not launched.
- [ ] #4 AC4 — `go test ./internal/cli ./internal/skill -run "^TestAfterDocs$" -count=1 -v` exits 0 and prints both named PASS lines. README.md, docs/design.md, docs/coding-agents.md, CLI help, the embedded skill, and `plugins/hum/skills/hum/SKILL.md` document the `after` field and its validation rules, the `skipped` outcome and `blocked_by`, the updated exit-code precedence, the `--no-wait` rejection, that `start` does not pull in dependencies, and that `down` is not reverse-ordered; docs/design.md no longer lists dependencies as a manifest exclusion.
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
