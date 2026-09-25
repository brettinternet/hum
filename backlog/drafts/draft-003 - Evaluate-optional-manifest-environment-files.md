---
id: DRAFT-003
title: Evaluate optional manifest environment files
status: Draft
assignee: []
created_date: '2026-09-25 20:18'
labels:
  - config
  - product-boundary
dependencies: []
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Every path in `environment.files` is required today: a missing file fails `hum up`, `start`, declared `run`, and `restart` before the daemon is contacted. HUM-108 listed "optional file APIs" as a non-goal. The per-worktree port pattern in examples/worktrees shows the cost. A Git-ignored `.env.local` must exist in every checkout, including the main checkout, so each new clone or worktree needs a manual or hook-driven setup step before `hum up` works. A common convention in the dotenv ecosystem is a committed `.env` with defaults plus an optional, uncommitted `.env.local` override. Hum cannot express that today.

Leading shape, following Docker Compose `env_file`: keep plain strings as required files and accept a mapping per entry with `path` and `required` (default true).

```yaml
environment:
  files:
    - .env
    - {path: .env.local, required: false}
```

A missing optional file is skipped; an optional file that exists is still validated exactly like a required one (containment, symlink escape, regular file, readable, syntax, bounds). Only absence is tolerated.

Case against: required files fail loudly. With an optional `.env.local`, a forgotten per-worktree file silently falls back to the committed default and can collide with another worktree on the same port, which is the failure this pattern exists to prevent. The current workaround is one command per checkout, and Worktrunk can automate it with a `pre-start` hook using `hash_port`. Promoting this draft reverses a HUM-108 non-goal, so it needs a recorded decision first.

Questions to settle during refinement: whether skipped optional files appear in `hum doctor`, `up` progress, or JSON/MCP output; whether `hum status` or drift reporting should notice a file that appears or disappears later (environment is not compared for drift today); how a dangling symlink is classified (absent or invalid); whether the mapping form or a separate key such as `optional_files` better fits the closed schema and SchemaStore users; and which docs, schema, and parser tests change.

This draft is intentionally not implementation-ready. Refinement should produce an explicit outcome, scope, non-goals, modified-file contract, and locally executable acceptance evidence before promotion.
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
