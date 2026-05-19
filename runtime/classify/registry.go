package classify

import (
	"context"
	"fmt"
	"sync"
)

// Registry holds named classifier and embedder instances keyed by an
// id supplied at config time (e.g. "hf", "hf-azure", "onnx-local").
// Eval handlers look up by id; the id-to-backend mapping is the only
// thing the handler config needs to know about.
//
// Backends register themselves into a Registry at engine startup;
// the registry then travels with context.Context to handlers via
// WithRegistry / FromContext.
type Registry struct {
	mu               sync.RWMutex
	audioClassifiers map[string]AudioClassifier
	textClassifiers  map[string]TextClassifier
	imageClassifiers map[string]ImageClassifier
	videoClassifiers map[string]VideoClassifier
	embedders        map[string]Embedder
	defaultAudio     string
	defaultText      string
	defaultImage     string
	defaultVideo     string
	defaultEmbedder  string
}

// NewRegistry returns an empty Registry. Backends are added via
// RegisterAudio / RegisterText / etc.; defaults are set with the
// SetDefault* methods.
func NewRegistry() *Registry {
	return &Registry{
		audioClassifiers: make(map[string]AudioClassifier),
		textClassifiers:  make(map[string]TextClassifier),
		imageClassifiers: make(map[string]ImageClassifier),
		videoClassifiers: make(map[string]VideoClassifier),
		embedders:        make(map[string]Embedder),
	}
}

// RegisterAudio adds an AudioClassifier under the given id.
// Calling twice with the same id replaces the previous instance.
func (r *Registry) RegisterAudio(id string, c AudioClassifier) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.audioClassifiers[id] = c
}

// RegisterText adds a TextClassifier.
func (r *Registry) RegisterText(id string, c TextClassifier) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.textClassifiers[id] = c
}

// RegisterImage adds an ImageClassifier.
func (r *Registry) RegisterImage(id string, c ImageClassifier) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.imageClassifiers[id] = c
}

// RegisterVideo adds a VideoClassifier.
func (r *Registry) RegisterVideo(id string, c VideoClassifier) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.videoClassifiers[id] = c
}

// RegisterEmbedder adds an Embedder.
func (r *Registry) RegisterEmbedder(id string, e Embedder) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.embedders[id] = e
}

// SetDefaultAudio names the AudioClassifier used when a handler
// doesn't pass an explicit id. The id must already be registered.
func (r *Registry) SetDefaultAudio(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.audioClassifiers[id]; !ok {
		return fmt.Errorf("classify: default audio classifier %q not registered", id)
	}
	r.defaultAudio = id
	return nil
}

// SetDefaultText names the default TextClassifier.
func (r *Registry) SetDefaultText(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.textClassifiers[id]; !ok {
		return fmt.Errorf("classify: default text classifier %q not registered", id)
	}
	r.defaultText = id
	return nil
}

// SetDefaultImage names the default ImageClassifier.
func (r *Registry) SetDefaultImage(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.imageClassifiers[id]; !ok {
		return fmt.Errorf("classify: default image classifier %q not registered", id)
	}
	r.defaultImage = id
	return nil
}

// SetDefaultVideo names the default VideoClassifier.
func (r *Registry) SetDefaultVideo(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.videoClassifiers[id]; !ok {
		return fmt.Errorf("classify: default video classifier %q not registered", id)
	}
	r.defaultVideo = id
	return nil
}

// SetDefaultEmbedder names the default Embedder.
func (r *Registry) SetDefaultEmbedder(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.embedders[id]; !ok {
		return fmt.Errorf("classify: default embedder %q not registered", id)
	}
	r.defaultEmbedder = id
	return nil
}

// AudioClassifier resolves by id, falling back to the configured
// default when id is empty. Returns a non-nil error when nothing
// matches.
func (r *Registry) AudioClassifier(id string) (AudioClassifier, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if id == "" {
		id = r.defaultAudio
	}
	if id == "" {
		return nil, fmt.Errorf("classify: no audio classifier id supplied and no default configured")
	}
	c, ok := r.audioClassifiers[id]
	if !ok {
		return nil, fmt.Errorf("classify: audio classifier %q not registered", id)
	}
	return c, nil
}

// TextClassifier resolves by id with default fallback.
func (r *Registry) TextClassifier(id string) (TextClassifier, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if id == "" {
		id = r.defaultText
	}
	if id == "" {
		return nil, fmt.Errorf("classify: no text classifier id supplied and no default configured")
	}
	c, ok := r.textClassifiers[id]
	if !ok {
		return nil, fmt.Errorf("classify: text classifier %q not registered", id)
	}
	return c, nil
}

// ImageClassifier resolves by id with default fallback.
func (r *Registry) ImageClassifier(id string) (ImageClassifier, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if id == "" {
		id = r.defaultImage
	}
	if id == "" {
		return nil, fmt.Errorf("classify: no image classifier id supplied and no default configured")
	}
	c, ok := r.imageClassifiers[id]
	if !ok {
		return nil, fmt.Errorf("classify: image classifier %q not registered", id)
	}
	return c, nil
}

// VideoClassifier resolves by id with default fallback.
func (r *Registry) VideoClassifier(id string) (VideoClassifier, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if id == "" {
		id = r.defaultVideo
	}
	if id == "" {
		return nil, fmt.Errorf("classify: no video classifier id supplied and no default configured")
	}
	c, ok := r.videoClassifiers[id]
	if !ok {
		return nil, fmt.Errorf("classify: video classifier %q not registered", id)
	}
	return c, nil
}

// Embedder resolves by id with default fallback.
func (r *Registry) Embedder(id string) (Embedder, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if id == "" {
		id = r.defaultEmbedder
	}
	if id == "" {
		return nil, fmt.Errorf("classify: no embedder id supplied and no default configured")
	}
	e, ok := r.embedders[id]
	if !ok {
		return nil, fmt.Errorf("classify: embedder %q not registered", id)
	}
	return e, nil
}

// registryContextKey is the unexported key used to attach a Registry
// to a context.Context. The type-as-key idiom avoids collisions with
// other context values.
type registryContextKey struct{}

// WithRegistry returns ctx with the Registry attached. Arena's eval
// orchestrator calls this once per run; handlers retrieve via
// FromContext.
func WithRegistry(ctx context.Context, r *Registry) context.Context {
	return context.WithValue(ctx, registryContextKey{}, r)
}

// FromContext returns the Registry attached to ctx, or nil if none.
// Eval handlers that don't find a registry fall back to a "skipped"
// result rather than failing — classification is an optional feature
// of the runtime, not a hard dependency.
func FromContext(ctx context.Context) *Registry {
	if ctx == nil {
		return nil
	}
	if r, ok := ctx.Value(registryContextKey{}).(*Registry); ok {
		return r
	}
	return nil
}
