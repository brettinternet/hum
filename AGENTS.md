@AGENTS.local.md

# Agents

## Tooling

- Install the project toolchain and hooks with `task init`.
- Use project `task` targets instead of reconstructing commands. Use `mise exec <tool> -- <command>` when a project-managed tool is not already on `PATH`.
- Use the smallest verification loop that covers the change. Do not run `task check` by default.
- Before adding a test, follow the placement rules in docs/development.md#tests.
- Before committing, stage the intended files and run `task check:staged`. It formats and re-stages applicable Go files, then scans the staged snapshot for secrets. Use `task fix:staged` when only the staged formatter is needed.
- Run `task check` only for cross-project changes, before a release, or when explicitly requested. Run the full `task ci` gate independently before pushing.
- Run relevant project-specific checks when they exist.

## Backlog

- `backlog/` is the only project task queue. Task files are provider-owned: read and change them
  only through `bl` (CLI or MCP), for example `bl show HUM-12 --json`. Never edit task Markdown
  directly and never create a second queue beside it.
- Pick and claim work in one step, in a stable session for the whole loop:

  ```sh
  bl next --claim --start --session SESSION --json
  ```

  Status is not a claim. Renew with `bl heartbeat --session SESSION` at least once per half TTL.
  Finish with `bl done ID --session SESSION`. If stopping incomplete, persist progress and run
  `bl release ID --session SESSION`.
- Read `bl instructions` and `bl instructions workflow` before acting. Use `bl note ID TEXT`,
  `bl ac ID check N --evidence TEXT`, and pass `--if-version VERSION` (from the last `show`/write)
  to writes that depend on that read.
- A task is only ready when its outcome, scope, non-goals, and modified-file contract are explicit
  and each acceptance criterion names a locally executable command and its expected result. Code
  presence, future CI, and "should work" are not evidence.
- Run `task ci` and satisfy the Definition of Done before `bl done`.
- Never skip, delete, or weaken a test to make the gate green. Stop and escalate instead.

## Git and GitHub

- Only use a worktree/branch when directed to do so.
- Agent-created branches MUST be created as Worktrunk worktrees under `.worktrees/`; do not create branches in the primary checkout. Use `mise exec worktrunk -- wt switch --create <branch> --base main --no-cd --format=json`; `.config/wt.toml` prepares the checkout before use.
- After a merged worktree branch is no longer needed, use `mise exec worktrunk -- wt remove <branch> --foreground --format=json` to remove the checkout and eligible branch.
- Use `gh` for GitHub operations; do not construct raw API calls or open GitHub URLs in a browser.
- Do not push or open a pull request without explicit instruction.
