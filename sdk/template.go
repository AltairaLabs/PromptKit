package sdk

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/persistence/memory"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/sdk/internal/pack"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/tools"
)

// PackTemplate is a pre-loaded, immutable representation of a pack file.
//
// Use PackTemplate when creating many conversations from the same pack to
// avoid redundant file I/O, JSON parsing, schema validation, prompt registry
// construction, and tool repository construction on each Open() call.
//
// PackTemplate is safe for concurrent use. All cached artifacts are immutable
// after construction.
//
// Usage:
//
//	tmpl, err := sdk.LoadTemplate("./assistant.pack.json")
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Create conversations efficiently — pack is loaded once
//	for req := range requests {
//	    conv, err := tmpl.Open("chat", sdk.WithProvider(myProvider))
//	    if err != nil {
//	        log.Printf("open failed: %v", err)
//	        continue
//	    }
//	    go handleConversation(conv, req)
//	}
type PackTemplate struct {
	// pack is the immutable loaded pack (read-only after construction).
	pack *pack.Pack

	// promptRegistry is the shared prompt registry (thread-safe, read-only
	// after construction via internal RWMutex caching).
	promptRegistry *prompt.Registry

	// toolRepository is the shared tool repository. Each conversation creates
	// its own tools.Registry wrapping this shared repository, so tool
	// descriptors are loaded once but executors remain per-conversation.
	toolRepository *memory.ToolRepository
}

// LoadTemplate loads a pack file and pre-builds shared, immutable resources.
//
// The returned PackTemplate caches:
//   - The parsed pack structure
//   - The prompt registry (prompt configs, fragments)
//   - The tool repository (tool descriptors)
//
// These are shared across all conversations created from this template.
// Per-conversation resources (tool executors, state stores, sessions) are
// still created fresh for each conversation.
//
// Options that affect pack loading can be passed:
//   - WithSkipSchemaValidation() to skip JSON schema validation
func LoadTemplate(packPath string, opts ...Option) (*PackTemplate, error) {
	// Apply options only to extract pack-loading config
	cfg := &config{}
	for _, opt := range opts {
		if err := opt(cfg); err != nil {
			return nil, fmt.Errorf("failed to apply option: %w", err)
		}
	}

	absPath, err := resolvePackPath(packPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve pack path: %w", err)
	}

	loadOpts := pack.LoadOptions{
		SkipSchemaValidation: cfg.skipSchemaValidation,
	}

	p, err := pack.Load(absPath, loadOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to load pack: %w", err)
	}

	return &PackTemplate{
		pack:           p,
		promptRegistry: p.ToPromptRegistry(),
		toolRepository: p.ToToolRepository(),
	}, nil
}

// Open creates a new conversation from this template for the given prompt.
//
// This is equivalent to [sdk.Open] but reuses pre-loaded pack resources,
// avoiding per-conversation file I/O and parsing overhead.
//
// Per-conversation resources are still created fresh:
//   - Tool registry (with shared repository but per-conversation executors)
//   - State store and session
//   - Capabilities
//   - Event bus and hooks
func (t *PackTemplate) Open(promptName string, opts ...Option) (*Conversation, error) {
	return t.openConversation(promptName, false, opts...)
}

// OpenDuplex creates a new duplex streaming conversation from this template.
//
// This is equivalent to [sdk.OpenDuplex] but reuses pre-loaded pack resources.
func (t *PackTemplate) OpenDuplex(promptName string, opts ...Option) (*Conversation, error) {
	return t.openConversation(promptName, true, opts...)
}

// Pack returns the loaded pack for inspection. The returned pack must not be modified.
func (t *PackTemplate) Pack() *pack.Pack {
	return t.pack
}

// openConversation is the shared implementation for Open and OpenDuplex on templates.
func (t *PackTemplate) openConversation(
	promptName string,
	duplex bool,
	opts ...Option,
) (*Conversation, error) {
	cfg, err := applyOptions(promptName, opts)
	if err != nil {
		return nil, err
	}

	packPrompt, err := t.validatePrompt(promptName)
	if err != nil {
		return nil, err
	}

	prov, err := resolveProvider(cfg)
	if err != nil {
		return nil, err
	}

	conv := t.newConversation(promptName, packPrompt, cfg)

	if err := t.initConversation(conv, packPrompt, cfg); err != nil {
		return nil, err
	}

	if err := t.initSession(conv, cfg, prov, duplex); err != nil {
		return nil, err
	}

	conv.evalMW = newEvalMiddleware(conv)

	if err := initMCPRegistry(conv, cfg); err != nil {
		return nil, err
	}

	conv.runSessionStart(context.Background())
	return conv, nil
}

// validatePrompt checks that the named prompt exists in the cached pack.
func (t *PackTemplate) validatePrompt(promptName string) (*pack.Prompt, error) {
	packPrompt, ok := t.pack.Prompts[promptName]
	if !ok {
		available := make([]string, 0, len(t.pack.Prompts))
		for name := range t.pack.Prompts {
			available = append(available, name)
		}
		return nil, fmt.Errorf("prompt %q not found in pack (available: %v)", promptName, available)
	}
	return packPrompt, nil
}

// newConversation creates a Conversation struct with shared and per-conversation resources.
func (t *PackTemplate) newConversation(
	promptName string,
	packPrompt *pack.Prompt,
	cfg *config,
) *Conversation {
	return &Conversation{
		pack:           t.pack,
		prompt:         packPrompt,
		promptName:     promptName,
		promptRegistry: t.promptRegistry,
		toolRegistry:   tools.NewRegistryWithRepository(t.toolRepository),
		config:         cfg,
		handlers:       make(map[string]ToolHandler),
		asyncHandlers:  make(map[string]sdktools.AsyncToolHandler),
		pendingStore:   sdktools.NewPendingStore(),
		resolvedStore:  sdktools.NewResolvedStore(),
	}
}

// initConversation sets up capabilities, hooks, and event bus on the conversation.
func (t *PackTemplate) initConversation(conv *Conversation, packPrompt *pack.Prompt, cfg *config) error {
	applyDefaultVariables(conv, packPrompt)
	convertPackValidatorsToHooks(packPrompt, cfg)

	allCaps := mergeCapabilities(cfg.capabilities, inferCapabilities(t.pack))
	allCaps = ensureA2ACapability(allCaps, cfg)
	allCaps = ensureSkillsCapability(allCaps, cfg)
	wireA2AConfig(allCaps, cfg)
	wireSkillsConfig(allCaps, cfg)
	for _, cap := range allCaps {
		if err := cap.Init(CapabilityContext{Pack: t.pack, PromptName: conv.promptName}); err != nil {
			return fmt.Errorf("capability %q init failed: %w", cap.Name(), err)
		}
	}
	conv.capabilities = allCaps

	initEventBus(cfg)
	conv.hookRegistry = cfg.buildHookRegistry()
	return nil
}

// initSession initializes the appropriate session type (unary or duplex).
func (t *PackTemplate) initSession(
	conv *Conversation,
	cfg *config,
	prov providers.Provider,
	duplex bool,
) error {
	if duplex {
		streamProvider, ok := prov.(providers.StreamInputSupport)
		if !ok {
			return fmt.Errorf(
				"provider %T does not support duplex streaming (must implement providers.StreamInputSupport)",
				prov,
			)
		}
		return initDuplexSession(conv, cfg, streamProvider)
	}
	return initInternalStateStore(conv, cfg)
}
