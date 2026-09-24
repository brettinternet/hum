---
id: HUM-144
title: Fuzz the parsers that read arbitrary child output and socket bytes
status: To Do
assignee: []
created_date: '2026-09-24 22:54'
updated_date: '2026-09-24 22:55'
labels:
  - output
  - protocol
dependencies: []
modified_files:
  - internal/output/*_test.go
  - internal/protocol/*_test.go
priority: low
type: task
ordinal: 28000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: the three functions that take bytes hum does not control get Go native fuzz targets. Their example-based tests stay as they are. There are no Fuzz functions in the repo today; the only non-Test functions are benchmarks in internal/output/bench_test.go. Seed corpora run as ordinary tests in `go test ./...`, so the targets cost milliseconds in CI and support longer local fuzzing on demand.

Targets and properties. Assert only what the documented contract guarantees; read each doc comment first.
1. `output.StripTerminalControl(text string) string` (internal/output/terminal_control.go:24) runs on every child output entry. Properties: no panic; the result contains no ESC byte (0x1b); len(result) <= len(text); input with no ESC byte, no UTF-8 C1 control (0xc2 followed by 0x80-0x9f), and no CR immediately before LF is returned unchanged. Do not assert idempotence or the absence of C1 in the output: removing a sequence can join a lone 0xc2 with a following byte into a new C1 pair. Seed with the cases in internal/output/terminal_control_test.go.
2. `output.LineWriter` (internal/output/splitter.go:33 NewLineWriter) splits arbitrary child bytes into entries. Drive it with fuzz bytes cut into fuzz-chosen chunk sizes, a small maxLineBytes (for example 1 to 64, taken from the fuzz input), idle disabled (0), and an append callback that records entries; then Close. Properties: no panic; every entry is within the byte bound the implementation enforces; the entries rebuild the input exactly. Take the exact rules for how LF is stored in an entry from the implementation and internal/output/splitter_test.go before you write the check.
3. `protocol.Decoder` (internal/protocol/codec.go:30 NewDecoder; DecodeRequest :72 and DecodeResponse :385) reads the daemon socket. Build a decoder with a small limit (for example 256) over the fuzz bytes. Call DecodeRequest repeatedly until it returns io.EOF, continuing past other errors. Properties: no panic; every non-EOF error satisfies `var decodeErr *protocol.DecodeError; errors.As(err, &decodeErr)`; the loop ends within len(input)+1 calls, which proves the decoder always advances. If the decoder is not designed to resume after a DecodeOversized or DecodeMalformed error, check that in codec.go and codec_test.go first. In that case stop at the first error and record why. Seed with lines from internal/protocol/codec_test.go.

Put each target in the existing test file of its package, or in a `fuzz_test.go` beside it. If fuzzing finds a real defect, keep the failing input under the generated testdata/fuzz directory, stop, and create a separate bug task with the input and the observed behavior. Do not relax the property to make it pass.

Non-goals: fuzzing manifest YAML or MCP JSON; changing production code; adding fuzzing to CI beyond the seed corpus.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `go test ./internal/output ./internal/protocol -run "^Fuzz" -count=1 -v` exits 0 and lists FuzzStripTerminalControl, a LineWriter fuzz target, and a Decoder fuzz target.
- [ ] #2 AC2 — `go test ./internal/output -run "^$" -fuzz "^FuzzStripTerminalControl$" -fuzztime 60s` exits 0.
- [ ] #3 AC3 — the LineWriter target run with `go test ./internal/output -run "^$" -fuzz "^<name>$" -fuzztime 60s` exits 0.
- [ ] #4 AC4 — the Decoder target run with `go test ./internal/protocol -run "^$" -fuzz "^<name>$" -fuzztime 60s` exits 0.
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
