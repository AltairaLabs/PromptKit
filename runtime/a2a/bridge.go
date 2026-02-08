package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

// ToolBridge discovers an A2A agent and creates ToolDescriptor entries for
// each of the agent's skills so they can be invoked through the standard
// tool registry.
type ToolBridge struct {
	client *Client
	tools  []*tools.ToolDescriptor
}

// NewToolBridge creates a ToolBridge backed by the given A2A client.
func NewToolBridge(client *Client) *ToolBridge {
	return &ToolBridge{client: client}
}

// RegisterAgent discovers the agent card and creates a ToolDescriptor for
// each skill. The descriptors are appended to the bridge's internal list
// (supporting multi-agent composition via GetToolDescriptors).
func (b *ToolBridge) RegisterAgent(ctx context.Context) ([]*tools.ToolDescriptor, error) {
	card, err := b.client.Discover(ctx)
	if err != nil {
		return nil, fmt.Errorf("a2a bridge: discover: %w", err)
	}

	var registered []*tools.ToolDescriptor
	for i := range card.Skills {
		td := skillToToolDescriptor(b.client.baseURL, card, &card.Skills[i])
		registered = append(registered, td)
	}

	b.tools = append(b.tools, registered...)
	return registered, nil
}

// GetToolDescriptors returns all tool descriptors accumulated via
// RegisterAgent calls.
func (b *ToolBridge) GetToolDescriptors() []*tools.ToolDescriptor {
	return b.tools
}

// skillToToolDescriptor converts a single AgentSkill into a ToolDescriptor.
func skillToToolDescriptor(agentURL string, card *AgentCard, skill *AgentSkill) *tools.ToolDescriptor {
	inputModes := skill.InputModes
	if len(inputModes) == 0 {
		inputModes = card.DefaultInputModes
	}

	outputModes := skill.OutputModes
	if len(outputModes) == 0 {
		outputModes = card.DefaultOutputModes
	}

	name := fmt.Sprintf("a2a_%s_%s", sanitizeName(card.Name), sanitizeName(skill.ID))

	description := skill.Description
	if description == "" {
		description = skill.Name
	}

	return &tools.ToolDescriptor{
		Name:         name,
		Description:  description,
		InputSchema:  generateInputSchema(inputModes),
		OutputSchema: generateOutputSchema(outputModes),
		Mode:         "a2a",
		A2AConfig: &tools.A2AConfig{
			AgentURL: agentURL,
			SkillID:  skill.ID,
		},
	}
}

// generateInputSchema builds a JSON Schema based on the skill's input modes.
func generateInputSchema(inputModes []string) json.RawMessage {
	props := map[string]any{
		"query": map[string]any{"type": "string"},
	}
	required := []string{"query"}

	for _, mode := range inputModes {
		if matchesMIME(mode, "image/") {
			props["image_url"] = map[string]any{"type": "string"}
			props["image_data"] = map[string]any{"type": "string"}
			break
		}
	}

	for _, mode := range inputModes {
		if matchesMIME(mode, "audio/") {
			props["audio_data"] = map[string]any{"type": "string"}
			break
		}
	}

	schema := map[string]any{
		"type":       "object",
		"properties": props,
		"required":   required,
	}
	data, _ := json.Marshal(schema)
	return data
}

// generateOutputSchema builds a JSON Schema based on the skill's output modes.
func generateOutputSchema(outputModes []string) json.RawMessage {
	props := map[string]any{
		"response": map[string]any{"type": "string"},
	}

	hasMedia := false
	for _, mode := range outputModes {
		if matchesMIME(mode, "image/") || matchesMIME(mode, "audio/") {
			hasMedia = true
			break
		}
	}
	if hasMedia {
		props["media_url"] = map[string]any{"type": "string"}
		props["media_type"] = map[string]any{"type": "string"}
	}

	schema := map[string]any{
		"type":       "object",
		"properties": props,
	}
	data, _ := json.Marshal(schema)
	return data
}

// matchesMIME checks if a mode string matches a MIME type prefix.
// For example, "image/*" and "image/png" both match prefix "image/".
func matchesMIME(mode, prefix string) bool {
	return strings.HasPrefix(mode, prefix)
}

var nonAlphanumeric = regexp.MustCompile(`[^a-z0-9]+`)

// sanitizeName converts a string to a safe tool-name component:
// lowercase, non-alphanumeric runs replaced with "_", trimmed.
func sanitizeName(s string) string {
	s = strings.ToLower(s)
	s = nonAlphanumeric.ReplaceAllString(s, "_")
	s = strings.Trim(s, "_")
	return s
}
