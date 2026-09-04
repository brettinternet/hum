package skill

import _ "embed"

//go:embed SKILL.md
var content string

// Content returns the embedded Agent Skills document unchanged.
func Content() string {
	return content
}
