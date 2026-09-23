---
id: HUM-099
title: Ship a Claude Code plugin marketplace alongside the Codex plugin
status: Done
assignee: []
created_date: '2026-09-11 17:04'
updated_date: '2026-09-11 18:43'
labels:
  - integration
  - docs
milestone: m-5
dependencies: []
references:
  - plugins/hum/.codex-plugin/plugin.json
  - .agents/plugins/marketplace.json
  - 'https://code.claude.com/docs/en/plugins-reference.md'
  - 'https://code.claude.com/docs/en/plugin-marketplaces.md'
modified_files:
  - .claude-plugin/marketplace.json
  - plugins/hum/.claude-plugin/plugin.json
  - internal/skill/skill_test.go
  - README.md
  - docs/coding-agents.md
priority: medium
type: feature
ordinal: 71800
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: Claude Code users install the hum skill and MCP server with two commands from this repository, matching the Codex plugin, instead of registering `hum mcp` by hand and copying the skill.

Scope: add `.claude-plugin/marketplace.json` at the repository root and `plugins/hum/.claude-plugin/plugin.json` beside the existing `.codex-plugin/plugin.json`, sharing the existing `plugins/hum/skills/` and `plugins/hum/.mcp.json` (Claude Code auto-discovers both at the plugin root; a bare `command: "hum"` resolves from PATH). Marketplace `name` is `hum`, `owner` is brettinternet, and the plugin `source` is `./plugins/hum`. Keep plugin `version` identical across the Codex and Claude manifests and extend the existing wiring tests in internal/skill/skill_test.go to cover the new manifests. Document `claude plugin marketplace add brettinternet/hum` followed by `claude plugin install hum@hum` in README and docs/coding-agents.md, keeping the manual `claude mcp add` path as the fallback.

Format verified 2026-09-11 against the Claude Code plugin reference: `.claude-plugin/` holds only the manifest; `skills/`, `.mcp.json`, and other components live at the plugin root; `claude plugin validate <path>` checks a plugin directory or, from the marketplace root, the marketplace plus every referenced local plugin.

Non-goals: a Cursor or Gemini plugin package, bundling the hum binary inside the plugin, changing SKILL.md content, adding hooks or commands, or publishing to the Anthropic community marketplace.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 AC1 — `claude plugin validate . --strict` from the repository root exits 0 for the marketplace and the referenced plugins/hum plugin.
- [x] #2 AC2 — `go test ./internal/skill -run 'TestPlugin' -count=1 -v` exits 0 and proves the Claude manifest name is `hum`, its version equals the Codex manifest version, and the marketplace source is `./plugins/hum`.
- [x] #3 AC3 — `claude plugin marketplace add brettinternet/hum && claude plugin install hum@hum` exits 0 on the published main branch and `claude plugin details hum@hum` lists the hum skill and the hum MCP server; the reverse `claude plugin uninstall hum@hum && claude plugin marketplace remove hum` restores the previous state.
- [x] #4 AC4 — `rg -n 'claude plugin marketplace add brettinternet/hum|claude plugin install hum@hum' README.md docs/coding-agents.md` exits 0 and the manual `claude mcp add` registration remains documented as the fallback.
- [x] #5 AC5 — `task test` and `task check` exit 0 with no deleted, skipped, or weakened tests.
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

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Commit 045f238 merged to local main.

AC#1 — PASS: `claude plugin validate . --strict` exited 0.
AC#2 — PASS: `go test ./internal/skill -run 'TestPlugin' -count=1 -v` exited 0; tests assert Claude name `hum`, Claude/Codex version equality, and marketplace source `./plugins/hum`.
AC#3 — BLOCKED ON PUBLISH: local marketplace add/install/details/uninstall/remove all exited 0; `claude plugin details hum@hum` listed skill `hum` and MCP server `hum`, and Claude state was restored. The exact `claude plugin marketplace add brettinternet/hum` path cannot pass until local main is pushed to GitHub; pushing was not authorized.
AC#4 — PASS: the specified `rg` command found both install commands in README.md and docs/coding-agents.md; `rg -n 'claude mcp add' docs/coding-agents.md` confirmed the manual fallback.
AC#5 — PASS: `task test` and `task check` exited 0; no tests were deleted, skipped, or weakened.

DoD evidence — `task ci` passed on final merged commit 045f238. An independent verifier passed AC1, AC2, AC4, AC5 and DoD1/4/5/6, passed the local AC3 lifecycle, and reported only the published-remote AC3 portion blocked. The diff touches exactly the five declared paths; no protected gate file changed.

AC#3 — PASS after publish: `claude plugin marketplace add brettinternet/hum`, `claude plugin install hum@hum`, `claude plugin details hum@hum`, `claude plugin uninstall hum@hum`, and `claude plugin marketplace remove hum` all exited 0 against published origin/main 045f238. Details listed skill `hum` and MCP server `hum`; final marketplace list was empty.

Final independent verifier — PASS for AC1–AC5. It independently confirmed origin/main at 045f238, repeated the published AC3 lifecycle with state restoration, and passed the focused validation, tests, documentation, and diff checks.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented and published the Claude Code plugin marketplace alongside the Codex plugin. Commit 045f238 is on origin/main; strict validation, focused tests, full test/check/CI gates, the published install/details/uninstall lifecycle, and independent verification all pass.
<!-- SECTION:FINAL_SUMMARY:END -->
