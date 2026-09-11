# Changelog

Notable changes to hum are documented here. This file is generated from conventional commits by [git-cliff](https://git-cliff.org/).

## Unreleased

### Added

- Generate and package man page
- Add verified curl installer
- Add Herdr process plugin
- **cli:** Add version capability command

### Documentation

- Polish plugin sections and version help
- Defer lifecycle event interface

## [v0.9.1](https://github.com/brettinternet/hum/compare/v0.9.0...v0.9.1) - 2026-09-11

### Added

- Add Claude Code plugin marketplace
- Publish releases to Homebrew
- **cli:** Publish machine output v1 contract
- **project:** Keep manifest schema in sync

### Fixed

- **app:** Drop control intent when the signal finds no process
- **app:** Clear exec readiness diagnostic on success

### Documentation

- Add readiness probe examples
- Demo agent log diagnosis
- Split terminal demos
- Show hum mcp configuration in README
- Describe CI Go caching
- Sync process guidance

## [v0.9.0](https://github.com/brettinternet/hum/compare/v0.8.0...v0.9.0) - 2026-09-11

### Added

- Add executable readiness probes
- Configure per-process stop grace
- **logs:** Add match context
- **logs:** Add system stream selection

### Fixed

- **daemon:** Recognize reaped process groups
- **app:** Preserve control signal intent
- **cli:** Preserve attached run handoffs
- Stabilize shutdown under process teardown

### Documentation

- Tighten README examples
- Show parallel agent worktrees

## [v0.8.0](https://github.com/brettinternet/hum/compare/v0.7.0...v0.8.0) - 2026-09-10

### Fixed

- Close CI signal and output races
- **cli:** Cancel manifest discovery with the command context
- **cli:** Fail daemon auto-start as soon as the child exits
- **daemon:** Restore pre-refactor wire error payloads
- **cli:** Type init global rejection at its origin
- **cli:** Type JSON error categories
- **cli:** Report declared stopped status
- **daemon:** Preserve explicit runtime settings
- Make full test suite race-clean
- **cli:** Clarify scope boundaries
- **mcp:** Enforce tool input schemas
- Bound daemon recovery startup
- **project:** Make discovery cancellable

### Changed

- **mcp:** Drop scope checks duplicated by schema preflight
- **app:** Centralize lifecycle transitions
- **daemon:** Use protocol DTOs directly

### Documentation

- **cli:** Make help concise and truthful
- Add MIT license

## [v0.7.0](https://github.com/brettinternet/hum/compare/v0.6.0...v0.7.0) - 2026-09-09

### Added

- **cli:** Remove all sessions in a scope

### Fixed

- **daemon:** Clarify version mismatch recovery

## [v0.6.0](https://github.com/brettinternet/hum/compare/v0.5.1...v0.6.0) - 2026-09-09

### Added

- **cli:** Add global process scope
- **cli:** Make attached run own one incarnation
- Canonicalize project scopes

### Fixed

- **app:** Expire stale control intent
- **cli:** Make foreground run report what actually happened
- **cli:** Classify global selector conflicts as usage errors
- **scope:** Stop cross-scope leaks in removed-worktree targeting
- **project:** Discover Task dev aliases

### Documentation

- Sync the plugin skill with scopes and foreground run

## [v0.5.1](https://github.com/brettinternet/hum/compare/v0.5.0...v0.5.1) - 2026-09-09

### Fixed

- **cli:** Reject run command without name

### Documentation

- Clarify manifest example

## [v0.5.0](https://github.com/brettinternet/hum/compare/v0.4.1...v0.5.0) - 2026-09-08

### Added

- **cli:** Summarize project status

### Documentation

- Add hum manifest schema

## [v0.4.1](https://github.com/brettinternet/hum/compare/v0.4.0...v0.4.1) - 2026-09-08

### Added

- Color process log prefixes

## [v0.4.0](https://github.com/brettinternet/hum/compare/v0.3.1...v0.4.0) - 2026-09-08

### Added

- Follow process output from hum up
- Summarize hum up results in a table

## [v0.3.1](https://github.com/brettinternet/hum/compare/v0.3.0...v0.3.1) - 2026-09-07

### Fixed

- Report surviving descendants
- Tolerate loaded daemon handshakes
- Close readiness wait races
- **mcp:** Report a cancelled request distinctly
- **process:** Qualify the Linux start identity by boot
- **daemon:** Kill orphan groups that outlive their leader
- **cli:** Stream attach, complete new names, classify input errors
- Synchronize installed git hooks

### Documentation

- Make guides easier to scan
- Silence demo shell hooks
- Polish terminal demo
- Correct the MCP shutdown grace and cover signal, since, and setup

## [v0.3.0](https://github.com/brettinternet/hum/compare/v0.2.0...v0.3.0) - 2026-09-07

### Added

- Report process exit signals
- Explain unobserved wait timeouts
- Add observational signal command
- **cli:** Add TTY-aware lifecycle colors
- **logs:** Add since window
- **cli:** Attach to running sessions
- **init:** Add force replacement
- **cli:** Emit structured JSON errors
- **cli:** Wait for readiness after restart
- Distinguish stopped process records
- **cli:** Add shell completion
- **mcp:** Serve requests concurrently
- **cli:** Add project directory override
- Show startup progress during hum up
- Order up by readiness dependencies
- Follow logs for multiple processes
- Add bounded crash relaunch policy
- Add one-shot tty input
- Strip terminal controls from bounded output
- Add interactive tty sessions

### Fixed

- **ci:** Remove invalid test assumptions
- Wire signal through MCP
- Preserve autonomous exit state
- **output:** Bound retained entry overhead
- Preserve tail continuation cursor
- Default logs to newest window
- **daemon:** Reclaim orphaned process groups
- **mcp:** Return object collection results
- **input:** Synchronize session release
- **wait:** Replay explicit terminal exits
- **input:** Acknowledge completed writes before exit
- **cli:** Polish errors, list, status, init, up, and aggregate logs output
- **mcp:** Describe every schema property, size tail reads correctly
- **project:** Clearer manifest errors, valid-key hints, consistent init template
- **protocol:** Omit zero event times from stream events
- **output:** Let tail exceed the default entry cap, generalize invalid-request wording
- **output:** Fit one maximum-size line in a default read
- **cli:** Report declared unstarted status, accept run options after name
- **app:** Retain exhausted records, guard pre-launch followers, skip readiness scans once ready
- **daemon:** Drain stalled followers, widen wire frame, isolate wire version
- **mcp:** Negotiate client protocol version
- **project:** Bound discovery commands, drop unused error aliases
- **cli:** Reject unknown commands and document global flags
- **cli:** Report manifest runtime drift
- Keep empty manifest up inert
- Preserve recovery state during up
- Flush follower errors before shutdown
- Cancel blocked tty writes

### Performance

- **output:** Single-pass tail reads, drop dead paths and unused exports

### Changed

- Share up orchestration
- Remove unused policy, root, and timeout aliases

### Documentation

- Clarify cursor tail semantics
- Finish README quickstart
- **cli:** Rewrite command help
- Update project description
- Add use case example
- Simplify README
- **skill:** Attach relaunch guidance to the restart bullet
- Fix MCP design anchor
- Show task runner forwarding
- Clarify readme example
- Show side-by-side log demo

## [v0.2.0](https://github.com/brettinternet/hum/compare/v0.1.1...v0.2.0) - 2026-09-04

### Added

- **cli:** Report follower counts
- **runtime:** Track session followers
- Complete durable supervision lifecycle
- Add durable follow and session removal
- **app:** Retain supervision sessions across launches

### Documentation

- Add terminal demo
- Align skill with durable sessions

## [v0.1.1](https://github.com/brettinternet/hum/compare/v0.1.0...v0.1.1) - 2026-09-04

### Added

- **cli:** Add source run task

### Fixed

- **cli:** Preserve readiness before exit
- **cli:** Keep followed logs free of cursor trailers

## [v0.1.0](https://github.com/brettinternet/hum/commits/v0.1.0) - 2026-09-04

### Added

- **plugin:** Package hum for Codex
- **cli:** Add consistent flag aliases
- **cli:** Ship shell-only skill
- **cli:** Scaffold manifests with init
- Expose process lifecycle over MCP
- Add project-scoped down command
- **cli:** Discover conventional dev entrypoints
- Add canonical process manifest
- **cli:** Add restart command
- **cli:** Add cursor-based wait command
- **cli:** Add process status command
- **cli:** Start daemon automatically
- Add process lifecycle commands
- Serve private daemon protocol
- Supervise project process trees
- **output:** Implement bounded process buffers
- **config:** Add typed runtime configuration
- Bootstrap Go CLI and project tooling

### Fixed

- Restore cross-platform CI
- **cli:** Satisfy init output checks
- Preserve MCP process contracts
- **app:** Harden restart lifecycle

### Changed

- Rename devproc to hum

### Documentation

- Condense design
- Streamline README
- Finish supervisor foundation release gate
- Move development notes out of README
- Define zero-config project discovery
- Make agent workflow manifest-first
- Update daemon lifecycle design
