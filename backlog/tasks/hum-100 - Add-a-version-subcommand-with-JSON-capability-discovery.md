---
id: HUM-100
title: Add a version subcommand with JSON capability discovery
status: To Do
assignee: []
created_date: '2026-09-11 17:38'
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
- [ ] #1 AC1 — `go test ./internal/cli -run '^TestVersionCommand' -count=1 -v` exits 0 and proves human output equals the `--version` flag output, JSON contains exactly `schema_version`, `version`, and `build_time`, and the command performs no project resolution and no daemon socket contact (runtime dir remains absent).
- [ ] #2 AC2 — `task cli:build && ./bin/hum version --json | python3 -c 'import json,sys; d=json.load(sys.stdin); assert d["schema_version"]==1 and set(d)=={"schema_version","version","build_time"}'` exits 0.
- [ ] #3 AC3 — `./bin/hum version --project /tmp; echo $?` and `./bin/hum version --global; echo $?` both print a usage error and exit 1, matching `hum mcp --project /tmp`.
- [ ] #4 AC4 — `./bin/hum completion zsh | rg -n 'version'` exits 0 and `rg -n 'hum version' docs/design.md docs/cli-json-v1.md README.md` exits 0 with the JSON shape documented under the version 1 contract.
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
