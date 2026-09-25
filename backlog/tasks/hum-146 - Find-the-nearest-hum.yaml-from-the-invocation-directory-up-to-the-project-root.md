---
id: HUM-146
title: Find the nearest hum.yaml from the invocation directory up to the project root
status: To Do
assignee: []
created_date: '2026-09-25 20:22'
labels:
  - cli
  - mcp
  - config
  - contract
dependencies: []
modified_files:
  - internal/project/resolver.go
  - internal/project/resolver_test.go
  - internal/project/manifest.go
  - internal/project/manifest_test.go
  - internal/cli/manifest.go
  - internal/cli/doctor.go
  - internal/cli/commands.go
  - internal/cli/mcp.go
  - internal/cli/discovery_test.go
  - internal/cli/doctor_test.go
  - internal/cli/json_errors_test.go
  - internal/cli/manifest_test.go
  - internal/cli/mcp_test.go
  - integration/manifest_test.go
  - docs/design.md
  - docs/cli-json-v1.md
  - docs/coding-agents.md
  - hum.schema.json
  - hum.example.yaml
  - examples/README.md
priority: medium
type: enhancement
ordinal: 30000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: without `--file`, runtime commands use the nearest `.hum.yaml` or `hum.yaml`. The search starts in the invocation directory (or the `--project DIR` / MCP `project_root` directory) and walks up to the project root. The Git root still sets the project scope and the process-name namespace. For example, `cd examples/interactive && hum up` starts `greeter` from `examples/interactive/hum.yaml` instead of failing with `manifest_missing`.

Why: today only `<root>/.hum.yaml` and `<root>/hum.yaml` are read, so a manifest next to you is ignored unless you pass `-F`. `just`, `task`, `npm`, and `git` all find the nearest config above you, and monorepos want `apps/web/hum.yaml` with `hum up` inside `apps/web`. The change is an implicit `-F`. Nested manifests already work through `-F`, including source identity, containment checks, and the shared namespace, so the design gains no new concepts. This came from running `hum up` in `examples/interactive/` (2026-09-26).

Decisions (settled; reviewer may overturn):
1. Child cwd base = the selected manifest's directory. For every manifest (default, nested, or `--file`), the manifest's directory is the default child cwd and the base for relative `cwd:`. Today it is the project root, while `environment.files` already resolve from the manifest directory. Without this change, `hum up` in `apps/web` would run `bun run dev` in the repo root. `cwd:` must still stay inside the canonical project root, so `cwd: ../api` from `apps/web/hum.yaml` is fine and an escape still fails. Root manifests do not change. The only break: a `--file` manifest below the root, with no `cwd:` or a relative one, now runs from its own directory.
2. Deterministic selection. The nearest directory that has either file wins. Within one directory the current rule holds: `.hum.yaml` > `hum.yaml`, with the shadowed warning. There is no merging, and a malformed, unsafe, or non-regular nearer file fails closed with no fallback to a parent. Human `hum up` (not `--json`) writes one stderr line naming the project-root-relative manifest path, but only when the selected manifest is not at the project root. Root-manifest output stays byte-for-byte unchanged. `source` stays `manifest:<root-relative path>`, as it is for `--file` today, and doctor's `manifest` detail shows the same relative path.
3. Bounded search. Stop at the project root, inclusive. Never search above it. With no Git root, the project root is the directory itself (see `DiscoverProjectRoot`), so only that directory is checked. A `~/hum.yaml` can never apply to an unrelated project.
4. Scope. Every default-manifest load uses the same lookup: `loadManifest` (so `up`, `start`, `restart`, `run` name checks, `input`, completion, and list/status definition joins), `doctor`, `--project DIR` (search from DIR), and MCP (search from the given `project_root` directory; `project_root` still resolves to the Git root for scope). Unchanged: `init` still writes at the project root, runtime-only commands stay project-wide, and the daemon's `manifestStopGrace` keeps re-resolving from the recorded `source`.

`manifest_missing`: add the search start directory to `ManifestMissingError`. When it differs from the root, the message reads `manifest is missing in DIR or its parents up to ROOT: run hum init ...`. When they are equal, the message is unchanged. The error code stays the same, and so does the `hum init` / `--project` guidance rewriting in `projectGuidanceError` (`internal/cli/root.go:708`).

Where the code is (lines as of f66fc3f):
- `internal/project/resolver.go:261` `DefaultManifestSelection(root)`, `:298` `ResolveDefinitionsContext(ctx, root)`, `:353` `ResolveDefinitionsReadOnly`: take the search start directory and the root, walk from start to root, and validate with `ResolveManifestPath`. `Relative` becomes the root-relative path (`examples/interactive/hum.yaml`). Keep `runtimeManifestDefinitions` producing `manifest:hum.yaml` for the root default.
- `internal/project/resolver.go:25` `ManifestMissingError`: add the start directory.
- `internal/project/manifest.go:354` (`cwd := root`) and `:701` `normalizeCwd`: use the manifest directory (`baseDir`, already passed to `parseDefinitions`) as the base. Keep containment against the root. Update the error wording ("relative to the manifest directory").
- `internal/cli/manifest.go:47` `loadManifest`: pass the lexical cwd as the search start. The shadowed-manifest check applies to the selected directory. All CLI and MCP (`internal/cli/mcp.go:66`) default loads go through here.
- `internal/cli/doctor.go:125-150` `loadDoctorManifest`: calls `DefaultManifestSelection(selection.root)` directly; search from `selection.cwd`.
- `internal/cli/commands.go` `up` action: the stderr notice from decision 2.

Documentation:
- `docs/design.md`: "Manifest selection" (the precedence block and the root/cwd bullets at about :512-528), "Selecting a project and manifest" (:136-140), the process-field table `cwd` row (:546), and :815.
- `docs/cli-json-v1.md` `manifest_missing` paragraph and example (:101-108).
- `docs/coding-agents.md` `project_root` sentence (:100).
- `hum.schema.json` `cwd` description (:54) and the `hum.example.yaml` `cwd` comment (:44).
- `examples/README.md`: an example can run with `cd examples/NAME && hum up`. Keep `-F` as the repo-root form.

Non-goals:
- Merging, overlays, or combining manifests from several levels.
- Searching above the project root, or a user/global manifest.
- `hum init` writing into a subdirectory.
- Changing project scope, the namespace, runtime-only commands, `--file` path resolution, or `environment.files` resolution.
- Resolving name conflicts between processes declared in different manifests of one project; existing `--file` behavior applies.
- A JSON field for the selected manifest beyond the existing `source`.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `go test ./internal/project -run "^(TestNearestManifestSelection|TestManifestCwdBase)$" -count=1 -v` exits 0. TestNearestManifestSelection (internal/project/resolver_test.go) covers: starting at the root picks the root manifest; starting in `apps/web/src`, with both `apps/web/hum.yaml` and a root `hum.yaml`, picks `apps/web/hum.yaml` with Relative `apps/web/hum.yaml`; `.hum.yaml` beats `hum.yaml` in the same nested directory; a malformed nearer `.hum.yaml` returns a configuration error even though a valid root manifest exists; a directory with no Git root whose parent has `hum.yaml` reports missing; ManifestMissingError prints the unchanged message when start equals root and `... in DIR or its parents up to ROOT: ...` otherwise. TestManifestCwdBase (internal/project/manifest_test.go) covers: a nested manifest default Cwd equals its directory; `cwd: ../api` resolves beside it; a `cwd:` that escapes the root fails; a root manifest Cwd is unchanged; an explicit `--file`-style nested selection uses its own directory.
- [ ] #2 AC2 — `go test ./internal/cli -run "^(TestManifestMissing|TestDoctorManifest|TestUpNestedManifestNotice|TestMCPResolverNearestManifest)" -count=1 -v` exits 0. New assertions: `hum up` from a nested directory with no manifest up to the root returns manifest_missing naming both the directory and the root (human and JSON); doctor reports `manifest` as the root-relative nested path; human `hum up` writes the nested manifest path to stderr, while `hum up --json` and root-manifest runs produce byte-for-byte unchanged output; the MCP resolver given a nested `project_root` returns the Git root as Root and the nested definitions.
- [ ] #3 AC3 — `go test ./integration -run "^TestNearestManifest$" -count=1 -v` exits 0. Against the built binary, in a temp Git repo with a root `hum.yaml` (process `top`) and `apps/web/hum.yaml` (process `web`, argv `/bin/sh -c pwd`): `hum up --detach` run from `apps/web/src` starts only `web`; `hum logs web` prints the canonical `apps/web` path; `hum status --json` shows `source` `manifest:apps/web/hum.yaml` and `root` equal to the repo root; `hum up --detach` from the repo root starts `top`; `hum up` in a non-Git temp directory whose parent holds a `hum.yaml` exits with manifest_missing.
- [ ] #4 AC4 — `go test ./internal/project ./internal/cli ./internal/mcp ./integration -count=1` exits 0. Any existing test that asserted a project-root cwd for a nested `--file` manifest is updated to the manifest-directory contract (not deleted or skipped) and named in Implementation Notes.
- [ ] #5 AC5 — `go test ./internal/cli -run "^(TestDocs|TestREADME)" -count=1` and `go test ./internal/project -run "^TestExampleManifestsLoad$" -count=1` exit 0, and `rg -n "nearest" docs/design.md docs/cli-json-v1.md docs/coding-agents.md` prints at least one line from each file.
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
