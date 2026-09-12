package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"hum/internal/app"
	"hum/internal/config"
	"hum/internal/daemon"
	mcpserver "hum/internal/mcp"
	"hum/internal/output"
	"hum/internal/process"
	"hum/internal/project"
	"hum/internal/protocol"

	urfavecli "github.com/urfave/cli/v3"
)

func mcpCLICommand(version, buildTime string, writer io.Writer) *urfavecli.Command {
	return &urfavecli.Command{
		Name:      "mcp",
		Usage:     "serve project process lifecycle tools over stdio MCP",
		ArgsUsage: "",
		Description: "Run a stdio Model Context Protocol server for one-time coding-agent registration. " +
			"Requests with IDs run concurrently up to 64 in flight; a 65th request returns -32001 without starting, duplicate in-flight IDs return -32600, and notifications/cancelled returns -32800. Responses are serialized, and EOF or parent cancellation cancels handlers and joins the response writer. " +
			"Every tool accepts scope (project by default or global). project_root is required for project and rejected for global; global addresses machine-wide ad_hoc retained sessions, and list all includes them. up honors manifest after readiness dependencies with concurrent roots, lexical results, and sorted direct blocked_by skips; no_wait is rejected before daemon contact when after is declared. start is explicitly named and never pulls in prerequisites. start and up accept only resolved explicit or discovered definitions and may start the daemon; status, logs, wait, input, restart, and stop control existing declared or ad_hoc records and never start it. " +
			"A process handed off by hum run is available as ad_hoc while its daemon retains the record; daemon shutdown or replacement loses that launch definition. " +
			"Bounded child-output logs and matches use terminal-control-stripped text, while system entries, stored bytes, cursors, and limit accounting remain raw; there is no --raw flag or other raw opt-out. Logs match context expands eligible entries from one immutable snapshot before tail and whole-entry bounds. " +
			"MCP wait timeout results include process_observed from the same daemon wait request without an extra round trip; false includes no-process guidance. " +
			"Explicit definitions use deterministic argv-based environment activation with the MCP server environment. ready.exec uses exact direct argv without a shell, probes immediately, retries serially after failures at the configured interval (1s by default), inherits launched cwd/environment, retains only a bounded terminal diagnostic, and never retains probe output; readiness gates startup rather than liveness. " +
			"The twelve tools are start, up, down, list, status, logs, wait, input, restart, stop, remove, and signal; run, serve, and shutdown are not MCP tools.",
		Action: func(ctx context.Context, cmd *urfavecli.Command) error {
			if err := rejectProjectOverride(cmd, "mcp"); err != nil {
				return err
			}
			if err := requireNoArgs(cmd, "mcp"); err != nil {
				return err
			}
			cfg, err := cliConfig(cmd, version, buildTime)
			if err != nil {
				return err
			}
			server := mcpserver.NewServer(mcpserver.Options{
				Resolver:      mcpResolver{},
				ClientFactory: mcpClientFactory(cfg),
				Environment:   manifestProcessEnv,
				Version:       version,
			})
			return server.Serve(nonNilContext(ctx), os.Stdin, writer)
		},
	}
}

type mcpResolver struct{}

func (mcpResolver) Resolve(ctx context.Context, root string) (mcpserver.Resolution, error) {
	return mcpResolver{}.ResolveManifest(ctx, root, "")
}

func (mcpResolver) ResolveManifest(ctx context.Context, root, filename string) (mcpserver.Resolution, error) {
	var (
		rootPath string
		defs     []project.Definition
		err      error
	)
	if filename == "" {
		manifest, loadErr := loadManifestOrEmpty(ctx, root)
		if loadErr != nil {
			return mcpserver.Resolution{}, loadErr
		}
		rootPath, defs = manifest.root, manifest.defs
	} else {
		selection, selectionErr := project.ResolveManifestPath(root, root, filename)
		if selectionErr != nil {
			return mcpserver.Resolution{}, selectionErr
		}
		defs, err = project.ResolveExplicitDefinitions(ctx, selection)
		if err != nil {
			return mcpserver.Resolution{}, err
		}
		rootPath = selection.Root
	}
	definitions := make([]mcpserver.Definition, 0, len(defs))
	for _, definition := range defs {
		definitions = append(definitions, mcpserver.Definition{
			Name: definition.Name, Source: definition.Source,
			Argv: append([]string(nil), definition.Argv...), Cwd: definition.Cwd,
			Ready: readinessConfig(definition), After: append([]string{}, definition.After...), TTY: definition.TTY, Restart: restartPolicy(definition),
		})
	}
	return mcpserver.Resolution{Root: rootPath, Definitions: definitions}, nil
}

func mcpClientFactory(cfg config.Config) mcpserver.ClientFactory {
	return func(ctx context.Context, ensure bool) (mcpserver.Client, error) {
		var client *daemon.Client
		var err error
		if ensure {
			client, err = runDaemonClient(ctx, cfg)
		} else {
			client, err = daemonClient(ctx, cfg)
		}
		if err != nil {
			if daemonUnavailable(err) {
				return nil, fmt.Errorf("%w: %v", mcpserver.ErrDaemonUnavailable, err)
			}
			return nil, err
		}
		return &mcpDaemonClient{client: client}, nil
	}
}

type mcpDaemonClient struct{ client *daemon.Client }

func (c *mcpDaemonClient) StartupWarnings() []protocol.StartupWarning {
	if c == nil || c.client == nil {
		return nil
	}
	return c.client.StartupWarnings()
}

func (c *mcpDaemonClient) Close() error { return c.client.Close() }
func (c *mcpDaemonClient) Start(ctx context.Context, request protocol.StartRequest) (protocol.Process, error) {
	process, err := c.client.Start(ctx, request)
	return mcpProcess(process), err
}
func (c *mcpDaemonClient) List(ctx context.Context, request protocol.ListRequest) ([]protocol.Process, error) {
	processes, err := c.client.List(ctx, request)
	if err != nil {
		return nil, err
	}
	result := make([]protocol.Process, 0, len(processes))
	for _, process := range processes {
		result = append(result, mcpProcess(process))
	}
	return result, nil
}
func (c *mcpDaemonClient) Get(ctx context.Context, request protocol.GetRequest) (protocol.Process, error) {
	process, err := c.client.Get(ctx, request)
	return mcpProcess(process), err
}
func (c *mcpDaemonClient) Output(ctx context.Context, request protocol.OutputRequest) (protocol.OutputResult, error) {
	result, err := c.client.Output(ctx, request)
	return mcpOutput(result), err
}
func (c *mcpDaemonClient) Wait(ctx context.Context, request protocol.WaitRequest) (protocol.WaitResponse, error) {
	result, err := c.client.Wait(ctx, request)
	if err != nil {
		return protocol.WaitResponse{}, err
	}
	response := protocol.NewWaitResponse(protocol.WaitOutcome(result.Outcome), protocol.Cursor(result.Cursor), mcpExit(result.Exit))
	response.ProcessObserved = result.ProcessObserved
	return response, nil
}
func (c *mcpDaemonClient) Input(ctx context.Context, request mcpserver.InputRequest) (mcpserver.InputResult, error) {
	result, err := c.client.Input(ctx, daemon.InputRequest{Name: request.Name, Scope: request.Scope, Cwd: request.Cwd, Root: request.Root, Data: append([]byte(nil), request.Data...)})
	if err != nil {
		var notRunning *daemon.SessionNotRunningError
		if errors.As(err, &notRunning) {
			return mcpserver.InputResult{}, &mcpserver.SessionNotRunningError{Name: request.Name}
		}
		return mcpserver.InputResult{}, err
	}
	return mcpserver.InputResult{Name: request.Name, Bytes: result.Bytes, LaunchCursor: result.LaunchCursor}, nil
}
func (c *mcpDaemonClient) SignalResult(ctx context.Context, request protocol.SignalRequest) (protocol.SignalResult, error) {
	return c.client.SignalResult(ctx, daemon.SignalRequest{Name: request.Name, Scope: request.Scope, Cwd: request.Cwd, Signal: request.Signal})
}
func (c *mcpDaemonClient) Stop(ctx context.Context, request protocol.StopRequest) error {
	return c.client.Stop(ctx, request)
}
func (c *mcpDaemonClient) Remove(ctx context.Context, request protocol.RemoveRequest) error {
	return c.client.Remove(ctx, request)
}
func (c *mcpDaemonClient) Restart(ctx context.Context, request protocol.RestartRequest) (protocol.Process, error) {
	process, err := c.client.Restart(ctx, request)
	return mcpProcess(process), err
}

func mcpProcess(process app.Process) protocol.Process {
	result := protocol.Process{
		Name: process.Name, Source: process.Source, Scope: process.Scope, Root: process.Root, TTY: process.TTY, PID: process.PID, PGID: process.PGID,
		Cwd: process.Cwd, Argv: append([]string(nil), process.Argv...), Start: process.Start,
		LaunchCursor: protocol.Cursor(process.LaunchCursor), State: string(process.State), Exit: mcpExit(process.Exit),
		ExitCode: process.ExitCode, ExitedAt: process.ExitedAt, RestartCount: process.RestartCount,
		Followers: process.Followers, Restart: string(process.Restart), StopGrace: process.StopGrace, StopGraceInherited: process.StopGraceInherited, Relaunches: process.Relaunches,
		NextLaunchAt: process.NextLaunchAt,
	}
	if process.NextCursor != 0 {
		cursor := protocol.Cursor(process.NextCursor)
		result.NextCursor = &cursor
	}
	if process.Readiness != nil {
		readiness := &protocol.Readiness{Method: process.Readiness.Method, Argv: append([]string(nil), process.Readiness.Argv...), Interval: process.Readiness.Interval, State: process.Readiness.State, Time: process.Readiness.Time, Match: process.Readiness.Match, Diagnostic: process.Readiness.Diagnostic}
		if process.Readiness.Cursor != nil {
			cursor := protocol.Cursor(*process.Readiness.Cursor)
			readiness.Cursor = &cursor
		}
		result.Readiness = readiness
	}
	return result
}

func mcpExit(result *process.Result) *protocol.Exit {
	if result == nil {
		return nil
	}
	message := ""
	if result.Err != nil {
		message = result.Err.Error()
	}
	exit := &protocol.Exit{Code: result.ExitCode, Time: result.ExitedAt, Error: message}
	if result.Signal != nil {
		exit.Signal = &protocol.SignalInfo{Name: result.Signal.Name, Number: result.Signal.Number}
	}
	return exit
}

func mcpOutput(result output.ReadResult) protocol.OutputResult {
	entries := make([]protocol.OutputEntry, 0, len(result.Entries))
	for _, entry := range result.Entries {
		entries = append(entries, protocol.OutputEntry{Cursor: protocol.Cursor(entry.Cursor), Stream: mcpStream(entry.Stream), Time: entry.Time, Text: entry.Text})
	}
	return protocol.OutputResult{Entries: entries, Next: mcpCursor(result.Next), Oldest: mcpCursor(result.Oldest), Latest: mcpCursor(result.Latest), EvictedThrough: mcpCursor(result.EvictedThrough), Truncated: result.Truncated, More: result.More}
}
func mcpStream(stream output.Stream) protocol.Stream {
	switch stream {
	case output.Stdout:
		return protocol.StreamStdout
	case output.Stderr:
		return protocol.StreamStderr
	case output.System:
		return protocol.StreamSystem
	default:
		return ""
	}
}

func mcpCursor(cursor *output.Cursor) *protocol.Cursor {
	if cursor == nil {
		return nil
	}
	value := protocol.Cursor(*cursor)
	return &value
}

// Compile-time checks keep the adapter aligned with the protocol-only seam.
var _ mcpserver.Resolver = mcpResolver{}
var _ mcpserver.Client = (*mcpDaemonClient)(nil)
