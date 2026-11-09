package skills

import (
	"context"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/v2/selection"
)

// SkillSelector determines which skills from the available set should be presented
// to the model in the Phase 1 index. Implementations can filter, rank, or transform
// the available skill list.
//
// It runs on the selection.Selector path (see AdaptSkillSelector), so it follows
// that path's rules: it is consulted once per user turn with the user's text as
// the query, never for an empty query, and an error or an empty choice lists
// every admitted skill. New selectors should implement selection.Selector.
type SkillSelector interface {
	// Select returns the names of skills that should appear in the Phase 1 index.
	// The query is typically the latest user message or conversation context.
	// Available contains all skills that passed directory filtering for the current state.
	Select(ctx context.Context, query string, available []SkillMetadata) ([]string, error)
}

// AdaptSkillSelector adapts a SkillSelector into a selection.Selector, the
// interface the skill index is narrowed through, so ExecutorConfig.Selector and
// the SDK's skill-selector options feed the same path as a selector bound in
// RuntimeConfig. Each candidate is handed over as SkillMetadata carrying its
// name, description, metadata and allowed-tools, so a selector can choose by
// the tools a skill grants. License and compatibility are not carried. A nil
// SkillSelector adapts to nil.
func AdaptSkillSelector(s SkillSelector) selection.Selector {
	if s == nil {
		return nil
	}
	return skillSelectorAdapter{inner: s}
}

// skillSelectorAdapter is the selection.Selector over a SkillSelector.
type skillSelectorAdapter struct {
	inner SkillSelector
}

// Name identifies the adapter in logs.
func (a skillSelectorAdapter) Name() string { return "skill-selector" }

// Init is a no-op: a SkillSelector takes no shared infrastructure.
func (a skillSelectorAdapter) Init(selection.SelectorContext) error { return nil }

// Select converts the candidates to SkillMetadata and asks the SkillSelector.
func (a skillSelectorAdapter) Select(
	ctx context.Context, q selection.Query, candidates []selection.Candidate,
) ([]string, error) {
	available := make([]SkillMetadata, 0, len(candidates))
	for _, c := range candidates {
		available = append(available, SkillMetadata{
			Name:         c.ID,
			Description:  c.Description,
			Metadata:     c.Metadata,
			AllowedTools: c.AllowedTools,
		})
	}
	return a.inner.Select(ctx, q.Text, available)
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
