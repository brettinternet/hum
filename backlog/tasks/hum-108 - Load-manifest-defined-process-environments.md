---
id: HUM-108
title: Load manifest-defined process environments
status: To Do
assignee: []
created_date: '2026-09-12 07:21'
updated_date: '2026-09-12 15:38'
labels: []
dependencies: []
modified_files:
  - hum.schema.json
  - hum.example.yaml
  - internal/project/manifest.go
  - internal/project/manifest_test.go
  - internal/project/manifest_schema_test.go
  - internal/project/environment.go
  - internal/project/environment_test.go
  - internal/cli/commands.go
  - internal/cli/manifest.go
  - internal/cli/manifest_test.go
  - internal/cli/environment_test.go
  - internal/cli/mcp.go
  - internal/cli/mcp_test.go
  - internal/mcp/tools.go
  - internal/mcp/tools_test.go
  - integration/manifest_test.go
  - integration/mcp_test.go
  - README.md
  - docs/design.md
  - docs/coding-agents.md
priority: medium
type: enhancement
ordinal: 80800
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: let a manifest provide the environment for declared Hum processes. Hum already owns the exact child, readiness, and recovery environment; this removes stale worktree variables inherited by shells and long-lived MCP servers and removes duplicate wrapper scripts. The feature is deterministic only for configured keys: the default baseline remains the caller environment.

API:
```yaml
version: 1
environment:
  inherit: true
  files: [.env]
processes:
  api:
    argv: [bun, run, api]
    env:
      PORT: "3000"
      LEGACY_DATABASE_URL: null
```

Manifest contract:
- `environment` is optional.
- When present, `inherit` is a boolean defaulting to true and `files` is a sequence of nonempty relative strings defaulting to empty; every listed file is required, including `.env.local` if added.
- `processes.<name>.env` is an optional mapping with string or null values only; empty strings are valid, numeric and boolean values must be quoted, null unsets a lower-layer key, duplicate or unknown mapping keys and other value types fail strict parsing with contextual diagnostics.
- A missing or empty/default environment and an absent or empty process map copy that target's baseline byte-for-byte, including order and duplicate inherited entries, and add no size restrictions.
- Names match `[A-Za-z_][A-Za-z0-9_]*` only for configured file/map keys; inherited names outside that grammar are preserved during active composition.
- NUL is prohibited with schema/parser parity.
- Composition is inherited baseline, then files in listed order, then the process map.
- Inherited duplicate keys use the last value; later layers replace keys, removing a file key exposes any inherited value, and null or `inherit: false` removes it.
- Final composed entries are lexical by key.
- `inherit: false` synthesizes nothing.
- The existing executor resolves the composed `PATH`; absolute and `./` relative executables work without it.
- Hum does not promise that children or tools cannot modify or reload their own environment.

Environment-file grammar:
- Files are UTF-8 with LF or CRLF; a final unterminated line is valid.
- Allow blank lines and full-line comments. Assignment lines may have optional `export` followed by horizontal whitespace and horizontal whitespace around the assignment; assignment lines require `=`. Bare `export KEY` and other non-comment, nonblank lines without `=` are rejected.
- The first `=` splits the assignment line.
- Unquoted values trim horizontal edges; `#` starts a comment only at the value start or after horizontal whitespace, otherwise it is literal.
- Whole single-quoted values are literal and allow expansion-shaped text.
- Whole double-quoted values support only backslash, escaped double quote, `\n`, `\r`, and `\t`; after a closing quote, horizontal whitespace and a `#` comment are allowed, other suffixes are rejected.
- Empty values are valid.
- Reject BOM, NUL, invalid UTF-8, physical multiline values, unsupported escapes, malformed quotes, duplicate names within one file, and expansion-shaped text in unquoted or double-quoted values (`$` followed by a name-start, `${`, `$(`, or backtick).
- Such errors identify only key/location and advise single-quoting a literal or using an external loader.
- YAML inline strings are literal and are not dotenv-parsed.
- Do not evaluate, interpolate, decrypt, include, or implicitly discover files.
- This is not a direnv/dotenvx compatibility promise.

Paths, reads, and bounds:
- Paths are relative to the selected manifest directory, including a CLI `--file` selector, and must remain inside the canonical project root; `../.env` is valid when it stays inside the root.
- Reject absolute, empty, root, lexically escaping, symlink-escaping, missing, nonregular, and unreadable paths.
- Generic parsing validates syntax and lexical containment only and never accesses files, so read-only tools remain usable after a file is removed.
- Launch preflight resolves, stats, and bounded-reads regular files, with no FIFO hang.
- Read each unique canonical file once per invocation, cache only within that invocation, and reapply layers per target.
- Use no cross-invocation/worktree cache and never `os.Setenv`.
- The snapshot is fixed after preflight; concurrent external edits have no transactional-consistency guarantee.
- Limits are 16 file entries, 1 MiB actual bounded bytes per file, 4,096 assignments per file, and 4 MiB per final target's `KEY=VALUE` bytes including NUL separators.
- File errors name path and line when applicable; file-count and total limits name the limit without an invented line.
- A composed request is also checked after protocol escaping against existing `protocol.MarshalLine` and `DefaultMaxLineBytes` (8 MiB); raw environment fitting the limits does not guarantee encoded request size.
- Reject before client factory or protocol contact; do not increase the protocol limit.
- OS exec failures remain existing runtime behavior.

Command behavior:
- Selected declared `start`, `up`, declared `run`, and `restart` preflight once before any client or daemon creation/contact.
- CLI uses `os.Environ`; MCP uses `Options.Environment`, falling back to `os.Environ`.
- Preflight includes the full `up` dependency closure and aborts mixed declared/retained batches before mutation on any environment failure.
- It validates every required file even when a selected running record will be preserved.
- Empty `up`, ad hoc-only commands, and read-only commands do not access files.
- Explicit-argv `run` keeps the caller environment.
- Retained-only start/run/restart reuse the saved environment; discovered definitions preserve existing inherited behavior.
- `up` keeps running, pending, and exhausted snapshots.
- Targeted `start` preserves running entries but retains existing revival behavior for pending/exhausted entries, composing a fresh environment unless existing non-environment drift prevents it.
- Explicit restart reloads environment-only changes; automatic relaunch and `ready.exec` reuse the retained snapshot.
- Existing argv/name conflicts and runtime partial-success behavior remain unchanged.
- There is no environment drift/hash/enumeration.
- MCP uses the selected manifest from CLI selectors or its `project_root` and manifest inputs, never the invoking checkout; separate worktrees do not share caches.

Privacy:
- Response metadata, snapshots, JSON/MCP/status/drift output never serialize the environment map or enumerate configured environment names and values. Untrusted child/probe text, user argv, and existing runtime errors may still appear in responses and are outside this guarantee.
- Hum's environment parsing, composition, and preflight diagnostics may identify a valid variable key, path, process, line, and rule, but never a value, raw input, or decode error containing one. This guarantee does not apply to untrusted child/probe text, user argv, or existing runtime errors, including when embedded in a response.
- Invalid-key diagnostics use location only, and YAML errors are sanitized when environment content could leak.
- Preserve the protocol prohibition on an `env` response key.
- Child output remains unredacted. `ready.exec` captures bounded stdout/stderr and appends it to a failed-probe terminal diagnostic; that captured text can appear in status, JSON, or MCP and is untrusted user-derived text, so it is not globally redacted. It is not added to the normal supervised log store. Add no new capture or redaction, and advise process and probe commands not to print secrets.
- User argv and readiness argv may contain text; no whole-output secret scrub is promised.

Implementation:
- Implement one shared `internal/project/environment.go` composer for CLI and MCP.
- Thread the private sensitive specification and prepared per-target environments through CLI `manifestState` and the schedulers in `internal/cli/commands.go`, and MCP `Resolution`/`ensureDefinition`; reuse existing `Env` requests, process, daemon, and protocol behavior.
- Update the schema, example, README, design, and coding-agent docs.
- Keep the existing modified-file list exactly.
- Non-goals are CLI override flags, optional file APIs, per-process files, overlays, implicit discovery, shell/profile activation, dotenvx/direnv feature parity, hot reload, daemon-side loading, environment drift, secret redaction, and changed process identity/discovery.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `mise exec go -- go test ./internal/project -run '^TestManifestEnvironmentContract$|^TestEnvironmentFileContract$|^TestEnvironmentCompositionContract$' -count=1 -v` exits 0 with RUN and PASS for all three exact tests. The corpus covers schema/YAML types, required inherit/files/env shapes, duplicate/unknown keys, NUL parity, defaults, per-target byte-identical no-op behavior including opaque inherited names and duplicates, layer precedence and null/unset, every specified file syntax and expansion rejection, path containment cases, canonical read-once behavior, and all bounds. Diagnostics include path and line when applicable, omit values, and do not invent lines for count/total limits.
- [ ] #2 AC2 — `mise exec go -- go test ./internal/cli -run '^TestManifestEnvironmentPreflightContract$' -count=1 -v` exits 0 with RUN and PASS. Using the existing harness or small local seams, it proves command and mixed-batch behavior, `up` dependency closure, one inherited snapshot, one canonical read per unique file, raw and encoded bounds, zero client creation/contact/mutation on failure, environment-configuration and preflight diagnostics that omit values while allowing valid-key/location context, read-only behavior even when files are missing, required-file validation for preserved running records, empty/ad hoc/discovery compatibility, retained environment reuse, and existing argv/name-conflict and recovery semantics. Child output, captured probe text, user argv, and existing runtime errors are outside the no-value assertion.
- [ ] #3 AC3 — `mise exec go -- go test ./internal/mcp -run '^TestMCPManifestEnvironmentContract$' -count=1 -v` exits 0 with RUN and PASS. It proves the MCP baseline is captured once from `Options.Environment` or `os.Environ`, per-process composition and `inherit: false`, concurrent requests do not mutate global environment, selected project-root/manifest resolution, retained semantics, missing-file read-only behavior, zero client contact for malformed or oversized input, and response metadata never serializes the environment map or enumerates configured environment names and values. Environment parsing, composition, and preflight diagnostics may name a valid key and location but never a value; captured probe text, child output, user argv, and existing runtime errors are untrusted user-derived text outside that guarantee, including when embedded in responses.
- [ ] #4 AC4 — `mise exec go -- go test ./integration -run '^TestManifestEnvironmentLifecycle$' -count=1 -v` exits 0 and prints `--- PASS: TestManifestEnvironmentLifecycle`. A real daemon proves process and readiness parity, stale-variable override, inheritance isolation, separate worktrees, nested alternate-manifest paths, explicit restart reload including clearing retained environment, unchanged ad hoc saved environments, `up` preservation, targeted-start recovery behavior, automatic relaunch reuse after files change or disappear, and zero launches after a failing multi-target preflight. A failing `ready.exec` that prints a `NONSECRET` environment sentinel to stderr leaves that sentinel in the retained bounded failed-probe diagnostic and absent from the normal supervised child log store.
- [ ] #5 AC5 — `task cli:check && task test` exits 0. Schema, example, README, design, and coding-agent docs describe the API, no-op/default and inherit caveat, grammar, ordinary whitespace and full-line/comment examples, expansion and external-loader limits, required-file/path rules, precedence, bounds, encoded-request limit, command/state/MCP/worktree behavior, retained snapshot lifecycle, the environment-configuration diagnostic boundary, and the unredacted child/probe-output boundary, including advice not to print secrets from process or probe commands. The design document removes environment values/files from its unsupported non-goals.
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
- [ ] Extend the strict manifest parser and schema for boolean `environment.inherit`, nonempty relative `environment.files`, and process `env` mappings, preserving per-target no-op byte-for-byte behavior and private sensitive specifications. No dependencies are required; HUM-103 and HUM-104 are done and HUM-105 is unrelated.
- [ ] Implement the shared environment-file parser, containment resolver, bounded canonical read, layer composer, and diagnostics in `internal/project`; keep the existing protocol encoded-size check in the CLI/MCP adapters using `protocol.MarshalLine` and `DefaultMaxLineBytes`.
- [ ] Add CLI preflight for start/up/declared run/restart, including dependency closure, required-file validation before preserved-running classification, retained/recovery rules, one baseline snapshot, zero-contact failure behavior, and unchanged argv/name-conflict and runtime partial-success behavior; keep both schedulers in `internal/cli/commands.go`.
- [ ] Add the equivalent MCP preflight using the configured server baseline and selected `project_root`/manifest inputs, without global environment mutation or cross-request caching.
- [ ] Thread prepared environments through existing `Env` requests and preserve existing process, daemon, protocol, readiness, automatic-restart, and ad hoc behavior.
- [ ] Add the named contract and lifecycle tests, update schema/example/README/design/coding-agent documentation, run all five acceptance commands, and record evidence without claiming implementation before those tests pass.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Review findings, not HUM-108 completion evidence. Existing executor behavior already resolves a supplied `PATH`, absolute and `./` relative executables do not require it, and the current process/app/CLI/MCP behavior preserves exact environments and recovery launch state. Baseline check passed: `mise exec go -- go test ./internal/process ./internal/app ./internal/cli ./internal/mcp -run '^(TestStartResolvesExecutableFromSuppliedPath|TestStartDoesNotSearchDaemonPathForNilEnv|TestStartUsesExactDirectoryAndEnvironment|TestEmptyEnvironmentPassedToChild|TestRestartPreservesLaunchAndOutputState|TestUpPreservesCrashRecovery)$' -count=1` passed all four packages. The revised grammar corrects the earlier draft's treatment of inline comments as value text and its overclaim of interpolation compatibility: it accepts common assignment whitespace and full-line comments, while rejecting unsupported expansion. See [dotenvx env-file documentation](https://dotenvx.com/docs/env-file/) as review evidence. No dependencies are required; HUM-103 and HUM-104 are done and HUM-105 is unrelated.
<!-- SECTION:NOTES:END -->
