package cli

import (
	"context"
	"fmt"
	"io"
	"os"

	"hum/internal/app"
	"hum/internal/config"
	"hum/internal/daemon"
	mcpserver "hum/internal/mcp"
	"hum/internal/output"
	"hum/internal/process"
	"hum/internal/protocol"

	urfavecli "github.com/urfave/cli/v3"
)

func mcpCLICommand(version, buildTime string, writer io.Writer) *urfavecli.Command {
	return &urfavecli.Command{
		Name:      "mcp",
		Usage:     "serve project process lifecycle tools over stdio MCP",
		ArgsUsage: "",
		Description: "Run a stdio Model Context Protocol server for one-time coding-agent registration. " +
			"Every tool requires an absolute existing project_root. start and up accept only resolved explicit or discovered definitions and may start the daemon; status, logs, wait, restart, and stop control existing declared or ad_hoc records and never start it. " +
			"A process handed off by hum run is available as ad_hoc while its daemon retains the record; daemon shutdown or replacement loses that launch definition. " +
			"Explicit definitions use deterministic argv-based environment activation with the MCP server environment. " +
			"The nine tools are start, up, down, list, status, logs, wait, restart, and stop; run, serve, and shutdown are not MCP tools.",
		Action: func(ctx context.Context, cmd *urfavecli.Command) error {
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

func (mcpResolver) Resolve(_ context.Context, root string) (mcpserver.Resolution, error) {
	manifest, err := loadManifestOrEmpty(root)
	if err != nil {
		return mcpserver.Resolution{}, err
	}
	definitions := make([]mcpserver.Definition, 0, len(manifest.defs))
	for _, definition := range manifest.defs {
		definitions = append(definitions, mcpserver.Definition{
			Name: definition.Name, Source: definition.Source,
			Argv: append([]string(nil), definition.Argv...), Cwd: definition.Cwd,
			Ready: readinessConfig(definition),
		})
	}
	return mcpserver.Resolution{Root: manifest.root, Definitions: definitions}, nil
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
	return protocol.NewWaitResponse(protocol.WaitOutcome(result.Outcome), protocol.Cursor(result.Cursor), mcpExit(result.Exit)), nil
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
		Name: process.Name, Source: process.Source, Root: process.Root, PID: process.PID, PGID: process.PGID,
		Cwd: process.Cwd, Argv: append([]string(nil), process.Argv...), Start: process.Start,
		LaunchCursor: protocol.Cursor(process.LaunchCursor), State: string(process.State), Exit: mcpExit(process.Exit),
		ExitCode: process.ExitCode, ExitedAt: process.ExitedAt, RestartCount: process.RestartCount,
		Followers: process.Followers,
	}
	if process.NextCursor != 0 {
		cursor := protocol.Cursor(process.NextCursor)
		result.NextCursor = &cursor
	}
	if process.Readiness != nil {
		readiness := &protocol.Readiness{State: process.Readiness.State, Time: process.Readiness.Time, Match: process.Readiness.Match}
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
	return &protocol.Exit{Code: result.ExitCode, Time: result.ExitedAt, Error: message}
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
