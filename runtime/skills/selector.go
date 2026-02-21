package skills

import (
	"context"
	"strings"
)

// SkillSelector determines which skills from the available set should be presented
// to the model in the Phase 1 index. Implementations can filter, rank, or transform
// the available skill list.
type SkillSelector interface {
	// Select returns the names of skills that should appear in the Phase 1 index.
	// The query is typically the latest user message or conversation context.
	// Available contains all skills that passed directory filtering for the current state.
	Select(ctx context.Context, query string, available []SkillMetadata) ([]string, error)
}

// ModelDrivenSelector is the default selector — it returns all available skills,
// letting the model decide which to activate via skill__activate.
// Effective for up to ~50 skills. Safe for concurrent use.
type ModelDrivenSelector struct{}

// NewModelDrivenSelector creates a new ModelDrivenSelector.
func NewModelDrivenSelector() *ModelDrivenSelector {
	return &ModelDrivenSelector{}
}

// Select returns all skill names from available.
func (s *ModelDrivenSelector) Select(
	_ context.Context,
	_ string,
	available []SkillMetadata,
) ([]string, error) {
	names := make([]string, len(available))
	for i, skill := range available {
		names[i] = skill.Name
	}
	return names, nil
}

// TagSelector pre-filters skills based on metadata tags.
// Skills whose Metadata map contains a "tags" key with a comma-separated list
// are matched against the selector's required tags. A skill is included if it
// has at least one tag in common with the selector's required tags.
// If a skill has no "tags" metadata key, it is excluded.
// Safe for concurrent use.
type TagSelector struct {
	tags map[string]bool
}

// NewTagSelector creates a TagSelector that matches skills having at least one
// of the specified tags. Duplicate tags are deduplicated automatically.
func NewTagSelector(tags []string) *TagSelector {
	tagSet := make(map[string]bool, len(tags))
	for _, t := range tags {
		trimmed := strings.TrimSpace(t)
		if trimmed != "" {
			tagSet[trimmed] = true
		}
	}
	return &TagSelector{tags: tagSet}
}

// Select returns the names of skills whose metadata["tags"] contains at least
// one tag matching the selector's required tags.
func (s *TagSelector) Select(
	_ context.Context,
	_ string,
	available []SkillMetadata,
) ([]string, error) {
	if len(s.tags) == 0 {
		return []string{}, nil
	}

	var names []string
	for _, skill := range available {
		rawTags, ok := skill.Metadata["tags"]
		if !ok {
			continue
		}
		for _, t := range strings.Split(rawTags, ",") {
			trimmed := strings.TrimSpace(t)
			if s.tags[trimmed] {
				names = append(names, skill.Name)
				break
			}
		}
	}

	if names == nil {
		return []string{}, nil
	}
	return names, nil
}
