//go:build darwin || linux

package integration

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"hum/internal/testutil"
)

func TestInitThenUp(t *testing.T) {
	fixture := testutil.BuildFixture(t)
	hum := testutil.BuildHum(t)
	projectRoot := zeroConfigCanonicalTempDir(t)
	shimDir := t.TempDir()
	runtimeDir := testutil.RuntimeDir(t)
	marker := filepath.Join(projectRoot, "managed")
	launchLog := filepath.Join(projectRoot, "launches.log")
	bodyMarker := filepath.Join(projectRoot, "body-executed")
	setupInitPackage(t, projectRoot, shimDir, fixture, marker, launchLog, bodyMarker)

	env := testutil.RuntimeEnv(
		runtimeDir,
		"PATH="+shimDir+string(os.PathListSeparator)+"/usr/bin"+string(os.PathListSeparator)+"/bin",
		"HUM_OUTPUT_BYTES=65536",
		"HUM_STOP_GRACE=1s",
	)
	t.Cleanup(func() {
		_ = testutil.Run(t, hum, projectRoot, env, "stop", "dev")
		_ = testutil.Run(t, hum, projectRoot, env, "shutdown", "--stop-processes")
	})

	manifestPath := filepath.Join(projectRoot, "hum.yaml")
	init := testutil.Run(t, hum, projectRoot, env, "init")
	zeroConfigAssertSuccess(t, init, "hum init")
	if !strings.Contains(init.Stdout, "path: "+manifestPath) {
		t.Fatalf("hum init output = %q, want written path %q", init.Stdout, manifestPath)
	}
	if !strings.Contains(init.Stdout, "outcome: generated") {
		t.Fatalf("hum init output = %q, want generated outcome", init.Stdout)
	}
	if !strings.Contains(init.Stdout, "next_command: hum up") {
		t.Fatalf("hum init output = %q, want next command guidance", init.Stdout)
	}
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("hum init manifest %q: %v", manifestPath, err)
	}
	zeroConfigAssertNoCandidateExecution(t, launchLog, bodyMarker)
	zeroConfigAssertNoDaemon(t, runtimeDir)

	up := testutil.Run(t, hum, projectRoot, env, "up", "--json")
	zeroConfigAssertSuccess(t, up, "hum up --json")
	launches := manifestDecodeLaunchResults(t, up.Stdout)
	if len(launches) != 1 {
		t.Fatalf("hum up returned %d launch results, want one: %s", len(launches), up.Stdout)
	}
	launch := launches[0]
	wantArgv := []string{"npm", "run", "dev"}
	if launch.Name != "dev" || launch.Source != "manifest" || launch.Outcome != "running_unverified" {
		t.Fatalf("hum up launch = %#v, want name=dev source=manifest outcome=running_unverified", launch)
	}
	if !reflect.DeepEqual(launch.Argv, wantArgv) {
		t.Fatalf("hum up argv = %#v, want discovery argv %#v", launch.Argv, wantArgv)
	}
	testutil.WaitForFile(t, marker+".started", zeroConfigWorkflowTimeout)
}

func setupInitPackage(t *testing.T, root, shimDir, fixture, marker, launchLog, bodyMarker string) {
	t.Helper()
	packageJSON, err := json.Marshal(map[string]any{
		"packageManager": "npm@10.0.0",
		"scripts": map[string]string{
			"dev": "touch " + bodyMarker,
		},
	})
	if err != nil {
		t.Fatalf("encode package.json: %v", err)
	}
	writeZeroConfigFile(t, filepath.Join(root, "package.json"), string(packageJSON)+"\n")
	writeZeroConfigExecutable(t, filepath.Join(shimDir, "npm"), fmt.Sprintf(`if [ "$#" -eq 2 ] && [ "$1" = "run" ] && [ "$2" = "dev" ]; then
  printf 'argv=%%s\n' "$*" >> %s
  exec %s stream %s
fi
printf 'unexpected npm argv: %%s\n' "$*" >&2
exit 64
`, zeroConfigShellQuote(launchLog), zeroConfigShellQuote(fixture), zeroConfigShellQuote(marker)))
}
