---
id: DRAFT-001
title: Expose opt-in ad hoc run over MCP
status: Draft
assignee: []
created_date: '2026-09-05 15:39'
updated_date: '2026-09-05 15:39'
labels:
  - mcp
  - security
  - process
  - docs
dependencies:
  - HUM-026
modified_files:
  - internal/cli/
  - internal/mcp/
  - internal/skill/
  - internal/testutil/
  - integration/
  - plugins/hum/skills/hum/SKILL.md
  - README.md
  - docs/design.md
  - docs/coding-agents.md
priority: low
type: feature
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: when the operator starts `hum mcp --allow-run`, the MCP server additionally exposes a bounded `run` tool that can create an ad hoc named supervision session from exact argv. This serves agents that have hum MCP access but no shell access. The default MCP surface remains definition-only and does not grant arbitrary process execution.

Why this remains a draft: this is a material capability expansion, not a completeness requirement. Promoting it requires evidence of a real target workflow where the agent lacks shell execution and committing to the security boundary below; convenience for agents that already have a shell is insufficient.

Proposed capability boundary: without `--allow-run`, the `run` tool is absent from tool discovery and calls are unknown-tool errors. The opt-in is visible in CLI help and startup documentation and applies to every client connected to that stdio MCP server; it is capability consent, not authentication or sandboxing. The bundled plugin keeps invoking plain `hum mcp`, so it does not enable arbitrary run by default.

Proposed tool: required `project_root`, `name`, and non-empty `argv: string[]`; optional `tty: boolean` defaults false. The server validates the existing safe-name rule, resolves the project exactly as other MCP tools do, executes exact argv without a shell, and fixes the child cwd to the resolved project root. It accepts no command string, cwd, environment, readiness, timeout, or shell-expansion fields. The child inherits the MCP server process environment under existing launch semantics.

Proposed lifecycle: the call is bounded and returns after daemon launch acknowledgement with the normal process snapshot; it never streams output or waits for readiness. Agents observe with bounded `logs`/`wait`, answer tty prompts with HUM-024 `input`, and stop or remove through existing tools. A declared name is rejected with guidance to use `start`; a stopped retained ad hoc session may have its argv/tty replaced under CLI `run -- COMMAND` collision rules; a running name never creates a second child. Exact retry/idempotency behavior for a lost MCP response must be resolved before promotion.

Proposed implementation boundary: prefer composing the existing daemon launch request and application collision rules; add no private protocol operation if those contracts suffice. Tool registration is conditional at server construction so disabled mode has no schema or handler surface. Tests must prove disabled discovery/calls, enabled exact argv including spaces/metacharacters, project-root cwd, inherited environment, tty propagation, collision behavior, bounded return, and compatibility with logs/wait/input.

Security documentation: enabling the flag allows the connected MCP client to execute any executable available to the hum process with that process's permissions and environment. Project-root cwd and argv-only execution reduce ambiguity but are not a sandbox. Documentation must warn against enabling it for untrusted or remotely reachable clients; hum adds no remote transport or authentication.

Non-goals: enabling run by default; changing the bundled plugin default; shell command strings or implicit `sh -c`; arbitrary cwd; caller-supplied environment or secrets; readiness configuration for ad hoc runs; synchronous command completion; output in the tool response; follow/streaming over MCP; remote transport, authentication, authorization policy, command allowlists, or sandboxing; replacing project-approved `hum.yaml` definitions for normal workflows; Windows.
<!-- SECTION:DESCRIPTION:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 An independent verifier pass returned PASS for every acceptance criterion
- [ ] #4 The diff touches only the paths declared in the task's modified-file list, or the deviation is justified in Implementation Notes
- [ ] #5 No test was deleted, skipped, or weakened
- [ ] #6 No protected gate file was modified unless the owner labelled this task tooling
<!-- DOD:END -->
