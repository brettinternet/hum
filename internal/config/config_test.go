package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestNewDefaultsAndBuildMetadata(t *testing.T) {
	build := BuildOpts{
		Version:   "v1.2.3",
		BuildTime: "2026-09-02T12:00:00Z",
	}

	cfg, err := New(build, Input{})
	if err != nil {
		t.Fatalf("New with default input: %v", err)
	}

	if cfg.Version != build.Version {
		t.Errorf("Version = %q, want %q", cfg.Version, build.Version)
	}
	if cfg.BuildTime != build.BuildTime {
		t.Errorf("BuildTime = %q, want %q", cfg.BuildTime, build.BuildTime)
	}
	if cfg.StopGrace != DefaultStopGrace {
		t.Errorf("StopGrace = %s, want default %s", cfg.StopGrace, DefaultStopGrace)
	}
	if cfg.OutputBytes != DefaultOutputBytes {
		t.Errorf("OutputBytes = %d, want default %d", cfg.OutputBytes, DefaultOutputBytes)
	}
	if cfg.CompletedRecords != DefaultCompletedRecords {
		t.Errorf("CompletedRecords = %d, want default %d", cfg.CompletedRecords, DefaultCompletedRecords)
	}
	if cfg.ReadEntries != DefaultReadEntries {
		t.Errorf("ReadEntries = %d, want default %d", cfg.ReadEntries, DefaultReadEntries)
	}
	if cfg.ReadBytes != DefaultReadBytes {
		t.Errorf("ReadBytes = %d, want default %d", cfg.ReadBytes, DefaultReadBytes)
	}
	if cfg.MaxLineBytes != MaxLineBytes {
		t.Errorf("MaxLineBytes = %d, want default %d", cfg.MaxLineBytes, MaxLineBytes)
	}
}

func TestStopGracePrecedence(t *testing.T) {
	tests := []struct {
		name  string
		input Input
		want  time.Duration
	}{
		{name: "default", input: Input{}, want: DefaultStopGrace},
		{
			name: "environment",
			input: Input{
				EnvStopGrace: "7s",
			},
			want: 7 * time.Second,
		},
		{
			name: "flag over environment",
			input: Input{
				FlagStopGrace: "3s",
				EnvStopGrace:  "7s",
			},
			want: 3 * time.Second,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg, err := New(BuildOpts{}, test.input)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if cfg.StopGrace != test.want {
				t.Errorf("StopGrace = %s, want %s", cfg.StopGrace, test.want)
			}
		})
	}
}

func TestOutputBytesPrecedence(t *testing.T) {
	tests := []struct {
		name  string
		input Input
		want  int64
	}{
		{name: "default", input: Input{}, want: DefaultOutputBytes},
		{
			name: "environment",
			input: Input{
				EnvOutputBytes: "131072",
			},
			want: 131072,
		},
		{
			name: "flag over environment",
			input: Input{
				FlagOutputBytes: "262144",
				EnvOutputBytes:  "131072",
			},
			want: 262144,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg, err := New(BuildOpts{}, test.input)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if cfg.OutputBytes != test.want {
				t.Errorf("OutputBytes = %d, want %d", cfg.OutputBytes, test.want)
			}
		})
	}
}

func TestCompletedRecordsPrecedence(t *testing.T) {
	tests := []struct {
		name  string
		input Input
		want  int
	}{
		{name: "default", input: Input{}, want: DefaultCompletedRecords},
		{
			name: "environment",
			input: Input{
				EnvCompletedRecords: "30",
			},
			want: 30,
		},
		{
			name: "flag over environment",
			input: Input{
				FlagCompletedRecords: "40",
				EnvCompletedRecords:  "30",
			},
			want: 40,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg, err := New(BuildOpts{}, test.input)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if cfg.CompletedRecords != test.want {
				t.Errorf("CompletedRecords = %d, want %d", cfg.CompletedRecords, test.want)
			}
		})
	}
}

func TestStopGraceValidation(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "invalid duration text", value: "not-a-duration"},
		{name: "negative duration", value: "-1ns"},
		{name: "negative sub-nanosecond duration", value: "-0.1ns"},
		{name: "negative zero duration", value: "-0s"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := New(BuildOpts{}, Input{FlagStopGrace: test.value})
			if err == nil {
				t.Fatalf("New with stop grace %q returned nil error", test.value)
			}
			assertErrorIdentifies(t, err, "stop", "grace")
		})
	}
}

func TestStopGraceZeroIsAccepted(t *testing.T) {
	cfg, err := New(BuildOpts{}, Input{FlagStopGrace: "0s"})
	if err != nil {
		t.Fatalf("New with zero stop grace: %v", err)
	}
	if cfg.StopGrace != 0 {
		t.Fatalf("StopGrace = %s, want 0", cfg.StopGrace)
	}
}

func TestIntegerParseFailuresIdentifySetting(t *testing.T) {
	tests := []struct {
		name  string
		input Input
		want  []string
	}{
		{
			name:  "output bytes",
			input: Input{FlagOutputBytes: "not-an-integer"},
			want:  []string{"output", "bytes"},
		},
		{
			name:  "completed records",
			input: Input{FlagCompletedRecords: "not-an-integer"},
			want:  []string{"completed", "records"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := New(BuildOpts{}, test.input)
			if err == nil {
				t.Fatalf("New with invalid %s returned nil error", test.name)
			}
			assertErrorIdentifies(t, err, test.want...)
		})
	}
}

func TestOutputBytesBounds(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		wantError bool
	}{
		{name: "below minimum", value: "65535", wantError: true},
		{name: "minimum", value: "65536", wantError: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg, err := New(BuildOpts{}, Input{FlagOutputBytes: test.value})
			if test.wantError {
				if err == nil {
					t.Fatalf("New with output bytes %s returned nil error", test.value)
				}
				assertErrorIdentifies(t, err, "output", "bytes")
				return
			}
			if err != nil {
				t.Fatalf("New with output bytes %s: %v", test.value, err)
			}
			if cfg.OutputBytes != 65536 {
				t.Fatalf("OutputBytes = %d, want 65536", cfg.OutputBytes)
			}
		})
	}
}

func TestCompletedRecordsBounds(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		wantError bool
	}{
		{name: "below minimum", value: "0", wantError: true},
		{name: "minimum", value: "1", wantError: false},
		{name: "maximum", value: "1000", wantError: false},
		{name: "above maximum", value: "1001", wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg, err := New(BuildOpts{}, Input{FlagCompletedRecords: test.value})
			if test.wantError {
				if err == nil {
					t.Fatalf("New with completed records %s returned nil error", test.value)
				}
				assertErrorIdentifies(t, err, "completed", "records")
				return
			}
			if err != nil {
				t.Fatalf("New with completed records %s: %v", test.value, err)
			}
			want := 1
			if test.value == "1000" {
				want = 1000
			}
			if cfg.CompletedRecords != want {
				t.Fatalf("CompletedRecords = %d, want %d", cfg.CompletedRecords, want)
			}
		})
	}
}

func TestRuntimeDir(t *testing.T) {
	xdg := filepath.Join(os.TempDir(), "config-test-xdg")

	t.Run("flag overrides HUM env and XDG", func(t *testing.T) {
		input := Input{
			FlagRuntimeDir:   filepath.Join(os.TempDir(), "flag-runtime"),
			EnvRuntimeDir:    filepath.Join(os.TempDir(), "hum-runtime"),
			EnvXDGRuntimeDir: xdg,
		}
		cfg, err := New(BuildOpts{}, input)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if cfg.RuntimeDir != input.FlagRuntimeDir {
			t.Fatalf("RuntimeDir = %q, want flag %q", cfg.RuntimeDir, input.FlagRuntimeDir)
		}
	})

	t.Run("HUM env overrides XDG", func(t *testing.T) {
		input := Input{
			EnvRuntimeDir:    filepath.Join(os.TempDir(), "hum-runtime"),
			EnvXDGRuntimeDir: xdg,
		}
		cfg, err := New(BuildOpts{}, input)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if cfg.RuntimeDir != input.EnvRuntimeDir {
			t.Fatalf("RuntimeDir = %q, want HUM env %q", cfg.RuntimeDir, input.EnvRuntimeDir)
		}
	})

	t.Run("XDG joins hum", func(t *testing.T) {
		cfg, err := New(BuildOpts{}, Input{EnvXDGRuntimeDir: xdg})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		want := filepath.Join(xdg, "hum")
		if cfg.RuntimeDir != want {
			t.Fatalf("RuntimeDir = %q, want %q", cfg.RuntimeDir, want)
		}
	})

	t.Run("fallback is per-user directory below temp", func(t *testing.T) {
		cfg, err := New(BuildOpts{}, Input{})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		wantBase := "hum-" + strconv.Itoa(os.Getuid())
		want := filepath.Join(os.TempDir(), wantBase)
		if cfg.RuntimeDir != want {
			t.Fatalf("RuntimeDir = %q, want %q", cfg.RuntimeDir, want)
		}
		if filepath.Dir(cfg.RuntimeDir) != filepath.Clean(os.TempDir()) {
			t.Fatalf("RuntimeDir parent = %q, want temp directory %q", filepath.Dir(cfg.RuntimeDir), filepath.Clean(os.TempDir()))
		}
		if filepath.Base(cfg.RuntimeDir) != wantBase {
			t.Fatalf("RuntimeDir basename = %q, want %q", filepath.Base(cfg.RuntimeDir), wantBase)
		}
	})
}

func assertErrorIdentifies(t *testing.T, err error, terms ...string) {
	t.Helper()
	message := strings.ToLower(err.Error())
	for _, term := range terms {
		if !strings.Contains(message, strings.ToLower(term)) {
			t.Errorf("error %q does not identify setting term %q", err, term)
		}
	}
}

func TestInputValuesAreDecimalIntegers(t *testing.T) {
	cfg, err := New(BuildOpts{}, Input{
		FlagOutputBytes:      "65536",
		FlagCompletedRecords: "010",
	})
	if err != nil {
		t.Fatalf("New with decimal integer values: %v", err)
	}
	if cfg.OutputBytes != 65536 {
		t.Errorf("OutputBytes = %d, want 65536", cfg.OutputBytes)
	}
	if cfg.CompletedRecords != 10 {
		t.Errorf("CompletedRecords = %d, want 10", cfg.CompletedRecords)
	}
}
