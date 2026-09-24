---
id: HUM-118
title: Provide a private Windows daemon transport and recoverable runtime ownership
status: Done
assignee: []
created_date: '2026-09-23 20:49'
updated_date: '2026-09-24 12:39'
labels:
  - daemon
  - security
  - architecture
  - reviewed
milestone: m-5
dependencies:
  - HUM-117
modified_files:
  - internal/daemon/runtime.go
  - internal/daemon/client.go
  - internal/daemon/server.go
  - internal/daemon/peer.go
  - internal/daemon/*windows*.go
  - internal/daemon/*unix*.go
  - internal/daemon/*_test.go
  - internal/config/config.go
  - internal/config/*windows*.go
  - internal/config/*unix*.go
  - internal/config/*_test.go
  - .taskfiles/windows.yaml
  - go.mod
  - go.sum
priority: high
type: feature
ordinal: 10000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: one native Windows daemon owns the runtime and serves the existing bounded JSON wire protocol to the same user only. Today internal/daemon/runtime.go binds a Unix socket and relies on flock, Unix permissions, and PID/PGID plus start identity for stale recovery. checkPrivateDir (runtime.go:405) refuses a runtime directory another user owns through syscall.Stat_t, and verifyPeer (internal/daemon/peer.go, with peerUID in peer_darwin.go and peer_linux.go) refuses a socket peer running as another user at both ends of every connection. internal/daemon/client.go and server.go dial Unix sockets. internal/config/config.go:126 and internal/daemon/runtime.go:137 default the runtime directory to temp plus os.Getuid(), which returns -1 on Windows.

Scope: private Windows IPC (a named pipe with an explicit current-user-only ACL, or an equally secure equivalent), cross-process startup exclusion, a Windows runtime directory, ACLs, and stale-owner recovery. Keep the existing wire protocol, the single-owner invariant, the refusal to displace an unverifiable live owner, and startup reconciliation of recorded children. Coordinate persisted group identity and crash cleanup with HUM-117. A Windows Job Object normally closes when the daemon dies, so distinguish children that are already dead from children whose ownership is uncertain, and never terminate an unverified reused PID. Keep Unix paths and permissions unchanged. Give Windows the same owner and peer checks, so neither end trusts a runtime directory or endpoint that another user owns or serves, including a pipe name another user created first. Validate same-user isolation and test concurrent startup.

Non-goals: a network-facing TCP endpoint, CLI autostart, ConPTY, and release packaging.

Read internal/daemon/runtime.go:137, :345-422, :540-698, and :755-770, internal/daemon/client.go:123-157, internal/daemon/peer.go, the listen path in internal/daemon/server.go, and internal/config/config.go:119-127. Do not remove existing Unix tests to get a green Windows gate.

Windows verification: after the owner approves, push HEAD to `windows/<task-id>` and run `task windows:watch` (HUM-122). Windows-only tests live in `*_windows_test.go` files so the Windows CI job runs them. Add every package this task ports to WINDOWS_PACKAGES in .taskfiles/windows.yaml.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 AC1 — After the owner-approved `git push origin HEAD:windows/HUM-118`, `task windows:watch` exits 0 on macOS, with WINDOWS_PACKAGES including ./internal/daemon ./internal/config. Windows tests show that a real client can round-trip over the private transport and that a second, simultaneous daemon cannot acquire the same runtime.
- [x] #2 AC2 — On macOS, `rg -n "func TestWindows" internal/daemon` lists tests in `*_windows_test.go` files that inspect transport and runtime ACLs to confirm access is no broader than intended, and that cover stale-owner and crash recovery, refusal on PID reuse or ownership mismatch, and reconciliation of recorded children without killing unrelated processes. AC1 run executes these tests.
- [x] #3 AC3 — On macOS, `go test ./internal/daemon ./internal/config -count=1` exits 0; socket permission, locking, and existing stale-runtime recovery tests still pass.
- [x] #4 AC4 — On macOS, `GOOS=windows GOARCH=amd64 go vet ./internal/daemon ./internal/config` exits 0.
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

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
AC1 — Approved git push origin HEAD:windows/HUM-118 on 315d00d; task windows:watch exited 0 for CI run 35965044098 (native Windows daemon/config tests passed, client round trip and simultaneous-process contention). WINDOWS_PACKAGES includes ./internal/daemon and ./internal/config.
AC2 — rg -n "func TestWindows" internal/daemon lists runtime_windows_test.go ACL, preempted pipe, stale owner, real crashed daemon/child Job Object reconciliation, PID reuse, mismatched ownership and unrelated-process safety tests; CI 35965044098 executed them.
AC3 — go test ./internal/daemon ./internal/config -count=1 exited 0 on macOS; Unix socket, lock, stale-recovery tests retained.
AC4 — GOOS=windows GOARCH=amd64 go vet ./internal/daemon ./internal/config exited 0.
Review — independent verifier PASS AC1, AC3, AC4; focused recheck PASS AC2 after adding real crash recovery test. No item-scoped defects remain.
Delivery — commits 79b3f3b, aeed8de, 1672ce6, c84968d, 315d00d fast-forward merged to main. GOFLAGS=-p=1 task ci exited 0 at 315d00d; unbounded-parallel task ci intermittently timed out unrelated TestAttachStreamsBurstWithoutAborting, which passed focused and in serialized full gate. Diff touches only declared modified files; Unix tests retained with !windows tags and Windows-specific coverage added; no protected gate files touched. Next: mark criteria and Done, commit provider evidence, remove owned worktree.

Review (86b26b2): with LOCALAPPDATA and APPDATA both unset, the default runtime became the relative path hum-runtime, so invocations from different directories could start separate daemons; config and daemon now fall back to os.TempDir() (TestWindowsDefaultRuntimeDirIsAbsoluteWithoutAppData, native Windows CI run 35999701233 passed). Declined: same-process retry after a failed bind (daemons retry from a new process) and a stricter concurrent-startup test (LockFileEx serialization is sound). No follow-up.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Private Windows named-pipe daemon transport and recoverable single-owner runtime shipped to main; native Windows CI, macOS regression, cross-vet, and full gate passed. No remaining blocker.
<!-- SECTION:FINAL_SUMMARY:END -->
