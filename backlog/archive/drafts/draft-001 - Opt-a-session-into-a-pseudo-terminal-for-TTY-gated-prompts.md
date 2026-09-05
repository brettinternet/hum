---
id: DRAFT-001
title: Opt a session into a pseudo-terminal for TTY-gated prompts
status: Draft
assignee: []
created_date: '2026-09-05 14:07'
labels:
  - cli
  - daemon
  - process
  - output
  - protocol
  - docs
dependencies: []
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: a supervised session can opt into a pseudo-terminal so devtools that require a TTY before prompting (npx package confirmation, Prisma `migrate dev`, inquirer/survey/dialoguer-style prompts, tools that open `/dev/tty`) prompt and accept answers from an attached `hum run`. This is the piece HUM-022 deliberately excludes: HUM-022 gives a child a pipe, so `isatty(0)` is false and there is no controlling terminal; TTY-gated tools refuse to prompt or pick non-interactive defaults.

Proposed shape (to refine before promotion):
- Opt-in per session via `tty: true` in `hum.yaml` and `--tty` on ad hoc `run`; default stays pipes so retained logs remain clean for agents.
- Launch allocates a PTY, the child gets the slave as stdin/stdout/stderr and as its controlling terminal (own session + TIOCSCTTY); stop/down semantics unchanged.
- Output for a TTY session is one merged stream captured from the master; entries record raw bytes so colours/redraws survive for attached terminals. Decide whether bounded `logs`/MCP strip ANSI or expose it unchanged.
- Attached `hum run` puts the local terminal in raw mode, forwards bytes, mirrors window size (SIGWINCH -> TIOCSWINSZ), and forwards Ctrl+C as 0x03; a documented detach key sequence replaces Ctrl+C detach for TTY sessions.
- Reuse the HUM-022 input lease, input connection, launch-cursor scoping, and cleanup rules; the transport difference is master fd instead of pipe write end.
- Likely dependency: `github.com/creack/pty` (justify in go.mod).

Non-goals: multiplexing/scrollback, changing pipe-mode sessions, MCP TTY input, Windows.

Depends on HUM-022. Open questions: ANSI handling in bounded reads; whether readiness `match` runs against raw or stripped bytes; SIGHUP delivery to the child when the daemon closes the master.
<!-- SECTION:DESCRIPTION:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 An independent verifier pass returned PASS for every acceptance criterion
- [ ] #4 The diff touches only the paths declared in the task's modified-file list, or the deviation is justified in Implementation Notes
- [ ] #5 No test was deleted, skipped, or weakened
- [ ] #6 No protected gate file was modified unless the owner labelled this task tooling
<!-- DOD:END -->
