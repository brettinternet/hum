# Interactive

`greeter` asks for a name. `tty: true` runs it in a pseudo-terminal so you can answer.

```sh
hum -F examples/interactive/hum.yaml up --detach
hum attach greeter                  # type a name; Ctrl+] detaches
hum input greeter --text $'Ada\n'   # or send one answer without attaching
hum logs greeter                    # name? Ada / hello, Ada
```

Only one client can type at a time. Input is sent once and never queued.
