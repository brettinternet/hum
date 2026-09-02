@AGENTS.local.md

# Agents

## Tooling

- Install the project toolchain and hooks with `task init`.
- Use project `task` targets instead of reconstructing commands. Use `mise exec <tool> -- <command>` when a project-managed tool is not already on `PATH`.
- Use the smallest verification loop that covers the change. Do not run `task check` by default.
- Before committing, stage the intended files and run `task check:staged`. It runs only the applicable staged formatting and secret checks.
- Run `task check` only for cross-project changes, before a release, or when explicitly requested.
- Run relevant project-specific checks when they exist.

## Backlog.md

- `backlog/` is the only project task queue. Task files are provider-owned: read and mutate them
  with the `backlog` CLI. Never edit task Markdown directly and never create a second queue beside
  it.
- A task is only ready when its outcome, scope, non-goals, and modified-file contract are explicit
  and each acceptance criterion names a locally executable command and its expected result. Code
  presence, future CI, and "should work" are not evidence.
- Never skip, delete, or weaken a test to make the gate green. Stop and escalate instead.

## Git and GitHub

- Agent-created branches MUST be created as worktrees under `.worktrees/`; do not create branches in the primary checkout.
- Create a worktree with `git worktree add -b <branch> .worktrees/<branch>` from the repository root.
- Run any documented worktree setup from inside the new worktree before making changes.
- Do not start shared services from a worktree unless the project explicitly supports it.
- After a merged worktree branch is no longer needed, remove the worktree, delete the branch, and prune stale worktree metadata before handoff.
- Use `gh` for GitHub operations; do not construct raw API calls or open GitHub URLs in a browser.
- Do not push or open a pull request without explicit instruction.

## Scope

- Do not edit files outside the current task scope.
