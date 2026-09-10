---
id: HUM-071
title: Remove the duplicate daemon wire model
status: Done
assignee: []
created_date: '2026-09-10 01:52'
updated_date: '2026-09-10 12:07'
labels: []
dependencies: []
modified_files:
  - internal/daemon/client.go
  - internal/daemon/server.go
  - internal/daemon/wire_protocol.go
  - internal/daemon/wire_protocol_test.go
  - internal/daemon/client_test.go
  - internal/daemon/server_test.go
  - internal/protocol/codec.go
  - internal/protocol/codec_test.go
priority: medium
type: chore
ordinal: 47700
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: The daemon and client dispatch directly through the canonical typed `internal/protocol` request and response DTOs, with only explicit app-domain conversions remaining. Evidence: protocol decoding already performs typed operation dispatch, but daemon code immediately flattens it into `wireRequest`/`wireResponse` and copies every field again in both directions across roughly 400 lines. Adding a protocol field can compile while silently dropping that field in a keyed adapter literal. Scope: remove the duplicate flattened DTOs and converters, use typed protocol values end to end, and retain exhaustive round-trip tests for each operation. Non-goals: do not change the on-socket JSON protocol, protocol version, public CLI/MCP output, or app domain types.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `mise exec go -- go test -race ./internal/protocol ./internal/daemon ./internal/mcp && mise exec go -- go test ./internal/cli` exits 0.
- [x] #2 `rg -n "type wire(Request|Response)|wireRequestFromProtocol|writeProtocolRequest|readProtocolResponse" internal/daemon` exits 1 with no matches.
- [x] #3 `mise exec go -- go test ./internal/protocol ./internal/daemon -run TestProtocolRoundTripAllFields -count=1` exits 0 after covering every operation and retaining every nonzero DTO field.
- [x] #4 `task ci` exits 0.
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

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Extend protocol codec response decoding so clients receive operation-specific canonical response DTOs.
2. Refactor daemon client and server dispatch to pass typed protocol requests/responses directly, retaining only app↔protocol domain conversions.
3. Replace duplicate-wire tests with exhaustive all-fields protocol round trips and update affected daemon tests.
4. Run focused race/unit checks, task ci, independent verification, then commit on main.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented canonical protocol request/response dispatch through the daemon client and server, leaving only protocol-to-app/output/process conversions. Added typed response decoding, exhaustive request/response/event round-trip coverage, strict input operation validation, and preserved staged TTY readiness.

Modified-file deviation: internal/daemon/daemon_test.go migrates direct duplicate-wire tests to canonical DTOs. internal/app/app.go and internal/app/tty_test.go preserve and test InputAttachRequest.Ready across the required protocol-to-app conversion.

AC#1 PASS - mise exec go -- go test -race ./internal/protocol ./internal/daemon ./internal/mcp plus mise exec go -- go test ./internal/cli exited 0.
AC#2 PASS - the required rg duplicate-wire search exited 1 with no matches.
AC#3 PASS - mise exec go -- go test ./internal/protocol ./internal/daemon -run TestProtocolRoundTripAllFields -count=1 exited 0; the protocol test enumerates every request, response, input, and event operation with populated DTO fields.
AC#4 PASS - task ci exited 0 on the final source tree; security, formatting, vet, staticcheck, unit/integration, race, build, and smoke checks passed.

Independent verifier executed all four acceptance commands and returned PASS for AC#1 through AC#4. Reviewer findings for input connection cleanup, operation validation, typed blank-op errors, all-fields coverage, staged readiness, and next_cursor test strength were fixed.

Final review: restored nonzero next_cursor omission coverage with an actual Server.dispatch test that proves start/list/stop omit a populated app cursor while get includes it. No test was deleted, skipped, or weakened. Final task ci rerun exited 0 after this change.
<!-- SECTION:NOTES:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-09-10 06:01
---
Refinement 2026-09-10: confirmed. internal/daemon/wire_protocol.go is 406 lines of `wireRequestFromProtocol`/`writeProtocolResponse` field copying with 35 wire* references in that file plus 42 in client.go and 27 in server.go. Worthwhile as a drift-prevention chore; protocol JSON unchanged per non-goals.
---
<!-- COMMENTS:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Removed the duplicate daemon wire request/response model and now dispatches canonical typed protocol DTOs end to end. Added typed response decoding, exhaustive operation/all-field round trips, stricter input response handling, and staged readiness preservation. Verified all four acceptance commands, including final task ci, plus independent AC verification.
<!-- SECTION:FINAL_SUMMARY:END -->
