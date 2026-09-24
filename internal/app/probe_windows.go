package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"hum/internal/output"
	"hum/internal/process"
)

const windowsProbe = true
const windowsStop = true

// The Unix-only probe branch in app.go is unreachable on Windows. A Windows
// probe must use the same owned-tree launch as supervised processes.
func configureProbe(cmd *exec.Cmd) {}
func killProbe(pid int)            {}

func stopOrphanChild(child Child) error {
	stopper, ok := child.(interface{ Stop() error })
	if !ok {
		return errors.New("windows child has no owned-tree stop capability")
	}
	return stopper.Stop()
}

func runWindowsReadinessProbe(parent context.Context, argv []string, cwd string, env []string, maxBytes int) (string, error) {
	store, err := output.NewStore(output.Limits{})
	if err != nil {
		return "", err
	}
	child, err := process.Start(process.Spec{
		Dir: cwd, Argv: argv, Env: env, Output: store,
		MaxLineBytes: max(1, maxBytes),
	})
	if err != nil {
		return capProbeDiagnostic(fmt.Sprintf("probe start: %v", err), maxBytes), err
	}
	select {
	case <-parent.Done():
	case <-child.LeaderDone():
	}
	// Even after the leader exits, terminate any descendants it left behind.
	if err := child.Stop(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return "", err
	}
	result := child.Wait()
	if err := parent.Err(); err != nil {
		return "", err
	}
	if result.Err != nil {
		return "", result.Err
	}
	if result.ExitCode != 0 {
		err := fmt.Errorf("exit status %d", result.ExitCode)
		message := fmt.Sprintf("probe exit: %v", err)
		if entries, readErr := store.Read(output.ReadOptions{}); readErr == nil {
			for _, entry := range entries.Entries {
				message += ": " + entry.Text
			}
		}
		return capProbeDiagnostic(message, maxBytes), err
	}
	return "", nil
}
