package adapters

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Registry manages registered recording adapters.
type Registry struct {
	mu       sync.RWMutex
	adapters []RecordingAdapter
}

// NewRegistry creates a new adapter registry with default adapters registered.
func NewRegistry() *Registry {
	r := &Registry{
		adapters: make([]RecordingAdapter, 0),
	}

	// Register built-in adapters
	r.Register(NewSessionRecordingAdapter())
	r.Register(NewArenaOutputAdapter())
	r.Register(NewTranscriptAdapter())

	return r
}

// Register adds an adapter to the registry.
// Adapters are checked in registration order, so register more specific
// adapters before generic ones.
func (r *Registry) Register(adapter RecordingAdapter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters = append(r.adapters, adapter)
}

// FindAdapter returns the first adapter that can handle the given path and type hint.
// Returns nil if no adapter can handle the format.
func (r *Registry) FindAdapter(path, typeHint string) RecordingAdapter {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, adapter := range r.adapters {
		if adapter.CanHandle(path, typeHint) {
			return adapter
		}
	}
	return nil
}

// Enumerate expands a source into individual recording references.
// Uses the first adapter that can handle the source.
// Returns an error if no adapter can handle the source or if enumeration fails.
func (r *Registry) Enumerate(source, typeHint string) ([]RecordingReference, error) {
	adapter := r.FindAdapter(source, typeHint)
	if adapter == nil {
		ext := filepath.Ext(source)
		if typeHint != "" {
			return nil, fmt.Errorf("no adapter found for type hint '%s' (source: %s)", typeHint, source)
		}
		return nil, fmt.Errorf("no adapter found for file extension '%s' (source: %s)", ext, source)
	}

	return adapter.Enumerate(source)
}

// Load finds an appropriate adapter and loads the recording from a reference.
// Returns an error if no adapter can handle the format or if loading fails.
func (r *Registry) Load(ref RecordingReference) ([]types.Message, *RecordingMetadata, error) {
	adapter := r.FindAdapter(ref.ID, ref.TypeHint)
	if adapter == nil {
		ext := filepath.Ext(ref.ID)
		if ref.TypeHint != "" {
			return nil, nil, fmt.Errorf("no adapter found for type hint '%s' (ref: %s)", ref.TypeHint, ref.ID)
		}
		return nil, nil, fmt.Errorf("no adapter found for file extension '%s' (ref: %s)", ext, ref.ID)
	}

	return adapter.Load(ref)
}

// hasExtension checks if the path has one of the given extensions (case-insensitive).
// Supports both simple extensions (.json) and compound extensions (.recording.json).
func hasExtension(path string, extensions ...string) bool {
	pathLower := strings.ToLower(path)
	for _, ext := range extensions {
		extLower := strings.ToLower(ext)
		// For compound extensions like ".recording.json", check if path ends with it
		if strings.HasSuffix(pathLower, extLower) {
			return true
		}
	}
	return false
}

// matchesTypeHint checks if the type hint matches any of the given types (case-insensitive).
func matchesTypeHint(typeHint string, acceptedTypes ...string) bool {
	if typeHint == "" {
		return false
	}
	hint := strings.ToLower(strings.TrimSpace(typeHint))
	for _, t := range acceptedTypes {
		if strings.EqualFold(t, hint) {
			return true
		}
	}
	return false
}
