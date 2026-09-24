//go:build !windows

package signals

import "syscall"

const signalsAvailable = true

// The table deliberately contains only the signals hum promises to expose.
var definitions = []signalDefinition{
	{name: "SIGHUP", number: syscall.SIGHUP},
	{name: "SIGINT", number: syscall.SIGINT},
	{name: "SIGQUIT", number: syscall.SIGQUIT},
	{name: "SIGKILL", number: syscall.SIGKILL},
	{name: "SIGTERM", number: syscall.SIGTERM},
	{name: "SIGUSR1", number: syscall.SIGUSR1},
	{name: "SIGUSR2", number: syscall.SIGUSR2},
}
