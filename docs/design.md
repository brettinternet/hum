# hum design

This is the detailed behavior reference. For an introduction, see the [README](../README.md).

```text
CLI ─┐                               ┌─▶ launch order
     ├─▶ internal/orchestrate ───────┼─▶ readiness
MCP ─┘            │                  └─▶ stable outcomes
                  ▼
           private daemon ──▶ process groups + bounded output
```

- [Scope and non-goals](#scope-and-non-goals)
- [Platforms](#platforms)
- [CLI](#cli)
- [Command semantics](#command-semantics)
- [Definitions and resolution](#definitions-and-resolution)
- [Readiness and output](#readiness-and-output)
- [Daemon and environments](#daemon-and-environments)
- [Event history](#event-history)
- [MCP adapter](#mcp-adapter)
- [Optional pseudo-terminals](#optional-pseudo-terminals)
- [Canonical project scopes](#canonical-project-scopes)

## Scope and non-goals

Hum is a local exact-argv process supervisor for humans and coding agents.

- A private daemon owns named process groups and their bounded output, independent of clients.
- CLI and MCP clients resolve project definitions and send exact argv to that daemon. They never
  create another supervisor or rebuild shell text.
- Process names are scoped to the nearest canonical Git root, or to the caller's working directory
  when there is no Git marker.

Hum deliberately does not provide:

- a TUI or web UI; Herdr provides panes;
- port allocation or a reverse proxy;
- cron scheduling, boot start, or shell-hook autostart;
- file-watch restarts or liveness/health monitoring;
- child CPU/RSS sampling or resource limits (wrap argv with platform tools instead; Hum still
  bounds its own retained output and machine-facing operations);
- log parsing or query languages;
- runtime shell interpretation or templating;
- arbitrary or queued input, remote transport or authentication, live event callbacks, an
  in-daemon plugin system, or OS service installation.

These boundaries are recorded in
[decision-001](../backlog/decisions/decision-001%20-%20Hum-stays-a-process-API-no-port-allocation-proxying-or-shell-level-conveniences.md).
Hum's value is its contracts and integrations: the versioned [CLI JSON v1](cli-json-v1.md)
contract, closed MCP schemas, read-only `doctor` preflight, and the Herdr, Claude Code, and Codex
plugins.

`internal/orchestrate` owns scheduling, readiness, recovery, drift, removal, and skip
classification. CLI and MCP adapt daemon snapshots and render the same model.

## Platforms

Hum supports macOS, Linux, and Windows amd64.

On Windows:

| Area | Behavior |
| --- | --- |
| Install | Release `hum-<version>-windows-x64.zip` contains `hum.exe`; unpack with PowerShell `Expand-Archive` and add it to `PATH` (see [installation](../README.md#install)). `install.sh` and `hum.1` are Unix-only. |
| Daemon | A private named pipe derived from the runtime directory (default `%LOCALAPPDATA%\hum-runtime`). The directory, runtime files, pipe ACL, and connecting peer must belong to the current user; a foreign runtime is rejected, not reused. |
| Children | Owned by a private Job Object from creation. Non-TTY children get a null stdin and separate bounded stdout/stderr. |
| Stop | `stop` and `down` terminate the whole owned job immediately after checking the root PID and creation-time identity, even if the leader already exited. `stop_grace` does not delay termination because Windows apps do not receive SIGTERM. A recorded PID alone never authorizes a stop after ownership is lost. |
| Exit | Results carry the real exit code and never invent a Unix signal. |
| Signals | `signal` returns an explicit unsupported error. |
| TTY | ConPTY with merged output and the exclusive input lease. In local raw console mode, Ctrl+C goes to the child console, Ctrl+] releases only the local lease, and Ctrl+D is forwarded as a byte, not EOF. Size changes are polled while attached. |

## CLI

```text
hum version [--json]
hum [--project DIR|-C DIR] [--file PATH|-F PATH] doctor [--json]
hum [--project DIR|-C DIR] init [--force] [--json]
hum serve [--daemon]
hum [--project DIR|-C DIR] start <name>... [--no-wait] [--timeout DURATION] [--json]
hum [--project DIR|-C DIR] up [<name>...] [--detach] [--no-wait] [--timeout DURATION] [--full] [--json]
hum [--project DIR|-C DIR] down [--json]
hum run [--project DIR|-C DIR] <name> [--detach] [--tty] [--json] [-- <command> [args...]]
hum [--project DIR|-C DIR] list [--all] [--full] [--json]
hum [--project DIR|-C DIR] status [<name>] [--json]
hum [--project DIR|-C DIR] attach <name> [--tail N]
hum [--project DIR|-C DIR] logs [<name>...] [--stream stdout|stderr|system|both] [--tail N] [--after-cursor N]
           [--since DURATION] [--limit-bytes N] [--match REGEX] [--context N] [--follow] [--json]
hum [--project DIR|-C DIR] events [<name>...] [--since DURATION] [--kind lifecycle|operation]
           [--failed] [--match REGEX] [--tail N] [--after-cursor N] [--full] [--json]
hum [--project DIR|-C DIR] wait <name> [--after-cursor N] [--match REGEX] [--timeout DURATION] [--json]
hum [--project DIR|-C DIR] input <name> (--text TEXT | --base64 PADDED_VALUE) [--json]
hum [--project DIR|-C DIR] signal <name> <signal> [--json]
hum [--project DIR|-C DIR] restart <name>... [--no-wait] [--timeout DURATION] [--json]
hum [--project DIR|-C DIR] stop <name>... [--json]
hum [--project DIR|-C DIR] remove (<name>... | --all) [--json]
hum shutdown [--stop-processes] [--json]
hum mcp
hum skill
hum completion bash|zsh|fish
```

`list` also answers to `ls`. Long command and option names are canonical in documentation,
scripts, output, and errors.

### Options and aliases

`-C DIR`/`--project DIR` and `-F PATH`/`--file PATH` may appear before or after the subcommand;
`run` takes them after NAME and before `--`, and `signal` before or after its positional names. Other
short aliases are command-local. Combined short options (`-dj`) are unsupported, and MCP fields have
no aliases.

| Alias | Long option | Commands |
| --- | --- | --- |
| `-h` | `--help` | global |
| `-v` | `--version` | global |
| `-C` | `--project` | project-scoped commands |
| `-F` | `--file` | project-scoped commands |
| `-g` | `--global` | commands that accept global scope |
| `-j` | `--json` | all supporting commands except `input` |
| `-d` | `--daemon`, `--detach` | `serve`, `run`, `up` |
| `-t` | `--timeout` | `start`, `up`, `wait`, `restart` |
| `-a` | `--all` | `list` |
| `-s` | `--stream` | `logs` |
| `-n` | `--tail` | `attach`, `logs`, `events` |
| `-c` | `--after-cursor` | `logs`, `wait`, `events` |
| `-b` | `--limit-bytes` | `logs` |
| `-m` | `--match` | `logs`, `wait`, `events` |
| `-f` | `--follow` | `logs` |

Long-only: `--force`, `--since`, `--no-wait`, `--tty`, `--stop-processes`, `--runtime-dir`,
`--stop-grace`, `--output-bytes`, and `--completed-records`. `input` adds no short aliases,
including for `--json`.

### Selecting a project and manifest

- `--project DIR` resolves DIR from the invocation directory, canonicalizes it to a physical
  absolute path, then uses the nearest Git root or DIR itself. That root scopes names and manifests;
  default-manifest search starts at DIR and is bounded by that root. Without `--project`, search
  starts at the invocation directory.
- Without `--file`, Hum selects the nearest `.hum.yaml` or `hum.yaml` from the search start up to
  the project root. `--file PATH` still resolves PATH from the invocation directory; it must be a
  regular file inside the selected project. Without `--project`, the file's location picks the project.
- Every manifest, selected or default, is read without blocking and must not exceed 1 MiB.
- Launch commands require an existing directory. Observation and lifecycle commands can target a
  removed worktree when DIR exactly matches a root the daemon still retains.
- `version`, `serve`, `shutdown`, `mcp`, and `skill` reject project, file, and global selectors
  because their scope is static, daemon-wide, or per-request.
- Guidance and next-command fields use a shell-safe absolute `--project` selector, including
  paths with spaces.

### Human output

Human-readable output is the default.

| Command | Default columns | Extra detail |
| --- | --- | --- |
| `status` | `NAME`, `STATE`, `PID`, `READINESS`, `RESTART`, `FOLLOWERS`; includes unlaunched declarations | `status NAME`: full single-process detail, readiness configuration, diagnostics |
| `list` | `NAME`, `STATE`, `PID` | `--full`: source, argv, readiness, followers (when followed), TTY, exit signal, restart details |
| `up` summary | `NAME`, `RESULT`, `STATE`, `PID` in lexical order | `--full`: readiness matcher or exec method/argv/interval |
| `doctor` | ordered `PASS`/`WARN`/`FAIL`/`INFO` rows and summary counts | — |

A declaration without a daemon record shows as stopped, even with no daemon running.

Color applies only when stdout is a terminal, `TERM` is not `dumb`, and `NO_COLOR` is unset (an
empty `NO_COLOR` also disables it). Piped output and JSON never contain ANSI styling.

| Styled element | Color |
| --- | --- |
| running, ready | green |
| starting | yellow |
| operator-stopped | cyan |
| successful autonomous exit | dim |
| failed exit, error, timeout, drift, exhausted recovery, dependency skip | red |
| `list` headers | bold |
| aggregate `[NAME]` log prefix | stable per-name color, never red or green |

Names outside log prefixes, paths, messages, and child output are never styled.

### JSON output

The public contract is [CLI JSON v1](cli-json-v1.md): every top-level object carries
`schema_version: 1`. `hum version --json` prints
`{"schema_version":1,"version":"<version>","build_time":"<time>"}` without resolving a project or
contacting the daemon, so clients can check support first. The contract is independent of MCP and
the private daemon protocol; clients must not use the daemon socket.

- JSON mode applies only when a standalone `--json` or `-j` appears before the payload separator.
  Attached `run` has no JSON mode and stays raw child output, including stderr.
- Failures print one `{"error":{"code":"...","message":"..."}}` object on stdout when no JSON was
  written yet. Streams (`start`, `up`, `logs --follow`) append one final typed `error` event after
  earlier events instead of buffering.
- JSON failures add nothing to stderr, and exit codes are unchanged.
- Daemon wire failures keep their wire code.
- Process snapshots always contain the full record: `name`, `source`, `argv`, `followers`, and
  identity, readiness, cursors, and errors when applicable. Aggregate `status` uses the same
  `{"processes":[...]}` shape as `list`.

| CLI error code | Meaning |
| --- | --- |
| `usage` | invalid command or flag input |
| `daemon_unavailable` | the daemon cannot be contacted |
| `manifest_missing` | no default manifest; suggests `hum init` or `hum run NAME -- COMMAND` |
| `manifest_invalid` | invalid manifest or project discovery configuration |
| `internal` | unexpected local CLI failure |

### Exit codes

| Command | 0 | 1 | 2 | 3 | Precedence |
| --- | --- | --- | --- | --- | --- |
| `start`, `up` | success | request error or `definition_drift` | readiness timeout | early exit; for `up`, also a declaration left not running by recovery | 1 > 3 > 2 > 0 |
| `restart` | success | request error | `timed_out` | `exited_before_ready` | 1 > 3 > 2 > 0 |
| `wait` | match, or exit without `--match` | request or usage error | timeout | `--match` saw exit first | — |
| `doctor` | no failed check | a failed check or invalid usage | — | — | — |
| `down` | all stopped or nothing running | any stop error | — | — | — |

Ctrl+C during `up` startup exits 130. Foreground `run` returns the child's exit status.

### Completion

`hum completion bash|zsh|fish` prints a script and never edits shell startup files. It is opt-in,
built from the command and flag tree, and never starts a daemon. NAME positions merge the selected
project's declarations with retained runtime records, deduplicated and sorted; a missing daemon
still completes declarations. Manifest or daemon errors return no candidates and no diagnostic.

### Clients

The Herdr plugin is one local client. Herdr owns discovery UI, pane creation, labels, focus, and
terminal lifecycle. Hum owns supervised processes, retained output, lifecycle state, and the
exclusive TTY input lease. The plugin checks `hum version --json`, discovers the scope with
`hum list --json`, keeps the canonical absolute `project_root`, and runs only exact-argv public CLI
commands with an explicit `--project`. It never connects to the daemon socket.

## Command semantics

### `hum init`

`init` reads the project and conservative source candidates without launching anything or starting
the daemon ([source detection](#init-source-detection)).

- With no default manifest it creates `hum.yaml`. When `.hum.yaml` exists it is the selected
  manifest, and plain `init` reports it without changing either file.
- One candidate produces a definition; none or several produce a commented, valid template.
- `--force` renders the full replacement, writes a mode-0600 temporary file in the project
  directory, syncs and closes it, then atomically renames it over the selected regular manifest.
  Symlinks and other non-regular targets are refused.
- Without `--force`, existing files are refused.
- Discovery, rendering, write, sync, close, and rename errors exit 1 and leave the original intact.
- Output includes the path, a `generated`, `template`, or `replaced` outcome, and `hum up` as the
  next command; JSON also lists candidates.

### `hum start` and `hum up`

`start` idempotently ensures named sessions are running. Concurrent starts create at most one
child. A retained stopped ad-hoc record reuses its exact argv, cwd, and environment; a declared
record uses the current definition and client environment. `start` never adds or waits for
prerequisites.

`up` does the same for current declarations, following `after`:

```text
db ──ready──▶ api ──ready──▶ web      roots launch concurrently;
migrate ──exit 0──▶ api               a dependent launches only when every direct
                                      prerequisite is satisfied
```

- A prerequisite is satisfied when this invocation observes it `started` or `already_running`
  with readiness `ready`, or as a `completed` exit-ready setup step.
- A running, ready prerequisite is not relaunched. A retained successful setup step is reused when
  none of its direct dependents needs to launch; otherwise it reruns first. `down` then `up`, an
  explicit `start`, or `restart` reruns it and waits for completion. Completion does not survive
  daemon replacement.
- Each timeout starts at that process's own launch or first running observation, so independent
  roots overlap and the critical path sets the total wait. A CLI timeout overrides the manifest.
- `up NAME...` selects those declarations plus their transitive `after` prerequisites and reports
  only that subgraph. With no names, `up` covers every declaration.
- `up` attempts every entry, emits results in lexical order, and leaves successful children
  running after other failures.
- `--no-wait` returns after spawn. It is rejected before daemon contact when the selected subgraph
  declares `after`.
- An empty manifest does not create a daemon. `up` may still inspect an existing daemon for removed
  manifest records. With none, human output names the manifest (`No processes are declared in
  .hum.yaml.`) and `--json` emits no records.

Blocked dependents:

- Request errors, exits before readiness, timeouts, drift, and earlier skips block a dependent. It
  is reported as `outcome: skipped` with `blocked_by` listing every direct unsatisfied prerequisite,
  sorted. Blockers are direct only, so a cascade names its immediate skipped parent.
- The scheduler waits for all direct results before finalizing blockers.
- Before finalizing, CLI and MCP read the blocked name's retained record without changing it. A
  present record adds `existing_state: running|stopped|exited` and its snapshot; human output says
  `existing process running|stopped|exited` or `not launched`. The result is still a skip.
- Skips do not change exit precedence.

Recovery and drift:

- While an `on-failure` process is backing off, `up` keeps the exited record and reports
  `recovery_pending`, or `recovery_exhausted` after the budget is spent, without starting or waiting
  for a successor. Such a declaration makes `up` exit 3. `hum start NAME` or `hum restart NAME`
  cancels recovery and launches immediately. `up` does not follow a successor; rerun it after
  recovery.
- A changed running, completed setup, or recovery-capable manifest record returns
  `definition_drift` ([compared fields](#definition-drift)) with `hum restart NAME` guidance. Drift
  never satisfies `after`.
- Unnamed `up` reports removed manifest records that are running or recovery-capable as lexical
  `removed_definition` warnings with `hum stop NAME` or `hum remove NAME` guidance. Warnings do
  not change exit status, exclude ad-hoc and discovered records, and require an explicit stop or
  remove. Named `up` reports only its subgraph.

Attached `up`:

- In an interactive terminal, plain `up` subscribes to every selected declaration before launch,
  streams atomic `[NAME]`-prefixed output from that point on, and keeps following after startup.
  It prints `Ctrl+C detaches; hum down stops processes`.
- Ctrl+C after startup detaches with exit 0 and prints
  `detached; processes still running (hum down stops them)`. Detaching never signals a process.
- Ctrl+C during startup aborts with exit 130, stops exactly the declarations this `up` launched,
  leaves ones it found running untouched, and names what it stopped. Gated declarations may remain
  unlaunched; rerun `hum up` to converge. Like `run`, a command stops what it is still bringing up
  and only observes once startup completes.
- If startup fails, exits before readiness, times out, or cannot reach running, attached mode exits
  nonzero instead of following. Launched children stay supervised.
- `--detach`, `--json`, and non-terminal output wait for readiness and return; automation never
  begins an indefinite follow implicitly.

Startup progress (stderr):

- Progress lines follow completion time, not declaration order, and are written whole. Each
  declaration gets at most two: one launch, observation, error, or blocked line, and, only if it
  entered `starting`, one ready, early-exit, or timeout line. Child output is separate.
- Errors read `hum up: NAME: error: MESSAGE`. Blocked lines list sorted direct blockers and say
  whether a record is running, exited, or `not launched`.
- Timeout and early-exit lines include `inspect retained logs: hum logs NAME`.
- Progress never changes scheduling, exit precedence, timeouts, or child lifetime.
- `up --json` writes no progress and keeps stderr empty on success. `up --no-wait`, `start`, and
  MCP `up` keep their bounded output.

### `hum run` and `hum attach`

`run NAME` uses an existing declaration or attaches to an existing running or stopped session.

- `run NAME -- COMMAND` creates an ad-hoc session or replaces a stopped session's retained launch
  spec. The `--` boundary is required; selectors go before it and everything after is child argv.
- A missing manifest does not affect the ad-hoc form; a malformed manifest still fails. An ad-hoc
  run cannot take a declared name.
- Ad-hoc `run` uses the selected directory as the child cwd.
- Foreground `run` launches exactly one incarnation, streams raw output, returns its exit status,
  stops on Ctrl+C or SIGTERM, and detaches on SIGHUP. `run --detach` is daemon-owned.
- In-flight CLI daemon requests observe cancellation (including SIGTERM and SIGHUP). The launch
  handoff has its own bound so a signal still reaches a child launched during it. A stop sent after
  cancellation gets the process's `stop_grace` plus two seconds; cleanup uses the same policy, and
  `down` shares one deadline (longest active grace) across remaining waves. Requests sent before
  cancellation have no CLI deadline because the daemon applies each grace.

`attach NAME` connects a terminal to an existing running session, for example `hum attach console`
or `hum attach console --tail 50`.

- It never starts or restarts a process or daemon.
- For a TTY target it uses the exclusive input lease; raw input and resize events go only to the
  owner. A non-TTY target follows output without input.
- `--tail N` replays the last N retained entries before live output; `--tail 0` skips replay.
- Missing or stopped names return guidance and leave the record unchanged.
- `attach` and `logs --follow` are read-only durable observers.

### `hum restart`

`restart` uses the current definition and client environment. A retained-only ad-hoc record reuses
its exact argv, cwd, and environment; daemon replacement loses ad-hoc definitions, so those cannot
restart.

- After each replacement, readiness is classified the same way as `up`; a process without `ready`
  is `running_unverified`.
- It waits per name by default. `--no-wait` returns after spawn; `--timeout` is a positive per-name
  duration from that name's launch. There is no whole-invocation timeout.
- Readiness failures are reported and later names continue. Request or validation errors stop
  later names.
- Outcomes are `restarted`, `completed`, `running_unverified`, `exited_before_ready`, `timed_out`,
  or `error`, with replacement PID (0 if none), launch cursor, readiness, and optional message, in
  input order.

For restart-with-work, `stop`, run the intermediate command, then `start`: the session keeps its
followers.

### `hum stop`, `hum remove`, `hum down`, `hum shutdown`

- `stop` stops a process and keeps its session and output.
- `remove` stops the child, closes followers, and discards launch state and output. It never edits
  the manifest. `remove --all` removes every retained runtime record in the selected project or
  global scope, lexically; it never spans scopes or touches unlaunched declarations. A name plus
  `--all` is rejected. The follower count never changes `remove` behavior.
- `down` is the project inverse of `up`. It stops declared processes in reverse `after` order: a
  prerequisite waits until every active dependent's stop request finishes. Each wave runs
  concurrently; independent roots and ad-hoc or undeclared records have no edges. Inactive records
  do not delay waves, and a failed dependent stop does not block prerequisites. Declared names with
  no record report `not_running`. Results are one name-sorted `stopped`, `not_running`, or `error`
  per name.
- `down` never starts or shuts down the daemon, touches other projects, or deletes records. With no
  daemon or names it prints `Nothing is running in this project.`
- `shutdown` stops the daemon for all projects. It refuses while any process is active unless
  `--stop-processes` is given.

### `hum status` and `hum list`

- `status` and `list` merge current declarations with runtime records. Without a daemon,
  declarations show as stopped.
- `list --all` covers every scope and merges unlaunched declarations for the selected project.
- `status`, `logs`, `wait`, `restart`, `stop`, and `remove` accept declared and ad-hoc names.
- When the recorded leader has exited but its process group lives on, the state is `descendants`:
  PID is 0 and the PGID stays the lifecycle barrier. The snapshot turns terminal only when the
  descendants exit.
- Terminal snapshots keep an autonomous exit status. A signal death is `exit_status: -1` with a
  `signal` object such as `{"name":"SIGTERM","number":15}`, carried by `list`, `status`, `up`, and
  `wait` (human and JSON) and MCP. Other exits omit `signal`.
- Operator-stopped snapshots stay `stopped` without exit details, distinct from a signal death.
- `followers` counts live attached-run and follow clients the daemon holds open, including ones
  waiting before launch or on a stopped session. It is not persisted and is 0 with no session.

### `hum logs`

```sh
hum logs web --tail 50                        # newest 50 entries
hum logs web --after-cursor 120               # page forward from cursor 120
hum logs web --since 5m --stream stderr       # recent stderr only
hum logs web --match panic --context 3        # matches with 3 entries either side
hum logs --follow                             # every declaration, live
```

- Without `--after-cursor`, bounded reads return the newest default window (same as the default
  `--tail`). `--after-cursor` without `--tail` pages forward from the oldest eligible entry.
- `--stream system` selects only Hum's supervision entries. The default `both` includes stdout,
  stderr, and system, for bounded and follow reads.
- `--since DURATION` must be a positive valid duration and captures one inclusive request-time
  cutoff shared by every name. Invalid, zero, negative, or overflowing values are rejected before
  daemon contact. With `--follow`, it filters the initial replay only.
- Selection order from one immutable snapshot: cursor, since, and stream pick eligible entries;
  `--match` with `--context N` adds up to N eligible entries on each side, merging overlapping or
  adjacent windows in cursor order without duplicates; then tail; then whole-entry and byte limits.
- Context never crosses cursor, time, stream, retention, or captured-latest bounds. It requires a
  non-empty `--match`, 0 means match only, and it is rejected with `--follow`.
- A forward page consumes unselected entries, but `next` stops just before the first selected entry
  that did not fit. Passing `next` back as `--after-cursor` loses and repeats nothing.

Several names:

- Names are optional and repeatable, in selection order; duplicates are rejected.
- With no names, `logs` resolves the current declarations once, in lexical order, without ad-hoc
  sessions, and membership does not change later.
- `--after-cursor` is rejected before daemon startup for more than one name.
- Filters, tail, context, and limits apply per name. Human entries are atomic `[NAME]`-prefixed
  writes; JSON uses named NDJSON events. A single name keeps the single-process output shape.
- Aggregate follow opens one follower per name, serializes writes, reports per-session errors by
  name without stopping the others, and cancels everything on daemon loss or output failure.
  Ctrl+C closes all followers and never signals a process.

### `hum wait`

- `wait` defaults to the current launch cursor and 30 seconds. With no explicit cursor on a stopped
  or unlaunched process, it waits for the next incarnation.
- Timeout results include `process_observed`, recorded during the same request: `true` if a
  matching record existed at the start or appeared and later stopped or was removed, `false` if none
  was seen. Human output for `false` adds `no process named "NAME" was observed during the wait;
  check the name or start it first.` Undeclared names can still be waited on for a future launch.

### `hum input`

`input` sends one payload to a running TTY session:

- `--text` sends exact non-empty bytes with no added newline. `--base64` accepts only standard
  padded base64 without whitespace. Payloads are 1–32768 bytes.
- It attaches only to the initial running incarnation, writes once at that launch cursor, and
  releases the lease before returning.
- It is at-most-once: a launch race or lost acknowledgement is reported, never resent.
- It never starts a daemon or process, waits for launch, queues, retries, retains, or echoes bytes.
- A stopped target returns `session_not_running`; a non-TTY target returns `input_not_tty`;
  ownership, closed-session, and stale-cursor races return the existing input error codes.

Prompt loop:

```text
wait --match "prompt" ──▶ input ──▶ wait --match "complete"
```

### `hum signal`

`signal NAME SIGNAL` sends one signal to a running process group.

- Signal names are case-insensitive with optional `SIG`. Positive numbers are accepted only when
  they map to the OS's supported names: `HUP`, `INT`, `QUIT`, `TERM`, `KILL`, and `USR1`/`USR2`
  where available.
- Result: one human line, or `{"name":"NAME","signal":{"name":"SIGHUP","number":1},"status":"sent"}`
  in JSON and MCP.
- Errors: `invalid_signal`, `not_found`, `not_running`.
- Signals are observational: even TERM and KILL never set stop intent or cancel automatic relaunch.
  Only `stop`, `down`, and `restart` control lifecycle policy.

### `hum doctor`

`doctor` inspects one filesystem project and rejects `--global`. In order, it checks the OS, Hum
settings, runtime path, the manifest, environment files and protocol limits, each process and
`ready.exec` executable with its exact cwd and environment, `ready.http`/`ready.tcp` syntax
(`readiness_http`/`readiness_tcp`), and any existing daemon's handshake.

- It never starts the daemon, runs argv or probes, connects to network targets, repairs files, or
  keeps state. A missing daemon is `INFO`; an incompatible or unreachable socket fails.
- It reports the active filename in `project.manifest` details and adds
  `shadowed_manifest: "hum.yaml"` when `.hum.yaml` shadows it; that is still a pass.
- JSON is one object with `ok`, ordered `checks`, and `summary`. Output never includes environment
  values, whole environments, or key lists.

## Definitions and resolution

### Manifest selection

```text
--file PATH  >  nearest .hum.yaml / hum.yaml  (first directory with either wins; never merged)
```

- Default search walks from the invocation directory (or `--project DIR`) up to the project root,
  inclusive, never above it. In each directory, `.hum.yaml` wins over `hum.yaml`; the nearest
  directory with either file is authoritative.
- Every file is complete; Hum has no overlays, inheritance, or merging. Alternates are
  conventionally named `hum.dev.yaml`, `hum.test.yaml`, and so on.
- A malformed, unreadable, unsafe, or non-regular `.hum.yaml` fails closed; Hum does not fall back
  to `hum.yaml`. A Git-ignored `.hum.yaml` is private and not shared with collaborators or CI.
- `up` and `start` resolve the manifest before starting or contacting the daemon. With no default
  manifest they return `manifest_missing`, naming the search directory and project root when they
  differ, and suggesting `hum init` or `hum run NAME -- COMMAND`; the runtime directory stays
  untouched. Ad-hoc `run NAME -- COMMAND` and runtime-only commands work without a manifest.
- Runtime resolution never auto-discovers commands; only `init` does.
- Human `up` reports a selected nested manifest's project-root-relative path on stderr; JSON and
  root-manifest output remain unchanged.
- Records carry `source: manifest:<project-root-relative-path>`, such as `manifest:.hum.yaml` or
  `manifest:hum.yaml`.
- All manifests in a project share the `(project root, process name)` namespace. The manifest's
  directory is the default child cwd and the base for relative `cwd`; the resolved cwd must remain
  inside the project root.
- Runtime-only commands stay project-wide. A file selector there only identifies the project; it is
  not parsed and does not filter records.
- A valid empty manifest has no definitions; an invalid one is an error.

SchemaStore-aware editors load [`hum.schema.json`](../hum.schema.json) for `hum.yaml`, `hum.*.yaml`,
`*.hum.yaml`, `hum.yml`, and `*.hum.yml`. `hum init` writes the inline schema directive first so
other YAML editors pick it up; others can select the schema manually. The schema is an editor aid;
the Go parser is authoritative.

### Process fields

See [`hum.example.yaml`](../hum.example.yaml) for a complete example.

| Field | Rule |
| --- | --- |
| name (key) | required; safe name; unique |
| `argv` | required; non-empty list of strings; run directly, never through a shell |
| `cwd` | optional; relative to the manifest directory; must exist and stay beneath the project root after lexical and symlink resolution |
| `ready` | optional; exactly one [readiness method](#readiness) |
| `after` | optional list of unique same-manifest names that declare `ready`; no self-reference or cycles |
| `restart` | `never` (default) or `on-failure` |
| `stop_grace` | optional duration between SIGTERM and SIGKILL; omitted inherits the daemon default (10s); `0s` kills immediately |
| `tty` | optional boolean; default false |
| `env` | optional map of literal strings or `null` |

Parsing is strict and single-document. Unknown or duplicate keys, YAML aliases or merges,
unsupported versions, invalid names, regexes, or durations, unsafe cwd, empty or non-string argv,
shell text, malformed or unknown `after` entries, dependencies without `ready`, and cycles of any
length are errors with file, process, and indexed-field context such as `process "web".after[1]`.
Non-string `restart` values are rejected. Definitions are name-sorted.

`stop_grace` is kept as the effective value in each snapshot, with `stop_grace_inherited`. Restarts
adopt a changed value; automatic relaunches keep the admitted value; orphan reclaim uses the daemon
default.

### Readiness

Readiness gates startup, `after`, and launch commands. It is not health monitoring.

| Method | Ready when | Options |
| --- | --- | --- |
| `match: REGEX` | a stripped output line matches | `timeout` |
| `exec: [argv...]` | the command exits 0 | `interval`, `timeout` |
| `http: URL` | GET returns 2xx | `interval`, `timeout` |
| `tcp: HOST:PORT` | a connection is accepted | `interval`, `timeout` |
| `exit: 0` | the process itself exits 0 | `timeout` only |

- `timeout` defaults to 30s; `interval` defaults to 1s and must be positive.
- Probes run immediately after launch, then serially after each failure.
- `exec` runs exact argv without a shell, inheriting the process cwd and launch environment. Probe
  output is never stored in the process log; only one bounded last-attempt diagnostic is kept.
- `http` and `tcp` run in-process. Targets are absolute HTTP(S) URLs or `host:port` with a literal
  IP or `localhost` (bracket IPv6). Each attempt is bounded to 1s and the remaining timeout. They
  inherit no environment, never expand variables, follow no redirects, keep one bounded
  status/dial diagnostic, and cancel on stop, restart, or shutdown.
- `exit: 0` makes a setup step ready on success. `up` reports it as `completed`. A nonzero exit,
  signal, or stop before completion blocks dependents.
- A launch records its readiness expression, launch cursor, and first matching cursor even when
  nobody is waiting. State moves from `starting` to `ready` and survives output eviction.
  A relaunch resets readiness at the new launch cursor, so old output never satisfies it.
- Processes without `ready`, including discovered definitions, report `running_unverified` and are
  never ready.
- Ad-hoc records have no readiness. Terminal manifest records keep their method, matcher or exec
  argv, interval, and diagnostic so drift can still be classified.

### Definition drift

A running, completed setup, or recovery-capable manifest record whose definition changed returns
`definition_drift` from `start` or `up` instead of being replaced. Only `restart` adopts changes.
`changed_fields` is sorted.

| Compared (drift) | Not compared (wait policy) |
| --- | --- |
| argv, canonical cwd, TTY, normalized `restart`, `stop_grace` | environment |
| readiness method (`readiness_exec`, `readiness_http`, `readiness_tcp`, `readiness_exit`), match, exec argv, HTTP/TCP target | readiness `timeout`, exec `interval` |

### Environment

```yaml
environment:
  inherit: true      # default; false starts empty
  files: [.env]      # required, applied in order
processes:
  api:
    env:
      PORT: "3001"
      OLD_URL: null  # unset
```

```text
caller environment ──▶ files, in order ──▶ processes.NAME.env      (later wins)
```

- Later values replace earlier ones, inherited duplicates use their last value, and `null` unsets a
  key. Active composition sorts final entries by key.
- With no environment configuration, the caller baseline passes through byte-for-byte, including
  order, duplicates, and opaque names. That default depends on the caller and can carry stale
  values.
- YAML `env` values are literal strings; quote numbers and booleans.

Environment file syntax:

| Allowed | Rejected |
| --- | --- |
| UTF-8, LF or CRLF, optional final newline | BOM, NUL, invalid UTF-8 |
| blank lines, full-line comments | physical multi-line values |
| optional `export` and horizontal whitespace, spaces around `=` | malformed quotes, unsupported escapes |
| empty values; first `=` splits | duplicate names |
| unquoted values (edges trimmed; `#` starts a comment at value start or after whitespace) | `$NAME`, `${...}`, `$()`, backticks |
| whole single-quoted literal values | includes, interpolation, discovery |
| whole double-quoted values with `\\`, `\"`, `\n`, `\r`, `\t`; only whitespace and a comment after | |

Hum never evaluates, decrypts, includes, or discovers environment files. Single-quote literal text
that looks like expansion, or use an external loader. This is not a dotenvx or direnv contract.

- Paths resolve from the selected manifest's directory (including `--file`). `../.env` is fine
  while it stays inside the canonical project root. Empty, absolute, root, lexical or symlink
  escapes, missing, non-regular, and unreadable paths fail.
- Manifest parsing checks syntax and containment without reading files. Launch preflight reads each
  unique canonical file once per invocation and reuses that snapshot per target. There is no cache
  across requests or worktrees and no global environment mutation.
- CLI and MCP never activate mise, nvm, direnv, or shell hooks.

| Limit | Value |
| --- | --- |
| file entries | 16 |
| bytes per file | 1 MiB |
| assignments per file | 4,096 |
| final `KEY=VALUE` bytes, including NUL separators | 4 MiB |
| encoded request line | 8 MiB |

### Init source detection

`hum init` reads supported root-level conventions without running task bodies or launching
anything. Exactly one candidate becomes `dev`, rooted at the project, with no readiness:

| Source | Required `dev` entry | argv |
| --- | --- | --- |
| Mise | local task | `mise run dev` |
| Task | task | `task dev` |
| Just | public recipe | `just dev` |
| Make | literal, non-pattern target | `make dev` |
| package.json | script | `<manager> run dev` |
| deno.json(c) | task | `deno task dev` |
| composer.json | script | `composer run-script dev` |
| bin/dev | executable file | `./bin/dev` |
| Mix | literal `{:phoenix, ...}` dependency in `mix.exs` | `mix phx.server` |

- Detectors are bounded and read-only and never evaluate repository code. Mix detection reads
  `mix.exs` as text, ignores comments and quoted values, and recognizes only a literal Phoenix
  tuple. Dynamic declarations fail closed and need an explicit `hum.yaml`.
- package.json: `packageManager` selects bun, pnpm, yarn, or npm (ignoring a version suffix) and
  rejects other or non-string values. Otherwise exactly one lockfile family decides: Bun
  (`bun.lock`, `bun.lockb`), pnpm (`pnpm-lock.yaml`), Yarn (`yarn.lock`), or npm
  (`package-lock.json`, `npm-shrinkwrap.json`). Several files of one family are fine; conflicting
  families are errors; no lockfile means npm.
- No nested packages are scanned, nothing beyond these sources is inferred, and commands are never
  tried to see what works.
- No candidates is a typed `NoCandidateError`; several is an `AmbiguityError` listing every source;
  malformed sources are `ConfigurationError`. All wrap their sentinel for `errors.As`. Caller
  cancellation propagates unchanged.
- The template includes an inert commented `restart: on-failure` example.

## Readiness and output

The daemon launches exact argv without a shell. On Unix each child gets its own process group and
stdin at `/dev/null`. `stop` and `down` send SIGTERM, wait the grace period, then send SIGKILL.
Client and follower disconnects never stop children.

### Cursors and entries

Each named session has one cursor sequence across stdout, stderr, and incarnations. An entry has a
stream, timestamp, raw stored text, and cursor.

| Field | Meaning |
| --- | --- |
| logs `next` | last source cursor consumed by this read; pass it to `--after-cursor`/`after` |
| process `next_cursor` | the next cursor that will be assigned |

The two fields are intentionally different and are not renamed in MCP.

### Terminal-control stripping

`StripTerminalControl` is the single byte-wise definition of stripped text. Per child
stdout/stderr entry it removes recognized terminal control sequences (introduced by ESC or a UTF-8
C1 control) and a CR immediately before LF.

- Stripped text is used by bounded `logs` (including JSON, tail, and match context), MCP `logs`,
  `logs --match`, `wait --match`, readiness matches, and ring predicates.
- Stored bytes and system entries stay raw. `logs --follow --match` selects with stripped text but
  prints raw entries; follow and attached `run` render raw.
- Patterns with raw ESC bytes no longer match stripped text; `^` matches colored output whose raw
  first byte is ESC.
- A control-only entry stays present with empty text.
- There is no `--raw` flag or other opt-out.
- This is not terminal emulation: a sequence split across entries can leave its tail visible, and
  carriage-return redraw frames stay separate.

### Retention and followers

- Read limits (`MaxBytes`, `--limit-bytes`) count text bytes only. Retention (`--output-bytes`)
  charges each entry `len(text)+128` bytes, which also bounds entry count. It is not an exact RSS
  cap. Cursors and entry limits use raw stored lengths.
- Eviction is reported explicitly. A live follower waiting before launch or on a stopped session
  keeps that session from completed-record eviction.
- Attached `run` and `logs --follow` may start before the first launch, replay retained output,
  print exit, wait, and launch boundaries, and stay open across stop/start and down/up until Ctrl+C,
  removal, or transport loss. Ctrl+C detaches only the observer.

### Crash relaunch

`restart: on-failure` retries a non-zero or signal exit:

```text
exit ─▶ 1s ─▶ 2s ─▶ 4s ─▶ 8s ─▶ 16s ─▶ gave up      (at most 5 automatic attempts)
```

- Discovered and ad-hoc sessions are always `never`.
- Exit 0 and operator controls (`stop`, `down`, `restart`, `remove`, `shutdown`, manual `start`)
  cancel and reset the loop. A child alive for 30 seconds resets the counter.
- A spawn failure consumes an attempt and appends a bounded `relaunch failed: ...` system entry.
- A generation token and supervisor lock order exit, timer claim, and operator intent, so stale
  timers never launch and a manual start or restart wins without creating two children.
- Automatic attempts reuse the last argv, cwd, environment, readiness, TTY, and stop grace and never
  reread the manifest; restart explicitly after editing. Readiness and client timeouts do not
  trigger relaunch.
- Snapshots (CLI JSON and MCP) include `restart`, `relaunches`, and optional whole-second
  `next_launch_at`.
- Pending and exhausted records resist completed-record eviction. They keep the response-safe
  readiness configuration and diagnostic, without environment, for drift classification.
- Followers stay attached through exit, backoff, and exhaustion; logs keep the failures and the
  `relaunching` and `gave up` system entries. Read the failing incarnation's logs before editing
  again.

## Daemon and environments

One daemon serves each runtime directory at `hum.sock`.

- Directories Hum creates are mode 0700. A pre-existing directory keeps its mode; Hum allows
  group/other read and execute but refuses one they can write or another user owns, before opening
  anything in it. The `/tmp/hum-UID` fallback can be pre-created by anyone, so it fails closed.
- Both ends check peer credentials (`SO_PEERCRED` on Linux, `LOCAL_PEERCRED` on macOS). A client
  refuses a daemon run by another user before sending environment or input; the daemon closes a
  connection from another user before reading a request.
- `serve --daemon`, `run`, `start`, `up`, `logs --follow`, and `wait` use a startup lock and
  readiness handshake. Bounded reads and controls never start an empty daemon.
- Foreground daemon exit and `shutdown --stop-processes` stop all managed groups.
- `HUM_STOP_GRACE=0s` is an explicit immediate kill, not a request for the 10s default.

### Durable state and reclaim

The mode-0600 `hum.state` file atomically records the daemon incarnation and each live group's
project, name, leader PID, PGID, and OS process-start identity. A launch is not reported successful
until its identity is durable.

- On startup, a dead daemon's groups are reclaimed with TERM, the configured grace, then KILL,
  only when PID, group leadership, and start identity all match. A zero grace kills immediately.
- Auto-start allows two grace periods per recorded group, sequentially, plus five seconds for setup
  and the handshake. Caller cancellation still bounds it, and a child that exits before signaling
  readiness fails the start at once.
- Dead groups are dropped. Mismatched or unverifiable identities are never signaled and block the
  same project and name.
- The reconciliation summary stays visible for the daemon's lifetime in human warnings and
  JSON/MCP `warnings`.
- Graceful shutdown removes `hum.state`; corrupt state fails closed with cleanup guidance.

The runtime directory holds the socket, PID/startup/readiness files, durable group state, bounded
daemon diagnostics, and private per-scope [event history](#event-history).

### Launch environment

The client supplies cwd and its baseline environment: the CLI takes one `os.Environ` snapshot; MCP
takes one `Options.Environment` snapshot (falling back to `os.Environ`) per request.

- Declared `start`, `up`, declared `run`, and `restart` compose the environment once before
  contacting the daemon. `up` covers its whole dependency closure, and any failure aborts a mixed
  declared/retained batch before changing anything. Required files are validated even for a running
  record that will be kept.
- Empty `up`, ad-hoc and retained-only commands, discovered definitions, explicit-argv `run`, and
  read-only commands never read environment files.
- `up` keeps running, pending, and exhausted snapshots. `start` keeps running entries but composes
  fresh when reviving pending or exhausted ones (unless other drift blocks it). Retained-only
  start/run/restart, automatic relaunch, and `ready.exec` reuse the saved snapshot. Declared
  `restart` reloads, including clearing a removed configuration.
- MCP resolves files from each request's project root and manifest, so worktrees never share
  snapshots.
- Manifest `cwd` is relative to its manifest directory and changes only the child directory;
  discovered definitions use the project root.

Privacy:

- Environment names and values are never returned or listed in metadata, status, JSON, or MCP, and
  protocol responses forbid an `env` key. There is no environment hash or drift field.
- Diagnostics may name a valid key, path, process, and line, never a value, raw input, or a decode
  error containing one. YAML syntax errors report only the line number and never forward parser
  text.
- User argv and runtime errors are outside that guarantee. Child output is unredacted, and a
  failing `ready.exec` diagnostic holds bounded untrusted stdout/stderr that can appear in status,
  JSON, or MCP. Commands and probes should not print secrets.

## Event history

`hum events [NAME...]` reads recent durable service history (starts, exits, failures, operator
actions) without a manifest or running daemon. MCP `events` returns the same bounded page and cannot
follow.

```sh
hum events api --kind lifecycle --failed
hum events --since 10m --match timeout
hum events --after-cursor 42 --json
```

- Filters: repeatable `--kind lifecycle|operation`, `--failed`, `--match REGEX`, `--since DURATION`,
  `--tail N`, `--after-cursor N`.
- JSON emits one record per event, then metadata with `next_cursor`, `truncated`, and `has_more`
  ([format](cli-json-v1.md#event-history-records)).
- Human output fits the terminal width (80 columns if unknown), elides detail first, and colors only
  event words under the usual color policy. `--full` prints complete multi-line details and any
  `log_cursor` on the detail line.
- Lifecycle `launch` carries an optional `log_cursor` immediately before its incarnation's output;
  `exit` carries the latest output cursor at exit. Pass either to `hum logs NAME --after-cursor N`
  (MCP `logs` `after`) to read entries after it. Omitted means no entry exists; 0 is a valid cursor.
  Other events have no `log_cursor`.
- Cursors do not carry across `hum remove` or daemon replacement: output is kept in memory and
  lost, while event history survives and new sessions restart cursors. Compare returned log entry
  `time` against event `time` when in doubt.

Storage:

- Per scope, readable up to the newest 2,000 records or 1 MiB, with 16 KiB records. Disk appends
  compact after 4,000 records or 2 MiB.
- History survives daemon replacement but not runtime-directory cleanup.
- Cursor reservations persist before payloads in blocks of 64, so a crash can skip cursors but never
  reuse them. Clean daemon shutdown releases the unused reservation, so a restart continues without
  a gap. Unreadable cursor metadata makes only that scope's history unavailable; control
  operations continue.
- A torn tail keeps its complete prefix. A malformed payload reads as empty and is diagnosed once
  per scope per daemon lifetime.
- Environment values, input, child output, argv, and unbounded raw errors are never stored.

### Why history is paged, not streamed

Adapters such as the Herdr picker need current state, not every transition. Each picker opening
reads a fresh `hum list --json`, and each action is a separate command that revalidates the named
process. A stale snapshot can offer an awkward action (Attach just after an exit) but never
authorizes a wrong one; the command applies to current state or returns an error. Fast transitions
missing from two snapshots still show their result in the next one. For a known process,
`logs --follow`, `logs --after-cursor`, and `wait --after-cursor` already give live output and
replay.

Measured on a warm workstation, full discovery (`version --json` plus `list --json`, 50 samples)
took 11.86/12.54 ms median/p95 for 1 process, 12.19/12.84 ms for 25, and 13.16/14.08 ms for 100.
`list --json` alone at 100 processes took 7.40/7.78 ms. That is cheap enough for on-demand refresh.

Rejected: rapid polling (unneeded work), a project-wide live stream (retention and reconnect
obligations with no demonstrated need), direct daemon access, and plugin callbacks inside Hum
(boundary violations).

Reconsider only when a real adapter shows that a correct operation cannot be recovered by
command-time validation plus a fresh snapshot, that every transition must be observed across
disconnects, or that p95 discovery exceeds 100 ms for 100 processes with a refresh need of one
second or less. Record the reproducer and measurements first. Any future contract must be local,
bounded, versioned, and out-of-process: project-scoped pages after an opaque cursor, with total
order, retention limits, explicit truncation for an old cursor, the next cursor on every page,
reconnect after the last committed cursor, and cursors that survive daemon restart or are explicitly
invalidated. Slow consumers page or see truncation; the daemon never queues for them.

## MCP adapter

`hum mcp` serves JSON-RPC over stdio with thirteen tools: `start`, `up`, `down`, `list`, `status`,
`logs`, `events`, `wait`, `input`, `restart`, `stop`, `remove`, and `signal`. There is no `run`,
`serve`, `shutdown`, follow, arbitrary-command tool, HTTP transport, authentication, or remote
access. The adapter uses a protocol-shaped daemon client and builds no supervisors or output stores
in-process.

Scope and arguments:

- Every tool accepts `scope`. `project` (default) requires an absolute existing `project_root`,
  resolved to its nearest Git root or the directory itself. `global` rejects `project_root` and
  addresses retained ad-hoc sessions.
- `up` is project-only. `list` with `all: true` is project-only and includes global records.
- Each tool rejects fields outside its closed input schema before project resolution or daemon
  contact. `tools/list` descriptions are brief; output schemas contain no prose.
- `start`, `up`, `restart`, and `list` accept optional `manifest`. A relative path resolves from
  `project_root`; an absolute one must stay inside it. It loads exactly that file; omitted, the
  usual default order applies. `manifest` is rejected (`invalid_request`) by `down`, `status`,
  `logs`, `wait`, `input`, `stop`, `remove`, `signal`, and every global call.
- `start` and `restart` fall back to a retained record when the manifest lacks the name. `list`
  merges stopped declarations with retained records; retained records win by name.
- `up` accepts optional `names` (unique, non-empty, declared) and `no_wait`. `down` accepts no
  `name`.

Behavior matches the CLI:

- `up` uses the same `after` scheduler, lexical results, `blocked_by`, drift, and
  `removed_definition` warnings. `no_wait: true` is rejected when any dependency is declared.
- `logs` accepts `stream` (`stdout`, `stderr`, `system`, `both`), `tail`, `after`, positive
  `since_ms`, `match`, and `context`, with the CLI's ordering and paging. Invalid `since_ms` is
  rejected before daemon contact.
- `restart` accepts `no_wait` and positive per-name `timeout_ms` and returns `name`, `outcome`,
  `readiness`, `pid`, `launch_cursor`, and optional `message`.
- `input` accepts one non-empty `text` or `base64` and returns `name`, decoded `bytes`, and
  `launch_cursor`.
- `signal` returns the same result object and errors as the CLI.
- In process results, `argv` is null before the first launch and `stop_grace` is in nanoseconds.
  `status` and `list` report `tty` and the same `followers` count. Recorded environments are never
  returned.
- Only `start` and `up` may create or replace a daemon; without a manifest they cannot start
  unknown names. Ad-hoc definitions from `hum run` disappear when the daemon is replaced. With no
  daemon, `list` reports stopped declarations, `stop` and `down` succeed, and other controls return
  daemon-unavailable errors.

Concurrency:

| Situation | Result |
| --- | --- |
| up to 64 requests with IDs | run concurrently |
| 65th in-flight request | rejected with `-32001`, not started |
| duplicate in-flight ID | rejected with `-32600` |
| notifications and incoming responses | use no slots |
| `notifications/cancelled` for an in-flight ID | that request returns `-32800` |
| cancellation for an unknown ID | ignored |
| stdin EOF or parent cancellation | cancel all requests, wait ≤1s for handlers, close the response transport, join the writer, return within 2s with no goroutines left |
| request abandoned at shutdown | tool error `cancelled` (distinct from `internal`, which is reserved for unexpected adapter failures) |

Responses are serialized through the Serve-owned closeable response transport.

## Optional pseudo-terminals

Set `tty: true` (or `hum run NAME --tty -- COMMAND`) only when a tool needs a controlling terminal.
Without it, stdin is `/dev/null` with separate stdout/stderr pipes.

- The daemon owns the PTY master, launches a session leader with `Setsid`/`Setctty`, and signals its
  process group on stop, restart, down, remove, and forced shutdown.
- PTY output is merged once as raw retained `stdout`; reads and matches apply the usual stripping.
- Exactly one attached client owns input. A second attachment and every `logs --follow` get output
  only. The owner alone forwards SIGWINCH resizes.
- Ctrl-] releases input and ends that `attach` or foreground `run`; the child keeps running. Local
  raw mode is restored on detach, panic, and transport loss. Echo is child output, and input is
  discarded while stopped.
- While a foreground TTY `run` owns input, Ctrl-C goes through the PTY. SIGTERM stops the
  incarnation; SIGHUP detaches without signaling the child.
- Stop and restart keep the lease across successors; remove and daemon shutdown close it.
- One-shot [`input`](#hum-input) is scoped to the initial running launch cursor; state events identify
  stopped and running successors.

## Canonical project scopes

Scope comes from the invocation directory. Git roots and linked worktrees are canonicalized
physically, so symlink aliases share records while separate worktrees do not. Child cwd stays
lexical.

```sh
hum --project /path/to/main status     # another checkout (-C)
hum --global run proxy -- caddy run    # machine-wide ad-hoc session (-g)
hum list --all                         # every scope, including a global group
```

- `--global` is only for machine-wide ad-hoc retained sessions. It never changes child cwd, reads a
  manifest, or falls back across scopes. It works before or after lifecycle commands and before or
  after `run` NAME (before `--`).
- `--global` conflicts with `--project` and `list --all`; `init`, `up`, and `doctor` reject it.
- Global `start` and `restart` reuse only a retained launch spec; child cwd remains the lexical run
  directory.
- `list --all` shows a `global` group with `hum --global` selectors. Project misses suggest a global
  match, such as `hum --global logs proxy`.
- JSON records carry `scope` (`project` or `global`); global records omit `project_root`.
