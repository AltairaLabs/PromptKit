package providers

import (
	"context"
	"errors"
	"testing"
)

// MockEmbeddingProvider is a test implementation of EmbeddingProvider.
type MockEmbeddingProvider struct {
	embeddings     map[string][]float32
	dimensions     int
	maxBatch       int
	id             string
	embedCallCount int
	shouldError    bool
	errorMessage   string
}

// NewMockEmbeddingProvider creates a mock embedding provider for testing.
func NewMockEmbeddingProvider() *MockEmbeddingProvider {
	return &MockEmbeddingProvider{
		embeddings: make(map[string][]float32),
		dimensions: 3, // Small dimension for testing
		maxBatch:   10,
		id:         "mock-embedding",
	}
}

// SetEmbedding sets a predetermined embedding for a text.
func (m *MockEmbeddingProvider) SetEmbedding(text string, embedding []float32) {
	m.embeddings[text] = embedding
}

// SetError configures the mock to return an error.
func (m *MockEmbeddingProvider) SetError(shouldError bool, message string) {
	m.shouldError = shouldError
	m.errorMessage = message
}

// Embed implements EmbeddingProvider.
func (m *MockEmbeddingProvider) Embed(ctx context.Context, req EmbeddingRequest) (EmbeddingResponse, error) {
	m.embedCallCount++

	if m.shouldError {
		return EmbeddingResponse{}, errors.New(m.errorMessage)
	}

	embeddings := make([][]float32, len(req.Texts))
	for i, text := range req.Texts {
		if preset, ok := m.embeddings[text]; ok {
			embeddings[i] = preset
		} else {
			// Generate a simple deterministic embedding based on text length
			embeddings[i] = make([]float32, m.dimensions)
			for j := range embeddings[i] {
				embeddings[i][j] = float32(len(text)+j) / 100.0
			}
		}
	}

	return EmbeddingResponse{
		Embeddings: embeddings,
		Model:      "mock-model",
		Usage:      &EmbeddingUsage{TotalTokens: len(req.Texts) * 10},
	}, nil
}

// EmbeddingDimensions implements EmbeddingProvider.
func (m *MockEmbeddingProvider) EmbeddingDimensions() int {
	return m.dimensions
}

// MaxBatchSize implements EmbeddingProvider.
func (m *MockEmbeddingProvider) MaxBatchSize() int {
	return m.maxBatch
}

// ID implements EmbeddingProvider.
func (m *MockEmbeddingProvider) ID() string {
	return m.id
}

// EmbedCallCount returns how many times Embed was called.
func (m *MockEmbeddingProvider) EmbedCallCount() int {
	return m.embedCallCount
}

func TestMockEmbeddingProvider_Embed(t *testing.T) {
	provider := NewMockEmbeddingProvider()

	req := EmbeddingRequest{
		Texts: []string{"hello", "world"},
	}

	resp, err := provider.Embed(context.Background(), req)
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	if len(resp.Embeddings) != 2 {
		t.Errorf("Expected 2 embeddings, got %d", len(resp.Embeddings))
	}

	if resp.Model != "mock-model" {
		t.Errorf("Expected model 'mock-model', got %s", resp.Model)
	}

	if resp.Usage == nil || resp.Usage.TotalTokens != 20 {
		t.Errorf("Expected usage with 20 tokens")
	}
}

func TestMockEmbeddingProvider_PresetEmbeddings(t *testing.T) {
	provider := NewMockEmbeddingProvider()

	// Set a specific embedding for "test"
	preset := []float32{0.1, 0.2, 0.3}
	provider.SetEmbedding("test", preset)

	req := EmbeddingRequest{
		Texts: []string{"test"},
	}

	resp, err := provider.Embed(context.Background(), req)
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	if len(resp.Embeddings) != 1 {
		t.Fatalf("Expected 1 embedding, got %d", len(resp.Embeddings))
	}

	for i, v := range resp.Embeddings[0] {
		if v != preset[i] {
			t.Errorf("Embedding[%d] = %f, expected %f", i, v, preset[i])
		}
	}
}

func TestMockEmbeddingProvider_Error(t *testing.T) {
	provider := NewMockEmbeddingProvider()
	provider.SetError(true, "API unavailable")

	req := EmbeddingRequest{
		Texts: []string{"test"},
	}

	_, err := provider.Embed(context.Background(), req)
	if err == nil {
		t.Error("Expected error, got nil")
	}
	if err.Error() != "API unavailable" {
		t.Errorf("Expected 'API unavailable', got %s", err.Error())
	}
}

func TestMockEmbeddingProvider_Dimensions(t *testing.T) {
	provider := NewMockEmbeddingProvider()

	if provider.EmbeddingDimensions() != 3 {
		t.Errorf("Expected dimensions 3, got %d", provider.EmbeddingDimensions())
	}
}

func TestMockEmbeddingProvider_MaxBatchSize(t *testing.T) {
	provider := NewMockEmbeddingProvider()

	if provider.MaxBatchSize() != 10 {
		t.Errorf("Expected max batch 10, got %d", provider.MaxBatchSize())
	}
}

func TestMockEmbeddingProvider_ID(t *testing.T) {
	provider := NewMockEmbeddingProvider()

	if provider.ID() != "mock-embedding" {
		t.Errorf("Expected ID 'mock-embedding', got %s", provider.ID())
	}
}

func TestMockEmbeddingProvider_CallCount(t *testing.T) {
	provider := NewMockEmbeddingProvider()

	if provider.EmbedCallCount() != 0 {
		t.Error("Expected 0 calls initially")
	}

	_, _ = provider.Embed(context.Background(), EmbeddingRequest{Texts: []string{"a"}})
	_, _ = provider.Embed(context.Background(), EmbeddingRequest{Texts: []string{"b"}})

	if provider.EmbedCallCount() != 2 {
		t.Errorf("Expected 2 calls, got %d", provider.EmbedCallCount())
	}
}

func TestEmbeddingRequest_Fields(t *testing.T) {
	req := EmbeddingRequest{
		Texts: []string{"hello", "world"},
		Model: "custom-model",
	}

	if len(req.Texts) != 2 {
		t.Errorf("Expected 2 texts, got %d", len(req.Texts))
	}
	if req.Model != "custom-model" {
		t.Errorf("Expected model 'custom-model', got %s", req.Model)
	}
}

func TestEmbeddingResponse_Fields(t *testing.T) {
	resp := EmbeddingResponse{
		Embeddings: [][]float32{{0.1, 0.2}, {0.3, 0.4}},
		Model:      "test-model",
		Usage:      &EmbeddingUsage{TotalTokens: 50},
	}

	if len(resp.Embeddings) != 2 {
		t.Errorf("Expected 2 embeddings, got %d", len(resp.Embeddings))
	}
	if resp.Model != "test-model" {
		t.Errorf("Expected model 'test-model', got %s", resp.Model)
	}
	if resp.Usage.TotalTokens != 50 {
		t.Errorf("Expected 50 tokens, got %d", resp.Usage.TotalTokens)
	}
}

func TestEmbeddingUsage_Fields(t *testing.T) {
	usage := EmbeddingUsage{
		TotalTokens: 100,
	}

	if usage.TotalTokens != 100 {
		t.Errorf("Expected 100 tokens, got %d", usage.TotalTokens)
	}
}

// Verify MockEmbeddingProvider implements EmbeddingProvider interface
var _ EmbeddingProvider = (*MockEmbeddingProvider)(nil)
