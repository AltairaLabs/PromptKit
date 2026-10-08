package sdk

import (
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/v2/persistence/memory"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/sdk/v2/internal/pack"
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

	// callCheck is the pack-only half of the RFC 0017 call-site check, built
	// once; each Open runs it against that conversation's bindings.
	callCheck *callProviderCheck
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
		promptRegistry: pack.ToPromptRegistry(p),
		toolRepository: pack.ToToolRepository(p),
		callCheck:      newCallProviderCheck(p),
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

// openConversation is the shared implementation for Open and OpenDuplex on
// templates. Everything after finding the prompt is sdk.Open's own code
// (prepareConversation, completeOpen); the template only supplies what it
// built once for the pack.
func (t *PackTemplate) openConversation(
	promptName string,
	duplex bool,
	opts ...Option,
) (*Conversation, error) {
	cfg, err := applyOpenOptions(promptName, opts)
	if err != nil {
		return nil, err
	}

	packPrompt, err := t.validatePrompt(promptName)
	if err != nil {
		return nil, err
	}

	conv, prov, err := prepareConversation(t.pack, packPrompt, promptName, cfg, conversationSources{
		prompts:   t.promptRegistry,
		tools:     t.toolRepository,
		callCheck: t.callCheck,
	})
	if err != nil {
		return nil, err
	}
	return completeOpen(conv, prov, duplex)
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
