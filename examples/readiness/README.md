# Readiness

Each process uses a different check to tell Hum it is ready. `hum up` waits for all of them. Requires `python3` and `curl`.

| Process | Ready when |
| --- | --- |
| `logs` | its output matches `ready` |
| `http` | `GET /` returns 2xx |
| `tcp` | port 8802 accepts a connection |
| `exec` | `curl` exits 0 (use any command, such as `pg_isready`) |

```sh
hum -F examples/readiness/hum.yaml up --detach   # prints "ready" for each
hum status                                       # READINESS column
```

Checks run only at startup. Hum does not monitor health afterward.
