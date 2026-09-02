---
id: DEVPROC-014
title: Declare project processes in a committed manifest
status: To Do
assignee: []
created_date: '2026-09-02 20:13'
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
  - internal/skill/
  - README.md
  - docs/design.md
priority: medium
type: feature
ordinal: 1100
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: a project commits one manifest that names its development processes, so humans and agents start, restart, and inspect them by name without knowing or retyping the command.

Scope: a manifest file at the project root (format decided in this task; argv arrays only, never shell text) declaring named processes with argv, optional cwd relative to the root, and an optional readiness regex; `hum start <name> [--wait] [--json]` launches a declared process detached and with `--wait` blocks until the readiness pattern matches, the process exits, or a timeout expires using the DEVPROC-009 wait semantics and exit codes; `hum up` starts every declared process; `run <name>` without argv uses the declared command; `list` shows declared-but-stopped processes with state stopped; `restart` prefers the current manifest entry over the recorded launch when the manifest exists; manifest validation errors name the file and entry. Update docs/design.md, README.md, and the shipped skill for the new commands.

Non-goals: runtime settings in the manifest, shell-text commands, dependency ordering between processes, health checks beyond the readiness pattern, or automatic restarts.

Modified-file contract: internal/manifest/, internal/app/, internal/protocol/, internal/cli/, internal/skill/, README.md, docs/design.md.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/manifest` exits 0 and covers parsing, argv-only validation, relative cwd resolution, readiness regex compilation, and precise errors naming file and entry.
- [ ] #2 `go test ./internal/cli -run 'TestStart|TestUp|TestManifestList'` exits 0 and covers start by name, `--wait` outcomes and exit codes, `up` starting every entry, declared-but-stopped listing, and `run <name>` without argv.
- [ ] #3 Against a fixture project with a manifest, the built binary runs `hum start web --wait` returning 0 once the readiness pattern matches, `hum list --json` shows every declared entry with state, and `hum restart web` relaunches from an edited manifest entry.
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
