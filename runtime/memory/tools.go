package memory

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
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

// toolNamespace is the namespace the memory tools are registered under.
const toolNamespace = "memory"

// Memory tool argument and result field names.
const (
	argQuery       = "query"
	argTypes       = "types"
	argType        = "type"
	argLimit       = "limit"
	argContent     = "content"
	argMemoryID    = "memory_id"
	resultMemories = "memories"
	resultCount    = "count"
	resultStatus   = "status"
)

// JSON Schema keywords and type names used in the memory tool schemas.
const (
	schemaKeyType        = "type"
	schemaKeyProperties  = "properties"
	schemaKeyDescription = "description"
	schemaKeyItems       = "items"
	schemaKeyRequired    = "required"
	schemaTypeObject     = "object"
	schemaTypeString     = "string"
	schemaTypeArray      = "array"
	schemaTypeInteger    = "integer"
	schemaTypeNumber     = "number"
)

// descMaxResults describes the limit argument of the recall and list tools.
const descMaxResults = "Maximum number of results."

// Executor implements tools.Executor for all memory tools.
// It routes by tool name to the appropriate Store method.
//
// The scope it was constructed with is a default, not a binding: every call
// prefers the scope on the context (see [WithScope]). One Executor is all a
// tools.Registry holds per name, so a host running concurrent conversations
// over a shared registry must scope per call or their memories cross.
type Executor struct {
	store Store
	scope map[string]string
}

// NewExecutor creates a Executor for the given store and default scope.
func NewExecutor(store Store, scope map[string]string) *Executor {
	return &Executor{store: store, scope: scope}
}

// scopeFor returns the conversation's scope from the context, falling back to
// the scope the Executor was constructed with.
func (e *Executor) scopeFor(ctx context.Context) map[string]string {
	if scope := ScopeFromContext(ctx); scope != nil {
		return scope
	}
	return e.scope
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
	// Decode typed fields and capture any unknown top-level args, the same
	// way remember does. Hosts use sdk.WithToolDescriptorOverride to extend
	// memory__recall's input schema with backend-specific fields (Omnia adds
	// graph expansion and point-in-time args); without this passthrough the
	// store never sees them. See AltairaLabs/PromptKit#1987.
	extras, err := tools.DecodeArgsExtras(args, &a, argQuery, argTypes, argLimit, "min_confidence")
	if err != nil {
		return nil, fmt.Errorf("memory recall: %w", err)
	}

	memories, err := e.store.Retrieve(ctx, e.scopeFor(ctx), a.Query, RetrieveOptions{
		Types:         a.Types,
		Limit:         a.Limit,
		MinConfidence: a.MinConfidence,
		Extras:        extras,
	})
	if err != nil {
		return nil, fmt.Errorf("memory recall: %w", err)
	}

	return json.Marshal(map[string]any{
		resultMemories: memories,
		resultCount:    len(memories),
	})
}

func (e *Executor) remember(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var a struct {
		Content    string         `json:"content"`
		Type       string         `json:"type,omitempty"`
		Confidence float64        `json:"confidence,omitempty"`
		Category   string         `json:"category,omitempty"`
		Metadata   map[string]any `json:"metadata,omitempty"`
	}
	// Decode typed fields and capture any unknown top-level args. Hosts use
	// sdk.WithToolDescriptorOverride to extend memory__remember's input
	// schema with deployment-specific fields (e.g. Omnia's `about` for
	// dedup); without this passthrough those fields would be dropped.
	extras, err := tools.DecodeArgsExtras(args, &a, argContent, argType, "confidence", "category", "metadata")
	if err != nil {
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
		Metadata:   tools.MergeExtrasIntoMetadata(a.Metadata, extras),
		Scope:      e.scopeFor(ctx),
	}
	// Stash the LLM-supplied consent category so downstream consumers
	// (e.g. Omnia's PII redactor / per-category retention) can read it
	// without re-classifying. Empty input is a no-op and lets a
	// server-side classifier act as the fallback.
	m.SetConsentCategory(ConsentCategory(a.Category))
	// Always set provenance — overwrites any LLM-provided value.
	m.SetProvenance(ProvenanceUserRequested)

	if err := e.store.Save(ctx, m); err != nil {
		return nil, fmt.Errorf("memory remember: %w", err)
	}

	return json.Marshal(map[string]string{
		resultStatus: "remembered",
		"id":         m.ID,
	})
}

func (e *Executor) list(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var a struct {
		Types  []string `json:"types,omitempty"`
		Limit  int      `json:"limit,omitempty"`
		Offset int      `json:"offset,omitempty"`
	}
	// Same passthrough as recall — see [Executor.recall].
	extras, err := tools.DecodeArgsExtras(args, &a, argTypes, argLimit, "offset")
	if err != nil {
		return nil, fmt.Errorf("memory list: %w", err)
	}

	memories, err := e.store.List(ctx, e.scopeFor(ctx), ListOptions{
		Types:  a.Types,
		Limit:  a.Limit,
		Offset: a.Offset,
		Extras: extras,
	})
	if err != nil {
		return nil, fmt.Errorf("memory list: %w", err)
	}

	return json.Marshal(map[string]any{
		resultMemories: memories,
		resultCount:    len(memories),
	})
}

func (e *Executor) forget(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var a struct {
		MemoryID string `json:"memory_id"`
	}
	// Same passthrough as recall — see [Executor.recall]. Delete takes no
	// options parameter, so the extras reach only a store that opts in by
	// implementing [ExtrasDeleter]; the rest keep the plain Delete.
	extras, err := tools.DecodeArgsExtras(args, &a, argMemoryID)
	if err != nil {
		return nil, fmt.Errorf("memory forget: %w", err)
	}
	if a.MemoryID == "" {
		return nil, fmt.Errorf("memory forget: memory_id is required")
	}

	if err := e.delete(ctx, a.MemoryID, extras); err != nil {
		return nil, fmt.Errorf("memory forget: %w", err)
	}

	return json.Marshal(map[string]string{
		resultStatus: "forgotten",
		argMemoryID:  a.MemoryID,
	})
}

// delete routes to the store's [ExtrasDeleter] implementation when it has
// one so backend-specific forget args survive, and to the plain
// [Store.Delete] otherwise.
func (e *Executor) delete(ctx context.Context, memoryID string, extras map[string]any) error {
	if d, ok := e.store.(ExtrasDeleter); ok {
		return d.DeleteWithOptions(ctx, e.scopeFor(ctx), memoryID, DeleteOptions{Extras: extras})
	}
	return e.store.Delete(ctx, e.scopeFor(ctx), memoryID)
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
		schemaKeyType: schemaTypeObject,
		schemaKeyProperties: map[string]any{
			resultMemories: map[string]any{schemaKeyType: schemaTypeArray},
			resultCount:    map[string]any{schemaKeyType: schemaTypeInteger},
		},
	})
	statusOutputSchema = mustJSON(map[string]any{
		schemaKeyType: schemaTypeObject,
		schemaKeyProperties: map[string]any{
			resultStatus: map[string]any{schemaKeyType: schemaTypeString},
			"id":         map[string]any{schemaKeyType: schemaTypeString},
		},
	})
	forgetOutputSchema = mustJSON(map[string]any{
		schemaKeyType: schemaTypeObject,
		schemaKeyProperties: map[string]any{
			resultStatus: map[string]any{schemaKeyType: schemaTypeString},
			argMemoryID:  map[string]any{schemaKeyType: schemaTypeString},
		},
	})
)

func buildMemoryToolDescriptors() []*tools.ToolDescriptor {
	return []*tools.ToolDescriptor{
		{
			Name:      RecallToolName,
			Namespace: toolNamespace,
			Description: "Search your memories for relevant information. " +
				"Use this to recall facts, preferences, or context from previous conversations.",
			OutputSchema: memoriesOutputSchema,
			InputSchema: mustJSON(map[string]any{
				schemaKeyType: schemaTypeObject,
				schemaKeyProperties: map[string]any{
					argQuery: map[string]any{
						schemaKeyType:        schemaTypeString,
						schemaKeyDescription: "What to search for in memory.",
					},
					argTypes: map[string]any{
						schemaKeyType:        schemaTypeArray,
						schemaKeyItems:       map[string]any{schemaKeyType: schemaTypeString},
						schemaKeyDescription: "Filter by memory type (e.g., 'preference', 'episodic').",
					},
					argLimit: map[string]any{
						schemaKeyType:        schemaTypeInteger,
						schemaKeyDescription: descMaxResults,
					},
					"min_confidence": map[string]any{
						schemaKeyType:        schemaTypeNumber,
						schemaKeyDescription: "Minimum confidence threshold (0.0-1.0).",
					},
				},
				schemaKeyRequired: []string{argQuery},
			}),
		},
		{
			Name:      RememberToolName,
			Namespace: toolNamespace,
			Description: "Store something in memory for future conversations. " +
				"Use this to remember user preferences, important facts, or decisions.",
			OutputSchema: statusOutputSchema,
			InputSchema: mustJSON(map[string]any{
				schemaKeyType: schemaTypeObject,
				schemaKeyProperties: map[string]any{
					argContent: map[string]any{
						schemaKeyType:        schemaTypeString,
						schemaKeyDescription: "What to remember (natural language).",
					},
					argType: map[string]any{
						schemaKeyType:        schemaTypeString,
						schemaKeyDescription: "Memory category (e.g., 'preference', 'fact', 'decision').",
					},
					"confidence": map[string]any{
						schemaKeyType:        schemaTypeNumber,
						schemaKeyDescription: "How confident you are (0.0-1.0). Default: 0.8.",
					},
					"category": map[string]any{
						schemaKeyType: schemaTypeString,
						schemaKeyDescription: "Optional consent category for downstream policies " +
							"(retention, redaction, opt-out). Pick the most specific match " +
							"for the memory's content; leave unset if uncertain so a " +
							"server-side classifier can decide. Suggested values:\n" +
							"  memory:identity     names, IDs, pronouns, contact details\n" +
							"  memory:preferences  likes, dislikes, settings, configuration\n" +
							"  memory:context      work/project context, domain knowledge, current task\n" +
							"  memory:location     geographic info, addresses, IP\n" +
							"  memory:health       health, dietary, medical, accessibility info\n" +
							"  memory:history      past conversations, decisions, what was said before",
					},
					"metadata": map[string]any{
						schemaKeyType:        schemaTypeObject,
						schemaKeyDescription: "Optional structured data to attach to the memory.",
					},
				},
				schemaKeyRequired: []string{argContent},
			}),
		},
		{
			Name:         ListToolName,
			Namespace:    toolNamespace,
			Description:  "List stored memories, optionally filtered by type.",
			OutputSchema: memoriesOutputSchema,
			InputSchema: mustJSON(map[string]any{
				schemaKeyType: schemaTypeObject,
				schemaKeyProperties: map[string]any{
					argTypes: map[string]any{
						schemaKeyType:        schemaTypeArray,
						schemaKeyItems:       map[string]any{schemaKeyType: schemaTypeString},
						schemaKeyDescription: "Filter by memory type.",
					},
					argLimit: map[string]any{
						schemaKeyType:        schemaTypeInteger,
						schemaKeyDescription: descMaxResults,
					},
				},
			}),
		},
		{
			Name:         ForgetToolName,
			Namespace:    toolNamespace,
			Description:  "Delete a specific memory by ID.",
			OutputSchema: forgetOutputSchema,
			InputSchema: mustJSON(map[string]any{
				schemaKeyType: schemaTypeObject,
				schemaKeyProperties: map[string]any{
					argMemoryID: map[string]any{
						schemaKeyType:        schemaTypeString,
						schemaKeyDescription: "The ID of the memory to forget.",
					},
				},
				schemaKeyRequired: []string{argMemoryID},
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
