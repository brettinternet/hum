# Minimal

One process that prints the time every second.

```sh
hum -F examples/minimal/hum.yaml up --detach
hum logs clock --tail 2   # the last two lines
hum down                  # stop it; logs are kept
```
