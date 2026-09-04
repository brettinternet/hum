# Coding-agent setup

`hum mcp` is an MCP server over stdio. Register it once by pointing the client
directly at the `hum` executable. Use an absolute path; do not wrap the command
in `sh -c` or include a project path in the registration.

## Register the MCP server

Claude Code:

```sh
claude mcp add --transport stdio hum -- /absolute/path/to/hum mcp
```

Codex CLI:

```sh
codex mcp add hum -- /absolute/path/to/hum mcp
```

Cursor and other clients that accept an `mcpServers` configuration:

```json
{
  "mcpServers": {
    "hum": {
      "command": "/absolute/path/to/hum",
      "args": ["mcp"]
    }
  }
}
```

Every tool call requires `project_root`, set to the project's absolute path.
The server exposes `start`, `up`, `down`, `list`, `status`, `logs`, `wait`,
`restart`, and `stop`. It has no arbitrary-command `run` tool; define repeatable
processes in `hum.yaml` and use the CLI for ad hoc commands.

See [the MCP design](design.md#mcp-stdio-adapter) for detailed behavior and
failure semantics.

## Deterministic environments

MCP does not run an interactive shell or activate Mise, nvm, direnv, or similar
hooks. If a process needs environment activation, commit a wrapper and use its
exact argv in `hum.yaml`:

```yaml
processes:
  web:
    argv: [./tools/run-with-project-env, bun, run, dev]
```

## Shell-only fallback

When MCP is unavailable, `hum skill` prints the embedded Agent Skills file for
installation in an agent's normal skill directory. MCP remains the preferred
integration.
