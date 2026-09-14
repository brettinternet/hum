---
id: HUM-112
title: Add native HTTP and TCP readiness probes
status: To Do
assignee: []
created_date: '2026-09-14 23:15'
updated_date: '2026-09-14 23:17'
labels:
  - config
  - daemon
  - cli
  - mcp
  - json
  - contract
  - docs
milestone: m-3
dependencies: []
references:
  - decision-001
  - docs/design.md
modified_files:
  - internal/project/manifest.go
  - internal/project/manifest_test.go
  - internal/project/manifest_schema_test.go
  - hum.schema.json
  - internal/protocol/protocol.go
  - internal/protocol/protocol_test.go
  - internal/app/app.go
  - internal/app/app_test.go
  - internal/orchestrate/orchestrate.go
  - internal/orchestrate/orchestrate_test.go
  - internal/cli/render.go
  - internal/cli/render_test.go
  - internal/cli/doctor.go
  - internal/cli/doctor_test.go
  - internal/mcp/tools.go
  - internal/mcp/tools_test.go
  - integration/manifest_test.go
  - README.md
  - docs/design.md
  - docs/cli-json-v1.md
  - internal/skill/SKILL.md
  - plugins/hum/skills/hum/SKILL.md
priority: medium
type: feature
ordinal: 84800
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: a manifest can gate startup on a listening TCP port or a 2xx HTTP response without shelling out to curl or nc, so silent servers get first-class readiness with the same bounded, exact, no-shell semantics as ready.exec.

## Manifest contract

Extend ready so exactly one of match, exec, http, or tcp is present:

    ready:
      http: http://127.0.0.1:3000/readyz   # GET; ready on any 2xx
      interval: 1s                          # optional, default 1s
      timeout: 30s                          # optional, default 30s

    ready:
      tcp: 127.0.0.1:5432                   # ready when a connection is accepted
      timeout: 30s

- http accepts an absolute http:// or https:// URL with a host that is a literal IP or localhost; no redirects are followed, no body is read beyond a bounded prefix, and TLS verification is not disabled. Non-2xx, connection errors, and per-attempt timeouts are retries.
- tcp accepts host:port with a literal IP or localhost. Ready when connect succeeds; the connection is closed immediately without writing.
- interval is permitted with exec, http, and tcp and rejected with match. Probes run in-process; no subprocess and no shell.
- Probes are startup gates only. They inherit no environment and cannot expand variables; a manifest env value such as PORT must be repeated literally in the URL. Document this.
- Drift: changing method or target reports readiness_http or readiness_tcp; interval and timeout remain wait policy, mirroring readiness_exec.

## Surfaces

- protocol.ReadinessConfig and Readiness gain a target field for http/tcp and method values http and tcp. Status, list --full, status --json, and MCP process results show method and target exactly as declared. Diagnostic keeps one bounded terminal reason (last status code or dial error).
- hum doctor validates http/tcp syntax (scheme, host restriction, port range) without connecting.
- hum.schema.json, README readiness section, docs/design.md scope paragraph, docs/cli-json-v1.md readiness fields, and both SKILL.md files document the new methods.

## Non-goals

Liveness or health monitoring; TLS options; custom headers, methods, or body matching; DNS hostnames other than localhost; ready.match on HTTP bodies; port allocation or discovery; CLI flags on hum run.

Modified-file contract: internal/project/manifest.go, internal/project/manifest_test.go, internal/project/manifest_schema_test.go, hum.schema.json, internal/protocol/protocol.go, internal/protocol/protocol_test.go, internal/app/app.go, internal/app/app_test.go, internal/orchestrate/orchestrate.go, internal/orchestrate/orchestrate_test.go, internal/cli/render.go, internal/cli/render_test.go, internal/cli/doctor.go, internal/cli/doctor_test.go, internal/mcp/tools.go, internal/mcp/tools_test.go, integration/manifest_test.go, README.md, docs/design.md, docs/cli-json-v1.md, internal/skill/SKILL.md, plugins/hum/skills/hum/SKILL.md.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — mise exec go -- go test ./internal/project -run '^TestManifestSchemaContract$|^TestParseReady' -count=1 -v exits 0 and proves ready accepts exactly one of match|exec|http|tcp, rejects interval with match, rejects non-literal-IP/localhost hosts, bad schemes, and out-of-range ports with a file:line diagnostic, and that hum.schema.json accepts and rejects the same documents as the parser.
- [ ] #2 AC2 — mise exec go -- go test ./internal/app -run '^TestReadiness(HTTP|TCP)' -count=1 -v exits 0 against in-test net/http and net listeners and proves: non-2xx then 2xx transitions to ready; redirects are not followed; connection refused retries at interval until timeout; per-attempt bound is honored; the retained diagnostic is bounded and names the last status or dial error; no subprocess is spawned.
- [ ] #3 AC3 — mise exec go -- go test ./internal/orchestrate ./internal/cli ./internal/mcp -run 'Readiness.*(HTTP|TCP|Drift)|^TestDoctor.*Readiness' -count=1 -v exits 0 and proves method/target changes report readiness_http or readiness_tcp while interval/timeout changes do not, status and list --full render method and target, status --json and MCP process results carry method and target under existing field names, and doctor validates syntax without opening a connection.
- [ ] #4 AC4 — mise exec go -- go test ./integration -run '^TestManifestHTTPReadiness$|^TestManifestTCPReadiness$' -count=1 -v exits 0 with a real hum up --detach against a fixture that listens late, exiting 0 once the probe passes and 2 on timeout.
- [ ] #5 AC5 — task cli:check && task test exits 0, and rg -n 'ready\.(http|tcp)|readiness_(http|tcp)' README.md docs/design.md docs/cli-json-v1.md internal/skill/SKILL.md plugins/hum/skills/hum/SKILL.md hum.schema.json exits 0 for every listed file.
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
