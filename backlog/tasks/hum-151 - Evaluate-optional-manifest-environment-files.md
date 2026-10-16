---
id: HUM-151
title: Allow optional environment files in hum.yaml
status: To Do
assignee: []
created_date: '2026-09-25 20:18'
updated_date: '2026-10-16 17:12'
labels:
  - config
  - contract
  - product-boundary
dependencies: []
modified_files:
  - internal/project/environment.go
  - internal/project/manifest.go
  - internal/project/environment_test.go
  - internal/project/manifest_test.go
  - internal/project/manifest_schema_test.go
  - internal/cli/doctor.go
  - internal/cli/doctor_test.go
  - internal/cli/environment_test.go
  - integration/manifest_test.go
  - hum.schema.json
  - hum.example.yaml
  - README.md
  - docs/design.md
  - examples/worktrees/hum.yaml
  - examples/worktrees/.env
  - examples/worktrees/README.md
priority: medium
type: feature
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: a manifest can mark an `environment.files` entry optional, so a committed `.env` supplies defaults and an uncommitted `.env.local` overrides it only where it exists. Plain strings stay required.

```yaml
environment:
  files:
    - .env
    - {path: .env.local, required: false}
```

`hum up` in a checkout without `.env.local` starts with the `.env` values; a checkout that has it gets its overrides.

Why: every listed file is required today, so a missing file fails `up`, `start`, declared `run`, and `restart` before the daemon is contacted. The per-worktree port pattern (examples/worktrees) therefore needs a Git-ignored `.env.local` in every checkout, including the main one, before `hum up` works. A committed default plus an optional local override is the common dotenv convention and matches Docker Compose `env_file` with `required: false`.

Owner decision (2026-10-16): this reverses HUM-108's "optional file APIs" non-goal. It is recorded here and in docs/design.md; decision-001 does not cover environment files and is unchanged. Accepted risk: a worktree that forgets `.env.local` falls back to the committed defaults. A server port collision then fails loudly at bind, and `hum status NAME` shows the listener when a framework falls back to another port. The remaining silent risk, a worktree reaching another checkout's service through a default URL, is documented, and the plain-string form remains for anyone who wants to fail before launch.

Decisions (settled; reviewer may overturn):
1. Shape: each `environment.files` item is a string (required, unchanged) or a closed mapping `{path: STRING, required: BOOL}`. `path` is required, `required` defaults to true, and unknown keys fail. One ordered list keeps "later wins" precedence across both forms; a separate `optional_files` key was rejected because its order relative to `files` would be ambiguous. The 16-entry cap and parse-time path rules (non-empty, relative, no NUL, lexically inside the project root) apply to both forms.
2. Absence: an optional file is skipped only when `os.Lstat` of its lexical path reports not-exist, including a missing parent directory. Anything that exists is validated exactly like a required file: containment, symlink escape, regular file, readable, 1 MiB bound, syntax, and composed-size limits. A dangling final symlink is invalid, not absent, because someone created that link deliberately.
3. Timing: absence is decided in the existing per-invocation launch preflight (`PrepareEnvironments`), just as file contents are read today. A file created or deleted later takes effect on the next `hum restart`. Environment stays out of drift and status.
4. Reporting: `up`, `start`, `run`, `restart`, status, JSON, and MCP output do not change. Skipping is the normal case in the main checkout, so reporting it there would be noise. `hum doctor` keeps `environment.composition` PASS and names skipped optional files (manifest-relative paths, never values) in its message and `details`.
5. Schema and docs: hum.schema.json accepts `string | {path, required}` items with `additionalProperties: false`. hum.example.yaml shows the mapping form. README "Environment" and "Projects and worktrees" and docs/design.md "Environment" and "Launch environment" say files are required unless `required: false`. The worktrees example switches to a committed `.env` with a default `PORT` plus an optional `.env.local`; its README explains the fallback, the bind failure on collision, and the strict string alternative.

Non-goals: per-process files; globbed or discovered files; overlays, profiles, or `--env-file` flags; interpolation; reporting skipped files outside `doctor`; environment drift detection; changing required-file behavior or messages; changing the `hum init` template.

Modified-file contract: the frontmatter list. Record a justification before touching anything else.

Testing: implement first; follow docs/development.md#tests. Regressions to catch: an optional file that exists but is invalid (escaping symlink, directory, bad syntax) skipped instead of failing; a dangling symlink treated as absent; string entries becoming optional; mapping entries breaking precedence order. Extend the existing contract tests (`TestManifestEnvironmentContract`, `TestEnvironmentFileContract`, `TestEnvironmentCompositionContract`, `TestManifestSchemaContract`, `TestDoctorProjectEnvironmentAndExecutableContract`, `TestManifestEnvironmentLifecycle`) rather than adding parallel suites.

Next action: change `EnvironmentSpec.Files` (internal/project/environment.go:25) from `[]string` to entries with a required flag, parse the mapping form in `parseEnvironment` (internal/project/manifest.go:434) and keep the per-process copy at :422, then skip absent optional entries in `PrepareEnvironments` (internal/project/environment.go:208) before `resolveEnvironmentFile` canonicalizes, and in `composeMap` (:298).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 mise exec go -- go test ./internal/project -run 'TestManifestEnvironmentContract|TestEnvironmentFileContract|TestEnvironmentCompositionContract|TestManifestSchemaContract|TestExampleManifestsLoad' -count=1 -v exits 0 with those tests executed: string and mapping items parse in order and required defaults to true; unknown mapping keys, a missing, empty, absolute, or escaping path, and a non-boolean required fail at parse; the schema accepts both forms and rejects unknown mapping keys; a missing optional file, including one under a missing directory, is skipped while later files still apply in order; an optional path that is a directory, an escaping symlink, a dangling symlink, unreadable, oversized, or malformed fails as a required file does; a missing required file still fails with the unchanged message; the worktrees example manifest loads.
- [ ] #2 mise exec go -- go test ./internal/cli -run 'TestDoctorProjectEnvironmentAndExecutableContract|TestManifestEnvironmentPreflightContract' -count=1 -v exits 0 with those tests executed: doctor reports environment.composition PASS and names a skipped optional file by manifest-relative path in its message and details, never a value; launch preflight with a missing optional file succeeds and with a missing required file fails before daemon contact.
- [ ] #3 mise exec go -- go test ./integration -run '^TestManifestEnvironmentLifecycle$' -count=1 -v exits 0 with the built binary: with files [.env, {path: .env.local, required: false}] and no .env.local, start succeeds and the child sees the .env value; after .env.local is created, hum restart applies its override.
- [ ] #4 task cli:test exits 0 with no test skipped, deleted, or weakened, and rg -n 'required: false' README.md docs/design.md hum.example.yaml examples/worktrees/hum.yaml prints at least one line from each file (the schema form is covered by TestManifestSchemaContract in AC1).
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
