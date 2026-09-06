---
id: HUM-039
title: Add a global project directory override
status: To Do
assignee: []
created_date: '2026-09-06 16:15'
labels:
  - cli
milestone: m-4
dependencies: []
modified_files:
  - internal/cli/root.go
  - internal/cli/commands.go
  - internal/cli/manifest.go
  - internal/cli/init.go
  - internal/cli/project_dir_test.go
  - docs/design.md
priority: low
type: feature
ordinal: 16700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: a global `--project DIR` option (short alias to be chosen; `-C` follows git and make) makes every command resolve the project root from DIR instead of the working directory, so `hum --project ~/src/app up` works from anywhere; ad-hoc `run` launches with cwd DIR. Documented in help and docs/design.md.

Why now: every command calls os.Getwd(); operating on a second project or from a scripts directory requires cd, and coding agents often run from a repository parent or worktree and must spawn a subshell just to change directory.

Non-goals: multi-project commands, project registries, MCP changes (project_root already exists).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/cli -run '^TestProjectDirFlag' -count=1 -v` exits 0 and prints PASS for up, list, logs, and status executed from an unrelated directory with the override pointing at a manifest project, and for a relative override path.
- [ ] #2 `task ci` exits 0.
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
