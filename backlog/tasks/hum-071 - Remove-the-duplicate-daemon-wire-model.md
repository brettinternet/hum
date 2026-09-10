---
id: HUM-071
title: Remove the duplicate daemon wire model
status: To Do
assignee: []
created_date: '2026-09-10 01:52'
updated_date: '2026-09-10 01:59'
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
- [ ] #1 `mise exec go -- go test -race ./internal/protocol ./internal/daemon ./internal/mcp && mise exec go -- go test ./internal/cli` exits 0.
- [ ] #2 `rg -n "type wire(Request|Response)|wireRequestFromProtocol|writeProtocolRequest|readProtocolResponse" internal/daemon` exits 1 with no matches.
- [ ] #3 `mise exec go -- go test ./internal/protocol ./internal/daemon -run TestProtocolRoundTripAllFields -count=1` exits 0 after covering every operation and retaining every nonzero DTO field.
- [ ] #4 `task ci` exits 0.
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
