package sdk

import (
	"encoding/json"

	"github.com/AltairaLabs/PromptKit/runtime/a2a"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/prompt/agentcard"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

// nsA2A is the namespace prefix for A2A agent tools.
const nsA2A = "a2a"

// EndpointResolver determines the A2A endpoint URL for a given agent member.
// Implementations can provide static URLs, service-discovery lookups, or
// test-friendly mock endpoints.
type EndpointResolver interface {
	// Resolve returns the base URL (e.g. "http://localhost:9000") for the
	// named agent member. An empty string means the agent has no reachable
	// endpoint and should be skipped.
	Resolve(agentName string) string
}

// StaticEndpointResolver returns the same base URL for every agent.
// This is useful when all agents are behind a single gateway or when
// developing locally against a single A2A server.
type StaticEndpointResolver struct {
	BaseURL string
}

// Resolve returns the static base URL for any agent name.
func (r *StaticEndpointResolver) Resolve(_ string) string {
	return r.BaseURL
}

// MapEndpointResolver maps each agent name to a specific endpoint URL.
// Unknown agents return an empty string and are silently skipped.
type MapEndpointResolver struct {
	Endpoints map[string]string
}

// Resolve returns the endpoint URL for the given agent name, or empty string
// if not found.
func (r *MapEndpointResolver) Resolve(agentName string) string {
	return r.Endpoints[agentName]
}

// AgentToolResolver resolves agent member references in tool lists
// to A2A-compatible tool descriptors.
type AgentToolResolver struct {
	cards    map[string]*a2a.AgentCard
	pack     *prompt.Pack
	resolver EndpointResolver
}

// NewAgentToolResolver creates a resolver from a compiled pack.
// Returns nil if the pack has no agents section.
func NewAgentToolResolver(pack *prompt.Pack) *AgentToolResolver {
	cards := agentcard.GenerateAgentCards(pack)
	if cards == nil {
		return nil
	}
	return &AgentToolResolver{cards: cards, pack: pack}
}

// SetEndpointResolver configures how agent URLs are resolved.
// When nil, descriptors are created without an AgentURL (suitable for
// testing or when endpoints are resolved later).
func (r *AgentToolResolver) SetEndpointResolver(er EndpointResolver) {
	if r == nil {
		return
	}
	r.resolver = er
}

// IsAgentTool checks if a tool name refers to an agent member.
// It accepts both bare member keys ("summarizer") and qualified names ("a2a__summarizer").
func (r *AgentToolResolver) IsAgentTool(toolName string) bool {
	if r == nil {
		return false
	}
	// Direct lookup (bare key)
	if _, ok := r.cards[toolName]; ok {
		return true
	}
	// Check qualified a2a__ name
	ns, local := tools.ParseToolName(toolName)
	if ns == nsA2A {
		_, ok := r.cards[local]
		return ok
	}
	return false
}

// ResolveAgentTools returns tool descriptors for all agent members
// that appear in the given tool names list.
// Each descriptor has Mode "a2a", an input schema with a required "query"
// field, and (if an EndpointResolver is set) an AgentURL in A2AConfig.
func (r *AgentToolResolver) ResolveAgentTools(toolNames []string) []*tools.ToolDescriptor {
	if r == nil {
		return nil
	}
	var descriptors []*tools.ToolDescriptor
	for _, name := range toolNames {
		card, ok := r.cards[name]
		if !ok {
			continue
		}
		for i := range card.Skills {
			agentURL := ""
			if r.resolver != nil {
				agentURL = r.resolver.Resolve(name)
			}

			desc := &tools.ToolDescriptor{
				Name:         tools.QualifyToolName(nsA2A, name),
				Description:  card.Skills[i].Description,
				InputSchema:  agentInputSchema(),
				OutputSchema: agentOutputSchema(),
				Mode:         "a2a",
				A2AConfig: &tools.A2AConfig{
					AgentURL: agentURL,
					SkillID:  card.Skills[i].ID,
				},
			}
			descriptors = append(descriptors, desc)
		}
	}
	return descriptors
}

// MemberNames returns the names of all agent members known to this resolver.
func (r *AgentToolResolver) MemberNames() []string {
	if r == nil {
		return nil
	}
	names := make([]string, 0, len(r.cards))
	for name := range r.cards {
		names = append(names, name)
	}
	return names
}

// agentInputSchema returns the JSON Schema for an agent tool's input.
// Agent tools accept a "query" string that is forwarded as the A2A message.
func agentInputSchema() json.RawMessage {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "The message to send to the agent",
			},
		},
		"required": []string{"query"},
	}
	data, _ := json.Marshal(schema)
	return data
}

// agentOutputSchema returns the JSON Schema for an agent tool's output.
func agentOutputSchema() json.RawMessage {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"response": map[string]any{
				"type":        "string",
				"description": "The agent's response text",
			},
		},
	}
	data, _ := json.Marshal(schema)
	return data
}
