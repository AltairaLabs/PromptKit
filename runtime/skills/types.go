package skills

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

// EffectiveDir returns the directory path, preferring Dir over Path.
func (s *SkillSource) EffectiveDir() string {
	if s.Dir != "" {
		return s.Dir
	}
	return s.Path
}
