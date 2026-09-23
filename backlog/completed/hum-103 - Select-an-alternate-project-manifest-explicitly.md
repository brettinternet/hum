---
id: HUM-103
title: Select an alternate project manifest explicitly
status: Done
assignee: []
created_date: '2026-09-12 01:16'
updated_date: '2026-09-12 03:01'
labels: []
dependencies: []
modified_files:
  - internal/cli/root.go
  - internal/cli/root_test.go
  - internal/cli/manifest.go
  - internal/cli/manifest_test.go
  - internal/cli/commands.go
  - internal/cli/commands_test.go
  - internal/cli/input.go
  - internal/cli/completion.go
  - internal/cli/completion_test.go
  - internal/cli/man.go
  - internal/cli/man_test.go
  - internal/cli/help_contract_test.go
  - internal/cli/surface_test.go
  - internal/project/manifest.go
  - internal/project/manifest_test.go
  - internal/project/resolver.go
  - internal/project/resolver_test.go
  - internal/daemon/server.go
  - internal/daemon/server_test.go
  - integration/manifest_test.go
  - integration/logs_test.go
  - README.md
  - docs/design.md
  - docs/coding-agents.md
priority: medium
type: enhancement
ordinal: 75800
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: users and coding agents can explicitly select one complete alternate Hum manifest, for example `hum -F hum.dev.yaml up` or `hum up -F hum.dev.yaml`, without overlays, environment loading, or a second supervision namespace. `hum.yaml` remains the zero-option default.

Why: some repositories need several complete process topologies (development, test, preview, or service subsets). Copying a complete manifest is understandable and cheap compared with defining merge semantics.

Flag and filename decisions:
- The selector is `--file PATH` / `-F PATH`, not `-f`: `logs --follow/-f` already owns `-f`, and `validateCLIFlags` rejects persistent/subcommand name reuse. `-F FILE` follows the ssh/git precedent; `--file` matches Compose.
- Document `hum.<variant>.yaml` (for example `hum.dev.yaml` and `hum.test.yaml`) so variants sort next to `hum.yaml`. The flag accepts any filename and enforces no naming pattern.

Selector and project contract:
- Add `--file/-F` as a persistent root flag with the placement rules of `--project/-C`: before or after a subcommand, after NAME and before `--` in `run`, and before positional names in the hand-parsed `signal` form. Help lists it beside `--project`.
- Resolve a relative path from the invocation directory. Validate that the selected path exists, resolves to a regular file, and does not escape the selected project through `..` or symlinks before daemon contact. An absolute path is allowed only inside the selected project.
- Without `--project/-C`, discover the canonical Git project from the selected file location. With both selectors, require the file to resolve inside that project. The project root remains the daemon namespace, default child cwd, and base for manifest `cwd` values; the manifest directory never becomes an implicit cwd.
- Reject `--file` with `--global` and on commands with no project selection (`version`, `serve`, `init`, `skill`, and `shutdown`). `init` continues to create only `hum.yaml`.

Definition resolution:
- With no `--file`, preserve authoritative `hum.yaml` loading and conventional discovery only when `hum.yaml` is absent. With `--file`, load exactly that file: no fallback, wildcard discovery, inheritance, or merging.
- Use selected definitions for argv-free `run`, `start`, `up`, `restart`, `list`, `status`, `logs` (aggregate name selection and named declaration fallback), and shell completion. Explicit-argv `run` and runtime-only `down`, `attach`, `stop`, `remove`, `signal`, `wait`, and `input` use the selected file only to identify the project; they validate the path but do not parse it or restrict the runtime record set.
- Empty-manifest and configuration errors name the selected file by normalized project-root-relative path rather than the literal `hum.yaml`. Alternate definitions use response-safe source `manifest:<project-root-relative path>`; the default source remains `manifest`, and no output exposes an absolute checkout path. Manifest classification, definition drift, removed-definition warnings, guidance, JSON, and completion recognize both source forms.

Runtime semantics:
- Every manifest in one Git project shares the existing process namespace; same-named definitions cannot run concurrently. Switching manifests uses the existing retained-record conflict, drift, and explicit-restart behavior.
- `start` and `restart` preserve retained-record fallback when a requested name is absent from the selected declarations. `status` and `list` merge retained records with stopped declarations from the selected/default manifest, and a retained record wins by name. Aggregate `logs` derives names from selected declarations; named `logs` reads the retained record while using a selected declaration only for existing not-launched/guidance behavior.
- Runtime-only commands remain project scoped. With `--file` they target the inferred/selected project but never only the definitions in that file. `list --all` remains one cross-project/global runtime view plus stopped declarations from the selected project; it creates no manifest scopes.
- Daemon stop grace: `manifestStopGrace` must re-read the path encoded by `manifest:<relative path>` on start/restart when a manifest request omits `stop_grace`, so an alternate definition never inherits a same-named `hum.yaml` grace. No daemon protocol field or operation is added.

Implementation map: add selection beside `projectFlag` in `internal/cli/root.go`; centralize explicit path validation/loading in `internal/project/manifest.go` and `internal/project/resolver.go`; carry project selection and optional definition loading through `internal/cli/manifest.go`. Audit every `loadManifest`/`loadManifestOrEmpty` call in commands, input, and completion, plus raw/post-NAME flag scanners and daemon stop-grace loading. Keep runtime identity `(project root, process name)`.

Non-goals: overlays/includes/inheritance; automatic variant discovery; environment literals/files, secrets, interpolation, activation, or profiles; per-manifest daemon namespaces; concurrent same-name variants; manifests outside the project; manifest-directory-relative cwd; file-scoped runtime operations; changed zero-config discovery or init output; changed `logs --follow/-f`; MCP inputs (HUM-104); SchemaStore changes (HUM-105); remote transport or authentication.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 AC1 — `go test ./internal/project -run "Test.*(ExplicitManifest|AlternateManifest|ManifestSelection)" -count=1 -v` exits 0 and prints PASS for default loading, exact alternate loading without fallback/discovery, normalized stable source identity, project-root-relative cwd, relative/absolute-inside-root paths, and rejection of missing, non-regular, symlink-escaped, and outside-project paths.
- [x] #2 AC2 — `go test ./internal/cli -run "Test.*(ManifestFile|AlternateManifest|FileFlag)" -count=1 -v` exits 0 and prints PASS for `-F` before/after definition-resolving commands and in raw run/signal scanners; no-flag behavior; `-C` composition and project inference; selected error/empty-manifest text; completion; retained-record fallback; project-wide runtime-only behavior without manifest parsing; and rejection with `--global` or version/serve/init/skill/shutdown before daemon contact.
- [x] #3 AC3 — `go test ./internal/daemon -run "Test.*AlternateManifestStopGrace" -count=1 -v` exits 0 and prints PASS proving start and restart requests sourced from `manifest:hum.dev.yaml` and omitting stop grace read `hum.dev.yaml`, never the same-named default definition, while default `manifest` behavior is unchanged.
- [x] #4 AC4 — `go test ./integration -run TestAlternateManifestSelection -count=1 -v` exits 0 and prints PASS after one real daemon proves project-wide identity across manifests, project-root cwd, retained-record precedence, selected stopped declarations, drift/removed-definition guidance, aggregate-log selection, and file-selected down/runtime control across the whole project.
- [x] #5 AC5 — `task cli:check && task test` exits 0 after generated help/manual assertions, README.md, docs/design.md, and docs/coding-agents.md document selector placement, the `hum.<variant>.yaml` convention, default versus exact resolution, project inference, shared namespace, cwd/runtime-operation rules, and non-goals, and after docs/design.md no longer says alternate filenames are ignored.
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
AC#1 — PASS: go test ./internal/project -run "Test.*(ExplicitManifest|AlternateManifest|ManifestSelection)" -count=1 -v.
AC#2 — PASS: go test ./internal/cli -run "Test.*(ManifestFile|AlternateManifest|FileFlag)" -count=1 -v.
AC#3 — PASS: go test ./internal/daemon -run "Test.*AlternateManifestStopGrace" -count=1 -v.
AC#4 — PASS: go test ./integration -run TestAlternateManifestSelection -count=1 -v.
AC#5 — PASS: task cli:check && task test.
Final gate — PASS: task ci on commit 6be7cb8.
Independent verifier — PASS for AC1-AC5 after README correction.
Review — three concrete findings fixed: selected-file guidance preservation, alternate filename in no-wait dependency errors, and no-flag discovery diagnostic compatibility.
Modified-file deviation — internal/cli/flag_alias_test.go and internal/cli/project_dir_test.go contain narrow required expectation updates for the new -F alias and selector wording; independently verified as justified collateral. No tests were deleted, skipped, or weakened; no protected gate files changed.

Integration — merged to main as f8d23f9; post-merge compatibility fix 3051103 updates pre-existing compact-list integration expectations. Focused affected integration tests and full task test pass on main.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented explicit --file/-F alternate-manifest selection with project-safe path validation, exact definition loading, shared runtime scope semantics, alternate source identities, daemon stop-grace reloads, completion/help/docs updates, and cross-layer tests. Commit: 6be7cb8.

Merged to main as f8d23f9; post-merge compatibility commit 3051103.
<!-- SECTION:FINAL_SUMMARY:END -->
