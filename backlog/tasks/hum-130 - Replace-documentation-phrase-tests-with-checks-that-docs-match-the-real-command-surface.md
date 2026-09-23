---
id: HUM-130
title: >-
  Replace documentation phrase tests with checks that docs match the real
  command surface
status: To Do
assignee: []
created_date: '2026-09-23 21:53'
updated_date: '2026-09-23 21:53'
labels:
  - docs
  - cli
  - mcp
milestone: m-4
dependencies: []
modified_files:
  - internal/cli/*_test.go
  - internal/mcp/*_test.go
  - internal/skill/*_test.go
  - README.md
  - docs/design.md
  - docs/coding-agents.md
  - internal/skill/SKILL.md
  - plugins/hum/skills/hum/SKILL.md
priority: medium
type: task
ordinal: 13000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Outcome: README.md, docs/*.md, both SKILL.md copies, `--help` text, and MCP tool descriptions can be rewritten without editing tests. Tests still fail when documentation references a command, flag, or tool that does not exist, or when an embedded copy drifts from its source. Today about 45 test functions pin exact words in prose. For example, TestLifecycleHelp (internal/cli/surface_test.go:491, 221 lines) requires `hum serve --help` to contain "diagnostics to stderr" and "pid and socket", and TestPinnedToolchainDocs (:740) repeats the mise.toml tool versions. Each prose edit breaks several of these tests, and each feature task adds more because its acceptance criteria were written as "help contains X". HUM-126 and HUM-127 rewrite most of that prose and would otherwise have to carry every pinned phrase forward.

Owner exception (2026-09-23): this task may delete the tests and assertions listed under Delete and Trim, which overrides the default rule that no test is deleted, skipped, or weakened. It may not remove anything else. Its Definition of Done replaces that rule with a scoped one.

Delete these whole functions. Each one asserts phrases in prose. Line numbers are from commit 465b774; if they have moved, find the function by name.
- internal/cli: TestAfterDocs after_docs_test.go:13; TestCompletionDocs completion_test.go:314; TestGlobalScopeDocs global_scope_test.go:103; TestDoctorHelpContract help_contract_test.go:123; TestLogsSystemStreamHelp help_contract_test.go:140; TestRestartReadinessDocs help_contract_test.go:157; TestInputDocs input_test.go:190; TestJSONErrorDocs json_errors_test.go:455; TestCursorDocs list_logs_test.go:536; TestLogsSinceDocs list_logs_test.go:1554; TestSignalExitDocs manifest_test.go:1817; TestUpProgressDocs manifest_test.go:2000; TestUpDriftDocs manifest_test.go:2630; TestMCPConcurrencyDescription mcp_test.go:127; TestProjectDirDocs project_dir_test.go:346; TestScopeDocs project_scope_test.go:214; TestColorDocs render_test.go:663; TestRestartPolicyDocs restart_policy_docs_test.go:13; TestUpRecoveryDocs restart_policy_test.go:117; TestSignalDocs signal_test.go:196; TestWaitHelpDescribesExitAndReadiness surface_test.go:436; TestLogsAggregateDocs surface_test.go:451; TestLifecycleHelp surface_test.go:491; TestOutputByteDocs surface_test.go:713; TestPinnedToolchainDocs surface_test.go:740; TestTerminalControlDocs terminal_control_docs_test.go:13; TestTTYHelpAndDocs tty_test.go:98; TestWaitObservedDocs wait_test.go:398; TestEventsHelp events_test.go:182; TestUpDescriptionNoDuplicateClause ergonomics_test.go:239.
- internal/mcp: TestMCPConcurrencyDocs server_test.go:1067; TestScopeDocs tools_test.go:2759.
- internal/skill: TestUpDriftDocs after_docs_test.go:9; TestAfterDocs after_docs_test.go:28; TestRestartPolicyDocs restart_policy_docs_test.go:9; TestTerminalControlDocs terminal_control_docs_test.go:9; TestInputDocs skill_test.go:12; TestResolvedProjectInstructions skill_test.go:72; TestTTYInstructions skill_test.go:227; TestScopeDocs skill_test.go:246.
Delete a test file once it becomes empty.

Trim these functions to their structural checks, deleting only the phrase loops:
- TestAttachSurface internal/cli/surface_test.go:255. Keep: attach exists, is distinct from logs, and advertises no unsupported flags.
- TestREADMEQuickstartStructure internal/cli/surface_test.go:15. Keep the heading order and section boundary.
- TestLogsStripTerminalControl internal/cli/terminal_control_test.go:14. Keep the behavior checks and drop the docs reads.
- TestMCPHelp internal/cli/mcp_test.go:30. Drop the "twelve tools" phrases.
- TestSkillContentHasRequiredFrontmatter internal/skill/skill_test.go:37. Keep the frontmatter shape.
- TestPluginPackageWiresSkillAndMCP internal/skill/skill_test.go:130. Keep the manifest wiring.
- Description-phrase checks inside TestGlobalScopeTools internal/mcp/tools_test.go:490, TestLogsSystemStream internal/mcp/tools_test.go:1889, and TestEvents internal/mcp/events_test.go:42.
If a deleted test holds a structural assertion, such as embedded skill equals source, move that assertion into a kept test before deleting.

Keep these unchanged because they are structural: TestHelpContract and TestHelpExitCodes (help_contract_test.go); TestMCPHelpScopeSchema, TestStatusAndWaitSurface, TestHelpWordBudgets, and TestHelpAdvertisesOnlySupportedScopeFlags (surface_test.go); TestSkillContentMatchesFileByteForByte and TestPluginMarketplaceEntry (internal/skill/skill_test.go); TestManifestSchemaContract (internal/project/manifest_schema_test.go:23); and every test that runs a command and checks behavior. internal/skill/SKILL.md and plugins/hum/skills/hum/SKILL.md differ on purpose, because the plugin copy is MCP-first. Do not add a test that requires them to be identical.

Add these structural replacements:
1. TestDocsReferenceRealCommandsAndFlags in internal/cli. Generalize TestSkillReferencesMatchRootCommandsAndFlags (surface_test.go:361), reusing its regexes and root-command walk, over the embedded skill, plugins/hum/skills/hum/SKILL.md, README.md, docs/design.md, and docs/coding-agents.md. Check each inline code span and each fenced code line that starts with `hum `. Every `hum WORD` must name a root command or alias, for example `ls`. Every `--flag` in the same span or line must be a flag of that command or a root flag. Ignore uppercase placeholders such as NAME and COMMAND. Replace the existing skill-only test with this one.
2. TestDocsCoverEveryCommand in internal/cli: every visible root command appears as `hum NAME` in docs/design.md.
3. TestDocsCoverEveryTool in internal/mcp: every tool name from toolDefinitions() appears in backticks in docs/coding-agents.md, and `hum mcp --help` output from cli.NewRootCommand names every tool. internal/mcp already imports internal/cli, and the reverse import would be a cycle. This check catches today's "twelve tools" help text at internal/cli/commands.go:35.
When a new check fails on current docs, fix the docs reference and note it in Implementation Notes.

Non-goals: rewriting prose (HUM-126, HUM-127); adding phrase tests; changing help or MCP text, except to fix a reference that a new check proves wrong.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 AC1 — `rg -n "^func (TestLifecycleHelp|TestPinnedToolchainDocs|TestRestartPolicyDocs|TestResolvedProjectInstructions|TestAfterDocs|TestUpDriftDocs|TestScopeDocs|TestTerminalControlDocs)\(" internal` prints nothing and exits 1.
- [ ] #2 AC2 — `go test ./internal/cli -run "^(TestDocsReferenceRealCommandsAndFlags|TestDocsCoverEveryCommand)$" -count=1 -v` and `go test ./internal/mcp -run "^TestDocsCoverEveryTool$" -count=1 -v` exit 0; Implementation Notes record that each check fails when a bogus `hum nosuchcommand` reference, a bogus `hum up --nosuchflag` reference, or a removed tool name is introduced temporarily.
- [ ] #3 AC3 — `go test ./internal/cli ./internal/mcp ./internal/skill ./internal/project ./cmd/hum -count=1` exits 0.
- [ ] #4 AC4 — `git diff --numstat main -- "*_test.go"` shows a net reduction in test lines; Implementation Notes record the number and list every deleted function and trimmed assertion.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 task ci passes on the final commit
- [ ] #2 Every checked acceptance criterion has an AC#N evidence line in Implementation Notes naming the command and its result
- [ ] #3 An independent verifier pass returned PASS for every acceptance criterion
- [ ] #4 The diff touches only the paths declared in the task's modified-file list, or the deviation is justified in Implementation Notes
- [ ] #5 No protected gate file was modified unless the owner labelled this task tooling
- [ ] #6 Only tests and assertions named in the Delete and Trim lists were removed; every other test is unchanged or strengthened
<!-- DOD:END -->
