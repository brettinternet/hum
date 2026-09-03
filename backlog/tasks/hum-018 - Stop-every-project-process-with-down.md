---
id: HUM-018
title: Stop every project process with down
status: To Do
assignee: []
created_date: '2026-09-03 03:15'
labels:
  - cli
  - process
  - docs
milestone: m-1
dependencies:
  - HUM-014
modified_files:
  - internal/app/
  - internal/protocol/
  - internal/cli/
  - integration/
  - README.md
  - docs/design.md
priority: high
type: feature
ordinal: 1110
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: a human or agent can stop everything hum manages in the current project with one command, without naming each process and without shutting down the daemon or touching other projects.

Scope: `hum down [--json]` resolves the current project root, applies the stop sequence (SIGTERM, bounded grace, SIGKILL if necessary) concurrently to every running record in that project, whether resolved or ad hoc, and returns one stable result per name: `stopped`, `not_running`, or `error` with a message. With resolved definitions present, every declared name appears in the result even when it was not running. Exit 0 when nothing is left running, 1 when any stop failed. `down` never starts a daemon: without a daemon, or with a daemon and nothing running, it succeeds and prints `Nothing is running in this project.` It never signals other projects' processes and never stops the daemon; `shutdown` remains the daemon-level operation. Help, README, and docs/design.md present `down` as the inverse of `up` and distinguish it from `stop <name>...` and `shutdown`.

Non-goals: stopping other projects, daemon shutdown, dependency ordering, MCP wiring (HUM-015 adds the tool), or removing runtime records.

Modified-file contract: internal/app/, internal/protocol/, internal/cli/, integration/, README.md, docs/design.md.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/app ./internal/protocol ./internal/cli -run Down -count=1` exits 0 and proves down stops every running record in the current project only, waits for graceful termination before SIGKILL per group, leaves other projects and the daemon running, returns one stable human/JSON result per name including not_running declared names, exits 0/1 as documented, and succeeds without a daemon printing `Nothing is running in this project.` while creating no runtime state.
- [ ] #2 `go test ./integration -run TestDownWorkflow -count=1` exits 0 and proves `up` followed by `down --json` stops every declared process and a detached ad hoc record in the same project, a process in a second project keeps running, `list` then shows every project process stopped, the daemon still answers, and a second `down` is a no-op success.
- [ ] #3 `go test ./internal/cli -run TestLifecycleHelp -count=1` exits 0 and README.md plus docs/design.md document `down` as the project-scoped inverse of `up`, distinct from `stop <name>...` and both `shutdown` modes.
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
