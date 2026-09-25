# Worktrees

Every Git worktree is its own Hum project, but two servers cannot share a port. Hum does not assign ports; each worktree reads its own from `ports.env`. Requires `python3`.

```sh
hum -F examples/worktrees/hum.yaml up --detach
curl -I http://127.0.0.1:8810/
```

In a real project, add `ports.env` to `.gitignore` and give each worktree its own:

```sh
echo PORT=8811 > .worktrees/feature/ports.env
hum -C .worktrees/feature up --detach
```
