package sdk

import (
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/middleware"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/storage"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

// PipelineBuilder provides low-level API for constructing custom pipelines with middleware.
//
// Use this when you need:
//   - Custom middleware injection
//   - Custom context builders
//   - Observability integration (LangFuse, DataDog, etc.)
//   - Advanced pipeline control
//
// For simple use cases, use ConversationManager instead.
//
// Example:
//
//	builder := sdk.NewPipelineBuilder().
//	    WithProvider(provider).
//	    WithMiddleware(customMiddleware).
//	    WithMiddleware(observabilityMiddleware)
//
//	pipe := builder.Build()
//	result, err := pipe.Execute(ctx, "user", "Hello!")
type PipelineBuilder struct {
	middleware []pipeline.Middleware
	config     *pipeline.RuntimeConfig
}

// NewPipelineBuilder creates a new pipeline builder
func NewPipelineBuilder() *PipelineBuilder {
	return &PipelineBuilder{
		middleware: []pipeline.Middleware{},
		config:     pipeline.DefaultRuntimeConfig(),
	}
}

// WithMiddleware adds middleware to the pipeline.
// Middleware executes in the order added.
func (pb *PipelineBuilder) WithMiddleware(m pipeline.Middleware) *PipelineBuilder {
	pb.middleware = append(pb.middleware, m)
	return pb
}

// WithProvider adds a provider middleware to the pipeline.
// This is a convenience method that wraps the runtime ProviderMiddleware.
func (pb *PipelineBuilder) WithProvider(provider providers.Provider, toolRegistry *tools.Registry, toolPolicy *pipeline.ToolPolicy) *PipelineBuilder {
	config := &middleware.ProviderMiddlewareConfig{
		MaxTokens:   1500,
		Temperature: 0.7,
	}
	pb.middleware = append(pb.middleware, middleware.ProviderMiddleware(provider, toolRegistry, toolPolicy, config))
	return pb
}

// WithSimpleProvider adds a provider middleware without tools or custom config.
// This is the simplest way to add LLM execution to a pipeline.
func (pb *PipelineBuilder) WithSimpleProvider(provider providers.Provider) *PipelineBuilder {
	config := &middleware.ProviderMiddlewareConfig{
		MaxTokens:   1500,
		Temperature: 0.7,
	}
	pb.middleware = append(pb.middleware, middleware.ProviderMiddleware(provider, nil, nil, config))
	return pb
}

// WithTemplate adds template substitution middleware.
// This replaces {{variable}} placeholders in the system prompt.
func (pb *PipelineBuilder) WithTemplate() *PipelineBuilder {
	pb.middleware = append(pb.middleware, middleware.TemplateMiddleware())
	return pb
}

// WithMediaExternalization adds media externalization middleware.
// This automatically stores large media content to a storage backend and replaces
// inline base64 data with storage references, reducing memory usage.
//
// Parameters:
//   - storageService: The storage backend for media files
//   - sizeThresholdKB: Media larger than this (in KB) will be externalized
//   - defaultPolicy: Retention policy name (e.g., "retain", "delete-after-30d")
//
// Example:
//
//	storage, _ := local.NewFileStore(local.FileStoreConfig{BaseDir: "./media"})
//	builder.WithMediaExternalization(storage, 100, "retain")
func (pb *PipelineBuilder) WithMediaExternalization(storageService storage.MediaStorageService, sizeThresholdKB int64, defaultPolicy string) *PipelineBuilder {
	config := &middleware.MediaExternalizerConfig{
		Enabled:         true,
		StorageService:  storageService,
		SizeThresholdKB: sizeThresholdKB,
		DefaultPolicy:   defaultPolicy,
	}
	pb.middleware = append(pb.middleware, middleware.MediaExternalizerMiddleware(config))
	return pb
}

// WithConfig sets the pipeline runtime configuration
func (pb *PipelineBuilder) WithConfig(config *pipeline.RuntimeConfig) *PipelineBuilder {
	pb.config = config
	return pb
}

// Build constructs the pipeline
func (pb *PipelineBuilder) Build() *pipeline.Pipeline {
	return pipeline.NewPipelineWithConfig(pb.config, pb.middleware...)
}

// CustomContextMiddleware is an example of custom middleware for context building.
// Users can implement similar middleware for their specific needs.
//
// Example:
//
//	type MyContextMiddleware struct {
//	    ragClient *RAGClient
//	}
//
//	func (m *MyContextMiddleware) Process(execCtx *pipeline.ExecutionContext, next func() error) error {
//	    // Extract query from last user message
//	    query := execCtx.Messages[len(execCtx.Messages)-1].Content
//
//	    // Fetch relevant documents
//	    docs, _ := m.ragClient.Search(query, 5)
//
//	    // Add to variables for template substitution
//	    execCtx.Variables["rag_context"] = formatDocs(docs)
//
//	    return next()
//	}
type CustomContextMiddleware interface {
	pipeline.Middleware
}

// ObservabilityMiddleware is an example of observability middleware.
// Users can implement similar middleware for LangFuse, DataDog, etc.
//
// Example:
//
//	type LangFuseMiddleware struct {
//	    client *langfuse.Client
//	}
//
//	func (m *LangFuseMiddleware) Process(execCtx *pipeline.ExecutionContext, next func() error) error {
//	    traceID := m.client.StartTrace(...)
//	    spanID := m.client.StartSpan(traceID, ...)
//
//	    start := time.Now()
//	    err := next()
//	    duration := time.Since(start)
//
//	    m.client.EndSpan(spanID, langfuse.SpanResult{
//	        Duration: duration,
//	        TokensInput: execCtx.CostInfo.InputTokens,
//	        TokensOutput: execCtx.CostInfo.OutputTokens,
//	        Error: err,
//	    })
//
//	    return err
//	}
type ObservabilityMiddleware interface {
	pipeline.Middleware
}
