# Worktrees

Every Git worktree is its own Hum project, so each one can run the same `hum.yaml` at once. Hum does not assign ports, so each worktree reads its own `PORT` from a Git-ignored `.env.local`. Requires `python3`.

```sh
echo PORT=8810 > examples/worktrees/.env.local
hum -F examples/worktrees/hum.yaml up --detach
curl -I http://127.0.0.1:8810/
```

If `.env.local` is missing, `hum up` fails instead of starting on a port another worktree may hold.

## With Git

Pick a free port for each new worktree:

```sh
git worktree add .worktrees/feature -b feature
echo PORT=8811 > .worktrees/feature/.env.local
hum -C .worktrees/feature up --detach
```

## With Worktrunk

Copy [`wt.toml`](wt.toml) to `.config/wt.toml`. Its `pre-start` hook writes `.env.local` in every new worktree:

```sh
wt hook pre-start                                # once, for the main checkout
wt switch --create feature -x hum -- up --detach # writes .env.local, then starts
```

Two branches can hash to the same port. If they do, edit one worktree's `.env.local`.
