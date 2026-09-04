package cli

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	urfavecli "github.com/urfave/cli/v3"
)

func TestFlagAliases(t *testing.T) {
	expected := map[string]map[string][]string{
		"serve":    {"daemon": {"d"}},
		"init":     {"json": {"j"}},
		"run":      {"detach": {"d"}, "json": {"j"}},
		"start":    {"no-wait": nil, "timeout": {"t"}, "json": {"j"}},
		"up":       {"no-wait": nil, "timeout": {"t"}, "json": {"j"}},
		"down":     {"json": {"j"}},
		"list":     {"all": {"a"}, "json": {"j"}},
		"status":   {"json": {"j"}},
		"logs":     {"stream": {"s"}, "tail": {"n"}, "after-cursor": {"c"}, "limit-bytes": {"b"}, "match": {"m"}, "follow": {"f"}, "json": {"j"}},
		"wait":     {"after-cursor": {"c"}, "match": {"m"}, "timeout": {"t"}, "json": {"j"}},
		"restart":  {"json": {"j"}},
		"stop":     {"json": {"j"}},
		"remove":   {"json": {"j"}},
		"shutdown": {"stop-processes": nil, "json": {"j"}},
		"mcp":      {},
		"skill":    {},
	}

	root := NewRootCommand("test", "test", &bytes.Buffer{}, &bytes.Buffer{})
	rootExpected := map[string][]string{
		"runtime-dir":       nil,
		"stop-grace":        nil,
		"output-bytes":      nil,
		"completed-records": nil,
	}
	flagAliasesAssertFlags(t, "hum", root.Flags, rootExpected)
	if got := urfavecli.HelpFlag.Names(); !reflect.DeepEqual(got, []string{"help", "h"}) {
		t.Fatalf("help names = %v, want [help h]", got)
	}
	if got := urfavecli.VersionFlag.Names(); !reflect.DeepEqual(got, []string{"version", "v"}) {
		t.Fatalf("version names = %v, want [version v]", got)
	}

	seenCommands := make(map[string]bool, len(root.Commands))
	for _, command := range root.Commands {
		want, ok := expected[command.Name]
		if !ok {
			t.Fatalf("unexpected command %q in alias surface", command.Name)
		}
		seenCommands[command.Name] = true
		flagAliasesAssertFlags(t, command.Name, cliCommandFlags(command), want)
	}
	for command := range expected {
		if !seenCommands[command] {
			t.Errorf("expected command %q not found", command)
		}
	}

	for command, flags := range expected {
		for name, aliases := range flags {
			for _, alias := range aliases {
				flagAliasesAssertHelpPair(t, command, name, alias)
			}
		}
	}
	flagAliasesAssertRootHelpPair(t, "help", "h")
	flagAliasesAssertRootHelpPair(t, "version", "v")

	collisionCases := []struct {
		name string
		root *urfavecli.Command
	}{
		{
			name: "duplicate alias",
			root: &urfavecli.Command{Name: "hum", Commands: []*urfavecli.Command{{Name: "child", Flags: []urfavecli.Flag{
				&urfavecli.BoolFlag{Name: "one", Aliases: []string{"x"}},
				&urfavecli.BoolFlag{Name: "two", Aliases: []string{"x"}},
			}}}},
		},
		{
			name: "canonical conflicts with alias",
			root: &urfavecli.Command{Name: "hum", Commands: []*urfavecli.Command{{Name: "child", Flags: []urfavecli.Flag{
				&urfavecli.BoolFlag{Name: "x"},
				&urfavecli.BoolFlag{Name: "two", Aliases: []string{"x"}},
			}}}},
		},
		{
			name: "builtin help conflict",
			root: &urfavecli.Command{Name: "hum", Commands: []*urfavecli.Command{{Name: "child", Flags: []urfavecli.Flag{
				&urfavecli.BoolFlag{Name: "other", Aliases: []string{"h"}},
			}}}},
		},
		{
			name: "builtin version conflict",
			root: &urfavecli.Command{Name: "hum", Version: "test", Flags: []urfavecli.Flag{
				&urfavecli.BoolFlag{Name: "other", Aliases: []string{"v"}},
			}},
		},
		{
			name: "inherited root conflict",
			root: &urfavecli.Command{Name: "hum", Flags: []urfavecli.Flag{
				&urfavecli.StringFlag{Name: "global"},
			}, Commands: []*urfavecli.Command{{Name: "child", Flags: []urfavecli.Flag{
				&urfavecli.BoolFlag{Name: "other", Aliases: []string{"global"}},
			}}}},
		},
	}
	for _, tc := range collisionCases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateCLICommandTree(tc.root); err == nil {
				t.Fatal("validateCLICommandTree returned nil for conflicting flags")
			}
		})
	}
}

func TestFlagAliasParity(t *testing.T) {
	type parityCase struct {
		command string
		short   []string
		long    []string
		flag    string
	}
	cases := []parityCase{
		{"serve", []string{"-d"}, []string{"--daemon"}, "daemon"},
		{"init", []string{"-j"}, []string{"--json"}, "json"},
		{"run", []string{"proc", "-d", "--", "echo"}, []string{"proc", "--detach", "--", "echo"}, "detach"},
		{"run", []string{"proc", "-j", "--", "echo"}, []string{"proc", "--json", "--", "echo"}, "json"},
		{"start", []string{"-t", "250ms"}, []string{"--timeout", "250ms"}, "timeout"},
		{"start", []string{"-j"}, []string{"--json"}, "json"},
		{"up", []string{"-t", "250ms"}, []string{"--timeout", "250ms"}, "timeout"},
		{"up", []string{"-j"}, []string{"--json"}, "json"},
		{"down", []string{"-j"}, []string{"--json"}, "json"},
		{"list", []string{"-a"}, []string{"--all"}, "all"},
		{"list", []string{"-j"}, []string{"--json"}, "json"},
		{"status", []string{"-j"}, []string{"--json"}, "json"},
		{"logs", []string{"-s", "stderr"}, []string{"--stream", "stderr"}, "stream"},
		{"logs", []string{"-n", "7"}, []string{"--tail", "7"}, "tail"},
		{"logs", []string{"-c", "8"}, []string{"--after-cursor", "8"}, "after-cursor"},
		{"logs", []string{"-b", "9"}, []string{"--limit-bytes", "9"}, "limit-bytes"},
		{"logs", []string{"-m", "ready"}, []string{"--match", "ready"}, "match"},
		{"logs", []string{"-f"}, []string{"--follow"}, "follow"},
		{"logs", []string{"-j"}, []string{"--json"}, "json"},
		{"wait", []string{"-c", "8"}, []string{"--after-cursor", "8"}, "after-cursor"},
		{"wait", []string{"-m", "ready"}, []string{"--match", "ready"}, "match"},
		{"wait", []string{"-t", "250ms"}, []string{"--timeout", "250ms"}, "timeout"},
		{"wait", []string{"-j"}, []string{"--json"}, "json"},
		{"restart", []string{"-j"}, []string{"--json"}, "json"},
		{"stop", []string{"-j"}, []string{"--json"}, "json"},
		{"shutdown", []string{"-j"}, []string{"--json"}, "json"},
	}
	for _, tc := range cases {
		t.Run(tc.command+"/"+tc.flag, func(t *testing.T) {
			short := flagAliasParityRun(t, tc.command, tc.short, tc.flag)
			long := flagAliasParityRun(t, tc.command, tc.long, tc.flag)
			if !reflect.DeepEqual(short, long) {
				t.Fatalf("short result = %#v, long result = %#v", short, long)
			}
		})
	}

	for _, tc := range []struct {
		name string
		args []string
	}{
		{"bare d", []string{"proc", "d", "--", "echo"}},
		{"bare j", []string{"proc", "j", "--", "echo"}},
		{"long d", []string{"proc", "--d", "--", "echo"}},
		{"long j", []string{"proc", "--j", "--", "echo"}},
		{"combined", []string{"proc", "-dj", "--", "echo"}},
		{"single-dash detach", []string{"proc", "-detach", "--", "echo"}},
		{"single-dash json", []string{"proc", "-json", "--", "echo"}},
		{"single-dash config", []string{"proc", "-runtime-dir", "/tmp", "--", "echo"}},
	} {
		t.Run("run rejects "+tc.name, func(t *testing.T) {
			result := flagAliasParityRun(t, "run", tc.args, "detach")
			if result.err == "" || !strings.Contains(result.err, "unknown run option") {
				t.Fatalf("error = %q, want unknown run option", result.err)
			}
		})
	}

	for _, tc := range []struct {
		name string
		args []string
	}{
		{"help", []string{"-h"}},
		{"version", []string{"-v"}},
	} {
		t.Run("root/"+tc.name, func(t *testing.T) {
			short := flagAliasParityRunRoot(t, tc.args)
			long := flagAliasParityRunRoot(t, []string{"--" + tc.name})
			if !reflect.DeepEqual(short, long) {
				t.Fatalf("short result = %#v, long result = %#v", short, long)
			}
		})
	}

	validationCases := []struct {
		name  string
		short []string
		long  []string
	}{
		{"logs tail", []string{"logs", "-n", "-1", "proc"}, []string{"logs", "--tail", "-1", "proc"}},
		{"logs bytes", []string{"logs", "-b", "-1", "proc"}, []string{"logs", "--limit-bytes", "-1", "proc"}},
		{"wait timeout", []string{"wait", "-t", "invalid", "proc"}, []string{"wait", "--timeout", "invalid", "proc"}},
		{"wait match", []string{"wait", "-m", "", "proc"}, []string{"wait", "--match", "", "proc"}},
	}
	for _, tc := range validationCases {
		t.Run("validation/"+tc.name, func(t *testing.T) {
			short := flagAliasParityRunRoot(t, tc.short)
			long := flagAliasParityRunRoot(t, tc.long)
			if (short.err == "") != (long.err == "") || short.exitCode != long.exitCode {
				t.Fatalf("short = %#v, long = %#v", short, long)
			}
		})
	}
}

type flagAliasParityResult struct {
	value         string
	set           bool
	outputMode    string
	daemonRequest bool
	detachRequest bool
	runName       string
	runArgv       []string
	stdout        string
	stderr        string
	exitCode      int
	err           string
}

func flagAliasesAssertFlags(t *testing.T, command string, flags []urfavecli.Flag, expected map[string][]string) {
	t.Helper()
	seenNames := make(map[string]string)
	observed := make(map[string][]string, len(flags))
	for _, flag := range flags {
		names := flag.Names()
		if len(names) == 0 {
			t.Fatalf("%s has unnamed flag", command)
		}
		observed[names[0]] = append([]string(nil), names[1:]...)
		for _, name := range names {
			if previous, ok := seenNames[name]; ok {
				t.Fatalf("%s flag name %q collides between %q and %q", command, name, previous, names[0])
			}
			seenNames[name] = names[0]
		}
	}
	if !reflect.DeepEqual(observed, expected) {
		t.Fatalf("%s aliases = %#v, want %#v", command, observed, expected)
	}
}

func flagAliasesAssertHelpPair(t *testing.T, command, name, alias string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	root := NewRootCommand("test", "test", &stdout, &stderr)
	if err := root.Run(context.Background(), []string{"hum", command, "--help"}); err != nil {
		t.Fatalf("%s help: %v", command, err)
	}
	flagAliasesRequireSameLine(t, stdout.String(), "--"+name, "-"+alias)
}

func flagAliasesAssertRootHelpPair(t *testing.T, name, alias string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	root := NewRootCommand("test", "test", &stdout, &stderr)
	if err := root.Run(context.Background(), []string{"hum", "--help"}); err != nil {
		t.Fatalf("root help: %v", err)
	}
	flagAliasesRequireSameLine(t, stdout.String(), "--"+name, "-"+alias)
}

func flagAliasesRequireSameLine(t *testing.T, output, long, short string) {
	t.Helper()
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, long) && strings.Contains(line, short) {
			return
		}
	}
	t.Fatalf("help does not render %s and %s together:\n%s", long, short, output)
}

func flagAliasParityRun(t *testing.T, commandName string, args []string, flagName string) flagAliasParityResult {
	t.Helper()
	var stdout, stderr bytes.Buffer
	root := NewRootCommand("test", "test", &stdout, &stderr)
	root.ExitErrHandler = func(context.Context, *urfavecli.Command, error) {}
	var result flagAliasParityResult
	for _, command := range root.Commands {
		if command.Name != commandName {
			continue
		}
		command.Action = func(_ context.Context, cmd *urfavecli.Command) error {
			if commandName == "run" {
				name, argv, err := parseRunArgs(cmd)
				if err != nil {
					return err
				}
				result.runName = name
				result.runArgv = argv
			}
			result.value = flagAliasParityValue(cmd, flagName)
			result.set = cmd.IsSet(flagName)
			result.outputMode = "human"
			if cmd.Bool("json") {
				result.outputMode = "json"
			}
			result.daemonRequest = cmd.Bool("daemon")
			result.detachRequest = cmd.Bool("detach")
			fmt.Fprintln(&stdout, result.outputMode)
			return urfavecli.Exit("", 7)
		}
		break
	}
	err := root.Run(context.Background(), append([]string{"hum", commandName}, args...))
	result.stdout = stdout.String()
	result.stderr = stderr.String()
	result.exitCode = flagAliasParityExitCode(err)
	if err != nil {
		result.err = err.Error()
	}
	return result
}

func flagAliasParityValue(cmd *urfavecli.Command, name string) string {
	switch name {
	case "daemon", "json", "detach", "all", "follow":
		return fmt.Sprint(cmd.Bool(name))
	case "tail", "limit-bytes":
		return fmt.Sprint(cmd.Int(name))
	case "after-cursor":
		return fmt.Sprint(cmd.Uint64(name))
	default:
		return cmd.String(name)
	}
}

func flagAliasParityRunRoot(t *testing.T, args []string) flagAliasParityResult {
	t.Helper()
	var stdout, stderr bytes.Buffer
	root := NewRootCommand("test", "test", &stdout, &stderr)
	root.ExitErrHandler = func(context.Context, *urfavecli.Command, error) {}
	err := root.Run(context.Background(), append([]string{"hum"}, args...))
	result := flagAliasParityResult{
		stdout:   stdout.String(),
		stderr:   stderr.String(),
		exitCode: flagAliasParityExitCode(err),
	}
	if err != nil {
		result.err = err.Error()
	}
	return result
}

func flagAliasParityExitCode(err error) int {
	if err == nil {
		return 0
	}
	if exitCoder, ok := err.(urfavecli.ExitCoder); ok {
		return exitCoder.ExitCode()
	}
	return 1
}
