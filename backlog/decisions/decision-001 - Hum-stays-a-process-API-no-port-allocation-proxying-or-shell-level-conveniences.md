---
id: decision-001
title: >-
  Hum stays a process API: no port allocation, proxying, or shell-level
  conveniences
date: '2026-09-14 23:10'
status: proposed
---
## Context

Hum was compared with pitchfork (jdx/pitchfork), a mature Rust dev-services manager with a TUI, web UI, port assignment and reverse proxy, cron, boot start, shell-hook autostart, file watching, health checks, SQLite structured logs, and `sh -c` run strings with templating. Matching that feature set is neither achievable nor desirable for Hum.

Hum's distinct value is a bounded, versioned, exact-argv process interface for tools and coding agents: canonical-root scoping that is correct for parallel worktrees, stable log cursors, `wait --match`, single-owner TTY input, closed-schema MCP tools, and `hum doctor`.

Two open questions were whether to add port allocation or networking across worktrees, and whether to keep zero-config `hum up` discovery of Mise, Task, Just, Make, `package.json`, Deno, Composer, `bin/dev`, and Mix `dev` entrypoints.

## Decision

1. Hum's domain is process lifecycle, observation, and input. Hum does not allocate ports, inject `$PORT`, template values between processes, or proxy traffic. Per-worktree networking is expressed with the existing manifest `environment.files`/`env` and `--file` alternate manifests. Readiness on a port is a probe and is in domain; allocation is not.
2. Native HTTP and TCP readiness probes are in domain and will be added so silent servers do not require `curl` or `nc` argv (HUM-112). They remain startup gates, not liveness monitoring.
3. Runtime conventional discovery is removed from `up`, `start`, `restart`, `list`, `status`, `doctor`, and MCP. An explicit `hum.yaml` or `--file` manifest is the only declaration source; `hum run NAME -- COMMAND` is the zero-config path. `hum init` keeps read-only file detection to scaffold a manifest and never spawns a subprocess (HUM-113).
4. These remain non-goals and are documented as such (HUM-114): TUI and web UI (Herdr provides panes), reverse proxy and port management, cron scheduling, boot start, shell-hook autostart, file-watch restarts, liveness/health monitoring, child CPU/RSS sampling or enforcement (operators wrap argv with platform-native tools; Hum bounds only its own retained output and machine-facing operations), log parsing or query languages, and runtime shell interpretation or templating. Windows was originally listed here; see the 2026-09-23 revision below.
5. Distribution borrows only what is cheap: a mise registry entry (HUM-115).

## Consequences

- About 2.6k lines of discovery code and its subprocess path leave `internal/project`; `doctor` and read-only commands have one manifest path.
- `hum up` in a project without a manifest becomes an actionable error instead of a launch. This is a breaking change before 1.0 and is recorded through a `!` conventional commit.
- Runtime `source` values other than `manifest:<path>` and `ad_hoc` stop being produced. CLI JSON v1 compatibility rules already require clients to tolerate enum values they do not see.
- Port collisions between worktrees remain the user's responsibility; documentation shows the environment-file pattern.
- Revisit port observation (reporting bound listeners, never allocation) only when a representative agent workflow cannot recover a service URL from the process's own output or its manifest environment.

## Revision 2026-09-23: native Windows is a goal

Native Windows support moves from non-goal to goal, tracked by HUM-117 through HUM-122. It extends the same exact-argv process interface to another platform and does not reopen any other non-goal. Until those tasks ship, Hum supports macOS and Linux.

## Revision 2026-10-09: read-only port observation

The revisit trigger occurred: development servers in parallel worktrees can choose a fallback port, launchers can start listener-owning descendants, and logs may omit or evict the startup banner. Named `hum status NAME --ports` and the MCP status tool's opt-in `ports` argument may therefore report a current snapshot of TCP listeners held by the supervised process group. This is observation only; port allocation, `$PORT` injection, proxying, and URL inference remain non-goals. Addresses and ports are returned as observed, never turned into URLs.

The snapshot includes descendants that remain in the recorded Unix process group or Windows Job Object, including a process group after its leader exits. Processes that leave the group, container runtimes and port-forwarding layers such as Docker Desktop or OrbStack are outside the observed boundary. Inspection is opt-in and read-only; ordinary status and list do not query sockets. Results are identity-checked snapshots and can race with later process or listener changes. An empty successful snapshot means no listener was observed at that moment, not that a service is unreachable; denied, partial, or unavailable inspection is reported separately.