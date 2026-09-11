# hum design

## Scope

Executable readiness is a startup gate: `ready.exec` is a non-empty exact argv executed directly without a shell. The first probe is immediate, then failed attempts retry serially after a positive `interval` (default 1s) until `timeout` (default 30s). Probes inherit cwd and environment; only one bounded terminal diagnostic is retained. This is not liveness monitoring. Method/argv changes report `readiness_exec`; interval and timeout are wait policy.

hum is a local process supervisor for humans and coding agents.

- A private Unix-socket daemon owns named process groups and bounded output independently of
  clients.
- CLI and MCP clients resolve project definitions and send exact argv to that daemon; they never
  create another supervisor or reconstruct shell text.

Process names are scoped to the nearest Git root, or the caller's working
directory when no Git marker exists. macOS and Linux are supported; Windows is
not.

## Shared orchestration

`internal/orchestrate` owns scheduling, readiness, recovery, drift, removal, and skip classification. CLI and MCP adapt daemon snapshots and render the same model.

Manifest processes may override the daemon SIGTERM-to-SIGKILL window with `stop_grace`. Omission inherits the daemon default and explicit `0s` remains distinct. The admitted effective value is retained in each process snapshot; restarts adopt the current definition, automatic relaunches retain the admitted policy, and orphan-group reclaim uses the daemon default.

```text
CLI ─┐                       ┌─> launch order
     ├─> internal/orchestrate ├─> readiness
MCP ─┘                       └─> stable outcomes
```

## CLI

```text
hum version [--json]
hum [--project DIR|-C DIR] init [--force] [--json]
hum serve [--daemon]
hum [--project DIR|-C DIR] start <name>... [--no-wait] [--timeout DURATION] [--json]
hum [--project DIR|-C DIR] up [--detach] [--no-wait] [--timeout DURATION] [--json]
hum [--project DIR|-C DIR] down [--json]
hum run [--project DIR|-C DIR] <name> [--detach] [--tty] [--json] [-- <command> [args...]]
hum [--project DIR|-C DIR] list [--all] [--json]
hum [--project DIR|-C DIR] status [<name>] [--json]
hum [--project DIR|-C DIR] attach <name> [--tail N]
hum [--project DIR|-C DIR] logs [<name>...] [--stream stdout|stderr|system|both] [--tail N] [--after-cursor N]
           [--since DURATION] [--limit-bytes N] [--match REGEX] [--context N] [--follow] [--json]
hum [--project DIR|-C DIR] wait <name> [--after-cursor N] [--match REGEX] [--timeout DURATION] [--json]
hum [--project DIR|-C DIR] input <name> (--text TEXT | --base64 PADDED_VALUE) [--json]
hum [--project DIR|-C DIR] signal <name> <signal> [--json]
hum [--project DIR|-C DIR] restart <name>... [--no-wait] [--timeout DURATION] [--json]
hum [--project DIR|-C DIR] stop <name>... [--json]
hum [--project DIR|-C DIR] remove <name>... [--json]
hum shutdown [--stop-processes] [--json]
hum mcp
hum skill
hum completion bash|zsh|fish
```

Short aliases are command-local except the global help and version aliases. Project-scoped commands also accept the persistent `-C DIR` alias for `--project DIR`; the selector may appear before or after the subcommand, and `run` accepts it after the process name before `--`.

`completion bash`, `completion zsh`, and `completion fish` print installable shell scripts.

- Completion is opt-in, uses the assembled command and flag tree, and never starts a daemon;
  NAME positions query only the merged declaration/runtime names for the selected project.
- Manifest or daemon errors return no candidates and no diagnostic.

Long options remain canonical in documentation, scripts, output, and errors.
Combined short options are unsupported; MCP fields have no aliases.

| Alias | Long option | Commands |
| --- | --- | --- |
| `-h` | `--help` | global |
| `-v` | `--version` | global |
| `-C` | `--project` | project-scoped commands |
| `-j` | `--json` | all supporting commands |
| `-d` | `--daemon`, `--detach` | `serve`, `run`, `up` |
| `-t` | `--timeout` | `start`, `up`, `wait`, `restart` |
| `-a` | `--all` | `list` |
| `-s` | `--stream` | `logs` |
| `-n` | `--tail` | `attach`, `logs` |
| `-c` | `--after-cursor` | `logs`, `wait` |
| `-b` | `--limit-bytes` | `logs` |
| `-m` | `--match` | `logs`, `wait` |
| `-f` | `--follow` | `logs` |

`--force`, `--since`, `--no-wait`, `--tty`, `--stop-processes`, `--runtime-dir`,
`--stop-grace`, `--output-bytes`, and `--completed-records` remain long-only. The `input`
command intentionally adds no short aliases, including for `--json`.

`--project DIR` resolves DIR relative to the invocation directory, canonicalizes it to a
physical absolute path, and applies the nearest-Git-root-or-directory-fallback rule. Launch
commands require an existing directory; observation and lifecycle commands can target a removed
worktree when DIR exactly matches a canonical root retained by the daemon.

- The resolved project root scopes names and manifests.
- `status` without a name renders a compact current-project process table and includes unlaunched
  manifest declarations; `status <name>` retains the full single-process detail view. A resolved
  declaration without a daemon record is reported as stopped, even when no daemon is running.
  Aggregate JSON uses the same `{"processes":[...]}` collection shape as `list`.
- An ad-hoc `run` keeps the selected DIR as the child cwd; a manifest definition keeps its
  declared root-relative `cwd`.
- `init` writes at the resolved root, and `list --all` uses the selected project while merging
  unlaunched declarations.
- Guidance and stable next-command fields preserve a canonical shell-safe absolute `--project`
  selector, including paths with spaces.
- `version`, `serve`, `shutdown`, `mcp`, and `skill` reject explicit project and global selectors
  because their scope is build-static, daemon-global, request-scoped, or static.
- Command-local `-d` remains `serve --daemon` and `run --detach`, and now also selects `up --detach`.

Human-readable output is the default.

- When stdout is a terminal, `TERM` is not `dumb`, and `NO_COLOR` is absent, `list`, `status`,
  `logs`, and `up` use a fixed minimal palette.
- Aggregate log prefixes use a stable color derived from the process name, excluding the red and
  green colors reserved for lifecycle meaning. Only `[NAME]` is styled; child output remains raw.
- States use these colors: running and ready are green; starting is yellow; operator-stopped is cyan;
  an autonomous successful exit is dim.
- Colors for failed
exits, errors, timeouts, definition drift, exhausted recovery, and dependency-skipped results are red.
- list headers are bold.
- Any presence of `NO_COLOR`, including an empty value, disables styling, as does `TERM=dumb`.
- Piped output and JSON never contain ANSI styling.
- Only renderer-owned lifecycle labels and aggregate log prefixes are styled; names outside log
  prefixes, paths, messages, and child output remain unchanged.

The public version 1 CLI JSON and NDJSON contract is defined in
[`docs/cli-json-v1.md`](cli-json-v1.md). Every covered top-level object has
`schema_version: 1`. `hum version --json` prints
`{"schema_version":1,"version":"<version>","build_time":"<time>"}` without resolving a project or
contacting the daemon, so clients can discover this capability before trusting field semantics. The
CLI contract is independent of MCP and the private daemon protocol; local clients must not consume
the daemon socket as a public interface.

JSON process snapshots include `name`, `source`, `argv`, and the integer `followers` count, plus
identity, readiness, cursors, and errors when applicable.

- Human `status` always prints `followers`; human `list` adds `followers=N` only to followed
  records, leaving ordinary unfollowed list output unchanged.
- JSON-capable commands classify failures as `usage`, `daemon_unavailable`, `manifest_invalid`,
  or `internal` and emit one newline-terminated `{"error":{"code":"...","message":"..."}}`
  object on stdout when no JSON has been written.
- Daemon wire failures retain their wire code.
- JSON failures never add a hum diagnostic to stderr, and exit codes remain unchanged.
- The `start`/`up` NDJSON streams and `logs --follow` append one final typed `error` event after
  earlier events when a later failure occurs; they do not buffer the stream.
- This contract applies only when standalone `--json` or the documented `-j` appears before the
  payload separator.
- Attached `run` does not support CLI JSON mode and remains raw child output, including child stderr;
  payload text that merely resembles `--json` is not a JSON mode request.

| JSON error code | Meaning |
| --- | --- |
| `usage` | command or flag input is invalid |
| `daemon_unavailable` | the daemon cannot be contacted |
| `manifest_invalid` | manifest or project discovery configuration is invalid |
| `internal` | an unexpected local CLI failure |

`start` and `up` emit one NDJSON launch result per name.

- `up` uses lexical declaration order, attempts every entry, and applies this exit-code
  precedence: request error or `definition_drift` (1), early exit (3), timeout (2), success (0).
- `definition_drift` includes sorted `changed_fields` for argv, canonical cwd, readiness
  method/match/exec argv, TTY, normalized restart policy, or `stop_grace` changes and never satisfies
  an `after` dependency; CLI `up` exits 1 for drift.
- A removed manifest-sourced running or recovery-capable record is emitted as
  `removed_definition` with stop/remove guidance such as `hum stop NAME` or `hum remove NAME`.
- Removed records require an explicit stop or remove; the warning does not alter aggregate exit
  status.
- Attached `run` still streams raw child output and is outside the JSON contract;
  `logs --json --follow` emits bounded NDJSON events.
- `logs` accepts optional, repeatable names in selection order.
- `--stream system` selects only hum-generated supervision entries. The default `both` includes
  stdout, stderr, and system for bounded and follow reads.
- With no names, it resolves the current declaration set once in lexical order, without adding
  ad-hoc sessions; duplicate names are rejected.
- Bounded logs without `--after-cursor` select the newest configured entry window, equivalent to
  the default `--tail`; an explicit `--after-cursor` without `--tail` keeps forward paging from
  the oldest eligible retained entry.
- `--after-cursor` is rejected before daemon startup for an aggregate invocation.
- `--since DURATION` requires a positive, valid duration and captures one inclusive request-time cutoff.
- The cutoff is shared by every selected name.
- Selection starts from one immutable retained snapshot: after-cursor, the inclusive since cutoff,
  and stream choose eligible source entries. Match context then expands every regex match by up to
  `--context N` eligible entries on each side, merges overlapping or adjacent windows without
  duplicates in cursor order, applies tail, and finally applies whole-entry and byte bounds.
- Context never returns entries across the cursor, time, stream, retention, or captured-latest
  boundaries. It requires a non-empty `--match`; zero preserves match-only behavior, and context is
  rejected with `--follow` because live reads do not buffer future after-context.
- A forward bounded page consumes unselected source entries, but `next` stops immediately before
  the first selected match-or-context entry that did not fit. Passing that `next` back as
  `--after-cursor` neither loses nor duplicates a selected entry.
- Since composes with follow: the cutoff filters the initial retained replay and later entries
  naturally pass.
- Invalid, zero, negative, or overflowing durations are rejected before daemon startup or
  contact.
- Aggregate filters, tail, and entry or byte limits apply independently per selected name,
  bounded output is returned in selection order, human entries are atomic `[NAME]`-prefixed
  writes, and aggregate JSON uses named NDJSON event objects.

Human `hum up` has an attached interactive mode and bounded startup progress.

- In an interactive terminal, plain `up` subscribes to every resolved declaration before launch,
  streams atomic `[NAME]`-prefixed output written from that invocation onward, and keeps following
  after successful startup. It prints `Ctrl+C detaches; hum down stops processes`; detaching never
  signals a managed process.
- Ctrl+C after successful startup detaches with exit 0. During startup it exits 130 immediately,
  reports that launched processes remain supervised, and may leave dependency-gated declarations
  unlaunched; rerun `hum up` to finish convergence.
- `up --detach` keeps the bounded readiness-and-return behavior. `up --no-wait` returns after
  spawn without following. JSON and non-terminal output are also bounded so automation does not
  begin an indefinite follow implicitly.
- Attached mode exits with the normal nonzero result instead of continuing to follow when initial
  startup fails, exits before readiness, times out, or cannot reach a running state. Successfully
  launched children remain supervised.
- Readiness progress writes newline-terminated startup transitions to stderr. The final stdout
  summary is a compact `NAME`, `RESULT`, `STATE`, and `PID` table in lexical declaration order;
  readiness match/cursor and exec method/argv/interval details are available in JSON and, when
  configured, human output; terminal exec diagnostics are bounded and probe output is not retained.
- Progress follows temporal transition completion rather than lexical declaration order, is
  serialized as complete lines, and uses at most two lines per declaration: one
  launch, observation, error, or dependency-blocked line and, only for a declaration that
  entered `starting`, one ready, early-exit, or timeout line. Child output is a separate prefixed
  stdout stream and does not count against that progress bound.
- `up --json` emits no progress and keeps stderr empty on success; `up --no-wait`, `start`, and
  MCP `up` keep their bounded output and timing.
- Timeout and early-exit progress names include `inspect retained logs: hum logs NAME`, preserving
  diagnostics for detached and non-terminal invocations.

### Command semantics

`init` resolves the project and zero-config candidates without launching or starting the daemon.

- It exclusively creates `hum.yaml`: one discovered candidate produces a definition; none or
  several produce a commented, valid template.
- `--force` resolves and renders the complete replacement before creating a mode-0600 temporary
  file in the project directory, syncing and closing it before atomically renaming it over an
  existing regular `hum.yaml`; symlinks and other non-regular targets are refused.
- Without `--force`, existing paths and their refusal remain unchanged.
- Discovery, rendering, write, sync, close, and rename errors exit 1 without changing the
  original manifest.
- Output includes the path, `generated`, `template`, or `replaced` outcome, and `hum up` as the
  next command; JSON also includes candidates.

`start` idempotently ensures a named session is running.

- It relaunches retained stopped records; retained ad hoc records reuse their exact argv, cwd,
  and environment, while resolved records use the current definition and client environment.
- Concurrent starts create at most one child.
- `up` does the same only for current resolved definitions.
- It launches every zero-dependency root concurrently, waits for each direct `after`
  prerequisite to settle, and launches a dependent only when all direct prerequisites were
  observed as `started` or `already_running` with readiness `ready`.
- A running-ready prerequisite is satisfied without relaunch.
- Each process timeout starts at its own launch or first running observation, so independent
  roots overlap and the critical path controls total wait time.
- During bounded recovery, CLI and MCP `up` preserve the exited record and report
  `recovery_pending` or `recovery_exhausted` without sending a start request or waiting for an
  automatic successor.
- A pending or exhausted declaration makes CLI `hum up` exit 3 because it is not running;
  targeted `hum start NAME` or `hum restart NAME` cancels recovery and launches immediately.
- Successful children remain running after other failures.
- CLI `start` and `up` use exit 0 for success, 1 for request errors or definition drift, 2 for
  readiness timeouts, and 3 for an early exit; `up` also uses 3 when recovery leaves a
  declaration not running.
- `wait` uses 0 for a match or an unfiltered exit, 1 for a request or usage error, 2 for
  timeout, and 3 when `--match` sees process exit first.
- Interactive plain `up` follows aggregate output after successful startup. `--detach` waits for
  readiness and returns, while `--no-wait` returns after spawn only for dependency-free manifests;
  when any `after` is declared, `--no-wait` is rejected before daemon creation/contact.
- `start NAME...` remains explicitly named and concurrent but never adds or waits for transitive
  prerequisites.
- `down` remains concurrent rather than reverse ordered.

`run <name>` uses an existing resolved definition or attaches to an existing running or stopped
session.

- `run <name> -- <command>...` creates an ad hoc session or replaces a stopped session's
  retained ad hoc launch spec; it keeps existing conflict rules while running.
- A missing zero-config candidate permits the ad hoc form; malformed, ambiguous, and
  introspection failures do not.
- A resolved name cannot be occupied by a conflicting ad hoc run.

`restart` uses the current resolved definition and client environment.

- If only a retained ad hoc record exists, it reuses its exact argv, cwd, and environment.
- Daemon replacement loses ad hoc definitions, so evicted records cannot be restarted.
- After each successful replacement, it uses the shared readiness classification path: a
  configured matcher or direct exec probe must become ready, while a process without readiness is
  reported as `running_unverified`. Exec probes use exact argv without a shell, start immediately,
  retry serially after failures at the interval (1s default), and inherit cwd/environment.
- By default `restart` waits per name; `--no-wait` returns after spawn, and `--timeout` accepts
  a positive per-name duration measured from that name's launch.
- Readiness failures are reported; remaining names continue.
- Request or validation errors stop subsequent names.
- Results preserve input order; exit precedence: 1 > 3 > 2 > 0 for request/error,
  `exited_before_ready`, `timed_out`, and success.
- There is no whole-invocation timeout.

`wait` timeout results include `process_observed` in CLI JSON and MCP structured content.

- The daemon records it during that single wait request without an extra `get` round trip: it is
  `true` when a matching runtime record existed initially or appeared and later stopped or was
  removed, and `false` only when no record was observed.
- Human CLI output for `false` adds `no process named "NAME" was observed during the wait; check
  the name or start it first.`; undeclared names remain eligible for future launch waiting.

`list` merges current definitions with all project runtime records.

- Without a daemon it reports resolved definitions as stopped.
- `status`, `logs`, `wait`, `restart`, `stop`, and `remove` operate on resolved and ad hoc
  records in the project.
- The recommended interactive workflow is plain `hum up`; use `hum up --detach` for a bounded
  readiness check or `hum logs --follow` to observe independently of startup.
- Bounded `logs` without `--after-cursor` shows the newest default entry window; use an explicit
  cursor without `--tail` to page forward from the oldest eligible retained entry.
- `logs` with multiple names follows the explicit selection order; its no-name form uses the
  same lexical declarations as `up`, does not include ad-hoc records, and does not change
  membership when declarations or runtime records change.
- Each aggregate name receives its own match-context selection and bounded limits.
- Human output prefixes each entry with `[NAME]`; JSON bounded output and follow output retain
  the named NDJSON event shape.
- An aggregate follow owns one follower per selected session, serializes writes, reports
  per-session errors with their names without stopping other sessions, and cancels the whole
  aggregate on daemon loss or output failure.
- Ctrl+C closes all aggregate followers and never signals managed processes.
- A single explicit name preserves the existing human and JSON output unchanged.
- While the original process group remains alive after its recorded leader exits, `status` and
  `list` report `state: descendants`, retain the PGID used as the lifecycle barrier, and report
  PID 0 rather than presenting the dead leader as a live process. The snapshot becomes terminal
  only after those descendants exit.
- Terminal snapshots retain an autonomous child's exit status.
- A child terminated by an OS signal is represented with `exit_status: -1` and an optional
  `signal` object containing its canonical name and number, for example
  `{"name":"SIGTERM","number":15}`; the same object is carried by CLI `list`, `status`, `up`,
  and `wait` JSON and human output, and by MCP text and structured content.
- Non-signal exits omit `signal`.
- Operator-stopped snapshots remain `stopped` without autonomous exit details, so an operator
  stop is distinct from a signal-terminated child.

`hum attach <name>` is the human-facing explicit terminal connection to an existing running
session.

- It never starts or restarts a process or daemon.
- It reuses the attached-session stream and, for a TTY target, the existing exclusive input
  lease; raw input and terminal resize events go to the sole owner, while a non-TTY target
  follows output without input.
- `--tail N` replays the final N retained entries in source order before live output; `--tail 0`
  suppresses retained replay.
- Missing or stopped names return actionable guidance and leave the retained record unchanged.
- Foreground `hum run NAME -- COMMAND` launches exactly one incarnation, streams raw output, returns
  its exit status, stops on Ctrl+C or SIGTERM, and detaches on SIGHUP. `hum run NAME --detach -- COMMAND`
  remains daemon-owned; `hum attach` and `logs --follow` remain read-only durable observers.
- Copy-pasteable examples are `hum attach console` and `hum attach console --tail 50`.
- `input` is the bounded request/response surface for an existing TTY record: `--text` sends
  exact non-empty text bytes without a newline, while `--base64` accepts only standard padded
  base64 without whitespace and decodes to at most 32 KiB.
- It attaches only to the initial running state, writes exactly once at that launch cursor, and
  releases the exclusive lease before returning.
- The client behavior is at-most-once: a launch race or lost acknowledgement is reported without
  resending to a successor.
- It never starts a daemon or process, waits for a launch, queues, retries, retains, or
  explicitly echoes bytes.
- The bounded prompt loop is observe with `logs` or `wait --match`, answer with `input`, then
  confirm with `wait --match`.
- A stopped initial state returns `session_not_running`; a non-TTY target returns
  `input_not_tty`; ownership, closed-session, and stale-cursor races return the existing input
  error codes.
- `signal` sends exactly one observational signal to the process group of an existing running
  record.
- Names are case-insensitive with an optional `SIG` prefix; positive decimal values are accepted
  only when they map to the current OS's supported named table (`HUP`, `INT`, `QUIT`, `TERM`,
  `KILL`, and `USR1`/ `USR2` where available).
- The canonical result is one line in human mode or
  `{"name":"NAME","signal":{"name":"SIGHUP","number":1},"status":"sent"}` in JSON/MCP; invalid
  specifications return `invalid_signal`, while missing and stopped records return `not_found`
  and `not_running`.
- Signaling never sets stop intent or cancels automatic relaunch, including for TERM and KILL;
  only stop, down, and restart control lifecycle policy.
- `stop` preserves the durable session; `remove` stops its child, closes followers, and discards
  runtime launch state and output without editing `hum.yaml`.
- The reported follower count is read-only: `remove` never warns, prompts, refuses, or otherwise
  changes behavior based on it.

`down` is the project-scoped inverse of `up`.

- It concurrently stops every running project record, includes declared-but-absent names as
  `not_running`, and returns one name-sorted `stopped`, `not_running`, or `error` result per
  name.
- It never starts or shuts down the daemon, affects other projects, or deletes records.
- With no daemon or names it succeeds with `Nothing is running in this project.` Any stop error
  exits 1.

`shutdown` controls daemon lifetime across projects. It refuses while any
process is active unless `--stop-processes` is given.

`completion` prints a script for bash, zsh, or fish and does not edit shell startup files.

- Its NAME callbacks merge manifest declarations with retained runtime records from the selected
  project, deduplicate and sort the names, and silently return no candidates when manifest or
  daemon resolution fails.
- A missing daemon still permits declaration completion.

## Definitions and resolution

### Manifest

The nearest Git project root may contain one authoritative `hum.yaml`.

- A valid empty manifest resolves to no definitions; an invalid manifest is an error.
- `hum up` on an empty manifest does not create a daemon when none exists: it may inspect an
  existing daemon for removed manifest-sourced recovery sessions.
- With no such records, human output is exactly `No processes are declared in hum.yaml.` and
  `--json` emits no NDJSON records.
- Discovery occurs only when the file is absent.
- Alternate filenames are ignored.

`hum init` puts the published root `hum.schema.json` directive first so compatible YAML editors
configure validation and completion automatically. Existing manifests can select
`hum.schema.json` manually in their editor. The schema is an editor contract; the Go
implementation remains the authoritative parser for manifest behavior.

```yaml
version: 1
processes:
  db:
    argv: [docker, compose, up, db]
    ready:
      match: "ready"
  api:
    argv: [bun, run, api]
    after: [db]
    ready:
      match: "Listening"
      timeout: 30s
  health:
    argv: [bun, run, api]
    ready:
      exec: [./bin/health, api]
      interval: 1s # First probe is immediate; retries are serial.
  web:
    argv: [bun, run, dev]
    cwd: web
    after: [api]
    ready:
      match: "Local:"
      timeout: 30s
    restart: on-failure
```

Each entry requires a safe name and a non-empty string argv.

- Optional `cwd` is root-relative and must exist and remain beneath the root after lexical and
  symlink resolution.
- `ready.match` is a regular expression; `ready.timeout` is a positive duration defaulting to 30
  seconds. Alternatively, `ready.exec` is an exact non-empty argv sequence run directly without a
  shell; `ready` requires exactly one of `match` or `exec`.
- An exec probe runs immediately after launch, then retries serially after each failed attempt at
  `interval` (a positive duration defaulting to 1s). It inherits the supervised process cwd and
  launch environment. Failed probes do not enter the process output store; only one bounded
  last-attempt terminal diagnostic is retained.
- Readiness is a startup gate for `after` and launch commands, not continuous liveness monitoring.
- Optional `after` is a list of same-manifest process names.
- Names must be unique, cannot self-reference, and must point to definitions that declare
  `ready`; absent `after` is empty.

Parsing is strict and single-document.

- Unknown or duplicate keys, YAML aliases or merges, unsupported versions, invalid names,
  regexes or durations, unsafe cwd, empty/non-string argv, shell text, malformed `after` values,
  unknown or unready dependencies, duplicate/self references, and cycles of any length are
  errors with file, process, and indexed-field context such as `process "web".after[1]`.
- Definitions are name-sorted and carry `source: manifest`; discovered definitions always have
  no dependencies.

The manifest defines processes and their client-side launch dependencies only: no
runtime settings, ports, HTTP checks, or environment values/files. Projects
needing environment activation must commit a runner and put it in argv; CLI and
MCP do not activate mise, nvm, direnv, or shell hooks.

### Zero-config discovery

Without `hum.yaml`, hum inspects supported root-level conventions without
executing task bodies or launching candidates. Exactly one candidate resolves
to `dev`, rooted at the project, with no inferred readiness:

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

Implicit discovery never evaluates repository code itself; the Task, Just, and mise probes run
those tools' own listing commands, and mise additionally honours its trust prompt for the
repository's configuration. Discovery probes honour cancellation from both the CLI command
context and `hum mcp`, so an interrupted command reaps a hung probe. Mix detection reads `mix.exs` as text,
ignores comments and quoted values, and recognizes only a literal Phoenix dependency tuple;
dynamic declarations fail closed and require an explicit `hum.yaml`. This static check may
identify the launch command, but `mix.exs` is evaluated only if the user later starts it.
Mise, Task, and Just retain their documented metadata commands, which are cancellable and run
inside the repository trust boundary. Command-backed sources are skipped when their executable
is unavailable.

- No candidates produce a typed `NoCandidateError`; several produce an `AmbiguityError` listing
  all sources.
- Malformed configuration and failed or malformed required introspection produce typed
  `ConfigurationError` and `IntrospectionError`; caller cancellation stops command-backed
  discovery and propagates unchanged.
- All wrap their sentinel and work with `errors.As`.

For package.json, `packageManager` selects bun, pnpm, yarn, or npm (ignoring an optional version
suffix) and rejects other or non-string values.

- Otherwise the runner comes from exactly one lockfile family: Bun (`bun.lock`, `bun.lockb`),
  pnpm (`pnpm-lock.yaml`), Yarn (`yarn.lock`), or npm (`package-lock.json`,
  `npm-shrinkwrap.json`).
- Multiple files in one family are allowed; conflicting families are errors.
- With no lockfile, npm is used.

Discovery does not scan nested packages or infer language/framework commands
(except confirmed `mix phx.server`), Docker Compose, multiple processes, ports,
readiness, or launch ordering. It never tries commands to see what succeeds.

Strict definition commands (`up`, `start`, and argv-free `run`) resolve before daemon startup.

- Ad hoc `run` alone treats `NoCandidate` as no definition.
- Control commands also treat it as no definition and may access existing runtime records.
- Every other resolution error propagates before daemon control.

## Readiness and output

The daemon launches exact argv directly, without an implicit shell. Each child
has a Unix process group and stdin at `/dev/null`. `stop` and `down` send SIGTERM,
wait a bounded grace period, then send SIGKILL. Client and follower disconnects
do not stop children.

A launch records its readiness expression, launch cursor, and first matching cursor even if
nobody is waiting.

- Configured processes move from `starting` to `ready`; the retained state survives output
  eviction.
- Relaunch resets readiness at a new launch cursor, so old output cannot satisfy it.
- Definitions without `ready`, including all discovered definitions, report `running_unverified`
  and are never reported ready.
- A CLI timeout overrides the manifest timeout.

For ordered `up`, a prerequisite result satisfies its dependency only when this invocation observes
`started` or `already_running` with readiness `ready`.

- Request errors, exits before readiness, timeouts, and prior skips block a dependent; the
  dependent is returned as `outcome: skipped` with `blocked_by` containing every direct
  unsatisfied prerequisite sorted by name.
- Blockers are direct only, so a cascade names its immediate skipped parent.
- The scheduler waits for all direct results before finalizing blockers, while output remains
  lexical after every node settles.
- Before finalizing a blocked node, CLI and MCP read its retained record without lifecycle
  mutation.
- A present record adds `existing_state: running|stopped|exited` and its process snapshot; human
  output says `existing process running`, `existing process stopped`, `existing process exited`,
  or `not launched`.
- The result remains skipped and cannot satisfy a downstream dependency.
- Skips do not change aggregate exit precedence: request error 1, exited before ready 3, timed
  out 2, success 0.
- An `on-failure` successor is not followed by the same `up`; rerun `up` after recovery.

Human `hum up` progress uses these rules:

- Launch or observation errors use `hum up: NAME: error: MESSAGE`.
- Blocked lines show the sorted direct blockers.
- Blocked lines distinguish an existing running or exited record from `not launched`.
- Progress never changes scheduling, exit precedence, readiness timeouts, or child lifetime.

Each durable named session has one cursor sequence across stdout, stderr, and incarnations.

- Entries contain stream, timestamp, raw stored text, stripped on bounded read and match as
  terminal-control-stripped text, and cursor.
- Logs output keeps the existing `next` field: it is the last source cursor consumed by that
  read and can be passed to `--after-cursor`/`after` for forward paging.
- Process snapshots keep the existing `next_cursor` field. It is the next cursor that will be assigned.
- `next` and `next_cursor` are intentionally different. Neither MCP field is renamed.
- `StripTerminalControl` is the single byte-wise definition of stripped text: it removes
  recognized terminal control sequences and CR immediately before LF from child stdout/stderr
  per entry.
- System entries remain raw, as do all stored bytes.
- Bounded `logs`, MCP `logs`, `wait --match`, readiness matches, and ring predicates use
  stripped child text.
- Patterns containing raw ESC bytes no longer match stripped child text; a `^` anchor now
  matches colourised output whose raw first byte is ESC.
- `logs --follow --match` selects with stripped text but emits selected raw entries.
- Control-only bounded child entries remain present with empty text.
- Read byte limits (`MaxBytes`, `--limit-bytes`) count text bytes only, while retention
  (`--output-bytes`) charges each entry `len(text)+128` bytes for conservative metadata,
  string/slice storage, and allocator slack.
- The charged size controls retention rejection, eviction, capacity, and accounting, so short
  entries also bound retained cardinality; it is not an exact RSS cap.
- There is no `--raw` flag or other raw opt-out.
- Stripping is not terminal emulation or redraw collapsing: a sequence split across entries can
  leave its tail visible, and carriage-return redraw frames remain separate.
- Byte-bounded retention reports eviction explicitly; a live pre-launch or stopped follower
  reserves its session from completed-record eviction.
- Aggregate `logs --follow` creates one follower per selected name and keeps per-session
  filters, tails, and limits independent.
- Attached `run` and `logs --follow` may start before the first launch, return retained output,
  print exit/wait/launch boundaries, and remain open across stop/start and down/up until Ctrl+C,
  removal, or transport loss; their rendering remains raw.
- Status, list, and their MCP equivalents report how many of these live followers the daemon
  currently holds open, including pre-launch and stopped-session followers; the count is not
  persisted and is zero when no session exists.
- Ctrl+C detaches only the observer.
- `wait` without an explicit cursor waits for the next incarnation when stopped or unlaunched
  and remains bounded (30 seconds by default).
- Ad hoc records omit readiness; terminal manifest records retain their configured readiness
  method, matcher or exact exec argv, interval, and bounded diagnostic for drift classification.

### Crash relaunch policy

A manifest entry may set `restart` to `never` (the default) or `on-failure`; validation rejects
every other value and non-string YAML scalar with file and entry context.

- Discovery and ad-hoc sessions are always `never`, and `hum init` comments an inert `restart:
  on-failure` example.
- `on-failure` schedules one second, two seconds, four seconds, eight seconds, and sixteen
  seconds of backoff, for at most five automatic attempts.
- Non-zero or signal exits trigger the loop unless an explicit operator control owns the exit;
  exit zero, stop, down, restart, remove, and shutdown cancel and reset it.
- A spawn failure consumes an attempt and appends a bounded `relaunch failed: ...` system entry.
- An automatic child alive for 30 seconds resets the counter.
- The generation token and supervisor lock linearize exit, timer claim, and operator intent, so
  stale timers never launch and a manual start/restart wins without two children.

Automatic attempts reuse the last effective argv, cwd, environment, readiness, TTY, and stop grace
and do not reread the manifest.

- For a running, pending-recovery, or exhausted manifest record, `start` and `up` report
  `definition_drift` rather than silently adopting changed argv, canonical cwd, readiness method,
  match or exact exec argv, TTY, normalized restart policy, or `stop_grace`; only explicit `restart`
  applies a changed definition. Readiness timeout and exec interval are wait policy, not drift identity.
- Readiness and client timeout do not trigger relaunch.
- `restart`, `relaunches`, and optional whole-second `next_launch_at` appear in process, CLI
  JSON, and MCP snapshots.
- During backoff, CLI and MCP `up` preserve that state and report `recovery_pending` without
  consuming an attempt; after the budget is exhausted they report `recovery_exhausted` without
  reviving the loop.
- Pending and exhausted records resist completed-record eviction.
- Followers stay attached through the exit/wait boundary, backoff, and exhaustion; bounded logs
  retain child failures and the `relaunching` and `gave up` system boundaries.
- Recovery snapshots retain the response-safe readiness method, match or exact exec argv, interval,
  and bounded terminal diagnostic without exposing environment so drift can be classified after exit.
  Exec probes run directly without a shell using inherited cwd/environment, start immediately, retry
  serially, and never retain probe output. Readiness is startup gating, not liveness monitoring.
- Agents should read the failing incarnation's retained output before editing again.

## Daemon and environments

One daemon serves each runtime directory at `hum.sock`. Directories created by hum use mode
0700. A pre-existing operator-managed directory keeps its mode; hum accepts read/execute access
for group or other users but refuses a directory they can write.

- `serve --daemon`, `run`, `start`, `up`, CLI `logs --follow`, and CLI `wait` use a startup lock
  and readiness handshake.
- Bounded reads and controls do not start an empty daemon.
- Foreground daemon exit and `shutdown --stop-processes` stop all managed groups.

The mode-0600 `hum.state` file atomically records the daemon incarnation and each live group's
project, name, leader PID, PGID, and OS process-start identity.

- A launch is not reported successful until that identity is durable.
- On startup, a dead daemon's groups are reclaimed with TERM, the configured grace period, and
  KILL only after PID, group leadership, and process-start identity all match. An explicit zero
  grace escalates immediately, both during startup reclamation and ordinary stop operations.
- Auto-start allows two configured grace periods per recorded group, processed sequentially, plus
  five seconds for setup and the readiness handshake. Caller cancellation still bounds that wait,
  and a child that exits before publishing readiness fails the start immediately.
- Dead groups are discarded; mismatched or unverifiable identities are never signaled and remain
  unresolved blockers for the same project and name.
- The startup reconciliation summary remains visible for the daemon lifetime through human
  warnings and JSON/MCP `warnings` arrays.
- Clean graceful shutdown removes `hum.state`; corrupt state fails closed with operator cleanup
  guidance.

The launching client supplies cwd and its full environment.

- Manifest `cwd` changes only the child directory; discovered definitions use the project root.
- Resolved restarts use the current argv, cwd, readiness, and requesting client's environment,
  so definition edits take effect through explicit `restart`.
- A running or recovery-capable manifest record with changed argv, canonical cwd, readiness
  method (including the readiness matcher), exact exec argv, TTY, normalized restart policy, or
  `stop_grace` returns `definition_drift` with sorted `changed_fields` and `hum restart NAME` guidance
  from `start` or `up`; CLI exits 1 for this result and it is not silently replaced. Readiness timeout
  and exec interval are wait policy, not drift identity.

## MCP adapter

`hum mcp` serves JSON-RPC over stdin/stdout.

- Every request accepts `scope`: `project` (default) requires an absolute existing
  `project_root`, while `global` rejects `project_root` and addresses retained ad-hoc sessions.
- It exposes twelve tools: `start`, `up`, `down`, `list`, `status`, `logs`, `wait`, `input`,
  `restart`, `stop`, `remove`, and `signal`.
- `input` accepts exactly one non-empty `text` or `base64` payload, uses the same bounded
  one-shot TTY semantics as the CLI, and returns `name`, decoded `bytes`, and `launch_cursor`.
- MCP `signal` accepts the same case-insensitive named or supported positive decimal signal
  forms as the CLI and returns
  `{"name":"NAME","signal":{"name":"SIGHUP","number":1},"status":"sent"}`.
- It returns `invalid_signal`, `not_found`, or `not_running` without delivering a signal when
  validation or target lookup fails.
- MCP `restart` accepts `no_wait` and a positive per-name `timeout_ms`, and its text and
  structured content carry `name`, `outcome`, `readiness`, `pid`, `launch_cursor`, and an
  optional `message` with the same single-name semantics as the CLI.

Requests with IDs run concurrently up to 64 in-flight requests.

- The mutex- protected request registry rejects a 65th request with JSON-RPC code `-32001`
  without starting it, and rejects a duplicate in-flight ID with `-32600`.
- Notifications and incoming responses consume no request slots.
- A `notifications/cancelled` notification cancels only its matching in-flight ID; that request
  receives code `-32800`, while an unknown cancellation ID is a no-op.
- Responses are serialized by the Serve-owned closeable response transport.
- On stdin EOF or parent cancellation, all request contexts are cancelled; Serve waits at most
  one second for handlers, closes the response transport to unblock writes, joins the writer,
  and returns within two seconds without leaving handler or writer goroutines behind.
- A request abandoned that way reports the tool error code `cancelled`, which is distinct from
  `internal`; `internal` remains reserved for unexpected adapter failures.

The tools share CLI definition, readiness, cursor, collision, and aggregate semantics.

- Bounded MCP `logs` accepts `stream` values `stdout`, `stderr`, `system`, and `both`; `system`
  selects hum-generated supervision entries only, while omitted or explicit `both` includes
  stdout, stderr, and system.
- Without `after`, it selects the newest default entry window, while explicit `after` without
  `tail` keeps forward paging from the oldest eligible retained entry.
- MCP `logs` accepts a positive `since_ms` duration and captures one immutable inclusive
  request-time cutoff before applying the same cursor, since, tail, and entry/byte ordering;
  invalid, zero, negative, or overflowing values are rejected without daemon contact.
- Its output keeps `next` as the last source cursor consumed; process snapshots keep
  `next_cursor` as the next cursor to be assigned.
- `up` applies the same client-side `after` DAG scheduler and lexical results as the CLI;
  independent roots launch concurrently, dependents wait for all direct prerequisites to be
  ready, and skipped entries include sorted direct `blocked_by`.
- Drifted entries never satisfy a dependency.
- `up` reports removed manifest-sourced running or recovery-capable records as lexical
  `removed_definition` warnings with stop/remove guidance; warnings do not change aggregate
  status and omit ad-hoc/discovered records.
- `up` with `no_wait: true` is rejected before daemon contact when any dependency is declared.
- `start` remains singular and explicit-only.
- Only `start` and `up` may create or replace a daemon.
- Without one, `list` reports stopped definitions; `stop` and `down` succeed; the other control
  tools return unavailable-daemon errors.
- Recorded environments are never returned.
- MCP `status` and `list` return the same `followers` integer as the CLI snapshot.

MCP exposes no follow or other unbounded operation; agents use bounded `wait`, `logs`, one-shot
`input`, and observational `signal`.

- The adapter receives a protocol-shaped daemon client and constructs no app services,
  supervisors, or output stores in-process.
- It has no `run`, `serve`, or `shutdown`, HTTP transport, authentication, remote access, or
  arbitrary-command tool.

## Non-goals

The foundation does not include:

- arbitrary or unbounded input
- queued input
- remote transport or authentication
- a web UI
- persistent process history
- a plugin system
- OS service installation
- environment literals or files

The runtime directory contains only the socket, PID/startup/readiness files, durable live-group state, and bounded daemon diagnostics.

## Optional pseudo-terminals

A process may declare `tty: true` when an interactive devtool requires a controlling terminal.

- The ad-hoc equivalent is `hum run NAME --tty -- COMMAND`; TTY remains opt-in and the default
  `/dev/null` stdin plus separate stdout/stderr pipes are unchanged.
- The daemon owns the PTY master, launches a session leader with `Setsid`/`Setctty`, and signals
  its process group during stop, restart, down, remove, and forced shutdown.
- PTY output is merged once as raw retained `stdout`; bounded reads and matches apply the
  byte-wise child-output strip described above, without terminal emulation.

Exactly one attached `hum run` owns input.

- A second attachment follows output only, `logs --follow` never owns input, and the one-shot
  CLI/MCP input operation accepts exact text or strict padded base64 payloads bounded to 1-32768
  bytes and scoped to the initial running launch cursor; state events identify stopped/running
  successors and the owner alone forwards SIGWINCH resize events from the attached terminal.
- Ctrl-] detaches input, local raw mode is restored on detach, panic, and transport-loss paths,
  terminal/application echo remains child output, and input is discarded while stopped.
- While a TTY foreground run owns input, Ctrl-C is forwarded through the PTY; after Ctrl-] releases
  input, Ctrl+C uses the foreground control-signal stop rules. SIGTERM stops the incarnation and SIGHUP
  detaches without signaling the daemon-owned child.
- Ordinary exit preserves the lease; remove and daemon shutdown close it.
- MCP exposes `tty` snapshots and the bounded `input` tool for exact prompt responses.

## Canonical project scopes

hum selects project scope automatically from the invocation directory. Git roots and linked worktrees are canonicalized physically, so symlink aliases share records while separate worktrees do not. Child cwd remains lexical. Use `hum --project /path/to/main` (or `-C`) for explicit cross-worktree access. The deliberate `--global` selector (interactive shorthand `-g`) creates a machine-wide namespace only for ad-hoc retained sessions; selection never changes child cwd, reads a manifest, or falls back across scopes. It applies before or after lifecycle commands and before or after `run` NAME, conflicts with `--project` and `list --all`, and is rejected by `init` and `up`. Global `start` and `restart` reuse only a retained launch specification. `hum remove --all` enumerates and removes retained runtime records lexically within the selected project or global scope; it never spans scopes or treats unlaunched manifest declarations as sessions. CLI and MCP reject combining a name with `all`. `hum list --all` includes a `global` group with `hum --global` selectors, and project misses include copyable global guidance such as `hum --global logs proxy`. JSON process records contain `scope` (`project` or `global`); global records omit `project_root`.
