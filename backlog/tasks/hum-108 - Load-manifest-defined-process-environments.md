---
id: HUM-108
title: Load manifest-defined process environments
status: To Do
assignee: []
created_date: '2026-09-12 07:21'
updated_date: '2026-09-12 07:36'
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
  - internal/cli/root.go
  - internal/cli/root_test.go
  - internal/cli/commands.go
  - internal/cli/manifest.go
  - internal/cli/manifest_test.go
  - internal/cli/environment_test.go
  - internal/cli/run_args_test.go
  - internal/cli/restart_test.go
  - internal/cli/flag_alias_lifecycle_parity_test.go
  - internal/cli/help_contract_test.go
  - internal/cli/surface_test.go
  - internal/cli/man.go
  - internal/cli/man_test.go
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
Outcome: manifest-backed Hum processes can load a deterministic project environment without command wrappers, and the same composed snapshot is used by the supervised process, `ready.exec`, and automatic relaunches. Projects can opt out of inherited variables so one checkout, worktree, or long-running MCP server cannot leak stale configuration into another.

Manifest API:
```yaml
version: 1
environment:
  inherit: false
  files:
    - .env
    - .env.local
processes:
  api:
    argv: [bun, run, api]
    env:
      PORT: "3000"
      LEGACY_DATABASE_URL: null
```
- `environment` is optional. `inherit` is a boolean defaulting to `true`; `files` is an ordered sequence of non-empty paths defaulting to empty. Preserve current behavior when the block is absent.
- `processes.<name>.env` is an optional mapping. String values set variables, including the empty string; `null` removes a variable. Reject other YAML value types and duplicate keys through the manifest's existing strict diagnostics.
- Environment names in files and inline maps must match `[A-Za-z_][A-Za-z0-9_]*`. Reject NUL in names and values. The schema and parser must enforce the same grammar.
- Effective precedence, lowest to highest, is inherited launch environment when enabled, selected environment files in order, then per-process `env`. Later files replace earlier values. `null` removes a key produced by any lower layer. Emit the final `[]string` in stable lexical key order.
- `inherit: false` means no implicit variables. Hum must not synthesize `PATH`, `HOME`, `PWD`, temporary-directory variables, or `HUM_*`; a bare executable therefore requires `PATH` in a file/process value, while an absolute executable continues to work.

Environment-file contract:
- Manifest-declared paths resolve relative to the directory containing the selected manifest, including alternate manifests selected with `--file`. They must be relative, resolve to regular files inside the canonical project root, and reject lexical escape, symlink escape, missing files, directories, and unreadable files before daemon contact.
- Add repeatable `--env-file PATH` and `--inherit-env=true|false` launch overrides. If any `--env-file` is present, its ordered list replaces, rather than supplements, manifest `environment.files`; later flags win. CLI paths resolve from the invocation directory and may be absolute because they are explicit operator input.
- Parse valid UTF-8 with LF or CRLF line endings. Allow empty lines; horizontal whitespace followed by a full-line `#` comment; and `NAME=VALUE`. An unquoted value is the literal remainder of its physical line, including spaces and `#`. Single-quoted values are literal. Double-quoted values support only `\\`, `\"`, `\n`, `\r`, and `\t` escapes. Quotes must enclose the complete value. Empty quoted/unquoted values are valid. Reject BOM, NUL, invalid UTF-8, leading whitespace before an assignment, spaces around the name or `=`, `export`, inline comments, unsupported escapes, multiline values, malformed/trailing quote content, and duplicate names within one file. Duplicate names across files are valid and follow layer precedence.
- Never interpolate variables, evaluate shell text or command substitution, process includes, or discover `.env` implicitly.
- Bound work before daemon contact: at most 16 selected files; each file at most 1 MiB; at most 4,096 assignments per file; and at most 4 MiB total UTF-8 bytes across the effective `KEY=VALUE` entries after composition. Errors name the source/path and limit but never include values.

CLI command/state matrix:
- `start NAME...` and `up [NAME...]`: overrides apply only to manifest-declared targets. Before opening or starting the daemon, resolve all selected target definitions, snapshot `os.Environ()` once, read every selected file once, validate every process override, and build all per-target environments. Any environment error causes zero daemon contact and zero process mutation. Running, recovery-pending, and recovery-exhausted records keep existing start/up classification and their retained environment.
- `restart NAME...`: without environment flags, preserve current declared/retained fallback. With either environment flag, require every requested name to exist in the selected manifest; otherwise reject the whole request before daemon contact. Preflight all target environments before restarting the first process. Declared stopped, active, or recovery records receive the new snapshot through the existing manifest-update restart path.
- `run NAME` without argv: overrides require NAME to be declared in the selected manifest; reject an undeclared retained fallback before daemon contact. A declared target receives the composed environment. `run NAME -- COMMAND` accepts the overrides for an ad-hoc launch, including project or global scope; with no manifest environment, the inherited baseline defaults to enabled. An ad-hoc command has no per-process manifest map.
- Reject these flags on every other command and whenever restart/run would preserve an existing launch specification instead of supplying a new one. Preserve existing root/subcommand placement conventions and `run` parsing before child `--`.
- Environment-specific validation is atomic across a multi-target invocation as described above. Runtime failures after successful preflight retain existing command partial-success behavior.

Lifecycle, adapters, and privacy:
- Generic manifest resolution stores only non-secret environment specifications and resolved safe paths; it must not read environment-file contents. `list`, `status`, `logs`, completion, and runtime-only commands therefore never open environment files.
- CLI launch preflight and the corresponding MCP start/up/restart handler resolve and compose the effective environments before client/daemon creation. One invocation/request snapshots the inherited baseline once and reads each unique file once; per-target maps are then applied to independent copies.
- Send completed `[]string` values through the existing start/restart request path. The daemon retains the exact snapshot, `ready.exec` uses the supervised process environment, and automatic `restart: on-failure` attempts reuse it without rereading files. Explicit manifest restart rereads files.
- `start`/`up` leave already-running processes unchanged. Environment contents and inherited-shell differences do not participate in `definition_drift`; applying changed files requires explicit `hum restart NAME`.
- MCP selected-manifest resolution uses the MCP server environment as the inherited baseline and has no CLI-only override fields. Apply manifest environment semantics only in definition-launching/updating tools. `inherit: false` prevents a long-running server's startup checkout from contaminating another worktree.
- Hum-generated process snapshots, JSON/NDJSON, status, errors, drift fields, and diagnostics must never serialize configured environment names or values. Child and `ready.exec` stdout/stderr remain untrusted user output governed by existing bounded capture and are not redacted; document that commands must not print secrets.
- Separate worktrees continue to use separate canonical project scopes and resolve their own selected manifest/environment files. A `--project` launch composes the selected project's files, not files from the invoking checkout.

Implementation map: extend the strict parser/schema in `internal/project`; add one shared environment-file resolver/composer there for CLI and MCP adapters; thread non-secret specs through CLI `manifestState` and MCP `Resolution`; add a pre-daemon invocation/request preflight that produces per-target snapshots; reuse the daemon/app/process environment transport instead of adding daemon-side file reads or another launch mechanism. Update command flags, raw `run` parsing, help/man text, examples, design documentation, and exact contract tests.

Current constraints: CLI manifest launches currently pass `os.Environ()` (`internal/cli/commands.go`, `internal/cli/manifest.go`); MCP uses its server environment (`internal/mcp/tools.go`); the daemon retains request environments and `ready.exec` copies the record environment (`internal/app/app.go`); `process.Start` assigns `cmd.Env` exactly (`internal/process/process.go`); protocol responses reject any JSON key named `env` (`internal/protocol/codec.go`). `docs/design.md` currently declares environment files a non-goal and must be intentionally revised.

Non-goals: secret redaction from arbitrary child/probe output; shell/profile activation; dotenvx compatibility; implicit `.env` discovery; interpolation or executable dotenv syntax; hot reload/watchers; liveness probes; daemon-side file loading; environment-based drift; per-process environment files; manifest overlays/includes; changed project/process runtime identity; changed conventional discovery except explicit-argv `run` with CLI environment flags.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `go test ./internal/project -run '^TestManifestEnvironmentContract$|^TestEnvironmentFileContract$|^TestEnvironmentCompositionContract$' -count=1 -v` exits 0 and output contains a RUN and PASS line for all three exact tests. Their table corpus proves schema/parser name and NUL parity; defaults; stable ordered precedence and set/unset; every quoted/unquoted, whitespace, comment, UTF-8, CRLF, duplicate, escape, and unsupported dotenv case in the task; manifest-directory path containment; and all file/count/aggregate bounds without value-bearing errors.
- [ ] #2 AC2 — `go test ./internal/cli -run '^TestEnvironmentFlagContract$|^TestManifestEnvironmentPreflightContract$|^TestRunEnvironmentContract$|^TestRestartEnvironmentContract$' -count=1 -v` exits 0 and output contains a RUN and PASS line for all four exact tests. They prove flag placement/replacement/order/inheritance, the complete command/state matrix, one inherited snapshot and one read per unique file, multi-target atomic preflight before client/daemon creation, retained/recovery behavior, explicit-argv and declared run, unsupported-command rejection, and unchanged no-flag behavior.
- [ ] #3 AC3 — `go test ./internal/cli ./internal/mcp -run '^TestMCPManifestEnvironmentContract$|^TestEnvironmentReadIsolationContract$' -count=1 -v` exits 0 and output contains a RUN and PASS line for both exact tests. They prove launch-time MCP composition from the configured server baseline, isolation with inheritance disabled, per-process set/unset, no CLI override fields, no environment-file reads by list/status/logs/completion/runtime-only paths, no daemon/client creation on malformed or oversized input, and no configured name/value sentinel in Hum-generated responses or errors.
- [ ] #4 AC4 — `go test ./integration -run '^TestManifestEnvironmentLifecycle$' -count=1 -v` exits 0 and output contains `--- PASS: TestManifestEnvironmentLifecycle`. One real daemon proves process/ready.exec environment parity, isolation from stale caller variables, selected-file behavior across two separate worktree roots, unchanged already-running environments, explicit-restart reload, retained-snapshot automatic relaunch, and zero launched processes after a bad multi-target preflight.
- [ ] #5 AC5 — `task cli:check && task test` exits 0 after schema and generated help/manual assertions plus README.md, docs/design.md, docs/coding-agents.md, and hum.example.yaml document the API, complete grammar and bounds, precedence, command/state matrix, path/CLI rules, worktree/MCP semantics, one-snapshot lifecycle, explicit restart, Hum-generated privacy boundary, and unredacted child/probe-output warning; docs/design.md no longer states that environment files are categorically unsupported.
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
