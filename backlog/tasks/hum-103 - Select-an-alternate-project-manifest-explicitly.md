---
id: HUM-103
title: Select an alternate project manifest explicitly
status: To Do
assignee: []
created_date: '2026-09-12 01:16'
updated_date: '2026-09-12 01:16'
labels: []
dependencies:
  - HUM-101
modified_files:
  - internal/cli/root.go
  - internal/cli/root_test.go
  - internal/cli/manifest.go
  - internal/cli/manifest_test.go
  - internal/cli/commands.go
  - internal/cli/completion.go
  - internal/cli/completion_test.go
  - internal/cli/mcp.go
  - internal/cli/mcp_test.go
  - internal/cli/man.go
  - internal/cli/man_test.go
  - internal/cli/help_contract_test.go
  - internal/cli/surface_test.go
  - internal/project/manifest.go
  - internal/project/manifest_test.go
  - internal/project/resolver.go
  - internal/project/resolver_test.go
  - internal/mcp/tools.go
  - internal/mcp/tools_test.go
  - integration/manifest_test.go
  - integration/logs_test.go
  - integration/mcp_test.go
  - README.md
  - docs/design.md
  - docs/coding-agents.md
priority: medium
type: enhancement
ordinal: 75800
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: users and coding agents can explicitly select one complete alternate Hum manifest, for example `hum -f dev.hum.yaml up`, without adding overlays, environment loading, or a second supervision namespace. `hum.yaml` remains the zero-option default.

Why: some repositories need several complete process topologies (development, test, preview, or service subsets). Copying a complete manifest is understandable and cheap compared with defining merge semantics. Docker Compose establishes the CLI precedent that a global `-f` selects a file while `logs -f` locally means follow.

CLI contract:
- Add the global `--file PATH` / `-f PATH` option. Because `logs` already owns local `--follow/-f`, manifest `-f` is accepted only before the subcommand. The canonical combined form is `hum -f dev.hum.yaml logs -f`; `hum logs -f` must remain follow with no behavior change. Help and usage errors must make this positional rule clear.
- With no `--file`, retain the existing authoritative `hum.yaml` behavior and conventional discovery only when `hum.yaml` is absent. With `--file`, load exactly that file: no fallback to `hum.yaml`, conventional discovery, wildcard discovery, inheritance, or merging.
- Resolve a relative CLI file from the invocation directory. Discover the canonical Git project from the file location when `--project/-C` is absent. When both selectors are present, the selected file must resolve inside the selected project. Reject missing, non-regular, directory, and outside-project files before daemon contact. The selected project—not the manifest directory—remains the daemon namespace, default child cwd, and base for manifest `cwd` values.
- Thread the selection through every CLI path that resolves declarations: argv-free `run`, `start`, `up`, `restart`, `list`, `status`, aggregate `logs`, and shell completion. `init` continues to create only `hum.yaml` and rejects `--file` as inapplicable.
- Alternate definitions use a stable response-safe source such as `manifest:dev.hum.yaml`; paths must not expose an absolute checkout path. Default-manifest source output remains backward compatible. Manifest source classification, definition drift, removed-definition warnings, guidance, JSON, and completion must all recognize alternate manifests.

Runtime semantics:
- All manifests in one Git project share the existing project process namespace. This feature does not permit two same-named definitions to run concurrently. Selecting another manifest compares its definition with the retained project record and uses existing conflict/drift/restart behavior.
- `status` and `list` merge all retained records in the selected project with stopped declarations from the selected/default manifest. A retained record wins by name even if it originated from another manifest. Named observation/control continues to address retained records by project and name.
- `down`, `stop`, `remove`, `signal`, `wait`, and `input` remain project/runtime scoped and are never restricted to definitions in the selected file. Aggregate `logs` honors the selected manifest only when resolving declaration names; `logs -f` remains follow. `list --all` remains a runtime view across project/global scopes and does not create per-manifest scopes.

MCP parity:
- Add an optional `manifest` path only to MCP tools that resolve or merge definitions (`start`, `up`, `restart`, `list`, `status`, and `logs`). MCP relative paths resolve from required `project_root`; absolute paths must remain within it. Omission retains `hum.yaml`/discovery behavior. Explicit selection loads only that file. Runtime-only tools and global scope do not accept it.
- Preserve project-root namespace, environment inheritance, bounded responses, structured error behavior, and the private daemon protocol; no daemon protocol field or operation should be needed. Update coding-agent guidance and MCP tool descriptions.

SchemaStore: after dependent HUM-101 lands the initial registration, update its file match from only `hum.yaml` to include `*.hum.yaml`, and verify the public catalog. The in-file schema directive remains supported.

Implementation map: selection starts in `internal/cli/root.go` and must reach `internal/cli/manifest.go`; explicit path-aware loading belongs in `internal/project/manifest.go` and `internal/project/resolver.go`. Audit definition-resolution call sites in `internal/cli/commands.go`, completion in `internal/cli/completion.go`, MCP adaptation in `internal/cli/mcp.go`, and tool inputs in `internal/mcp/tools.go`. Existing runtime identity remains `(project root, process name)`.

Non-goals: manifest overlays/includes/inheritance; automatic discovery of `dev.hum.yaml` or other wildcard files; environment literals, env files, secret management, interpolation, shell activation, or environment names/profiles; per-manifest daemon namespaces; concurrent same-name variants; manifests outside the selected Git project; changing child cwd to the manifest directory; file-scoped down/stop/remove; changing zero-config discovery; changing `hum init` output; changing `logs --follow/-f`; remote transport or authentication.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `go test ./internal/project -run 'Test.*(ExplicitManifest|AlternateManifest|ManifestSelection)' -count=1 -v` exits 0 and prints PASS for default `hum.yaml`, exact alternate-file loading without discovery fallback, stable alternate source identity, project-root-relative cwd, and rejection of missing, non-regular, symlink-escaped, and outside-project paths.
- [ ] #2 AC2 — `go test ./internal/cli -run 'Test.*(ManifestFile|AlternateManifest|FileFlag)' -count=1 -v` exits 0 and prints PASS for `hum -f dev.hum.yaml ...` across run/start/up/restart/list/status/aggregate logs/completion, for `hum -f dev.hum.yaml logs -f` retaining follow semantics, for the no-flag default, for `-C` composition and inferred project roots, and for rejection after the subcommand and on `init`.
- [ ] #3 AC3 — `go test ./integration -run TestAlternateManifestSelection -count=1 -v` exits 0 and prints PASS after proving one real daemon keeps a project-wide namespace across manifests, selected definitions launch with project-root cwd, status/list merge retained records with selected declarations, drift and removed-definition guidance remain correct, aggregate logs select the requested declarations, and down remains project-wide.
- [ ] #4 AC4 — `go test ./internal/mcp -run 'Test.*ManifestSelection' -count=1 -v` exits 0 and prints PASS for omitted/default and explicit `manifest` inputs on definition-resolving project tools, project-root-relative resolution, exact-file/no-discovery behavior, outside-root rejection before daemon contact, rejection on global/runtime-only tools, and unchanged project namespace and bounded response contracts.
- [ ] #5 AC5 — `task cli:check && task test` exits 0 after CLI help, generated manual assertions, README.md, docs/design.md, and docs/coding-agents.md document the positional `--file/-f` contract, default and explicit resolution, shared namespace, cwd rules, MCP input, and non-goals.
- [ ] #6 AC6 — `python3 -c 'import json,urllib.request; c=json.load(urllib.request.urlopen("https://www.schemastore.org/api/json/catalog.json")); e=next(x for x in c["schemas"] if x.get("name")=="hum"); assert "hum.yaml" in e["fileMatch"] and "*.hum.yaml" in e["fileMatch"]'` exits 0 after the SchemaStore change is merged and publicly available.
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
