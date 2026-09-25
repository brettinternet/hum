# Examples

Each folder holds one small `hum.yaml` and a README. Run an example from the repository root
with `-F`, or run it from its own directory using nearest-manifest selection:

```sh
hum -F examples/minimal/hum.yaml up --detach
hum status
hum remove --all   # stop and forget everything before the next example

cd examples/interactive && hum up --detach
```

To use one in your own project, copy its `hum.yaml` into the repository or application directory
and run `hum up` there; the nearest manifest is selected automatically.

| Example | Shows |
| --- | --- |
| [minimal](minimal/) | One process: start, read logs, stop |
| [readiness](readiness/) | `match`, `http`, `tcp`, and `exec` ready checks |
| [dependencies](dependencies/) | Start order with `after` and a setup step with `exit: 0` |
| [restart](restart/) | Relaunch after a crash with `restart: on-failure` |
| [worktrees](worktrees/) | A different port in each worktree, by hand or with Worktrunk |
| [interactive](interactive/) | Type into a process with `tty: true` |

[`hum.example.yaml`](../hum.example.yaml) lists every option.
