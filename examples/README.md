# Examples

Each folder holds one small `hum.yaml` and a README. Run an example from the repository root:

```sh
hum -F examples/minimal/hum.yaml up --detach
hum status
hum remove --all   # stop and forget everything before the next example
```

To use one in your own project, copy its `hum.yaml` to the root of your Git repository and drop `-F`.

| Example | Shows |
| --- | --- |
| [minimal](minimal/) | One process: start, read logs, stop |
| [readiness](readiness/) | `match`, `http`, `tcp`, and `exec` ready checks |
| [dependencies](dependencies/) | Start order with `after` and a setup step with `exit: 0` |
| [restart](restart/) | Relaunch after a crash with `restart: on-failure` |
| [worktrees](worktrees/) | A different port in each worktree, by hand or with Worktrunk |
| [interactive](interactive/) | Type into a process with `tty: true` |

[`hum.example.yaml`](../hum.example.yaml) lists every option.
