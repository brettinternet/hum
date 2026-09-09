---
id: HUM-058
title: Canonicalize project scopes and add an explicit global namespace
status: To Do
assignee: []
created_date: '2026-09-09 16:05'
updated_date: '2026-09-09 16:15'
labels:
  - cli
  - daemon
  - mcp
milestone: m-4
dependencies: []
modified_files:
  - internal/project/root.go
  - internal/project/root_test.go
  - internal/app/app.go
  - internal/app/app_test.go
  - internal/protocol/protocol.go
  - internal/protocol/protocol_test.go
  - internal/daemon/client.go
  - internal/daemon/server.go
  - internal/daemon/wire_protocol.go
  - internal/daemon/runtime.go
  - internal/daemon/daemon_test.go
  - internal/daemon/wire_protocol_test.go
  - internal/daemon/runtime_test.go
  - internal/cli/root.go
  - internal/cli/commands.go
  - internal/cli/config.go
  - internal/cli/render.go
  - internal/cli/completion.go
  - internal/cli/project_scope_test.go
  - internal/cli/completion_test.go
  - internal/cli/help_contract_test.go
  - internal/mcp/tools.go
  - internal/mcp/server.go
  - internal/mcp/tools_test.go
  - internal/mcp/server_test.go
  - cmd/hum/integration_test.go
  - integration/project_scope_test.go
  - README.md
  - docs/design.md
  - docs/coding-agents.md
  - internal/skill/SKILL.md
  - internal/skill/skill_test.go
priority: high
type: feature
ordinal: 35700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
**Problem.** hum keys process names by the nearest Git or worktree root, but discovery keeps the lexical path, so a symlink alias to the same worktree creates a second namespace. Machine-wide helpers such as a proxy or tunnel have no home and land in whichever project the caller happened to be in. An agent working in a linked worktree cannot tell whether a name is missing, lives in the main checkout, or was launched globally.

**Outcome.** Project identity is the canonical physical path (symlinks resolved) of the nearest Git root or linked-worktree root, falling back to the canonical invocation or selected directory outside Git. Every call selects its scope automatically from where it runs, and each worktree is its own scope even when worktrees share one repository. An explicit `--global` scope holds machine-wide ad-hoc sessions. Discovery across scopes is one command away, cross-scope access is always explicit, and a local miss never silently reaches another scope.

**Scope identity.**
- Canonicalization applies to identity only. Child cwd for ad-hoc `run`, manifest-relative cwd, and `init` output keep the caller's lexical path so tools that read `$PWD` or symlinked module trees are unaffected.
- Canonicalize at every external boundary: CLI selection, MCP `project_root`, daemon requests, runtime-state load, and crash reconciliation. Aliases can neither create nor address duplicate records.
- Snapshots and persistent state carry the canonical root. There is no basename label: basenames collide across repos and worktrees and would invite use as identity.

**Default scope and explicit cross-scope access.**
- Defaults stay current-scope-only: `list`, `status`, name completion, and every lifecycle command.
- `--project PATH`/`-C PATH` targets another scope as today. Observation and lifecycle commands (`list`, `status`, `logs`, `wait`, `signal`, `stop`, `remove`, `down`) also accept a PATH that no longer exists on disk when it exactly matches a canonical root the daemon knows, so records of a removed worktree stay addressable. Launch commands (`run`, `start`, `restart`, `up`, `init`) keep requiring an existing directory.
- Empty-state messages name the canonical scope: `Nothing is running in /abs/worktree. Use hum list --all to see every scope.`

**Discovery and not-found guidance.**
- `list --all` covers every project scope plus global. Human output groups rows under one heading per scope showing the canonical root or `global` and the exact selector prefix (`hum --project /abs/root`, `hum --global`); rows keep the current columns. JSON adds `scope` (`project` or `global`) to every process and keeps `project_root`, omitted for global.
- A name missing in the current scope fails with `not_found`; the message names the current canonical scope. When the same name exists elsewhere, the daemon lists each match in error `details.other_scopes` (`scope`, `project_root`) and the CLI renders one copyable command per match (`hum --project /abs/main logs web`, `hum --global logs web`) followed by `hum list --all`. This is read-only guidance; no command acts on another scope implicitly.

**Global namespace.**
- Selected by `--global`, shorthand `-g`, with the same placement rules as `--project`/`-C`, including `run` before or after NAME. Mutually exclusive with `--project`/`-C`. `--global` with `list --all` is a usage error because `--all` already spans every scope. Conflicts fail before daemon contact.
- Global sessions are ad-hoc retained sessions only. `run` uses the invocation directory as child cwd. Under `--global`, `start` and `restart` never read a manifest; they act on retained global sessions and otherwise report not-found. `down` stops every running global session. `init` and `up` reject `--global` before daemon contact and point to `hum --global run NAME -- COMMAND`.
- `--global list` and nameless `--global status` show only global sessions. Name completion under `--global` completes global names.
- `(scope, name)` is the record key: equal names coexist in two worktrees and global, and records never move between scopes.

**Wire protocol, runtime state, MCP.**
- Requests carry an explicit scope; legacy requests without one remain project-scoped. Project requests still require an absolute existing root and canonicalize it; global requests carry no project identity.
- Runtime state persists scope and canonical root. On load, aliases that collapse to one key or an incompatible prior format produce the established actionable upgrade failure; never silently orphan, merge, or retarget a live record.
- MCP tools accept `scope` (`project` default, or `global`). `project_root` is required for project and rejected for global. `list` gains `all`. Not-found tool errors carry the same `other_scopes` details. Existing project-root validation is unchanged.

**Documentation.** CLI help, README.md, docs/design.md, docs/coding-agents.md, MCP descriptions, and the embedded skill document automatic directory scope, separate worktree defaults, explicit `--project`/`-C` cross-worktree access, symlink canonicalization, `--global`/`-g`, placement and conflicts, not-found guidance, `list --all`, global ad-hoc limits, and the `scope`/`project_root` JSON fields. Generated commands and JSON spell `--global` and `--project`; `-g` and `-C` appear once each as shorthand. Examples: `hum run web -- bun dev`, `hum -C /path/to/main logs web`, `hum -g run proxy -- caddy run`, `hum list --all`.

**Non-goals.** Sharing names across worktrees, identity from a remote URL or basename, moving processes between scopes, declaring global processes in `hum.yaml`, multi-scope mutation beyond `list --all`, guessing a main or parent worktree, silent fallback to another scope after a local miss, an environment variable for scope selection, or changing child cwd because the supervision scope is global.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `go test ./internal/project -run '^TestCanonicalProjectIdentity$' -count=1 -v` exits 0 and prints PASS, proving nested paths and symlink aliases resolve to one canonical physical project root, separate Git worktree roots stay distinct despite a shared common Git directory, non-Git directories use a canonical physical fallback, and the lexical path is preserved separately for child cwd.
- [ ] #2 `go test ./internal/app -run '^TestSupervisorProjectAndGlobalScopes$' -count=1 -v` exits 0 and prints PASS, proving `(scope, name)` is the record key: equal names coexist across two projects and global, a symlink alias reaches the same project record, project list/get/output/wait/stop/remove cannot see another project or global record, global operations work independently of caller cwd, and a not-found result for a name present elsewhere carries `other_scopes` matches.
- [ ] #3 `go test ./internal/cli -run '^TestProjectAndGlobalScopeSelection$' -count=1 -v` exits 0 and prints PASS, proving calls without a selector use the canonical scope of the invocation directory; default list, status, name completion, and lifecycle commands stay current-scope-only; `--global` and `-g` work before or after run, start, down, list, status, attach, logs, wait, input, signal, restart, stop, and remove, and before or after NAME for run; a global ad-hoc run records the lexical invocation directory as cwd; `--global` name completion lists only global names; `--global` with `--project`/`-C`, with `list --all`, with init, or with up fails before daemon contact with actionable guidance; and `--global start`/`restart` on an unknown name reports not-found without reading a manifest.
- [ ] #4 `go test ./internal/cli -run '^TestCrossWorktreeScopeDiscovery$' -count=1 -v` exits 0 and prints PASS, proving a process in the main checkout is hidden by default from a linked worktree and reachable through `--project MAIN` and `-C MAIN`; `list --all` groups human rows under per-scope headings with the canonical root or `global` and the selector prefix, and JSON carries `scope` and `project_root`; empty default output names the canonical scope and points to `hum list --all`; a missing local name yields `not_found` naming the current scope with one copyable `hum --project PATH ...` or `hum --global ...` command per other-scope match plus `hum list --all`, and nothing was mutated; and `--project` on a removed worktree path still lists, stops, and removes its records while run, start, up, and init reject it.
- [ ] #5 `go test ./internal/protocol ./internal/daemon ./internal/mcp -run Scope -count=1 -v` exits 0 and prints PASS, proving the versioned wire protocol and MCP tools carry explicit project versus global scope; legacy requests remain project-scoped; project requests require and canonicalize an absolute existing root; global requests carry no project identity; all-scope listing includes global records; MCP accepts `scope`, requires `project_root` for project and rejects it for global, exposes `list` `all`, and returns `other_scopes` in not-found errors; and invalid or conflicting selectors are rejected.
- [ ] #6 `go test ./internal/daemon -run '^TestRuntimeStateScopeIdentity$' -count=1 -v` exits 0 and prints PASS, proving canonical project scope and explicit global scope survive daemon crash recovery, alias paths cannot restore duplicate keys, and an incompatible prior protocol or runtime-state format produces the established actionable upgrade failure instead of silently orphaning, merging, or retargeting a live process.
- [ ] #7 `go test ./internal/cli ./internal/mcp ./internal/skill -run 'ScopeDocs|HelpContract|FlagAliases' -count=1 -v` exits 0 and prints PASS, proving CLI help, README.md, docs/design.md, docs/coding-agents.md, MCP descriptions, and the embedded skill document automatic directory scope, separate worktree defaults, explicit `--project`/`-C` cross-worktree access, symlink canonicalization, `--global` and `-g`, placement and conflicts, not-found guidance, `list --all`, global ad-hoc limits, and the `scope`/`project_root` JSON fields with copyable examples, and that generated output uses long canonical flags.
- [ ] #8 `task ci` exits 0.
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

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Canonical identity in internal/project and internal/app: resolve physical roots, keep the lexical path for cwd, key records by (scope, name), and return other-scope matches on not-found.
2. Protocol, daemon, runtime state: explicit scope with project-scoped legacy default, canonicalization at request and state-load boundaries, versioned upgrade failure for collapsing aliases or incompatible state.
3. CLI: --global/-g selection and conflicts, run parser placement, grouped list --all, scope-aware empty states and not-found guidance, removed-worktree targeting for observation and lifecycle commands, completion.
4. MCP scope and list all; then help, README, design, coding-agents, and skill docs with focused doc tests; finish with task ci.
<!-- SECTION:PLAN:END -->
