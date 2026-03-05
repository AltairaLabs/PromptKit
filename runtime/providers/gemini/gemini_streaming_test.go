package gemini

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestStreamResponse_IncrementalParsing(t *testing.T) {
	provider := &Provider{}

	t.Run("parses multi-chunk text response", func(t *testing.T) {
		responses := []geminiResponse{
			{
				Candidates: []geminiCandidate{
					{Content: geminiContent{Parts: []geminiPart{{Text: "Hello"}}}},
				},
			},
			{
				Candidates: []geminiCandidate{
					{Content: geminiContent{Parts: []geminiPart{{Text: " world"}}}},
				},
			},
			{
				Candidates: []geminiCandidate{
					{
						Content:      geminiContent{Parts: []geminiPart{{Text: "!"}}},
						FinishReason: "STOP",
					},
				},
				UsageMetadata: &geminiUsage{
					PromptTokenCount:     10,
					CandidatesTokenCount: 3,
				},
			},
		}

		body := marshalJSONArray(t, responses)
		outChan := make(chan providers.StreamChunk, 10)

		provider.streamResponse(context.Background(), io.NopCloser(body), outChan)

		chunks := collectChunks(t, outChan)

		// 3 text chunks + 1 final chunk with finish reason = 4 total
		if len(chunks) != 4 {
			t.Fatalf("expected 4 chunks, got %d", len(chunks))
		}

		// First chunk: "Hello"
		if chunks[0].Delta != "Hello" {
			t.Errorf("expected delta 'Hello', got %q", chunks[0].Delta)
		}
		if chunks[0].Content != "Hello" {
			t.Errorf("expected accumulated 'Hello', got %q", chunks[0].Content)
		}

		// Second chunk: " world"
		if chunks[1].Delta != " world" {
			t.Errorf("expected delta ' world', got %q", chunks[1].Delta)
		}
		if chunks[1].Content != "Hello world" {
			t.Errorf("expected accumulated 'Hello world', got %q", chunks[1].Content)
		}

		// Third chunk: "!" (text delta from the last response element)
		if chunks[2].Delta != "!" {
			t.Errorf("expected delta '!', got %q", chunks[2].Delta)
		}
		if chunks[2].Content != "Hello world!" {
			t.Errorf("expected accumulated 'Hello world!', got %q", chunks[2].Content)
		}

		// Final chunk with finish reason
		if chunks[3].FinishReason == nil || *chunks[3].FinishReason != "STOP" {
			t.Errorf("expected finish reason 'STOP', got %v", chunks[3].FinishReason)
		}
		if chunks[3].Content != "Hello world!" {
			t.Errorf("expected final content 'Hello world!', got %q", chunks[3].Content)
		}
		if chunks[3].CostInfo == nil {
			t.Error("expected cost info in final chunk")
		}
	})

	t.Run("handles single chunk response", func(t *testing.T) {
		responses := []geminiResponse{
			{
				Candidates: []geminiCandidate{
					{
						Content:      geminiContent{Parts: []geminiPart{{Text: "Done"}}},
						FinishReason: "STOP",
					},
				},
			},
		}

		body := marshalJSONArray(t, responses)
		outChan := make(chan providers.StreamChunk, 10)

		provider.streamResponse(context.Background(), io.NopCloser(body), outChan)

		chunks := collectChunks(t, outChan)

		// Text chunk + final chunk with finish reason
		if len(chunks) != 2 {
			t.Fatalf("expected 2 chunks, got %d", len(chunks))
		}

		if chunks[0].Delta != "Done" {
			t.Errorf("expected delta 'Done', got %q", chunks[0].Delta)
		}

		if chunks[1].FinishReason == nil || *chunks[1].FinishReason != "STOP" {
			t.Errorf("expected finish reason 'STOP'")
		}
	})

	t.Run("handles empty array", func(t *testing.T) {
		body := strings.NewReader("[]")
		outChan := make(chan providers.StreamChunk, 10)

		provider.streamResponse(context.Background(), io.NopCloser(body), outChan)

		chunks := collectChunks(t, outChan)

		if len(chunks) != 1 {
			t.Fatalf("expected 1 chunk (final), got %d", len(chunks))
		}

		if chunks[0].FinishReason == nil || *chunks[0].FinishReason != "stop" {
			t.Errorf("expected finish reason 'stop', got %v", chunks[0].FinishReason)
		}
	})

	t.Run("handles invalid JSON", func(t *testing.T) {
		body := strings.NewReader("not json at all")
		outChan := make(chan providers.StreamChunk, 10)

		provider.streamResponse(context.Background(), io.NopCloser(body), outChan)

		chunks := collectChunks(t, outChan)

		if len(chunks) != 1 {
			t.Fatalf("expected 1 error chunk, got %d", len(chunks))
		}

		if chunks[0].Error == nil {
			t.Fatal("expected error in chunk")
		}
		if chunks[0].FinishReason == nil || *chunks[0].FinishReason != "error" {
			t.Error("expected finish reason 'error'")
		}
	})

	t.Run("handles non-array JSON", func(t *testing.T) {
		body := strings.NewReader(`{"candidates": []}`)
		outChan := make(chan providers.StreamChunk, 10)

		provider.streamResponse(context.Background(), io.NopCloser(body), outChan)

		chunks := collectChunks(t, outChan)

		if len(chunks) != 1 {
			t.Fatalf("expected 1 error chunk, got %d", len(chunks))
		}

		if chunks[0].Error == nil {
			t.Fatal("expected error in chunk")
		}
	})

	t.Run("handles malformed element in array", func(t *testing.T) {
		body := strings.NewReader(`[{"candidates": []}, INVALID]`)
		outChan := make(chan providers.StreamChunk, 10)

		provider.streamResponse(context.Background(), io.NopCloser(body), outChan)

		chunks := collectChunks(t, outChan)

		// Should get an error chunk for the malformed element
		lastChunk := chunks[len(chunks)-1]
		if lastChunk.Error == nil {
			t.Fatal("expected error for malformed element")
		}
		if lastChunk.FinishReason == nil || *lastChunk.FinishReason != "error" {
			t.Error("expected finish reason 'error'")
		}
	})

	t.Run("handles context cancellation", func(t *testing.T) {
		responses := []geminiResponse{
			{
				Candidates: []geminiCandidate{
					{Content: geminiContent{Parts: []geminiPart{{Text: "Hello"}}}},
				},
			},
			{
				Candidates: []geminiCandidate{
					{Content: geminiContent{Parts: []geminiPart{{Text: " world"}}}},
				},
			},
		}

		body := marshalJSONArray(t, responses)
		outChan := make(chan providers.StreamChunk, 10)

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		provider.streamResponse(ctx, io.NopCloser(body), outChan)

		chunks := collectChunks(t, outChan)

		// Should get a canceled chunk
		lastChunk := chunks[len(chunks)-1]
		if lastChunk.FinishReason == nil || *lastChunk.FinishReason != "canceled" {
			t.Errorf("expected finish reason 'canceled', got %v", lastChunk.FinishReason)
		}
		if lastChunk.Error == nil {
			t.Error("expected context error in chunk")
		}
	})

	t.Run("handles response with no finish reason", func(t *testing.T) {
		responses := []geminiResponse{
			{
				Candidates: []geminiCandidate{
					{Content: geminiContent{Parts: []geminiPart{{Text: "partial"}}}},
				},
			},
		}

		body := marshalJSONArray(t, responses)
		outChan := make(chan providers.StreamChunk, 10)

		provider.streamResponse(context.Background(), io.NopCloser(body), outChan)

		chunks := collectChunks(t, outChan)

		if len(chunks) != 2 {
			t.Fatalf("expected 2 chunks, got %d", len(chunks))
		}

		// Last chunk should have "stop" as default finish reason
		lastChunk := chunks[len(chunks)-1]
		if lastChunk.FinishReason == nil || *lastChunk.FinishReason != "stop" {
			t.Errorf("expected finish reason 'stop', got %v", lastChunk.FinishReason)
		}
		if lastChunk.Content != "partial" {
			t.Errorf("expected content 'partial', got %q", lastChunk.Content)
		}
	})

	t.Run("handles tool calls", func(t *testing.T) {
		responses := []geminiResponse{
			{
				Candidates: []geminiCandidate{
					{
						Content: geminiContent{
							Parts: []geminiPart{
								{
									FunctionCall: &geminiPartFuncCall{
										Name: "get_weather",
										Args: json.RawMessage(`{"location":"NYC"}`),
									},
								},
							},
						},
						FinishReason: "STOP",
					},
				},
			},
		}

		body := marshalJSONArray(t, responses)
		outChan := make(chan providers.StreamChunk, 10)

		provider.streamResponse(context.Background(), io.NopCloser(body), outChan)

		chunks := collectChunks(t, outChan)

		// Final chunk should have tool calls
		lastChunk := chunks[len(chunks)-1]
		if len(lastChunk.ToolCalls) != 1 {
			t.Fatalf("expected 1 tool call, got %d", len(lastChunk.ToolCalls))
		}
		if lastChunk.ToolCalls[0].Name != "get_weather" {
			t.Errorf("expected tool call name 'get_weather', got %q", lastChunk.ToolCalls[0].Name)
		}
	})

	t.Run("handles empty candidates", func(t *testing.T) {
		responses := []geminiResponse{
			{Candidates: []geminiCandidate{}},
			{
				Candidates: []geminiCandidate{
					{
						Content:      geminiContent{Parts: []geminiPart{{Text: "data"}}},
						FinishReason: "STOP",
					},
				},
			},
		}

		body := marshalJSONArray(t, responses)
		outChan := make(chan providers.StreamChunk, 10)

		provider.streamResponse(context.Background(), io.NopCloser(body), outChan)

		chunks := collectChunks(t, outChan)

		// Should skip empty candidate and process the second one
		if len(chunks) != 2 {
			t.Fatalf("expected 2 chunks, got %d", len(chunks))
		}
		if chunks[0].Delta != "data" {
			t.Errorf("expected delta 'data', got %q", chunks[0].Delta)
		}
	})

	t.Run("handles read error from body", func(t *testing.T) {
		body := &errorReader{err: io.ErrUnexpectedEOF}
		outChan := make(chan providers.StreamChunk, 10)

		provider.streamResponse(context.Background(), io.NopCloser(body), outChan)

		chunks := collectChunks(t, outChan)

		if len(chunks) != 1 {
			t.Fatalf("expected 1 error chunk, got %d", len(chunks))
		}
		if chunks[0].Error == nil {
			t.Fatal("expected error in chunk")
		}
	})
}

func TestProcessGeminiStreamChunk_Incremental(t *testing.T) {
	provider := &Provider{}

	t.Run("empty candidates returns unchanged state", func(t *testing.T) {
		chunk := geminiResponse{Candidates: []geminiCandidate{}}
		outChan := make(chan providers.StreamChunk, 10)
		var sb strings.Builder
		sb.WriteString("prev")

		tokens, toolCalls, finished := provider.processGeminiStreamChunk(
			chunk, &sb, 5, nil, outChan)

		if sb.String() != "prev" || tokens != 5 || finished {
			t.Errorf("expected unchanged state, got acc=%q tokens=%d finished=%v", sb.String(), tokens, finished)
		}
		if len(toolCalls) != 0 {
			t.Errorf("expected no tool calls, got %d", len(toolCalls))
		}
	})

	t.Run("empty parts returns unchanged state", func(t *testing.T) {
		chunk := geminiResponse{
			Candidates: []geminiCandidate{
				{Content: geminiContent{Parts: []geminiPart{}}},
			},
		}
		outChan := make(chan providers.StreamChunk, 10)
		var sb strings.Builder
		sb.WriteString("prev")

		tokens, _, finished := provider.processGeminiStreamChunk(
			chunk, &sb, 5, nil, outChan)

		if sb.String() != "prev" || tokens != 5 || finished {
			t.Errorf("expected unchanged state, got acc=%q tokens=%d finished=%v", sb.String(), tokens, finished)
		}
	})

	t.Run("text part emits chunk and accumulates", func(t *testing.T) {
		chunk := geminiResponse{
			Candidates: []geminiCandidate{
				{Content: geminiContent{Parts: []geminiPart{{Text: "delta"}}}},
			},
		}
		outChan := make(chan providers.StreamChunk, 10)
		var sb strings.Builder
		sb.WriteString("prev")

		tokens, _, finished := provider.processGeminiStreamChunk(
			chunk, &sb, 5, nil, outChan)

		if sb.String() != "prevdelta" {
			t.Errorf("expected accumulated 'prevdelta', got %q", sb.String())
		}
		if tokens != 6 {
			t.Errorf("expected 6 tokens, got %d", tokens)
		}
		if finished {
			t.Error("expected not finished")
		}

		select {
		case sc := <-outChan:
			if sc.Delta != "delta" {
				t.Errorf("expected delta 'delta', got %q", sc.Delta)
			}
		default:
			t.Fatal("expected chunk on channel")
		}
	})

	t.Run("function call accumulates tool calls", func(t *testing.T) {
		chunk := geminiResponse{
			Candidates: []geminiCandidate{
				{
					Content: geminiContent{
						Parts: []geminiPart{
							{
								FunctionCall: &geminiPartFuncCall{
									Name: "myTool",
									Args: json.RawMessage(`{"key":"val"}`),
								},
							},
						},
					},
					FinishReason: "STOP",
				},
			},
		}
		outChan := make(chan providers.StreamChunk, 10)
		var sb strings.Builder

		_, toolCalls, finished := provider.processGeminiStreamChunk(
			chunk, &sb, 0, nil, outChan)

		if !finished {
			t.Error("expected finished")
		}
		if len(toolCalls) != 1 {
			t.Fatalf("expected 1 tool call, got %d", len(toolCalls))
		}
		if toolCalls[0].Name != "myTool" {
			t.Errorf("expected tool name 'myTool', got %q", toolCalls[0].Name)
		}
	})
}

// --- Helpers ---

// marshalJSONArray marshals a slice to a JSON array and returns it as a Reader.
func marshalJSONArray(t *testing.T, v interface{}) *strings.Reader {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("failed to marshal test data: %v", err)
	}
	return strings.NewReader(string(data))
}

// collectChunks drains a closed channel into a slice.
func collectChunks(t *testing.T, ch <-chan providers.StreamChunk) []providers.StreamChunk {
	t.Helper()
	var chunks []providers.StreamChunk
	for c := range ch {
		chunks = append(chunks, c)
	}
	return chunks
}

// errorReader is an io.Reader that always returns an error.
type errorReader struct {
	err error
}

func (e *errorReader) Read([]byte) (int, error) {
	return 0, e.err
}

// Ensure types.MessageToolCall is used (avoid unused import if tests are adjusted).
var _ = types.MessageToolCall{}
