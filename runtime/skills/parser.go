// Package skills provides types and utilities for loading, parsing, and managing
// PromptKit skills defined via the AgentSkills.io SKILL.md format.
package skills

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	maxNameLen        = 64
	maxDescriptionLen = 1024
	frontmatterDelim  = "---"
)

var nameRegex = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// ParseSkillFile parses a SKILL.md file at the given path into a Skill.
// It extracts YAML frontmatter and markdown body.
func ParseSkillFile(path string) (*Skill, error) {
	data, err := os.ReadFile(path) // #nosec G304 - path is provided by skill discovery
	if err != nil {
		return nil, fmt.Errorf("reading skill file: %w", err)
	}

	meta, body, err := ParseSkillContent(data)
	if err != nil {
		return nil, fmt.Errorf("parsing skill file %s: %w", path, err)
	}

	return &Skill{
		SkillMetadata: *meta,
		Instructions:  body,
		Path:          filepath.Dir(path),
	}, nil
}

// ParseSkillMetadata parses only the YAML frontmatter from a SKILL.md file.
// Used for Phase 1 discovery — avoids loading the full body.
func ParseSkillMetadata(path string) (*SkillMetadata, error) {
	data, err := os.ReadFile(path) // #nosec G304 - path is provided by skill discovery
	if err != nil {
		return nil, fmt.Errorf("reading skill file: %w", err)
	}

	meta, _, err := ParseSkillContent(data)
	if err != nil {
		return nil, fmt.Errorf("parsing skill metadata %s: %w", path, err)
	}

	return meta, nil
}

// ParseSkillContent parses SKILL.md content from bytes.
// It returns the parsed metadata, the markdown body, and any error.
func ParseSkillContent(content []byte) (*SkillMetadata, string, error) {
	frontmatter, body, err := splitFrontmatter(content)
	if err != nil {
		return nil, "", err
	}

	var meta SkillMetadata
	if err := yaml.Unmarshal(frontmatter, &meta); err != nil {
		return nil, "", fmt.Errorf("invalid YAML frontmatter: %w", err)
	}

	if err := validateMetadata(&meta); err != nil {
		return nil, "", err
	}

	return &meta, body, nil
}

// splitFrontmatter splits SKILL.md content into frontmatter YAML bytes and body string.
// The expected format is: ---\n<yaml>\n---\n<body>
func splitFrontmatter(content []byte) (fm []byte, body string, err error) {
	trimmed := bytes.TrimSpace(content)
	if len(trimmed) == 0 {
		return nil, "", fmt.Errorf("empty skill content")
	}

	if !bytes.HasPrefix(trimmed, []byte(frontmatterDelim)) {
		return nil, "", fmt.Errorf("missing frontmatter: content must start with ---")
	}

	// Skip the opening --- and its trailing newline
	afterFirst := skipDelimiterLine(trimmed[len(frontmatterDelim):])

	// Find the closing --- on its own line
	fmBytes, rest, found := findClosingDelimiter(afterFirst)
	if !found {
		return nil, "", fmt.Errorf("missing closing frontmatter delimiter ---")
	}

	body = strings.TrimSpace(string(rest))
	return fmBytes, body, nil
}

// skipDelimiterLine advances past the newline following a --- delimiter.
func skipDelimiterLine(data []byte) []byte {
	if len(data) > 1 && data[0] == '\r' && data[1] == '\n' {
		return data[2:]
	}
	if len(data) > 0 && data[0] == '\n' {
		return data[1:]
	}
	return data
}

// findClosingDelimiter scans lines looking for a standalone --- delimiter.
// It returns the frontmatter bytes, remaining bytes after the delimiter, and whether it was found.
func findClosingDelimiter(data []byte) (fm, rest []byte, found bool) {
	lines := bytes.SplitAfter(data, []byte("\n"))
	var fmLen int

	for i, line := range lines {
		trimLine := bytes.TrimRight(line, "\r\n")
		if bytes.Equal(trimLine, []byte(frontmatterDelim)) {
			fm := data[:fmLen]
			rest := bytes.Join(lines[i+1:], nil)
			return fm, rest, true
		}
		fmLen += len(line)
	}

	return nil, nil, false
}

// validateMetadata validates required fields and constraints on SkillMetadata.
func validateMetadata(meta *SkillMetadata) error {
	if meta.Name == "" {
		return fmt.Errorf("skill name is required")
	}
	if !nameRegex.MatchString(meta.Name) {
		return fmt.Errorf(
			"invalid skill name %q: must be kebab-case (lowercase alphanumeric and hyphens)",
			meta.Name,
		)
	}
	if len(meta.Name) > maxNameLen {
		return fmt.Errorf(
			"skill name %q exceeds maximum length of %d characters",
			meta.Name, maxNameLen,
		)
	}
	if meta.Description == "" {
		return fmt.Errorf("skill description is required")
	}
	if len(meta.Description) > maxDescriptionLen {
		return fmt.Errorf(
			"skill description exceeds maximum length of %d characters",
			maxDescriptionLen,
		)
	}
	return nil
}
