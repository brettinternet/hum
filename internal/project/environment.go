package project

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	maxEnvironmentFiles       = 16
	maxEnvironmentFileBytes   = 1 << 20
	maxEnvironmentAssignments = 4096
	maxEnvironmentBytes       = 4 << 20
)

// EnvironmentSpec is the manifest's process environment specification. Values
// are intentionally not included in process snapshots or response models.
type EnvironmentSpec struct {
	Inherit bool               `json:"-"`
	Files   []string           `json:"-"`
	Values  map[string]*string `json:"-"`
	BaseDir string             `json:"-"`
	Root    string             `json:"-"`
}

// EnvironmentAssignment is one parsed assignment in an environment file.
type EnvironmentAssignment struct{ Key, Value string }

var environmentKeyPattern = regexpMustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ParseEnvironmentFile parses the deliberately small, non-expanding env-file
// grammar used by hum. Error text never includes an input value.
func ParseEnvironmentFile(path string, data []byte) ([]EnvironmentAssignment, error) {
	if bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}) {
		return nil, envFileError(path, 1, "file must not contain a BOM")
	}
	if offset := bytes.IndexByte(data, 0); offset >= 0 {
		return nil, envFileError(path, byteLine(data, offset), "file must not contain NUL")
	}
	if offset := invalidUTF8Offset(data); offset >= 0 {
		return nil, envFileError(path, byteLine(data, offset), "file must contain valid UTF-8")
	}

	lines := bytes.Split(data, []byte{'\n'})
	if len(lines) != 0 && len(lines[len(lines)-1]) == 0 {
		lines = lines[:len(lines)-1]
	}
	seen := map[string]struct{}{}
	result := make([]EnvironmentAssignment, 0, len(lines))
	for index, rawLine := range lines {
		lineNo := index + 1
		if bytes.HasSuffix(rawLine, []byte{'\r'}) {
			if index == len(lines)-1 && !bytes.HasSuffix(data, []byte{'\n'}) {
				return nil, envFileError(path, lineNo, "line endings must be LF or CRLF")
			}
			rawLine = rawLine[:len(rawLine)-1]
		}
		if bytes.IndexByte(rawLine, '\r') >= 0 {
			return nil, envFileError(path, lineNo, "line endings must be LF or CRLF")
		}
		line := string(rawLine)
		if strings.Trim(line, " \t") == "" || strings.HasPrefix(strings.TrimLeft(line, " \t"), "#") {
			continue
		}
		left := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(left, "export") && (len(left) == 6 || left[6] == ' ' || left[6] == '\t') {
			left = strings.TrimLeft(left[6:], " \t")
		}
		eq := strings.IndexByte(left, '=')
		if eq < 0 {
			return nil, envFileError(path, lineNo, "assignment requires =")
		}
		key := strings.Trim(left[:eq], " \t")
		if !environmentKeyPattern.MatchString(key) {
			return nil, envFileError(path, lineNo, "invalid key")
		}
		if _, ok := seen[key]; ok {
			return nil, envFileError(path, lineNo, "duplicate key")
		}
		seen[key] = struct{}{}
		value, err := parseEnvironmentValue(left[eq+1:])
		if err != nil {
			return nil, envFileError(path, lineNo, err.Error())
		}
		result = append(result, EnvironmentAssignment{Key: key, Value: value})
		if len(result) > maxEnvironmentAssignments {
			return nil, envFileError(path, lineNo, "assignment limit exceeded")
		}
	}
	return result, nil
}

func byteLine(data []byte, offset int) int {
	return bytes.Count(data[:offset], []byte{'\n'}) + 1
}

func invalidUTF8Offset(data []byte) int {
	for offset := 0; offset < len(data); {
		_, size := utf8.DecodeRune(data[offset:])
		if size == 1 && data[offset] >= utf8.RuneSelf {
			return offset
		}
		offset += size
	}
	return -1
}

func parseEnvironmentValue(raw string) (string, error) {
	raw = strings.TrimLeft(raw, " \t")
	if raw == "" {
		return "", nil
	}
	if raw[0] == '\'' {
		end := strings.IndexByte(raw[1:], '\'')
		if end < 0 {
			return "", errors.New("unterminated single quote")
		}
		end++
		if suffix := strings.TrimLeft(raw[end+1:], " \t"); suffix != "" && !strings.HasPrefix(suffix, "#") {
			return "", errors.New("invalid quoted suffix")
		}
		return raw[1:end], nil
	}
	if raw[0] == '"' {
		var b strings.Builder
		closed := false
		for i := 1; i < len(raw); i++ {
			c := raw[i]
			if c == '"' {
				closed = true
				suffix := strings.TrimLeft(raw[i+1:], " \t")
				if suffix != "" && !strings.HasPrefix(suffix, "#") {
					return "", errors.New("invalid quoted suffix")
				}
				break
			}
			if c == '$' && i+1 < len(raw) && (isEnvNameStart(raw[i+1]) || raw[i+1] == '{' || raw[i+1] == '(') {
				return "", errors.New("unsupported expansion; single-quote a literal or use an external loader")
			}
			if c == '`' {
				return "", errors.New("unsupported expansion; single-quote a literal or use an external loader")
			}
			if c == '\\' {
				if i+1 >= len(raw) {
					return "", errors.New("unsupported escape")
				}
				i++
				switch raw[i] {
				case '\\':
					b.WriteByte('\\')
				case '"':
					b.WriteByte('"')
				case 'n':
					b.WriteByte('\n')
				case 'r':
					b.WriteByte('\r')
				case 't':
					b.WriteByte('\t')
				default:
					return "", errors.New("unsupported escape")
				}
			} else {
				b.WriteByte(c)
			}
		}
		if !closed {
			return "", errors.New("unterminated double quote")
		}
		return b.String(), nil
	}
	raw = strings.TrimRight(raw, " \t")
	// A comment starts at value start or after horizontal whitespace.
	for i := 0; i < len(raw); i++ {
		if raw[i] == '#' && (i == 0 || raw[i-1] == ' ' || raw[i-1] == '\t') {
			raw = strings.TrimRight(raw[:i], " \t")
			break
		}
	}
	for i := 0; i < len(raw); i++ {
		if raw[i] == '$' && i+1 < len(raw) && (isEnvNameStart(raw[i+1]) || raw[i+1] == '{' || raw[i+1] == '(') || raw[i] == '`' {
			return "", errors.New("unsupported expansion; single-quote a literal or use an external loader")
		}
	}
	return raw, nil
}
func isEnvNameStart(c byte) bool { return c == '_' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' }
func envFileError(path string, line int, message string) error {
	return fmt.Errorf("%s:%d: %s", path, line, message)
}

func regexpMustCompile(pattern string) *regexp.Regexp { return regexp.MustCompile(pattern) }

// PreparedEnvironment contains one invocation's immutable file snapshot.
type PreparedEnvironment struct {
	baseline []string
	files    map[string][]EnvironmentAssignment
	paths    map[string]string
}

// PrepareEnvironments validates and loads each unique file once, then composes
// a target environment for every definition. It also checks encoded protocol
// request size without exposing values in diagnostics.
func PrepareEnvironments(definitions []Definition, baseline []string) (map[string][]string, error) {
	prepared := &PreparedEnvironment{
		baseline: append([]string(nil), baseline...),
		files:    map[string][]EnvironmentAssignment{},
		paths:    map[string]string{},
	}
	for _, definition := range definitions {
		if definition.Environment == nil || environmentNoop(definition.Environment) {
			continue
		}
		for _, filename := range definition.Environment.Files {
			canonical, err := resolveEnvironmentFile(definition.Environment.BaseDir, definition.Environment.Root, filename)
			if err != nil {
				return nil, err
			}
			prepared.paths[environmentFileRef(definition.Environment, filename)] = canonical
			if _, ok := prepared.files[canonical]; ok {
				continue
			}
			info, err := os.Stat(canonical)
			if err != nil {
				return nil, fmt.Errorf("environment file %q: %w", filename, err)
			}
			if !info.Mode().IsRegular() {
				return nil, fmt.Errorf("environment file %q is not a regular file", filename)
			}
			if info.Size() > maxEnvironmentFileBytes {
				return nil, fmt.Errorf("environment file %q exceeds the 1 MiB limit", filename)
			}
			file, err := os.Open(canonical)
			if err != nil {
				return nil, fmt.Errorf("environment file %q: %w", filename, err)
			}
			data, readErr := io.ReadAll(io.LimitReader(file, maxEnvironmentFileBytes+1))
			_ = file.Close()
			if readErr != nil {
				return nil, fmt.Errorf("environment file %q: %w", filename, readErr)
			}
			if len(data) > maxEnvironmentFileBytes {
				return nil, fmt.Errorf("environment file %q exceeds the 1 MiB limit", filename)
			}
			assignments, parseErr := ParseEnvironmentFile(filename, data)
			if parseErr != nil {
				return nil, parseErr
			}
			prepared.files[canonical] = assignments
		}
	}
	result := make(map[string][]string, len(definitions))
	for _, definition := range definitions {
		env, err := prepared.compose(definition.Environment)
		if err != nil {
			return nil, err
		}
		result[definition.Name] = env
	}
	return result, nil
}

// HasEnvironmentConfiguration reports whether a specification changes the
// inherited baseline. Default and empty specifications are deliberate no-ops.
func HasEnvironmentConfiguration(spec *EnvironmentSpec) bool {
	return spec != nil && (!spec.Inherit || len(spec.Files) != 0 || len(spec.Values) != 0)
}

func environmentNoop(spec *EnvironmentSpec) bool { return !HasEnvironmentConfiguration(spec) }
func (p *PreparedEnvironment) compose(spec *EnvironmentSpec) ([]string, error) {
	if spec == nil || environmentNoop(spec) {
		return append([]string(nil), p.baseline...), nil
	}
	if !spec.Inherit {
		pmap := map[string]string{}
		return p.composeMap(pmap, spec)
	}
	values := map[string]string{}
	for _, entry := range p.baseline {
		if key, value, ok := splitEnvEntry(entry); ok {
			values[key] = value
		}
	}
	return p.composeMap(values, spec)
}

func (p *PreparedEnvironment) composeMap(values map[string]string, spec *EnvironmentSpec) ([]string, error) {
	for _, filename := range spec.Files {
		canonical, ok := p.paths[environmentFileRef(spec, filename)]
		if !ok {
			return nil, fmt.Errorf("environment file %q was not prepared", filename)
		}
		for _, entry := range p.files[canonical] {
			values[entry.Key] = entry.Value
		}
	}
	for key, value := range spec.Values {
		if value == nil {
			delete(values, key)
		} else {
			values[key] = *value
		}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	total := 0
	for _, key := range keys {
		entry := key + "=" + values[key]
		total += len(entry) + 1
		if total > maxEnvironmentBytes {
			return nil, errors.New("composed environment exceeds the 4 MiB limit")
		}
		result = append(result, entry)
	}
	return result, nil
}
func environmentFileRef(spec *EnvironmentSpec, filename string) string {
	return spec.Root + "\x00" + spec.BaseDir + "\x00" + filename
}

func splitEnvEntry(entry string) (string, string, bool) {
	i := strings.IndexByte(entry, '=')
	if i <= 0 {
		return "", "", false
	}
	return entry[:i], entry[i+1:], true
}
func resolveEnvironmentFile(base, root, name string) (string, error) {
	if name == "" || strings.IndexByte(name, 0) >= 0 || filepath.IsAbs(name) {
		return "", fmt.Errorf("environment file %q must be a non-empty relative path", name)
	}
	lexical := filepath.Clean(filepath.Join(base, name))
	if lexical == base || !pathWithin(root, lexical) {
		return "", fmt.Errorf("environment file %q escapes the project root", name)
	}
	canonicalRoot, rootErr := filepath.EvalSymlinks(root)
	if rootErr != nil {
		canonicalRoot = root
	}
	canonical, err := filepath.EvalSymlinks(lexical)
	if err != nil {
		return "", fmt.Errorf("environment file %q: %w", name, err)
	}
	if !pathWithin(canonicalRoot, canonical) {
		return "", fmt.Errorf("environment file %q resolves outside the project root", name)
	}
	return canonical, nil
}
