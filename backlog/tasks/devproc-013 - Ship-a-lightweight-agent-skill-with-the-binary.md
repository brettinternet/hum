---
id: DEVPROC-013
title: Ship a lightweight agent skill with the binary
status: To Do
assignee: []
created_date: '2026-09-02 20:13'
labels:
  - cli
  - docs
milestone: m-0
dependencies:
  - DEVPROC-008
  - DEVPROC-009
  - DEVPROC-012
modified_files:
  - internal/skill/
  - internal/cli/
  - README.md
priority: high
type: feature
ordinal: 975
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: an agent working in any project learns the hum workflow from one short, versioned skill file shipped inside the binary, without reading the design docs or the README.

Scope: `internal/skill/SKILL.md` in the Agent Skills format (YAML frontmatter with `name: hum` and a one-sentence `description` that says when to use it, then instructions that fit on one screen) telling an agent to start processes with `run --detach`, wait for readiness with `wait --match` and branch on its exit codes, read bounded output with `logs --tail` or `logs --after-cursor --json`, `restart` after configuration changes, `stop` when done, use `list` to discover what is already running, and never run dev servers directly in its own shell; the file is embedded with `embed` from the same package and printed byte-for-byte by `hum skill`, so `hum skill > .claude/skills/hum/SKILL.md` installs it for Claude Code and the equivalent path works for other agents; a test asserts every `hum <command>` and `--flag` named in the skill exists in the root command tree; README documents installation in three lines.

Non-goals: MCP, per-agent installers or auto-installation, a skill marketplace, or prose beyond one screen.

Modified-file contract: internal/skill/, internal/cli/, README.md.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/skill ./internal/cli -run Skill` exits 0 and proves `hum skill` prints the embedded file byte-for-byte, the frontmatter has `name: hum` and a non-empty description, and every `hum <command>` and `--flag` mentioned in the file exists in the root command tree.
- [ ] #2 `./bin/hum skill > "$TMP/.claude/skills/hum/SKILL.md"` exits 0 and produces a file under 80 lines that starts with YAML frontmatter and mentions `run --detach`, `wait --match`, `logs`, `restart`, `stop`, and `list`.
- [ ] #3 `go list -deps ./internal/skill` lists only standard-library packages.
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
