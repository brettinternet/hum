---
id: HUM-013
title: Ship a resolved-project skill for shell-only agents
status: Done
assignee: []
created_date: '2026-09-02 20:13'
updated_date: '2026-09-04 03:00'
labels:
  - cli
  - docs
milestone: m-1
dependencies:
  - HUM-015
  - HUM-018
  - HUM-019
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

Scope: `internal/skill/SKILL.md` in the Agent Skills format, with YAML frontmatter containing `name: hum` and a one-sentence description that says when to use it, followed by instructions that fit on one screen. Teach agents to try `up` for the project (it waits for readiness by default), use `start <name>` for one resolved process, `list` for discovery, source visibility, and readiness, bounded `logs --tail` or `logs --after-cursor --json`, `wait` when a later condition matters, `restart` after definition changes, `stop <name>` when asked, and `down` to stop everything in the project. Explicitly forbid deriving or executing the underlying `npm run dev`-style command. A missing `hum.yaml` is normal when conservative discovery resolves one `dev` process; when resolution reports ambiguity or no candidate, or the project needs multiple commands, cwd, or readiness, ask the developer to run `hum init` and commit the result. The file is embedded with `embed` from the same package and printed byte-for-byte by `hum skill` for installation in an agent normal skill location. README documents this shell-only fallback separately from the primary MCP interface. A test asserts every hum command and flag named in the skill exists in the root command tree.

Non-goals: MCP implementation, automatic edits to third-party agent configuration, per-agent installers, a skill marketplace, running `hum init` itself, or prose beyond one screen.

Modified-file contract: internal/skill/, internal/cli/, README.md.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `go test ./internal/skill ./internal/cli -run Skill` exits 0 and proves `hum skill` prints the embedded file byte-for-byte, the frontmatter has `name: hum` and a non-empty description, and every hum command and flag mentioned in the file exists in the root command tree.
- [x] #2 `go list -deps ./internal/skill` succeeds and lists only standard-library packages.
- [x] #3 `go test ./internal/cli -run TestSkillHelp` exits 0 and proves CLI help identifies the emitted skill as the shell-only fallback and points MCP-capable agents to `hum mcp`.
- [x] #4 `go test ./internal/skill -run TestResolvedProjectInstructions` exits 0 and proves the skill is under 80 lines, tries `up`, names `start`, `list`, source and readiness visibility, bounded `logs`, `wait`, `restart`, `stop`, and `down`, treats missing hum.yaml as compatible with discovery, points the developer to `hum init` when explicit YAML is needed, and neither instructs raw `run -- <command>` nor contains package-manager development commands.
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
- [x] Add internal/skill/SKILL.md and a standard-library-only embed API that returns its exact bytes.
- [x] Add hum skill to the root CLI with shell-only fallback help and preserve MCP as the primary agent interface.
- [x] Test skill metadata, workflow instructions, forbidden raw commands, embedded output, command/flag references, and help text.
- [x] Document the shell-only skill fallback in README.md.
- [x] Run all acceptance commands, independent verification, task ci, then record evidence and finalize the task.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
AC#1: `go test ./internal/skill ./internal/cli -run Skill` passed for both packages; filtered tests prove byte-exact output, frontmatter, and command/flag references.
AC#2: `go list -deps ./internal/skill` passed; output contained only Go standard-library packages plus `hum/internal/skill`.
AC#3: `go test ./internal/cli -run TestSkillHelp` passed and proves shell-only fallback/MCP guidance.
AC#4: `go test ./internal/skill -run TestResolvedProjectInstructions` passed and proves the complete resolved-project instruction contract.
DOD: Independent verifier returned PASS for AC#1-AC#4. Final commit b3bcd93 passed `task ci`, including gofmt, vet, staticcheck, all tests, race tests, build, and built-binary smoke. Diff is limited to internal/skill/, internal/cli/, and README.md; no tests were deleted, skipped, or weakened; no protected gate file changed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Shipped `hum skill` with a byte-exact embedded Agent Skills document for shell-only agents, kept MCP primary, documented the fallback, and added content/CLI/reference coverage. Commit b3bcd93 passed all acceptance commands, independent verification, and `task ci`.
<!-- SECTION:FINAL_SUMMARY:END -->
