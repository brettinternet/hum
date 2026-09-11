package cli

import (
	"os"
	"os/exec"
	"regexp"
	"testing"
)

const cliIsolatedTestEnv = "HUM_CLI_ISOLATED_TEST"

// runCLIIsolatedTest runs tests that mutate process-global state in their own
// process so their parent tests can safely overlap with the rest of the package.
// It returns true in the parent process after the isolated test has completed.
func runCLIIsolatedTest(t *testing.T) bool {
	t.Helper()
	if os.Getenv(cliIsolatedTestEnv) == t.Name() {
		return false
	}

	t.Parallel()
	command := exec.Command(os.Args[0], "-test.run=^"+regexp.QuoteMeta(t.Name())+"$", "-test.count=1")
	command.Env = append(os.Environ(), cliIsolatedTestEnv+"="+t.Name())
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("isolated test failed: %v\n%s", err, output)
	}
	return true
}
