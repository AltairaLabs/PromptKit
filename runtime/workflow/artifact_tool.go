package workflow

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

// ArtifactToolName is the qualified name of the workflow set_artifact tool.
const ArtifactToolName = "workflow__set_artifact"

// ArtifactExecutorMode is the executor name for Mode-based routing.
const ArtifactExecutorMode = "workflow-artifact"

// ArtifactExecutor implements tools.Executor for workflow__set_artifact.
// Unlike TransitionExecutor, artifact mutations are safe to apply immediately
// since they only modify the context map without closing/reopening conversations.
type ArtifactExecutor struct {
	sm *StateMachine
}

// NewArtifactExecutor creates an ArtifactExecutor for the given state machine.
func NewArtifactExecutor(sm *StateMachine) *ArtifactExecutor {
	return &ArtifactExecutor{sm: sm}
}

// Name implements tools.Executor.
func (e *ArtifactExecutor) Name() string { return ArtifactExecutorMode }

// Execute implements tools.Executor. Sets the artifact value on the state machine.
func (e *ArtifactExecutor) Execute(
	_ context.Context, _ *tools.ToolDescriptor, args json.RawMessage,
) (json.RawMessage, error) {
	var a struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("failed to parse set_artifact args: %w", err)
	}
	if a.Name == "" {
		return nil, fmt.Errorf("artifact name is required")
	}

	e.sm.SetArtifact(a.Name, a.Value)

	return json.Marshal(map[string]string{
		"status":   "artifact_set",
		"artifact": a.Name,
	})
}

// RegisterArtifactTool registers the workflow__set_artifact tool in the given
// registry. Only registers if the spec declares artifacts on any state.
func RegisterArtifactTool(registry *tools.Registry, spec *Spec) {
	if registry == nil || spec == nil {
		return
	}
	// Check if any state declares artifacts
	hasArtifacts := false
	for _, state := range spec.States {
		if len(state.Artifacts) > 0 {
			hasArtifacts = true
			break
		}
	}
	if !hasArtifacts {
		return
	}

	desc := BuildArtifactToolDescriptor(spec)
	desc.Mode = ArtifactExecutorMode
	_ = registry.Register(desc)
}

// BuildArtifactToolDescriptor creates a tools.ToolDescriptor for the
// workflow__set_artifact tool, with artifact names as an enum constraint.
func BuildArtifactToolDescriptor(spec *Spec) *tools.ToolDescriptor {
	// Collect all artifact names across all states
	names := collectArtifactNames(spec)

	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"enum":        names,
				"description": "The artifact name to set.",
			},
			"value": map[string]any{
				"type":        "string",
				"description": "The artifact value.",
			},
		},
		"required": []string{"name", "value"},
	}
	schemaJSON, _ := json.Marshal(schema)

	return &tools.ToolDescriptor{
		Name:        ArtifactToolName,
		Namespace:   "workflow",
		Description: "Set a workflow artifact value. Artifacts persist across state transitions.",
		InputSchema: schemaJSON,
	}
}

// collectArtifactNames returns a deduplicated sorted list of artifact names.
func collectArtifactNames(spec *Spec) []string {
	seen := map[string]bool{}
	for _, state := range spec.States {
		for name := range state.Artifacts {
			seen[name] = true
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	return SortedStrings(names)
}

// SortedStrings returns a sorted copy of the input slice.
func SortedStrings(s []string) []string {
	cp := make([]string, len(s))
	copy(cp, s)
	sortStrings(cp)
	return cp
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
