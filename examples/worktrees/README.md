# Worktrees

Every Git worktree is its own Hum project, so each one can run the same `hum.yaml` at once. Hum does not assign ports: this example commits `PORT=8810` in `.env`, then lets a Git-ignored `.env.local` override it. Requires `python3`.

```sh
echo PORT=8811 > examples/worktrees/.env.local
hum -F examples/worktrees/hum.yaml up --detach
curl -I http://127.0.0.1:8811/
```

If `.env.local` is missing, `hum up` falls back to the committed `.env` value. A port collision fails loudly when the server binds; use `hum status web` to inspect the listener if the app silently falls back to another port. Be aware that a default service URL can still connect one worktree to another worktree's service. To fail launch preflight whenever the local override is missing, list `.env.local` as a plain string instead of `{path: .env.local, required: false}`.

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
