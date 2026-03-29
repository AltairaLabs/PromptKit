package memory

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

// Tool names in the memory namespace.
const (
	RecallToolName   = "memory__recall"
	RememberToolName = "memory__remember"
	ListToolName     = "memory__list"
	ForgetToolName   = "memory__forget"
)

// ExecutorMode is the executor name for Mode-based routing.
const ExecutorMode = "memory"

// Executor implements tools.Executor for all memory tools.
// It routes by tool name to the appropriate Store method.
type Executor struct {
	store Store
	scope map[string]string
}

// NewExecutor creates a Executor for the given store and scope.
func NewExecutor(store Store, scope map[string]string) *Executor {
	return &Executor{store: store, scope: scope}
}

// Name implements tools.Executor.
func (e *Executor) Name() string { return ExecutorMode }

// Execute implements tools.Executor. Routes by tool name.
func (e *Executor) Execute(
	ctx context.Context, desc *tools.ToolDescriptor, args json.RawMessage,
) (json.RawMessage, error) {
	if desc == nil {
		return nil, fmt.Errorf("memory executor: nil descriptor")
	}
	switch desc.Name {
	case RecallToolName:
		return e.recall(ctx, args)
	case RememberToolName:
		return e.remember(ctx, args)
	case ListToolName:
		return e.list(ctx, args)
	case ForgetToolName:
		return e.forget(ctx, args)
	default:
		return nil, fmt.Errorf("memory executor: unknown tool %q", desc.Name)
	}
}

func (e *Executor) recall(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var a struct {
		Query         string   `json:"query"`
		Types         []string `json:"types,omitempty"`
		Limit         int      `json:"limit,omitempty"`
		MinConfidence float64  `json:"min_confidence,omitempty"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("memory recall: %w", err)
	}

	memories, err := e.store.Retrieve(ctx, e.scope, a.Query, RetrieveOptions{
		Types:         a.Types,
		Limit:         a.Limit,
		MinConfidence: a.MinConfidence,
	})
	if err != nil {
		return nil, fmt.Errorf("memory recall: %w", err)
	}

	return json.Marshal(map[string]any{
		"memories": memories,
		"count":    len(memories),
	})
}

func (e *Executor) remember(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var a struct {
		Content    string         `json:"content"`
		Type       string         `json:"type,omitempty"`
		Confidence float64        `json:"confidence,omitempty"`
		Metadata   map[string]any `json:"metadata,omitempty"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("memory remember: %w", err)
	}
	if a.Content == "" {
		return nil, fmt.Errorf("memory remember: content is required")
	}

	if a.Type == "" {
		a.Type = "general"
	}
	if a.Confidence <= 0 {
		a.Confidence = 0.8
	}

	m := &Memory{
		Type:       a.Type,
		Content:    a.Content,
		Confidence: a.Confidence,
		Metadata:   a.Metadata,
		Scope:      e.scope,
	}

	if err := e.store.Save(ctx, m); err != nil {
		return nil, fmt.Errorf("memory remember: %w", err)
	}

	return json.Marshal(map[string]string{
		"status": "remembered",
		"id":     m.ID,
	})
}

func (e *Executor) list(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var a struct {
		Types  []string `json:"types,omitempty"`
		Limit  int      `json:"limit,omitempty"`
		Offset int      `json:"offset,omitempty"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("memory list: %w", err)
	}

	memories, err := e.store.List(ctx, e.scope, ListOptions{
		Types:  a.Types,
		Limit:  a.Limit,
		Offset: a.Offset,
	})
	if err != nil {
		return nil, fmt.Errorf("memory list: %w", err)
	}

	return json.Marshal(map[string]any{
		"memories": memories,
		"count":    len(memories),
	})
}

func (e *Executor) forget(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var a struct {
		MemoryID string `json:"memory_id"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("memory forget: %w", err)
	}
	if a.MemoryID == "" {
		return nil, fmt.Errorf("memory forget: memory_id is required")
	}

	if err := e.store.Delete(ctx, e.scope, a.MemoryID); err != nil {
		return nil, fmt.Errorf("memory forget: %w", err)
	}

	return json.Marshal(map[string]string{
		"status":    "forgotten",
		"memory_id": a.MemoryID,
	})
}

// RegisterMemoryTools registers the four base memory tools with executor routing.
func RegisterMemoryTools(registry *tools.Registry) {
	for _, desc := range buildMemoryToolDescriptors() {
		desc.Mode = ExecutorMode
		_ = registry.Register(desc)
	}
}

// Common output schemas to avoid duplication.
var (
	memoriesOutputSchema = mustJSON(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"memories": map[string]any{"type": "array"},
			"count":    map[string]any{"type": "integer"},
		},
	})
	statusOutputSchema = mustJSON(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"status": map[string]any{"type": "string"},
			"id":     map[string]any{"type": "string"},
		},
	})
	forgetOutputSchema = mustJSON(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"status":    map[string]any{"type": "string"},
			"memory_id": map[string]any{"type": "string"},
		},
	})
)

func buildMemoryToolDescriptors() []*tools.ToolDescriptor {
	return []*tools.ToolDescriptor{
		{
			Name:      RecallToolName,
			Namespace: "memory",
			Description: "Search your memories for relevant information. " +
				"Use this to recall facts, preferences, or context from previous conversations.",
			OutputSchema: memoriesOutputSchema,
			InputSchema: mustJSON(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "What to search for in memory.",
					},
					"types": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Filter by memory type (e.g., 'preference', 'episodic').",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of results.",
					},
					"min_confidence": map[string]any{
						"type":        "number",
						"description": "Minimum confidence threshold (0.0-1.0).",
					},
				},
				"required": []string{"query"},
			}),
		},
		{
			Name:      RememberToolName,
			Namespace: "memory",
			Description: "Store something in memory for future conversations. " +
				"Use this to remember user preferences, important facts, or decisions.",
			OutputSchema: statusOutputSchema,
			InputSchema: mustJSON(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"content": map[string]any{
						"type":        "string",
						"description": "What to remember (natural language).",
					},
					"type": map[string]any{
						"type":        "string",
						"description": "Memory category (e.g., 'preference', 'fact', 'decision').",
					},
					"confidence": map[string]any{
						"type":        "number",
						"description": "How confident you are (0.0-1.0). Default: 0.8.",
					},
					"metadata": map[string]any{
						"type":        "object",
						"description": "Optional structured data to attach to the memory.",
					},
				},
				"required": []string{"content"},
			}),
		},
		{
			Name:         ListToolName,
			Namespace:    "memory",
			Description:  "List stored memories, optionally filtered by type.",
			OutputSchema: memoriesOutputSchema,
			InputSchema: mustJSON(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"types": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Filter by memory type.",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum number of results.",
					},
				},
			}),
		},
		{
			Name:         ForgetToolName,
			Namespace:    "memory",
			Description:  "Delete a specific memory by ID.",
			OutputSchema: forgetOutputSchema,
			InputSchema: mustJSON(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"memory_id": map[string]any{
						"type":        "string",
						"description": "The ID of the memory to forget.",
					},
				},
				"required": []string{"memory_id"},
			}),
		},
	}
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("mustJSON: %v", err))
	}
	return b
}
