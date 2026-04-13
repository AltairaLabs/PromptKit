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
	// Path is the consumer-facing skill directory. For sources without
	// MountAs this is the real on-disk path. For sources with MountAs it
	// is the *virtual* path (MountAs prefix + relative subpath under the
	// source dir) — workflow glob filters and ListForDir match against
	// this. To read files from the skill, always go through
	// Registry.ReadResource; do not treat Path as a filesystem path.
	Path string `json:"path"`
}

// SkillSource describes one source of skills to register with a Registry.
//
// A source is either:
//   - directory-based — set Dir (or Path as an alias). The registry walks the
//     directory for SKILL.md files and registers each one found. MountAs and
//     Preload apply.
//   - inline — set Name, Description, and Instructions. The skill is
//     registered as if it had been read from disk. MountAs is rejected here.
//
// Multiple SkillSources can be passed to Registry.Discover. They are processed
// in order; the first source to register a given skill name wins for path and
// metadata. Preload is OR-combined: a later source can upgrade preload from
// false to true but never the reverse.
type SkillSource struct {
	// For directory-based skills.
	// Dir is the real on-disk directory to walk for SKILL.md files. Path is a
	// YAML/JSON alias accepted for legacy schemas; if both are set Dir wins.
	Dir  string `yaml:"dir,omitempty" json:"dir,omitempty"`
	Path string `yaml:"path,omitempty" json:"path,omitempty"`

	// For inline skills — Name is required and uniquely identifies the
	// skill in the registry. Description and Instructions populate the
	// in-memory Skill directly; no SKILL.md is read.
	Name         string `yaml:"name,omitempty" json:"name,omitempty"`
	Description  string `yaml:"description,omitempty" json:"description,omitempty"`
	Instructions string `yaml:"instructions,omitempty" json:"instructions,omitempty"`

	// MountAs, when set on a directory-based source, presents discovered
	// skills to the registry, workflow filters, and Skill.Path consumers
	// as if they lived under this virtual directory prefix instead of
	// their real on-disk location. ReadResource always uses the real
	// path — MountAs only affects display and glob matching, never IO
	// or path-containment checks.
	//
	// Subdirectory structure under Dir is preserved: a skill at
	// /foo/agentskills/billing/payments/SKILL.md discovered with
	// Dir: "/foo/agentskills" and MountAs: "skills" is exposed as
	// virtual path "skills/billing/payments", which means a workflow
	// state with `skills: "skills/billing/*"` will match it.
	//
	// MountAs must be a literal directory prefix — glob metacharacters
	// (* ? [) and ".." segments are rejected at Discover time. Setting
	// MountAs on an inline source (Name set, Dir empty) is also rejected.
	MountAs string `yaml:"mount_as,omitempty" json:"mount_as,omitempty"`

	// Preload, when true, causes the skill's instructions to be loaded
	// into the system prompt at session start instead of being activated
	// on demand via skill__activate. Use sparingly — preloaded skills
	// consume context budget every turn. Across duplicate sources Preload
	// is OR-combined: a later source can upgrade preload from false to
	// true, but never the reverse.
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

// EffectiveDir returns the real on-disk source directory, preferring Dir over
// the legacy Path alias. Returns the empty string for inline sources. This is
// always the *real* path; MountAs has no effect here.
func (s *SkillSource) EffectiveDir() string {
	if s.Dir != "" {
		return s.Dir
	}
	return s.Path
}
