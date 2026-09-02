---
id: HUM-013
title: Ship a resolved-project skill for shell-only agents
status: To Do
assignee: []
created_date: '2026-09-02 20:13'
updated_date: '2026-09-02 20:43'
labels:
  - cli
  - docs
milestone: m-1
dependencies:
  - HUM-015
modified_files:
  - internal/skill/
  - internal/cli/
  - README.md
priority: high
type: feature
ordinal: 1250
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: a shell-capable coding agent learns the resolved-project hum workflow from one short, versioned skill file and can manage development processes by name without reading project scripts or running underlying development commands.

Scope: `internal/skill/SKILL.md` in the Agent Skills format, with YAML frontmatter containing `name: hum` and a one-sentence description that says when to use it, followed by instructions that fit on one screen. Teach agents to try `up --wait` for the project, use `start <name> --wait` for one resolved process, `list` for discovery and source visibility, bounded `logs --tail` or `logs --after-cursor --json`, `wait` when a later condition matters, `restart` after definition changes, and `stop` when asked. Explicitly forbid deriving or executing the underlying `npm run dev`-style command. A missing `hum.yaml` is normal when conservative discovery resolves one `dev` process; ask the developer for `hum.yaml` only when resolution reports ambiguity/no candidate or the project needs multiple commands, cwd, or readiness. The file is embedded with `embed` from the same package and printed byte-for-byte by `hum skill` for installation in an agent's normal skill location. README documents this shell-only fallback separately from the primary MCP interface. A test asserts every hum command and flag named in the skill exists in the root command tree.

Non-goals: MCP implementation, automatic edits to third-party agent configuration, per-agent installers, a skill marketplace, manifest generation, or prose beyond one screen.

Modified-file contract: internal/skill/, internal/cli/, README.md.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/skill ./internal/cli -run Skill` exits 0 and proves `hum skill` prints the embedded file byte-for-byte, the frontmatter has `name: hum` and a non-empty description, and every hum command and flag mentioned in the file exists in the root command tree.
- [ ] #2 `go test ./internal/skill -run TestResolvedProjectInstructions` exits 0 and proves the skill is under 80 lines, tries `up --wait`, names `start --wait`, `list`, source visibility, bounded `logs`, `wait`, `restart`, and `stop`, treats missing hum.yaml as compatible with discovery, explains when explicit YAML is needed, and neither instructs raw `run -- <command>` nor contains package-manager development commands.
- [ ] #3 `go list -deps ./internal/skill` succeeds and lists only standard-library packages.
- [ ] #4 `go test ./internal/cli -run TestSkillHelp` exits 0 and proves CLI help identifies the emitted skill as the shell-only fallback and points MCP-capable agents to `hum mcp`.
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
