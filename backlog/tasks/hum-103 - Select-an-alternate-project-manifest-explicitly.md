---
id: HUM-103
title: Select an alternate project manifest explicitly
status: To Do
assignee: []
created_date: '2026-09-12 01:16'
updated_date: '2026-09-12 01:34'
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
Outcome: users and coding agents can explicitly select one complete alternate Hum manifest, for example `hum -F hum.dev.yaml up` or `hum up -F hum.dev.yaml`, without adding overlays, environment loading, or a second supervision namespace. `hum.yaml` remains the zero-option default.

Why: some repositories need several complete process topologies (development, test, preview, or service subsets). Copying a complete manifest is understandable and cheap compared with defining merge semantics.

Flag decision: the selector is `--file PATH` / `-F PATH`, not `-f`. `logs --follow/-f` already owns `-f`, and `validateCLIFlags` in `internal/cli/root.go` rejects a persistent root flag whose name a subcommand reuses, so `-f` could only be a root-local flag accepted before the subcommand. That would make manifest selection the only selector whose placement differs from `--project/-C`, and `hum logs -f hum.dev.yaml` would silently parse as follow plus a process name. `-F FILE` follows the ssh and git commit precedent for "read this file"; the long name `--file` matches Compose.

Filename convention: document `hum.<variant>.yaml` (for example `hum.dev.yaml`, `hum.test.yaml`) so variants sort next to `hum.yaml`; the flag accepts any path and no name pattern is enforced.

CLI contract:
- Add `--file PATH` / `-F PATH` as a persistent root flag with exactly the placement rules of `--project/-C`: before or after the subcommand, after NAME and before `--` in `run`, and before positional names in `signal`. Help lists it beside `--project`.
- With no `--file`, retain the existing authoritative `hum.yaml` behavior and conventional discovery only when `hum.yaml` is absent. With `--file`, load exactly that file: no fallback to `hum.yaml`, conventional discovery, wildcard discovery, inheritance, or merging.
- Resolve a relative CLI file from the invocation directory. Discover the canonical Git project from the file location when `--project/-C` is absent. When both selectors are present, the selected file must resolve inside the selected project. Reject missing, non-regular, directory, symlink-escaped, and outside-project files before daemon contact. The selected project, not the manifest directory, remains the daemon namespace, default child cwd, and base for manifest `cwd` values.
- Thread the selection through every CLI path that resolves declarations: argv-free `run`, `start`, `up`, `restart`, `list`, `status`, aggregate `logs`, and shell completion. `init` continues to create only `hum.yaml` and rejects `--file` as inapplicable.
- The empty-manifest message from `up` and every manifest configuration error name the selected file by its project-root-relative path instead of the literal `hum.yaml`.
- Alternate definitions use the stable response-safe source `manifest:<project-root-relative path>`, for example `manifest:hum.dev.yaml`; output must never expose an absolute checkout path. The default-manifest source stays `manifest`. Manifest source classification, definition drift, removed-definition warnings, guidance, JSON, and completion must all recognize alternate sources.

Runtime semantics:
- All manifests in one Git project share the existing project process namespace. This feature does not permit two same-named definitions to run concurrently. Selecting another manifest compares its definition with the retained project record and uses existing conflict/drift/restart behavior.
- `status` and `list` merge all retained records in the selected project with stopped declarations from the selected or default manifest. A retained record wins by name even if it originated from another manifest. Named observation and control continue to address retained records by project and name.
- `down`, `stop`, `remove`, `signal`, `wait`, and `input` remain project/runtime scoped and are never restricted to definitions in the selected file. Aggregate `logs` honors the selected manifest only when resolving declaration names; `logs -f` remains follow. `list --all` remains a runtime view across project/global scopes and does not create per-manifest scopes.
- Daemon stop grace: `manifestStopGrace` in `internal/daemon/server.go` re-reads `hum.yaml` on start and restart when a manifest-sourced request omits `stop_grace`. It must derive the file from the `manifest:<path>` source suffix so a `hum.dev.yaml` process never inherits a same-named `hum.yaml` process's `stop_grace`. The source string already carries the path, so no daemon protocol field or operation is added.

Out of this task: the MCP `manifest` input is HUM-104 and the SchemaStore `hum.*.yaml` file match is HUM-105; both depend on this task. MCP tools keep `hum.yaml`/discovery behavior until HUM-104 lands.

Implementation map: selection starts in `internal/cli/root.go` beside `projectFlag` and must reach `loadManifest` in `internal/cli/manifest.go`; explicit path-aware loading belongs in `internal/project/manifest.go` and `internal/project/resolver.go`. Audit the seventeen `loadManifest`/`loadManifestOrEmpty` call sites in `internal/cli/commands.go`, `internal/cli/input.go`, and `internal/cli/completion.go`, the hand-parsed post-NAME options in `applyRunOptions` and the `signal` argument scanner, and the daemon stop-grace read. Existing runtime identity remains `(project root, process name)`.

Non-goals: manifest overlays/includes/inheritance; automatic discovery of `hum.dev.yaml` or other wildcard files; environment literals, env files, secret management, interpolation, shell activation, or environment names/profiles; per-manifest daemon namespaces; concurrent same-name variants; manifests outside the selected Git project; changing child cwd to the manifest directory; file-scoped down/stop/remove; changing zero-config discovery; changing `hum init` output; changing `logs --follow/-f`; MCP inputs; SchemaStore changes; remote transport or authentication.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `go test ./internal/project -run 'Test.*(ExplicitManifest|AlternateManifest|ManifestSelection)' -count=1 -v` exits 0 and prints PASS for default `hum.yaml`, exact alternate-file loading without discovery fallback, stable `manifest:<relative path>` source identity, project-root-relative cwd, and rejection of missing, non-regular, symlink-escaped, and outside-project paths.
- [ ] #2 AC2 — `go test ./internal/cli -run 'Test.*(ManifestFile|AlternateManifest|FileFlag)' -count=1 -v` exits 0 and prints PASS for `-F hum.dev.yaml` before and after the subcommand across run/start/up/restart/list/status/aggregate logs/completion, for `hum run NAME -F hum.dev.yaml -- CMD`, for `hum logs -F hum.dev.yaml -f` following and `hum logs -f hum.dev.yaml` still parsing as follow plus a name, for the no-flag default, for `-C` composition and inferred project roots, for the empty-manifest message and configuration errors naming the selected file, and for rejection on `init`.
- [ ] #3 AC3 — `go test ./internal/daemon -run 'Test.*AlternateManifestStopGrace' -count=1 -v` exits 0 and prints PASS proving start and restart requests with source `manifest:hum.dev.yaml` and omitted stop grace read `hum.dev.yaml` and never the same-named `hum.yaml` definition.
- [ ] #4 AC4 — `go test ./integration -run TestAlternateManifestSelection -count=1 -v` exits 0 and prints PASS after proving one real daemon keeps a project-wide namespace across manifests, selected definitions launch with project-root cwd, status/list merge retained records with selected declarations, drift and removed-definition guidance remain correct, aggregate logs select the requested declarations, and down remains project-wide.
- [ ] #5 AC5 — `task cli:check && task test` exits 0 after CLI help, generated manual assertions, README.md, docs/design.md, and docs/coding-agents.md document the `--file/-F` selector and its `-C`-equivalent placement, the `hum.<variant>.yaml` convention, default and explicit resolution, shared namespace, cwd rules, and non-goals, and after the `Alternate filenames are ignored.` sentence in docs/design.md is replaced.
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
