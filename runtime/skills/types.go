package skills

import (
	"fmt"
	"strings"
)

// SkillMetadata holds the YAML frontmatter from a SKILL.md file.
// This is the Phase 1 data — loaded at discovery time (~50 tokens per skill).
type SkillMetadata struct {
	Name          string            `yaml:"name" json:"name"`
	Description   string            `yaml:"description" json:"description"`
	License       string            `yaml:"license,omitempty" json:"license,omitempty"`
	Compatibility string            `yaml:"compatibility,omitempty" json:"compatibility,omitempty"`
	Metadata      map[string]string `yaml:"metadata,omitempty" json:"metadata,omitempty"`
	AllowedTools  []string          `yaml:"allowed-tools,omitempty" json:"allowed_tools,omitempty"`
}

// Skill holds a fully loaded skill — metadata + instructions + path.
// This is the Phase 2 data — loaded on activation.
type Skill struct {
	SkillMetadata
	Instructions string `json:"instructions"` // Markdown body from SKILL.md
	Path         string `json:"path"`         // Filesystem path to skill directory
}

// SkillSource represents a skill reference from the pack YAML.
type SkillSource struct {
	// For directory-based skills
	Dir  string `yaml:"dir,omitempty" json:"dir,omitempty"`
	Path string `yaml:"path,omitempty" json:"path,omitempty"` // schema alias for dir

	// For inline skills
	Name         string `yaml:"name,omitempty" json:"name,omitempty"`
	Description  string `yaml:"description,omitempty" json:"description,omitempty"`
	Instructions string `yaml:"instructions,omitempty" json:"instructions,omitempty"`

	// Options
	Preload bool `yaml:"preload,omitempty" json:"preload,omitempty"`
}

// SkillRef represents a parsed skill reference like @org/skill-name@v1.0.0.
type SkillRef struct {
	Org     string // e.g., "anthropic"
	Name    string // e.g., "pdf-processing"
	Version string // e.g., "v1.0.0" (empty = latest)
}

// FullName returns "org/skill-name".
func (r SkillRef) FullName() string { return r.Org + "/" + r.Name }

// GitURL returns the GitHub clone URL.
func (r SkillRef) GitURL() string {
	return "https://github.com/" + r.Org + "/" + r.Name
}

// ParseSkillRef parses "@org/name[@version]" into a SkillRef.
func ParseSkillRef(s string) (SkillRef, error) {
	if !strings.HasPrefix(s, "@") {
		return SkillRef{}, fmt.Errorf("skill reference must start with @: %q", s)
	}

	s = s[1:] // strip leading @

	// Split version if present: org/name@version
	var version string
	if idx := strings.LastIndex(s, "@"); idx >= 0 {
		version = s[idx+1:]
		s = s[:idx]
		if version == "" {
			return SkillRef{}, fmt.Errorf("empty version in skill reference: @%s@", s)
		}
	}

	// Split org/name
	const orgNameParts = 2
	parts := strings.SplitN(s, "/", orgNameParts)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return SkillRef{}, fmt.Errorf("skill reference must be @org/name: @%s", s)
	}

	// Validate no extra slashes in name
	if strings.Contains(parts[1], "/") {
		return SkillRef{}, fmt.Errorf("skill name must not contain /: %q", parts[1])
	}

	return SkillRef{
		Org:     parts[0],
		Name:    parts[1],
		Version: version,
	}, nil
}

// EffectiveDir returns the directory path, preferring Dir over Path.
func (s *SkillSource) EffectiveDir() string {
	if s.Dir != "" {
		return s.Dir
	}
	return s.Path
}
