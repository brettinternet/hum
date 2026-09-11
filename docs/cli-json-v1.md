# CLI machine output version 1

Hum's public CLI machine-output contract is the newline-delimited JSON described here. Every covered top-level object has the required integer field `schema_version: 1`.

This contract is separate from Hum's private daemon protocol and from the MCP tool schemas. Local clients must invoke the CLI rather than connect to the daemon socket or import daemon protocol types. CLI and MCP versions may evolve independently even when fields have the same names.

## Coverage and framing

The contract applies when a command that supports `--json` (or `-j`, where documented) is invoked in JSON mode:

- `init`; detached `run`; `list`; `status`; bounded and followed `logs`; `wait`; `input`; `signal`; `shutdown`
- `start`, `up`, `down`, `restart`, `stop`, and `remove`
- terminal errors emitted by any of those commands before or after other machine records

Attached `hum run` is the only exception. It does not support CLI JSON mode: stdout and stderr remain the raw child streams and the command preserves the child's exit status. A `--json` token after the payload separator is only a child argument.

Each JSON document is a UTF-8 object followed by `\n`. Commands with one result emit one document. Commands that can report several names or stream events emit NDJSON: one complete object per line, with no surrounding array. Consumers must ignore object-key order. They may process records as they arrive and must not wait for the command to exit before decoding them.

A command's exit code retains its documented meaning; JSON output does not turn failure into success. A nonzero command may emit a terminal error object or, for a stream, successful records followed by one terminal error record. JSON-mode Hum diagnostics are not duplicated on stderr. Child stderr remains raw only for attached `run`.

## Common fields

`schema_version` is required on every top-level object and NDJSON record and is always the integer `1`. Nested objects do not repeat it.

The following table defines the required top-level fields. Fields not listed as required are optional and are present only when the state or command makes them applicable. Existing command-specific fields retain the meanings documented in [design.md](design.md).

| Output family | Commands | Required top-level fields |
| --- | --- | --- |
| Manifest creation | `init` | `schema_version`, `path`, `outcome`, `next_command`, `candidates` |
| Aggregate snapshot | `list`, aggregate `status` | `schema_version`, `processes`; `warnings` is optional |
| Single-process snapshot | named `status` | `schema_version`, `name`, `scope`, `tty`, `pid`, `pgid`, `cwd`, `argv`, `started_at`, `state`, `exit_status`, `restart_count`, `followers`, `restart`, `relaunches`, `stop_grace`, `stop_grace_inherited`, `next_cursor` |
| Detached launch | detached `run` | `schema_version`, `name`, `pid`, `cursor`; launch metadata is optional where unavailable |
| Launch/restart lifecycle record | `start`, `up`, `restart` | `schema_version`, `name`, `outcome`, `restart`, `relaunches`; state, process, readiness, dependency, guidance, and error fields depend on the outcome |
| Stop/remove/down lifecycle record | `stop`, `remove`, `down` | `schema_version`, `name`, `status`; `process` and `message` are optional |
| Signal acknowledgement | `signal` | `schema_version`, `name`, `status`, `signal` |
| Input acknowledgement | `input` | `schema_version`, `name`, `bytes`, `launch_cursor` |
| Shutdown result | `shutdown` | `schema_version`, `status` |
| Wait result | `wait` | `schema_version`, `op`, `ok`, `outcome`, `cursor`, `process_observed`; `exit`, `message`, and `error` depend on the outcome |
| Bounded log result | single-name `logs` | `schema_version`, `op`, `ok`, `entries`; cursor bounds and truncation flags are optional when not applicable |
| Named log/launch stream record | aggregate or followed `logs`, `start`, `up` | `schema_version`, `op`, `type`; `name` is required for named records, and the fields below depend on `type` |
| Terminal error before output | any covered command | `schema_version`, `error` |
| Terminal stream error | streaming command after prior output | `schema_version`, `op`, `type`, `error`; `type` is `error` and `name` is present when the failure belongs to one process |

A process snapshot's `processes` value and a log record's `entries` value are arrays, including when empty. `next` is the last source cursor consumed by a bounded log read; process `next_cursor` is the next cursor that will be assigned. Timestamps use RFC 3339 JSON strings. Durations use the existing integer nanosecond representation unless a field is explicitly documented as a duration string, such as `stop_grace`.

Stream `type` values include lifecycle outcomes plus `output`, `exit`, `warning`, and `error`. Output records carry `entries` and cursor metadata. Exit records carry `cursor` and `exit`. Warning records carry `warnings`. Error records carry `error`. Records are emitted in observed stream order; per-process output entries remain in ascending cursor order. Aggregate command result ordering follows the command semantics in [design.md](design.md), including lexical declaration order for `up` and caller selection order for logs.

`error` contains required string fields `code` and `message`; command- or daemon-supplied `details` is optional. CLI-classified codes are `usage`, `daemon_unavailable`, `manifest_invalid`, and `internal`. Daemon-originated errors retain their existing wire code, but that does not make the private daemon protocol public.

## Compatibility

Version 1 may add optional object fields and new enum values. Clients must ignore unknown object fields and handle unknown enum values without failing JSON decoding.

Within version 1 Hum will not:

- remove or rename a field documented here;
- change a documented field's JSON type or meaning;
- make an optional field required for decoding an existing outcome;
- change JSON versus NDJSON framing or stop newline-terminating records.

A change that violates those rules requires a new `schema_version`. Human-readable output, attached-run raw output, MCP schemas, and the private daemon protocol are outside this compatibility promise.
