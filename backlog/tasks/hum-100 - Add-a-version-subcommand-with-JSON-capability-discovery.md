---
id: HUM-100
title: Add a version subcommand with JSON capability discovery
status: Done
assignee: []
created_date: '2026-09-11 17:38'
updated_date: '2026-09-11 18:49'
labels:
  - cli
  - json
  - contract
  - integration
milestone: m-5
dependencies:
  - HUM-093
references:
  - HUM-093
  - HUM-092
  - docs/design.md
modified_files:
  - internal/cli/
  - cmd/hum/
  - docs/design.md
  - docs/cli-json-v1.md
  - README.md
priority: medium
type: enhancement
ordinal: 72800
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: external adapters and scripts detect which Hum they are talking to, and which machine-output contract it speaks, with one daemon-free command instead of parsing the human `--version` line.

Scope: add a `hum version [--json]` subcommand. Human output is byte-identical to `hum --version` today (`hum version <version> (built <time>)`). `--json` emits one object `{"schema_version": 1, "version": "<version>", "build_time": "<time>"}` and nothing else. The command never resolves a project, reads a manifest, or contacts or starts the daemon, and like `serve`, `mcp`, and `skill` it rejects `--project`/`-C` and `--global` as usage errors. Add it to shell completion and the help-contract and surface tests, and document it in the CLI grammar in docs/design.md, the version 1 contract in docs/cli-json-v1.md, and the README coding-agents section as the recommended feature-detection call.

Evidence (2026-09-11): `hum --version --json` currently fails with `flag provided but not defined: -json`, and `hum version` is an unknown command. The Herdr plugin (HUM-092) and any script need to distinguish a Hum without `schema_version` from one with it before trusting field semantics.

Non-goals: reporting daemon version or daemon state (that belongs to a daemon-aware command), Go runtime or build metadata beyond version and build time, changing the `--version`/`-v` flag output, or contacting the daemon.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 AC1 — `go test ./internal/cli -run '^TestVersionCommand' -count=1 -v` exits 0 and proves human output equals the `--version` flag output, JSON contains exactly `schema_version`, `version`, and `build_time`, and the command performs no project resolution and no daemon socket contact (runtime dir remains absent).
- [x] #2 AC2 — `task cli:build && ./bin/hum version --json | python3 -c 'import json,sys; d=json.load(sys.stdin); assert d["schema_version"]==1 and set(d)=={"schema_version","version","build_time"}'` exits 0.
- [x] #3 AC3 — `./bin/hum version --project /tmp; echo $?` and `./bin/hum version --global; echo $?` both print a usage error and exit 1, matching `hum mcp --project /tmp`.
- [x] #4 AC4 — `./bin/hum completion zsh | rg -n 'version'` exits 0 and `rg -n 'hum version' docs/design.md docs/cli-json-v1.md README.md` exits 0 with the JSON shape documented under the version 1 contract.
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
Implementation started in an isolated worktree; selected by task backlog:next and claimed with worklease.

AC#1 — go test ./internal/cli -run '^TestVersionCommand' -count=1 -v passed; it proves human output is byte-identical to hum --version, JSON has exactly schema_version/version/build_time, execution succeeds outside a project, and the configured runtime directory remains absent.
AC#2 — task cli:build plus the specified Python JSON assertion passed with schema_version 1 and exactly the three contract keys.
AC#3 — ./bin/hum version --project /tmp and ./bin/hum version --global each emitted a usage error and exited 1, matching the hum mcp --project /tmp usage-error class.
AC#4 — ./bin/hum completion zsh piped to rg -n version and rg -n 'hum version' docs/design.md docs/cli-json-v1.md README.md passed; the exact JSON shape is documented in the v1 contract.
AC#5 — task test and task check passed with no deleted, skipped, or weakened tests.
DOD — Independent verifier returned PASS for AC1–AC5 and found no defects. All changed paths are within the declared modified-file contract. task check:staged passed before commit. task ci passed on task commit 436399a and merged main commit c399736. No protected gate files changed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added hum version with human output identical to hum --version and daemon-free JSON capability discovery for CLI schema version 1. Added completion/help/surface coverage and documented the command in the CLI grammar, v1 machine-output contract, and coding-agent guidance. Task commit 436399a merged to main as c399736.
<!-- SECTION:FINAL_SUMMARY:END -->
