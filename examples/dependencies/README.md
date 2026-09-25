# Dependencies

`api` needs `migrate`, which needs `db`. Each process starts only after everything in its `after` list is ready. `migrate` is a one-shot setup step: it counts as ready when it exits 0.

```sh
hum -F examples/dependencies/hum.yaml up --detach   # db → migrate → api
hum status migrate                                  # exited, ready
```

If `migrate` exits non-zero, `api` does not start. `hum -F examples/dependencies/hum.yaml up api` starts `api` and everything it needs.
