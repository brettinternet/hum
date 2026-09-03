// Command hum-fixture is a deterministic child-process fixture for integration tests.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const usage = `usage: hum-fixture <mode> [arguments...]

modes:
  inspect <args...>
      Print SNAPSHOT JSON (argv, cwd, and HUM_TEST_* env) followed by
      separate raw stdout/stderr fragments, then exit with status 23.
  stream <marker>
      Write <marker>.started, emit live stdout/stderr fragments, report
      counted SIGINTs, and write <marker>.terminated on SIGTERM.
  burst <gate> <count>
      Emit alternating stdout:NNNN and stderr:NNNN lines, wait for <gate>
      after the first half, then emit the remaining lines.
  tree <marker> <graceful|ignore-term>
      Create a parent/child/grandchild in the inherited process group and
      write PID/readiness markers. SIGTERM writes .parent.term, .child.term,
      and .grandchild.term; graceful descendants wait for .release, while
      ignore-term descendants remain alive until forcibly killed.

Markers are created with mode 0600. The fixture never creates .release; the
caller creates it to release a graceful tree.`

const (
	fixturePollInterval = 2 * time.Millisecond
	treeReadyTimeout    = 10 * time.Second
)

type treeMode uint8

const (
	treeGraceful treeMode = iota + 1
	treeIgnoreTerm
)

func main() {
	code, err := run(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "hum-fixture: %v\n\n%s\n", err, usage)
		os.Exit(2)
	}
	os.Exit(code)
}

func run(args []string) (int, error) {
	if len(args) == 0 {
		return 0, errors.New("a mode is required")
	}
	switch args[0] {
	case "-h", "--help", "help":
		if len(args) != 1 {
			return 0, errors.New("help does not accept arguments")
		}
		_, err := os.Stdout.Write([]byte(usage + "\n"))
		return 0, err
	case "inspect":
		return runInspect()
	case "stream":
		if len(args) != 2 || args[1] == "" {
			return 0, errors.New("stream requires exactly one non-empty marker path")
		}
		return runStream(args[1])
	case "burst":
		if len(args) != 3 || args[1] == "" {
			return 0, errors.New("burst requires a gate path and positive count")
		}
		count, err := strconv.Atoi(args[2])
		if err != nil || count <= 0 {
			return 0, errors.New("burst count must be a positive integer")
		}
		return runBurst(args[1], count)
	case "tree":
		if len(args) != 3 || args[1] == "" {
			return 0, errors.New("tree requires a marker path and graceful or ignore-term")
		}
		mode, err := parseTreeMode(args[2])
		if err != nil {
			return 0, err
		}
		return runTreeParent(args[1], mode)
	case "tree-child":
		// These two modes are internal argv used only by tree's descendants.
		if len(args) != 3 || args[1] == "" {
			return 0, errors.New("internal tree-child invocation is malformed")
		}
		mode, err := parseTreeMode(args[2])
		if err != nil {
			return 0, err
		}
		return runTreeChild(args[1], mode)
	case "tree-grandchild":
		if len(args) != 3 || args[1] == "" {
			return 0, errors.New("internal tree-grandchild invocation is malformed")
		}
		mode, err := parseTreeMode(args[2])
		if err != nil {
			return 0, err
		}
		return runTreeGrandchild(args[1], mode)
	default:
		return 0, fmt.Errorf("unknown mode %q", args[0])
	}
}

type inspectSnapshot struct {
	Argv []string          `json:"argv"`
	Cwd  string            `json:"cwd"`
	Env  map[string]string `json:"env"`
}

func runInspect() (int, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return 0, fmt.Errorf("get working directory: %w", err)
	}
	snapshot, err := json.Marshal(inspectSnapshot{
		Argv: append([]string(nil), os.Args...),
		Cwd:  cwd,
		Env:  selectedTestEnvironment(),
	})
	if err != nil {
		return 0, fmt.Errorf("marshal snapshot: %w", err)
	}
	if err := writeFile(os.Stdout, "SNAPSHOT "+string(snapshot)+"\nstdout:raw with spaces \r\nstdout:partial"); err != nil {
		return 0, fmt.Errorf("write stdout: %w", err)
	}
	if err := writeFile(os.Stderr, "stderr:raw with spaces \r\nstderr:partial"); err != nil {
		return 0, fmt.Errorf("write stderr: %w", err)
	}
	return 23, nil
}

func selectedTestEnvironment() map[string]string {
	env := make(map[string]string)
	for _, item := range os.Environ() {
		key, value, ok := strings.Cut(item, "=")
		if !ok || !strings.HasPrefix(key, "HUM_TEST_") {
			continue
		}
		env[key] = value
	}
	return env
}

func runStream(marker string) (int, error) {
	signals := make(chan os.Signal, 8)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(signals)

	if err := writeMarker(marker+".started", "started"); err != nil {
		return 0, fmt.Errorf("write stream readiness marker: %w", err)
	}
	if err := writeFile(os.Stdout, "stdout:live with spaces \r\nstdout:live-partial"); err != nil {
		return 0, fmt.Errorf("write stream stdout: %w", err)
	}
	if err := writeFile(os.Stderr, "stderr:live with spaces \r\nstderr:live-partial"); err != nil {
		return 0, fmt.Errorf("write stream stderr: %w", err)
	}

	interrupts := 0
	for sig := range signals {
		switch sig {
		case syscall.SIGINT:
			interrupts++
			if err := writeFile(os.Stdout, fmt.Sprintf("\nfixture:sigint-%d\n", interrupts)); err != nil {
				return 0, fmt.Errorf("write SIGINT report: %w", err)
			}
		case syscall.SIGHUP:
			if err := writeFile(os.Stdout, "\nfixture:sighup\n"); err != nil {
				return 0, fmt.Errorf("write SIGHUP report: %w", err)
			}
		case syscall.SIGTERM:
			if err := writeMarker(marker+".terminated", "terminated"); err != nil {
				return 0, fmt.Errorf("write stream termination marker: %w", err)
			}
			return 0, nil
		}
	}
	return 0, nil
}

func runBurst(gate string, count int) (int, error) {
	half := count / 2
	if err := writeBurstLines(0, half); err != nil {
		return 0, err
	}
	if err := waitForFile(gate, 0); err != nil {
		return 0, fmt.Errorf("wait for burst gate: %w", err)
	}
	if err := writeBurstLines(half, count); err != nil {
		return 0, err
	}
	return 0, nil
}

func writeBurstLines(start, end int) error {
	for i := start; i < end; i++ {
		if err := writeFile(os.Stdout, fmt.Sprintf("stdout:%04d\n", i)); err != nil {
			return fmt.Errorf("write burst stdout line %d: %w", i, err)
		}
		if err := writeFile(os.Stderr, fmt.Sprintf("stderr:%04d\n", i)); err != nil {
			return fmt.Errorf("write burst stderr line %d: %w", i, err)
		}
	}
	return nil
}

func parseTreeMode(value string) (treeMode, error) {
	switch value {
	case "graceful":
		return treeGraceful, nil
	case "ignore-term":
		return treeIgnoreTerm, nil
	default:
		return 0, fmt.Errorf("tree mode must be graceful or ignore-term, got %q", value)
	}
}

func runTreeParent(marker string, mode treeMode) (int, error) {
	signals := treeSignals()
	defer signal.Stop(signals)

	if err := writePID(marker + ".parent.pid"); err != nil {
		return 0, fmt.Errorf("write parent PID marker: %w", err)
	}
	executable, err := fixtureExecutable()
	if err != nil {
		return 0, err
	}
	child := exec.Command(executable, "tree-child", marker, treeModeName(mode))
	// Leave SysProcAttr unset: the supervisor-created process group must be
	// inherited by both descendants on macOS and Linux.
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr
	if err := child.Start(); err != nil {
		return 0, fmt.Errorf("start tree child: %w", err)
	}
	if err := waitForFile(marker+".child.pid", treeReadyTimeout); err != nil {
		return 0, fmt.Errorf("wait for child PID marker: %w", err)
	}
	if err := waitForFile(marker+".grandchild.pid", treeReadyTimeout); err != nil {
		return 0, fmt.Errorf("wait for grandchild PID marker: %w", err)
	}
	if err := writeMarker(marker+".started", "started"); err != nil {
		return 0, fmt.Errorf("write tree readiness marker: %w", err)
	}

	for sig := range signals {
		switch sig {
		case syscall.SIGTERM:
			if err := writeMarker(marker+".parent.term", "term"); err != nil {
				return 0, fmt.Errorf("write parent TERM marker: %w", err)
			}
			if mode == treeIgnoreTerm {
				select {}
			}
			if err := waitForFile(marker+".release", 0); err != nil {
				return 0, fmt.Errorf("wait for tree release: %w", err)
			}
			if err := child.Wait(); err != nil {
				return 0, fmt.Errorf("wait for tree child: %w", err)
			}
			return 0, nil
		}
	}
	return 0, nil
}

func runTreeChild(marker string, mode treeMode) (int, error) {
	signals := treeSignals()
	defer signal.Stop(signals)

	if err := writePID(marker + ".child.pid"); err != nil {
		return 0, fmt.Errorf("write child PID marker: %w", err)
	}
	executable, err := fixtureExecutable()
	if err != nil {
		return 0, err
	}
	grandchild := exec.Command(executable, "tree-grandchild", marker, treeModeName(mode))
	// No SysProcAttr here: this child and its child inherit the original group.
	grandchild.Stdout = os.Stdout
	grandchild.Stderr = os.Stderr
	if err := grandchild.Start(); err != nil {
		return 0, fmt.Errorf("start tree grandchild: %w", err)
	}
	if err := waitForFile(marker+".grandchild.pid", treeReadyTimeout); err != nil {
		return 0, fmt.Errorf("wait for grandchild readiness: %w", err)
	}

	for sig := range signals {
		switch sig {
		case syscall.SIGTERM:
			if err := writeMarker(marker+".child.term", "term"); err != nil {
				return 0, fmt.Errorf("write child TERM marker: %w", err)
			}
			if mode == treeIgnoreTerm {
				select {}
			}
			if err := waitForFile(marker+".release", 0); err != nil {
				return 0, fmt.Errorf("wait for tree release: %w", err)
			}
			if err := grandchild.Wait(); err != nil {
				return 0, fmt.Errorf("wait for tree grandchild: %w", err)
			}
			return 0, nil
		}
	}
	return 0, nil
}

func runTreeGrandchild(marker string, mode treeMode) (int, error) {
	signals := treeSignals()
	defer signal.Stop(signals)

	if err := writePID(marker + ".grandchild.pid"); err != nil {
		return 0, fmt.Errorf("write grandchild PID marker: %w", err)
	}
	for sig := range signals {
		switch sig {
		case syscall.SIGTERM:
			if err := writeMarker(marker+".grandchild.term", "term"); err != nil {
				return 0, fmt.Errorf("write grandchild TERM marker: %w", err)
			}
			if mode == treeIgnoreTerm {
				select {}
			}
			if err := waitForFile(marker+".release", 0); err != nil {
				return 0, fmt.Errorf("wait for tree release: %w", err)
			}
			return 0, nil
		}
	}
	return 0, nil
}

func treeSignals() chan os.Signal {
	signals := make(chan os.Signal, 8)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	return signals
}

func treeModeName(mode treeMode) string {
	if mode == treeIgnoreTerm {
		return "ignore-term"
	}
	return "graceful"
}

func fixtureExecutable() (string, error) {
	if executable, err := os.Executable(); err == nil && executable != "" {
		return executable, nil
	}
	if os.Args[0] != "" {
		return os.Args[0], nil
	}
	return "", errors.New("locate fixture executable")
}

func writePID(path string) error {
	return writeMarker(path, strconv.Itoa(os.Getpid()))
}

func writeMarker(path, contents string) error {
	return os.WriteFile(path, []byte(contents), 0o600)
}

func writeFile(file *os.File, contents string) error {
	_, err := file.Write([]byte(contents))
	return err
}

// waitForFile uses marker polling rather than a readiness sleep. A zero
// timeout means wait until the caller creates the marker.
func waitForFile(path string, timeout time.Duration) error {
	var timer *time.Timer
	var timeoutCh <-chan time.Time
	if timeout > 0 {
		timer = time.NewTimer(timeout)
		defer timer.Stop()
		timeoutCh = timer.C
	}

	ticker := time.NewTicker(fixturePollInterval)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		select {
		case <-ticker.C:
		case <-timeoutCh:
			return fmt.Errorf("timed out waiting for %s", path)
		}
	}
}
