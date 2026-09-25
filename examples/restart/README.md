# Restart

`flaky` crashes every two seconds. Hum relaunches it with growing delays, then gives up after five attempts.

```sh
hum -F examples/restart/hum.yaml up --detach
hum events flaky   # exit, relaunch_scheduled, relaunch_attempt, ...
hum stop flaky     # stop always wins over restarts
```
