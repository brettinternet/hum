---
id: HUM-108
title: Load manifest-defined process environments
status: To Do
assignee: []
created_date: '2026-09-12 07:21'
updated_date: '2026-09-12 14:38'
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
Outcome: a manifest can declare the project environment its processes run in, so `hum up` from any shell, worktree, or long-running MCP server launches a process with the same deterministic environment and no per-process wrapper script. The composed snapshot is shared by the supervised process, `ready.exec`, and automatic `restart: on-failure` relaunches.

Motivating case: a project uses direnv (`.envrc` -> `dotenv`) so the shell that runs `hum` or hosts the MCP server carries one worktree's `.env` (`DEV_ENV_ID`, ports, hosts) into another worktree's processes. Task's `dotenv:` does not override inherited values, so today every `argv` and `ready.exec` is wrapped in a committed script that unsets a hand-maintained key list and re-execs through `dotenvx run --overload`. Hum owns the exact child environment already (`cmd.Env` is assigned verbatim, the daemon retains it, probes copy it); the missing piece is composing it from the manifest instead of `os.Environ()`.

Manifest API:
```yaml
version: 1
environment:
  inherit: true      # default true; false starts from an empty environment
  files: [.env, .env.local]
processes:
  api:
    argv: [bun, run, api]
    env:
      PORT: "3000"
      LEGACY_DATABASE_URL: null
```
- `environment` is optional. `inherit` is a boolean defaulting to `true`; `files` is an ordered sequence of non-empty relative paths defaulting to empty. Behaviour with the block absent and no `env` maps is byte-for-byte the current behaviour.
- `processes.<name>.env` is an optional mapping. A string value sets the variable (empty string allowed); `null` removes it. Any other value type, and duplicate keys, fail through the existing strict manifest diagnostics with `process "api".env.PORT`-style context.
- Names in files and inline maps must match `[A-Za-z_][A-Za-z0-9_]*`; NUL is rejected in names and values. Schema and parser enforce the same grammar.
- Precedence, lowest to highest: inherited launch environment (when `inherit` is true), `files` in order, then the process `env` map. Later files replace earlier values; `null` removes a key from any lower layer. The result is emitted as `[]string` in stable lexical key order.
- `inherit: false` synthesises nothing: no `PATH`, `HOME`, `PWD`, temp-dir, or `HUM_*` variables. A bare executable then needs `PATH` from a file or `env` value; absolute executables keep working. Document that projects which extend `PATH` in their shell (mise, direnv) normally want `inherit: true` plus `files`, which already overrides every stale key the file defines.

Environment-file contract:
- Paths resolve relative to the directory containing the selected manifest (including `--file` manifests), must be relative, and must resolve to regular readable files inside the canonical project root. Lexical escape, symlink escape, missing files, directories, and unreadable files are errors reported before any daemon contact.
- Files are UTF-8 with LF or CRLF endings. Permitted lines: empty; optional horizontal whitespace then a full-line `#` comment; `NAME=VALUE`. An unquoted value is the literal remainder of the physical line, including spaces and `#`. Single-quoted values are literal. Double-quoted values support only `\\`, `\"`, `\n`, `\r`, `\t`. Quotes must enclose the whole value. Empty quoted and unquoted values are valid.
- Rejected: BOM, NUL, invalid UTF-8, leading whitespace before an assignment, whitespace around the name or `=`, `export`, inline comments, unsupported escapes, multiline values, trailing content after a closing quote, and duplicate names within one file. Duplicates across files follow layer precedence.
- Never interpolate `${VAR}`, evaluate shell text or command substitution, process includes, or discover `.env` implicitly. The grammar is deliberately a strict subset of direnv/dotenvx so a file hum accepts means the same thing to those tools.
- Bounds, checked before daemon contact: at most 16 files; each at most 1 MiB; at most 4,096 assignments per file; at most 4 MiB total across the composed `KEY=VALUE` entries. Errors name the path, line number, and limit but never a value.

Command behaviour (no new flags):
- `start NAME...`, `up [NAME...]`, `run NAME` (declared, no argv), and `restart NAME...` on a manifest-declared name compose the environment from the selected manifest. `run NAME -- COMMAND` and retained ad hoc restarts keep passing the client's `os.Environ()` unchanged.
- Preflight is atomic per invocation: resolve all selected definitions, snapshot `os.Environ()` once, read each unique file once, compose every target, and only then open or start the daemon. Any environment error means zero daemon contact and zero process mutation. Runtime failures after a successful preflight keep the existing partial-success behaviour.
- `start`/`up` leave running, recovery-pending, and recovery-exhausted records unchanged with their retained environment. Environment differences do not participate in `definition_drift`; picking up a changed file requires explicit `hum restart NAME`, which rereads files through the existing manifest-update restart path.
- The daemon retains the exact snapshot; `ready.exec` uses it; automatic `restart: on-failure` attempts reuse it without rereading files.
- Generic manifest resolution stores only the non-secret specification (inherit flag, resolved safe paths, per-process maps) and never reads file contents, so `list`, `status`, `logs`, completion, and runtime-only commands never open environment files.
- MCP start/up/restart tools compose with the MCP server's own environment as the inherited baseline, the same shared composer, and the selected worktree's manifest directory for file resolution. There are no MCP-only override fields.
- `--project` composes the selected project's files, never files from the invoking checkout. Separate worktrees keep separate canonical scopes and read their own manifest and files.

Privacy: Hum-generated snapshots, JSON/NDJSON, status, drift fields, errors, and diagnostics never serialise configured environment names or values (the protocol already rejects an `env` key in responses; keep that). Child and `ready.exec` output stays untrusted user output under the existing bounded capture and is not redacted; document that commands must not print secrets.

Implementation map: extend the strict parser and `hum.schema.json` in `internal/project`; add one shared environment-file resolver/composer there (`environment.go`) used by both adapters; thread the non-secret spec through CLI `manifestState` and MCP `Resolution`; replace `manifestProcessEnv()` (`internal/cli/manifest.go:706`) and `Server.environment()` (`internal/mcp/tools.go:740`) call sites at `internal/cli/commands.go:684,2677,3123`, `internal/cli/mcp.go:49`, and `internal/mcp/tools.go:1779` with per-target composed snapshots produced in a pre-daemon preflight; send them through the existing `Env` request field. No daemon-side file reads and no new launch mechanism. Update `hum.example.yaml`, README, `docs/design.md` (which today declares environment values/files a non-goal at the manifest section and in Non-goals), and `docs/coding-agents.md`.

Non-goals: `--env-file` / `--inherit-env` or any CLI override flags (the manifest is the single source; add later only with a concrete need); secret redaction from child or probe output; shell/profile activation; dotenvx/direnv feature parity (encryption, interpolation, includes); implicit `.env` discovery; hot reload or watchers; daemon-side file loading; environment-based drift; per-process `files`; manifest overlays; changed project/process identity or discovery.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `go test ./internal/project -run '^TestManifestEnvironmentContract$|^TestEnvironmentFileContract$|^TestEnvironmentCompositionContract$' -count=1 -v` exits 0 with RUN and PASS lines for all three exact tests. Their table corpus covers: schema/parser name-grammar and NUL parity; defaults and byte-identical behaviour with the block absent; ordered precedence with set/unset across inherited, files, and process map; every quoted/unquoted, whitespace, comment, UTF-8, CRLF, duplicate, escape, and rejected-syntax case listed in the task; manifest-directory path resolution and root containment including symlink escape; and every file-count, size, assignment, and aggregate bound with errors that name path/line/limit and contain no value.
- [ ] #2 AC2 — `go test ./internal/cli -run '^TestManifestEnvironmentPreflightContract$' -count=1 -v` exits 0 with a RUN and PASS line. Using a fake client it proves: start, up, declared run, and manifest restart send the composed snapshot; ad hoc `run NAME -- CMD` and retained ad hoc restart send the unchanged client environment; one `os.Environ()` snapshot and one read per unique file per invocation; a bad file or map in any one target of a multi-target start/up/restart produces zero client/daemon creation and a path-naming error without values; list, status, logs, and completion never open environment files; and no configured name or value sentinel appears in any Hum-generated output or error.
- [ ] #3 AC3 — `go test ./internal/mcp -run '^TestMCPManifestEnvironmentContract$' -count=1 -v` exits 0 with a RUN and PASS line. It proves start/up/restart tools compose from the configured server baseline, `inherit: false` excludes a baseline sentinel, per-process set/unset applies, list/status/logs perform no file reads, malformed or oversized input produces no daemon request, and no configured name/value appears in tool results or errors.
- [ ] #4 AC4 — `go test ./integration -run '^TestManifestEnvironmentLifecycle$' -count=1 -v` exits 0 and prints `--- PASS: TestManifestEnvironmentLifecycle`. Against one real daemon it proves process and `ready.exec` environment parity, a stale caller variable overridden by a file value and removed by `inherit: false`, two worktree roots each resolving their own files, an already-running process unchanged by `up`, explicit `restart` reloading an edited file, an automatic `on-failure` relaunch reusing the retained snapshot without rereading, and zero launched processes after a failed multi-target preflight.
- [ ] #5 AC5 — `task cli:check && task test` exits 0 after schema, README.md, docs/design.md, docs/coding-agents.md, and hum.example.yaml document the API, grammar and bounds, precedence, per-command behaviour, path rules, worktree/MCP semantics, one-snapshot lifecycle with explicit restart reload, the Hum-generated privacy boundary, and the unredacted child/probe-output warning; docs/design.md no longer lists environment values/files as unsupported.
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
