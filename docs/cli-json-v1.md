# CLI JSON output, version 1

Pass `--json` (or `-j` where supported) and Hum writes JSON objects, one per line. Every top-level
object has `schema_version: 1`.

```console
$ hum status hi --json
{"schema_version":1,"name":"hi","session_id":"a1b2...","source":"ad_hoc","scope":"project","state":"exited","exit_status":0,"stop_grace":"10s",...}

$ hum logs api --tail 2 --json
{"schema_version":1,"op":"output","ok":true,"session_id":"a1b2...","entries":[{"cursor":1,"stream":"stdout","time":"...","text":"GET /health 200\n"},...],"next":2,"oldest":0,"latest":2}
```

Check support before relying on this contract. This call never resolves a project, reads a
manifest, or contacts the daemon:

```console
$ hum version --json
{"schema_version":1,"version":"<version>","build_time":"<time>"}
```

This contract is separate from MCP tool schemas and from Hum's private daemon protocol. Local
clients must call the CLI, not the daemon socket or its types. CLI and MCP versions evolve
independently even where field names match.

## Coverage

JSON mode covers:

- `version`, `doctor`, `init`, detached `run`, `list`, `status`, `events`, bounded and followed
  `logs`, `wait`, `input`, `signal`, `shutdown`
- `start`, `up`, `down`, `restart`, `stop`, `remove`
- terminal errors from any of these, before or after other records

Attached `hum run` is the exception: stdout and stderr stay the raw child streams and the command
returns the child's exit status. A `--json` after the `--` separator is just a child argument.

## Framing

- Each document is a UTF-8 object followed by `\n`.
- Single-result commands emit one object. Commands that report several names or stream events emit
  NDJSON: one object per line, no surrounding array.
- Key order is not significant. Process records as they arrive; do not wait for exit.
- Exit codes keep their meaning. A failing command may emit a terminal error object, or, for a
  stream, earlier records followed by one error record.
- Hum diagnostics are not repeated on stderr in JSON mode.

## Required fields

`schema_version` is required on every top-level object and is always the integer `1`; nested
objects do not repeat it. Fields not listed below are optional and appear only when they apply.
Command-specific meanings are in [design.md](design.md).

| Output | Commands | Required top-level fields |
| --- | --- | --- |
| Capability discovery | `version` | `schema_version`, `version`, `build_time` |
| Diagnostic preflight | `doctor` | `schema_version`, `ok`, `checks`, `summary` |
| Manifest creation | `init` | `schema_version`, `path`, `outcome`, `next_command`, `candidates` |
| Aggregate snapshot | `list`, aggregate `status` | `schema_version`, `processes`; `warnings` optional |
| Port lookup | `list --port PORT` | `schema_version`, `processes`; `incomplete` and `warnings` optional |
| Single-process snapshot | named `status` | `schema_version`, `name`, `session_id`, `scope`, `tty`, `pid`, `pgid`, `cwd`, `argv`, `started_at`, `state`, `exit_status`, `restart_count`, `followers`, `restart`, `relaunches`, `stop_grace`, `stop_grace_inherited`, `next_cursor`; `ports` on running snapshots only |
| Detached launch | detached `run` | `schema_version`, `name`, `session_id`, `pid`, `cursor`; launch metadata optional when unavailable |
| Launch/restart record | `start`, `up`, `restart` | `schema_version`, `name`, `session_id`, `outcome`, `restart`, `relaunches`; other fields depend on the outcome |
| Stop/remove/down record | `stop`, `remove`, `down` | `schema_version`, `name`, `status`; `process` and `message` optional |
| Signal acknowledgement | `signal` | `schema_version`, `name`, `status`, `signal` |
| Input acknowledgement | `input` | `schema_version`, `name`, `bytes`, `launch_cursor` |
| Shutdown result | `shutdown` | `schema_version`, `status` |
| Wait result | `wait` | `schema_version`, `op`, `ok`, `outcome`, `cursor`, `session_id`, `process_observed`; `exit`, `message`, `error` depend on the outcome |
| Bounded log result | single-name `logs` | `schema_version`, `op`, `ok`, `session_id`, `entries`; cursor bounds and truncation flags optional |
| Event history | `events` | events: `schema_version`, `type`, `cursor`, `time`, `kind`, `name`, `event`; lifecycle launch and exit may include `log_cursor`, `log_session_id`; trailing metadata: `schema_version`, `type`, `next_cursor`, `truncated`, `has_more` |
| Named stream record | aggregate or followed `logs`, `start`, `up` | `schema_version`, `op`, `type`; `name` for named records; output records include `session_id`; other fields depend on `type` |
| Error before output | any covered command | `schema_version`, `error` |
| Stream error after output | streaming commands | `schema_version`, `op`, `type` (`error`), `error`; `name` when the failure belongs to one process |

## Field notes

`doctor`: exactly one object. `checks` is ordered; each entry requires `name`, `status` (`PASS`,
`WARN`, `FAIL`, or `INFO`), and `message`, with optional `details` that never contain environment
values, a whole environment, or a list of keys. `summary` has integer `pass`, `warn`, `fail`, and
`info`. `ok` is false exactly when a check is `FAIL`; warnings keep exit 0.

Readiness: `readiness_method` is `match`, `exec`, `http`, `tcp`, or `exit`. `http` and `tcp` also
carry the literal `readiness_target`, which never expands variables. `ready: {exit: 0}` reports a
successful exit as readiness `ready` and outcome `completed`; a nonzero exit, signal, or stop is
`exited_before_ready`.

Values:

| Field | Format |
| --- | --- |
| `processes`, `entries` | always arrays, even when empty |
| logs `next` | last source cursor consumed by this read |
| process `next_cursor` | next cursor to be assigned |
| `session_id`, `log_session_id` | opaque output-session identity paired with log cursors |
| timestamps | RFC 3339 strings |
| durations | integer nanoseconds, unless documented as a duration string (such as `stop_grace`) |

Named `status` automatically includes `ports` for running processes with `state` (`available`, `partial`, `denied`, or `unavailable`), a
`listeners` array, and an optional `diagnostic`. Each listener has `transport: "tcp"`, its literal local
`address`, numeric `port`, and sorted holder `pids`. `list --port PORT` returns only matching holders, each
with `ports.listeners` filtered to that port; `incomplete`, when present, names records that prevented a
complete lookup. Both fields are additive and keep `schema_version: 1`. Plain list and aggregate status
never inspect sockets. See [design.md](design.md#port-observation) for scope, deadlines, and snapshot
limitations. On Windows, `pids` contains only the API's verified binding owner, not every inherited holder;
inspection is always `partial` with a diagnostic, including empty results.

Streams: `type` is a lifecycle outcome or `output` (`session_id`, `entries` and cursor metadata),
`exit` (`session_id`, `cursor`, `exit`), `warning` (`warnings`), or `error` (`error`). Records arrive
in observed order; each process's entries stay in ascending cursor order. Result order follows
[design.md](design.md): lexical declaration order for `up`, caller order for `logs`.

Errors: `error` has string `code` and `message`, and optional `details`. CLI codes are `usage`,
`daemon_unavailable`, `manifest_missing`, `manifest_invalid`, and `internal`. Daemon wire code
`cursor_stale` means a cursor's `session_id` no longer identifies the current output session; its
message names the current ID when available and suggests reading without a cursor. Without a default
manifest, `manifest_missing` names the nearest-manifest search directory and project root when they
differ, and suggests `hum init` or `hum run NAME -- COMMAND`. The nearest lookup never searches
above the project root. Daemon errors keep their wire code; that does not make the daemon protocol
public.

```console
$ hum up --json
{"schema_version":1,"error":{"code":"manifest_missing","message":"manifest is missing in /tmp/app: run hum --project /tmp/app init to create hum.yaml, or use hum run NAME -- COMMAND"}}
```

## Event history records

`hum events --json` emits `type: "event"` records in cursor order, then one `type: "metadata"`
record:

```json
{"schema_version":1,"type":"event","cursor":65,"time":"...","kind":"operation","name":"hi","event":"run","origin":"cli","outcome":"success","operation_id":"145e..."}
{"schema_version":1,"type":"event","cursor":129,"time":"...","kind":"lifecycle","name":"hi","event":"exit","exit_code":0,"log_cursor":8,"log_session_id":"a1b2...","operation_id":"145e..."}
{"schema_version":1,"type":"metadata","next_cursor":192,"truncated":false,"has_more":false}
```

- Each event has `cursor`, `time`, `kind`, `name`, and `event`. Lifecycle events may add exit
  fields and a directly attributable `operation_id`; operation events carry `origin`, `outcome`,
  and `operation_id`.
- Lifecycle `launch` and `exit` records may include `log_cursor` (an output cursor, distinct from
  the event `cursor`) and `log_session_id` (the output session that owns it). For a launch, the cursor
  points to the entry immediately before that incarnation's output, usually the `NAME launched`
  marker; it is absent when there is no preceding entry. For an exit, it points to the latest output
  entry at exit time; it is absent when there is no output. Other events omit both fields. Cursor 0
  is valid. Pair both fields when passing the cursor to `hum logs NAME --after-cursor N --session-id ID`
  or MCP `logs` (`after` and `session_id`).
- Session IDs remain stable across stop/start, restart, and automatic relaunch, and change after
  `hum remove`, completed-record eviction, or daemon replacement. A CLI `--session-id` is optional
  for manual paging but requires `--after-cursor`; scripts should always pass it. MCP requires
  `session_id` whenever `after` is present. A stale ID returns `cursor_stale`; timestamps cannot
  verify cursor validity.
- Records are bounded and never truncated to terminal width.
- `next_cursor` is the last returned cursor when more forward pages remain, otherwise the read's
  fixed high-water mark. `truncated` reports a cursor gap from eviction or discarded data.
  `has_more` reports another matching forward event.
- MCP `events` returns the same fields, is versioned separately, and cannot follow.

## Compatibility

Version 1 may add optional fields and new enum values. Clients must ignore unknown fields and
tolerate unknown enum values.

Within version 1, Hum will not:

- remove or rename a documented field;
- change a documented field's JSON type or meaning;
- make an optional field required to decode an existing outcome;
- change JSON versus NDJSON framing, or stop ending records with a newline.

Breaking any of these requires a new `schema_version`. Human output, attached-run output, MCP
schemas, and the daemon protocol are outside this promise.
