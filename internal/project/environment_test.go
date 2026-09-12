package project

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestEnvironmentFileContract(t *testing.T) {
	t.Run("grammar", func(t *testing.T) {
		got, err := ParseEnvironmentFile(".env", []byte("# comment\r\n\texport A = one # note\r\nB='${A}'\nC=\"x\\n\\r\\t\\\\\\\"\" # note\nEMPTY=\nHASH=a#b\nCOMMENT= # ignored\n"))
		if err != nil {
			t.Fatal(err)
		}
		want := []EnvironmentAssignment{
			{Key: "A", Value: "one"},
			{Key: "B", Value: "${A}"},
			{Key: "C", Value: "x\n\r\t\\\""},
			{Key: "EMPTY", Value: ""},
			{Key: "HASH", Value: "a#b"},
			{Key: "COMMENT", Value: ""},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("assignments=%#v want %#v", got, want)
		}
	})

	invalid := []struct {
		name string
		data []byte
		line string
	}{
		{name: "BOM", data: []byte("\xef\xbb\xbfA=x\n"), line: ":1:"},
		{name: "NUL", data: []byte("A=x\nB=secret\x00value\n"), line: ":2:"},
		{name: "invalid UTF-8", data: []byte("A=x\nB=\xff\n"), line: ":2:"},
		{name: "lone CR", data: []byte("A=x\r"), line: ":1:"},
		{name: "embedded CR", data: []byte("A=x\ry\n"), line: ":1:"},
		{name: "bare export", data: []byte("export KEY\n"), line: ":1:"},
		{name: "missing equals", data: []byte("KEY\n"), line: ":1:"},
		{name: "invalid key", data: []byte("BAD-KEY=SECRET_VALUE\n"), line: ":1:"},
		{name: "duplicate", data: []byte("KEY=x\nKEY=SECRET_VALUE\n"), line: ":2:"},
		{name: "unterminated single quote", data: []byte("KEY='SECRET_VALUE\n"), line: ":1:"},
		{name: "unterminated double quote", data: []byte("KEY=\"SECRET_VALUE\n"), line: ":1:"},
		{name: "quoted suffix", data: []byte("KEY='x' SECRET_VALUE\n"), line: ":1:"},
		{name: "unsupported escape", data: []byte("KEY=\"x\\qSECRET_VALUE\"\n"), line: ":1:"},
		{name: "unquoted name expansion", data: []byte("KEY=$SECRET_VALUE\n"), line: ":1:"},
		{name: "unquoted braced expansion", data: []byte("KEY=${SECRET_VALUE}\n"), line: ":1:"},
		{name: "unquoted command expansion", data: []byte("KEY=$(SECRET_VALUE)\n"), line: ":1:"},
		{name: "double expansion", data: []byte("KEY=\"$SECRET_VALUE\"\n"), line: ":1:"},
		{name: "backtick", data: []byte("KEY=`SECRET_VALUE`\n"), line: ":1:"},
		{name: "physical multiline", data: []byte("KEY='first\nsecond'\n"), line: ":1:"},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseEnvironmentFile("private.env", test.data)
			if err == nil {
				t.Fatal("invalid environment file accepted")
			}
			if !strings.Contains(err.Error(), "private.env"+test.line) {
				t.Fatalf("error %q does not identify expected location", err)
			}
			if strings.Contains(err.Error(), "SECRET_VALUE") {
				t.Fatalf("error leaked value: %q", err)
			}
		})
	}

	t.Run("assignment bound", func(t *testing.T) {
		var contents strings.Builder
		for index := 0; index <= maxEnvironmentAssignments; index++ {
			contents.WriteString("K")
			contents.WriteString(strconv.Itoa(index))
			contents.WriteString("=x\n")
		}
		_, err := ParseEnvironmentFile("large.env", []byte(contents.String()))
		if err == nil || !strings.Contains(err.Error(), "assignment limit") {
			t.Fatalf("assignment bound error = %v", err)
		}
	})

	t.Run("self-referential paths are not files", func(t *testing.T) {
		for _, name := range []string{".", "sub/.."} {
			root := t.TempDir()
			manifest := fmt.Sprintf("version: 1\nenvironment:\n  files: [%q]\nprocesses: {}\n", name)
			mustWriteEnvironmentFile(t, filepath.Join(root, "hum.yaml"), manifest)
			_, err := LoadDefinitions(root)
			if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("path %q is not a file", name)) {
				t.Fatalf("path %q error = %v", name, err)
			}
			if strings.Contains(err.Error(), "escapes the project root") {
				t.Fatalf("path %q incorrectly reported an escape: %v", name, err)
			}
		}
	})
}

func TestEnvironmentCompositionContract(t *testing.T) {
	root := t.TempDir()
	mustWriteEnvironmentFile(t, filepath.Join(root, "first.env"), "A=first\nB=file\n")
	mustWriteEnvironmentFile(t, filepath.Join(root, "second.env"), "A=second\nC=file\n")

	t.Run("byte-identical no-op", func(t *testing.T) {
		baseline := []string{"OPAQUE-NAME=one", "DUP=first", "DUP=last", strings.Repeat("X", maxEnvironmentBytes+1) + "=large"}
		definitions := []Definition{
			{Name: "absent"},
			{Name: "default", Environment: &EnvironmentSpec{Inherit: true, Values: map[string]*string{}, Root: root, BaseDir: root}},
		}
		got, err := PrepareEnvironments(definitions, baseline)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"absent", "default"} {
			if !reflect.DeepEqual(got[name], baseline) {
				t.Fatalf("%s changed baseline", name)
			}
		}
	})

	t.Run("precedence null sorting and duplicates", func(t *testing.T) {
		spec := &EnvironmentSpec{
			Inherit: true,
			Files:   []string{"first.env", "second.env"},
			Values: map[string]*string{
				"A": envString("map"),
				"B": nil,
				"D": envString(""),
			},
			BaseDir: root,
			Root:    root,
		}
		got, err := PrepareEnvironments([]Definition{{Name: "api", Environment: spec}}, []string{"Z=base", "A=base", "Z=last", "ODD-NAME=kept"})
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"A=map", "C=file", "D=", "ODD-NAME=kept", "Z=last"}
		if !reflect.DeepEqual(got["api"], want) {
			t.Fatalf("environment=%v want %v", got["api"], want)
		}
	})

	t.Run("opaque baseline entries", func(t *testing.T) {
		baseline := []string{"A=base", "NO_EQUALS", "=empty-key", "Z=base"}
		spec := &EnvironmentSpec{
			Inherit: true,
			Values:  map[string]*string{"A": envString("configured")},
			BaseDir: root,
			Root:    root,
		}
		got, err := PrepareEnvironments([]Definition{{Name: "api", Environment: spec}}, baseline)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"A=configured", "NO_EQUALS", "=empty-key", "Z=base"}
		if !reflect.DeepEqual(got["api"], want) {
			t.Fatalf("environment=%v want %v", got["api"], want)
		}
	})

	t.Run("inherit false", func(t *testing.T) {
		spec := &EnvironmentSpec{Inherit: false, Values: map[string]*string{"ONLY": envString("configured")}, BaseDir: root, Root: root}
		got, err := PrepareEnvironments([]Definition{{Name: "isolated", Environment: spec}}, []string{"PATH=/bin", "STALE=yes"})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got["isolated"], []string{"ONLY=configured"}) {
			t.Fatalf("environment=%v", got["isolated"])
		}
	})

	t.Run("nested parent path and canonical aliases", func(t *testing.T) {
		nested := filepath.Join(root, "config")
		if err := os.Mkdir(nested, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(root, "first.env"), filepath.Join(nested, "alias.env")); err != nil {
			t.Fatal(err)
		}
		spec := &EnvironmentSpec{Inherit: false, Files: []string{"../first.env", "alias.env"}, Values: map[string]*string{}, BaseDir: nested, Root: root}
		got, err := PrepareEnvironments([]Definition{{Name: "nested", Environment: spec}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got["nested"], []string{"A=first", "B=file"}) {
			t.Fatalf("environment=%v", got["nested"])
		}
	})

	pathErrors := []struct {
		name string
		path string
	}{
		{name: "empty", path: ""},
		{name: "absolute", path: filepath.Join(root, "first.env")},
		{name: "root", path: "."},
		{name: "lexical escape", path: "../outside.env"},
		{name: "missing", path: "missing.env"},
		{name: "nonregular", path: "config"},
	}
	for _, test := range pathErrors {
		t.Run("path "+test.name, func(t *testing.T) {
			spec := &EnvironmentSpec{Inherit: true, Files: []string{test.path}, Values: map[string]*string{}, BaseDir: root, Root: root}
			if _, err := PrepareEnvironments([]Definition{{Name: "bad", Environment: spec}}, nil); err == nil || !strings.Contains(err.Error(), "environment file") {
				t.Fatalf("path error = %v", err)
			}
		})
	}

	if runtime.GOOS != "windows" {
		t.Run("symlink escape", func(t *testing.T) {
			outside := t.TempDir()
			mustWriteEnvironmentFile(t, filepath.Join(outside, "outside.env"), "SECRET=outside\n")
			link := filepath.Join(root, "outside.env")
			if err := os.Symlink(filepath.Join(outside, "outside.env"), link); err != nil {
				t.Fatal(err)
			}
			spec := &EnvironmentSpec{Inherit: true, Files: []string{"outside.env"}, Values: map[string]*string{}, BaseDir: root, Root: root}
			if _, err := PrepareEnvironments([]Definition{{Name: "bad", Environment: spec}}, nil); err == nil || !strings.Contains(err.Error(), "outside the project root") {
				t.Fatalf("symlink error = %v", err)
			}
		})
	}

	t.Run("file byte bound", func(t *testing.T) {
		mustWriteEnvironmentFile(t, filepath.Join(root, "oversized.env"), strings.Repeat("X", maxEnvironmentFileBytes+1))
		spec := &EnvironmentSpec{Inherit: false, Files: []string{"oversized.env"}, Values: map[string]*string{}, BaseDir: root, Root: root}
		if _, err := PrepareEnvironments([]Definition{{Name: "bad", Environment: spec}}, nil); err == nil || !strings.Contains(err.Error(), "1 MiB") || strings.Contains(err.Error(), ":1:") {
			t.Fatalf("file bound error = %v", err)
		}
	})

	t.Run("final byte bound", func(t *testing.T) {
		value := strings.Repeat("v", maxEnvironmentBytes)
		spec := &EnvironmentSpec{Inherit: false, Values: map[string]*string{"KEY": &value}, BaseDir: root, Root: root}
		if _, err := PrepareEnvironments([]Definition{{Name: "bad", Environment: spec}}, nil); err == nil || !strings.Contains(err.Error(), "4 MiB") || strings.Contains(err.Error(), ":1:") {
			t.Fatalf("total bound error = %v", err)
		}
	})
}

func mustWriteEnvironmentFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func envString(value string) *string { return &value }
