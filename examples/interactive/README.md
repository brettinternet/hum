# Interactive

`greeter` asks for a name. `tty: true` runs it in a pseudo-terminal so you can answer.

From `examples/interactive`:

```sh
hum -F ./hum.yaml up -d
hum -F ./hum.yaml attach greeter           # type Bob, Enter; Ctrl-] detaches without stopping it
hum -F ./hum.yaml input greeter --text $'Ada\n' # one answer without attaching
hum -F ./hum.yaml logs greeter --tail 1    # prompt and next cursor on separate lines
hum -F ./hum.yaml logs greeter --stream stdout
hum -F ./hum.yaml logs greeter --stream system
```

The greeter's `name? ` prompt is unterminated. Replayed system events such as
`greeter launched` can therefore appear beside it in combined logs; they are
not bytes sent to child stdin. The terminal may also echo what you type while
attached. Only `attach` (the single input owner) and `input --text` send child
stdin; include `\n` in one-shot input to answer the prompt. `--stream stdout`
shows child output without system events; `--stream system` shows lifecycle
events. Retained output and `--json` keep the child's original bytes.
