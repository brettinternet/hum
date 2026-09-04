# hum

`hum` is a local process supervisor for humans and coding agents. It keeps
project processes alive between commands and gives every client the same
bounded logs and lifecycle controls.

```sh
hum up
hum logs dev --follow
hum down
```

With no configuration, `hum up` finds one conventional `dev` task in Mise,
Task, Just, Make, `package.json`, Deno, Composer, `bin/dev`, or Phoenix.

For projects with multiple processes, commit a `hum.yaml`:

```yaml
version: 1
processes:
  web:
    argv: [bun, run, dev]
    ready:
      match: "Local:"
  worker:
    argv: [bun, run, worker]
```

```sh
hum up
hum status web
hum logs worker --tail 50
hum restart web
hum down
```

Run a one-off named process without a manifest:

```sh
hum run preview -- bun run preview
```

## Build

macOS and Linux are supported. Install [mise](https://mise.jdx.dev/), then:

```sh
mise install
task init
task cli:build
./bin/hum --help
```

## Coding agents

Codex users can install the bundled hum skill and MCP registration from a
repository checkout after placing `hum` on `PATH`:

```sh
codex plugin marketplace add .
codex plugin add hum@hum
```

`hum mcp` exposes the same project processes and bounded output over MCP for
manual registration with other coding agents.

See [coding-agent setup](docs/coding-agents.md) for Claude Code, Cursor, the MCP
tool surface, and the shell-only skill fallback.

## Documentation

- [Design and command semantics](docs/design.md)
- [Development setup and checks](docs/development.md)
- [Coding-agent setup](docs/coding-agents.md)
