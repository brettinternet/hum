package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// BuildOpts contains build metadata injected at link time.
type BuildOpts struct {
	Version   string
	BuildTime string
}

// Input contains raw command-line flag and environment values.
type Input struct {
	FlagRuntimeDir       string
	FlagStopGrace        string
	FlagOutputBytes      string
	FlagCompletedRecords string

	EnvRuntimeDir       string
	EnvXDGRuntimeDir    string
	EnvStopGrace        string
	EnvOutputBytes      string
	EnvCompletedRecords string
}

// Config contains resolved runtime settings.
type Config struct {
	Version          string
	BuildTime        string
	RuntimeDir       string
	StopGrace        time.Duration
	OutputBytes      int64
	CompletedRecords int
	ReadEntries      int
	ReadBytes        int64
	MaxLineBytes     int64
}

const (
	// DefaultStopGrace is the default graceful-stop period.
	DefaultStopGrace time.Duration = 10 * time.Second
	// DefaultOutputBytes is the default retained output limit per process.
	DefaultOutputBytes int64 = 4 * 1024 * 1024
	// MinOutputBytes is the smallest retained output limit per process.
	MinOutputBytes int64 = 64 * 1024
	// DefaultCompletedRecords is the default number of completed records retained.
	DefaultCompletedRecords int = 20
	// DefaultReadEntries is the default number of log entries read at a time.
	DefaultReadEntries int = 100
	// DefaultReadBytes is the default number of log bytes read at a time.
	DefaultReadBytes int64 = 16 * 1024
	// MaxLineBytes is the maximum size of a single log line.
	MaxLineBytes int64 = 64 * 1024

	minCompletedRecords = 1
	maxCompletedRecords = 1000
)

// New resolves and validates configuration from build metadata, flags, and environment values.
func New(build BuildOpts, input Input) (Config, error) {
	cfg := Config{
		Version:          build.Version,
		BuildTime:        build.BuildTime,
		StopGrace:        DefaultStopGrace,
		OutputBytes:      DefaultOutputBytes,
		CompletedRecords: DefaultCompletedRecords,
		ReadEntries:      DefaultReadEntries,
		ReadBytes:        DefaultReadBytes,
		MaxLineBytes:     MaxLineBytes,
	}

	cfg.RuntimeDir = resolveRuntimeDir(input)

	if raw := firstNonEmpty(input.FlagStopGrace, input.EnvStopGrace); raw != "" {
		if raw[0] == '-' {
			return Config{}, fmt.Errorf("stop grace: must not be negative")
		}
		value, err := time.ParseDuration(raw)
		if err != nil {
			return Config{}, fmt.Errorf("stop grace: %w", err)
		}
		if value < 0 {
			return Config{}, fmt.Errorf("stop grace: must not be negative")
		}
		cfg.StopGrace = value
	}

	if raw := firstNonEmpty(input.FlagOutputBytes, input.EnvOutputBytes); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("output bytes: %w", err)
		}
		if value < MinOutputBytes {
			return Config{}, fmt.Errorf("output bytes: must be at least %d", MinOutputBytes)
		}
		cfg.OutputBytes = value
	}

	if raw := firstNonEmpty(input.FlagCompletedRecords, input.EnvCompletedRecords); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("completed records: %w", err)
		}
		if value < minCompletedRecords || value > maxCompletedRecords {
			return Config{}, fmt.Errorf("completed records: must be between %d and %d", minCompletedRecords, maxCompletedRecords)
		}
		cfg.CompletedRecords = int(value)
	}

	return cfg, nil
}

func resolveRuntimeDir(input Input) string {
	if value := firstNonEmpty(input.FlagRuntimeDir, input.EnvRuntimeDir); value != "" {
		return value
	}
	if input.EnvXDGRuntimeDir != "" {
		return filepath.Join(input.EnvXDGRuntimeDir, "hum")
	}
	return filepath.Join(os.TempDir(), "hum-"+strconv.Itoa(os.Getuid()))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
