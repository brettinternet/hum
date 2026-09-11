---
id: HUM-099
title: Ship a Claude Code plugin marketplace alongside the Codex plugin
status: To Do
assignee: []
created_date: '2026-09-11 17:04'
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
- [ ] #1 AC1 — `claude plugin validate . --strict` from the repository root exits 0 for the marketplace and the referenced plugins/hum plugin.
- [ ] #2 AC2 — `go test ./internal/skill -run 'TestPlugin' -count=1 -v` exits 0 and proves the Claude manifest name is `hum`, its version equals the Codex manifest version, and the marketplace source is `./plugins/hum`.
- [ ] #3 AC3 — `claude plugin marketplace add brettinternet/hum && claude plugin install hum@hum` exits 0 on the published main branch and `claude plugin details hum@hum` lists the hum skill and the hum MCP server; the reverse `claude plugin uninstall hum@hum && claude plugin marketplace remove hum` restores the previous state.
- [ ] #4 AC4 — `rg -n 'claude plugin marketplace add brettinternet/hum|claude plugin install hum@hum' README.md docs/coding-agents.md` exits 0 and the manual `claude mcp add` registration remains documented as the fallback.
- [ ] #5 AC5 — `task test` and `task check` exit 0 with no deleted, skipped, or weakened tests.
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
