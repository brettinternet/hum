package integration

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

var (
	integrationHumPath     string
	integrationFixturePath string
)

func TestMain(m *testing.M) {
	binaryDir, err := os.MkdirTemp("", "hum-integration-binaries-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create integration binary directory: %v\n", err)
		os.Exit(1)
	}

	integrationHumPath = filepath.Join(binaryDir, "hum")
	integrationFixturePath = filepath.Join(binaryDir, "hum-fixture")
	if err := buildIntegrationBinary(integrationHumPath, "../cmd/hum"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		_ = os.RemoveAll(binaryDir)
		os.Exit(1)
	}
	if err := buildIntegrationBinary(integrationFixturePath, "../internal/testutil/cmd/hum-fixture"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		_ = os.RemoveAll(binaryDir)
		os.Exit(1)
	}

	code := m.Run()
	if err := os.RemoveAll(binaryDir); err != nil {
		fmt.Fprintf(os.Stderr, "remove integration binary directory: %v\n", err)
		code = 1
	}
	os.Exit(code)
}

func buildIntegrationBinary(output, packagePath string) error {
	cmd := exec.Command("go", "build", "-o", output, packagePath)
	if buildOutput, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("build %s: %w\n%s", packagePath, err, buildOutput)
	}
	return nil
}

func integrationHum(t testing.TB) string {
	t.Helper()
	return integrationHumPath
}

func integrationFixture(t testing.TB) string {
	t.Helper()
	return integrationFixturePath
}
