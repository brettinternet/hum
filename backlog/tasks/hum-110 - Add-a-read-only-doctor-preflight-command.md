---
id: HUM-110
title: Add a read-only doctor preflight command
status: To Do
assignee: []
created_date: '2026-09-13 06:54'
labels:
  - cli
  - diagnostics
  - environment
dependencies: []
modified_files:
  - internal/cli/commands.go
  - internal/cli/config.go
  - internal/cli/doctor.go
  - internal/cli/doctor_test.go
  - internal/cli/help_contract_test.go
  - internal/cli/surface_test.go
  - internal/cli/man.go
  - internal/cli/man_test.go
  - internal/process/process.go
  - internal/process/process_test.go
  - integration/doctor_test.go
  - README.md
  - docs/design.md
  - docs/cli-json-v1.md
priority: medium
type: feature
ordinal: 82800
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: `hum doctor` gives humans and automation one fast, deterministic preflight for Hum settings, project configuration, manifest environments, executable availability, and an existing daemon without starting the daemon or any managed process.

Command contract:
- Support `hum doctor [--json]` with the existing `--project/-C` and `--file/-F` selectors. Reject `--global`: the command diagnoses one filesystem project.
- Run bounded checks for the supported OS, effective Hum configuration (`HUM_RUNTIME_DIR`, `XDG_RUNTIME_DIR`, `HUM_STOP_GRACE`, `HUM_OUTPUT_BYTES`, and `HUM_COMPLETED_RECORDS`), runtime path usability, project/manifest or conventional discovery, environment-file loading/composition and protocol bounds, process `argv[0]`, and `ready.exec[0]`.
- Resolve executables with each process's exact composed environment, cwd, and existing executor semantics. Check slash-containing argv directly. Do not execute process commands or readiness probes.
- Contact only an already-present daemon socket to report reachability and protocol compatibility. A missing daemon is informational because launch commands start it automatically; never create runtime artifacts merely to inspect daemon state.
- Human output uses ordered PASS/WARN/FAIL/INFO rows, safe paths/counts, and a final summary. Exit 0 when no check fails and 1 for any failed check or invalid usage; warnings and informational results do not fail.
- JSON emits one newline-terminated schema-versioned object with `ok`, `checks`, and summary counts. Each check has a stable name, status, and message, with optional non-sensitive details. Add this result family to CLI JSON v1.

Privacy and safety:
- Never print, serialize, or include in errors any environment value or complete composed environment. Process environment keys are not enumerated. Existing safe parser diagnostics may identify a file, line, process, rule, or valid key under the established environment privacy boundary.
- The command is observational: it does not start/stop/restart processes, start the daemon, run probes, repair files, or retain cross-invocation state. A temporary runtime writability probe is permitted only when created and removed within the command; leave no artifact on success.

Non-goals: inferring required application variables from argv; adding required-env manifest syntax; port/network/service health checks; dependency version constraints; package installation or automatic repair; daemon configuration introspection beyond the existing public handshake; MCP exposure; nested `doctor env` subcommands.

Modified-file contract: internal/cli/commands.go, internal/cli/config.go, internal/cli/doctor.go, internal/cli/doctor_test.go, internal/cli/help_contract_test.go, internal/cli/surface_test.go, internal/cli/man.go, internal/cli/man_test.go, internal/process/process.go, internal/process/process_test.go, integration/doctor_test.go, README.md, docs/design.md, docs/cli-json-v1.md.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `mise exec go -- go test ./internal/cli -run '^TestDoctorConfigurationAndRuntimeContract$' -count=1 -v` exits 0 and prints RUN/PASS. The table covers defaults and every supported HUM_/XDG input, malformed values, precedence, supported-platform reporting, usable/unusable runtime paths, bounded temporary-probe cleanup, stable human ordering/summary, warning semantics, failure exit status, and proves that no absent daemon or runtime path is created.
- [ ] #2 AC2 — `mise exec go -- go test ./internal/cli -run '^TestDoctorProjectEnvironmentAndExecutableContract$' -count=1 -v` exits 0 and prints RUN/PASS. It covers default and alternate manifests, conventional discovery, strict manifest/dependency/cwd/readiness failures, missing or malformed environment files, composition/protocol bounds, effective-PATH and cwd executable resolution for process and ready.exec argv, slash-containing argv, multiple failures, cancellation, value/key privacy, and zero command/probe execution.
- [ ] #3 AC3 — `mise exec go -- go test ./internal/cli ./integration -run '^TestDoctorJSONContract$|^TestDoctorDoesNotStartDaemon$|^TestDoctorExistingDaemon$' -count=1 -v` exits 0 with RUN/PASS for all three exact tests. JSON is one newline-terminated schema-version-1 object with stable check names/statuses and summary counts; missing daemon is INFO and successful, a compatible existing daemon is PASS, incompatible/unreachable existing runtime state is diagnosed without mutation, selectors work, and stdout/stderr contain no environment values.
- [ ] #4 AC4 — `task cli:check && task test` exits 0. Root/help/completion/man-page surfaces, README, design semantics, and `docs/cli-json-v1.md` document `hum doctor`, selectors, checks, daemon absence, safety/privacy, output, and exit behavior.
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
