package statestore

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// jsonDeepCopyState is the old implementation kept for benchmarking comparison.
func jsonDeepCopyState(state *ConversationState) *ConversationState {
	if state == nil {
		return nil
	}
	data, err := json.Marshal(state)
	if err != nil {
		return nil
	}
	var stateCopy ConversationState
	if err := json.Unmarshal(data, &stateCopy); err != nil {
		return nil
	}
	return &stateCopy
}

// jsonDeepCopyMessages is the old implementation for message slice cloning.
func jsonDeepCopyMessages(msgs []types.Message) []types.Message {
	if len(msgs) == 0 {
		return nil
	}
	data, err := json.Marshal(msgs)
	if err != nil {
		return nil
	}
	var result []types.Message
	if err := json.Unmarshal(data, &result); err != nil {
		return nil
	}
	return result
}

// buildSmallState creates a small ConversationState for benchmarking.
func buildSmallState() *ConversationState {
	return &ConversationState{
		ID:           "conv-bench-small",
		UserID:       "user-alice",
		SystemPrompt: "You are a helpful assistant.",
		Messages: []types.Message{
			{Role: "user", Content: "Hello", Timestamp: time.Now()},
			{Role: "assistant", Content: "Hi there! How can I help?", Timestamp: time.Now()},
		},
		TokenCount:     50,
		LastAccessedAt: time.Now(),
		Metadata:       map[string]interface{}{"key": "value"},
	}
}

// buildMediumState creates a medium ConversationState with 20 messages and metadata.
func buildMediumState() *ConversationState {
	msgs := make([]types.Message, 20)
	for i := range msgs {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		msgs[i] = types.Message{
			Role:      role,
			Content:   "This is a message with some content to make it realistic in size.",
			Timestamp: time.Now(),
			Meta: map[string]interface{}{
				"turn":   i,
				"source": "benchmark",
			},
		}
	}
	msgs[1].CostInfo = &types.CostInfo{
		InputTokens:   100,
		OutputTokens:  50,
		InputCostUSD:  0.001,
		OutputCostUSD: 0.002,
		TotalCost:     0.003,
	}
	msgs[1].Validations = []types.ValidationResult{
		{
			ValidatorType: "content_filter",
			Passed:        true,
			Details:       map[string]interface{}{"checked": true},
			Timestamp:     time.Now(),
		},
	}
	return &ConversationState{
		ID:           "conv-bench-medium",
		UserID:       "user-alice",
		SystemPrompt: "You are a helpful AI assistant. Answer questions accurately and concisely.",
		Messages:     msgs,
		Summaries: []Summary{
			{StartTurn: 0, EndTurn: 5, Content: "User asked about Go programming.", TokenCount: 20, CreatedAt: time.Now()},
		},
		TokenCount:     2000,
		LastAccessedAt: time.Now(),
		Metadata: map[string]interface{}{
			"topic":   "programming",
			"nested":  map[string]interface{}{"level2": map[string]interface{}{"level3": "deep"}},
			"tags":    []interface{}{"go", "programming", "ai"},
			"session": "abc-123",
		},
	}
}

// buildLargeState creates a large ConversationState with 100 messages, tool calls, and media.
func buildLargeState() *ConversationState {
	msgs := make([]types.Message, 100)
	for i := range msgs {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		msgs[i] = types.Message{
			Role:      role,
			Content:   "This is a longer message with more content to simulate real-world conversations.",
			Timestamp: time.Now(),
			Meta: map[string]interface{}{
				"turn":       i,
				"source":     "benchmark",
				"assertions": map[string]interface{}{"passed": true, "details": map[string]interface{}{"score": 0.95}},
			},
		}
		if i%5 == 0 && role == "assistant" {
			msgs[i].ToolCalls = []types.MessageToolCall{
				{ID: "call-1", Name: "search", Args: json.RawMessage(`{"query":"test search"}`)},
			}
		}
		if i%7 == 0 {
			msgs[i].CostInfo = &types.CostInfo{
				InputTokens:   200,
				OutputTokens:  100,
				InputCostUSD:  0.002,
				OutputCostUSD: 0.004,
				TotalCost:     0.006,
			}
		}
	}
	return &ConversationState{
		ID:           "conv-bench-large",
		UserID:       "user-alice",
		SystemPrompt: "You are a helpful AI assistant with access to tools. Use them wisely.",
		Messages:     msgs,
		Summaries: []Summary{
			{StartTurn: 0, EndTurn: 20, Content: "First batch summary.", TokenCount: 50, CreatedAt: time.Now()},
			{StartTurn: 21, EndTurn: 40, Content: "Second batch summary.", TokenCount: 55, CreatedAt: time.Now()},
			{StartTurn: 41, EndTurn: 60, Content: "Third batch summary.", TokenCount: 60, CreatedAt: time.Now()},
		},
		TokenCount:     10000,
		LastAccessedAt: time.Now(),
		Metadata: map[string]interface{}{
			"topic":   "complex workflow",
			"context": map[string]interface{}{"entities": []interface{}{"user", "system", "tool"}},
			"history": map[string]interface{}{"summaries": 3, "total_turns": 100},
		},
	}
}

func BenchmarkDeepCopyState_Small_JSON(b *testing.B) {
	state := buildSmallState()
	b.ResetTimer()
	for b.Loop() {
		jsonDeepCopyState(state)
	}
}

func BenchmarkDeepCopyState_Small_Structural(b *testing.B) {
	state := buildSmallState()
	b.ResetTimer()
	for b.Loop() {
		deepCopyState(state)
	}
}

func BenchmarkDeepCopyState_Medium_JSON(b *testing.B) {
	state := buildMediumState()
	b.ResetTimer()
	for b.Loop() {
		jsonDeepCopyState(state)
	}
}

func BenchmarkDeepCopyState_Medium_Structural(b *testing.B) {
	state := buildMediumState()
	b.ResetTimer()
	for b.Loop() {
		deepCopyState(state)
	}
}

func BenchmarkDeepCopyState_Large_JSON(b *testing.B) {
	state := buildLargeState()
	b.ResetTimer()
	for b.Loop() {
		jsonDeepCopyState(state)
	}
}

func BenchmarkDeepCopyState_Large_Structural(b *testing.B) {
	state := buildLargeState()
	b.ResetTimer()
	for b.Loop() {
		deepCopyState(state)
	}
}

func BenchmarkDeepCopyMessages_20_JSON(b *testing.B) {
	state := buildMediumState()
	b.ResetTimer()
	for b.Loop() {
		jsonDeepCopyMessages(state.Messages)
	}
}

func BenchmarkDeepCopyMessages_20_Structural(b *testing.B) {
	state := buildMediumState()
	b.ResetTimer()
	for b.Loop() {
		cloneMessages(state.Messages)
	}
}

func BenchmarkDeepCopyMessages_100_JSON(b *testing.B) {
	state := buildLargeState()
	b.ResetTimer()
	for b.Loop() {
		jsonDeepCopyMessages(state.Messages)
	}
}

func BenchmarkDeepCopyMessages_100_Structural(b *testing.B) {
	state := buildLargeState()
	b.ResetTimer()
	for b.Loop() {
		cloneMessages(state.Messages)
	}
}

// buildMultimodalState creates a state with multimodal messages containing media content.
// This simulates conversations with embedded base64 images where deep copy cost is highest.
func buildMultimodalState() *ConversationState {
	strPtr := func(s string) *string { return &s }
	intPtr := func(i int) *int { return &i }
	int64Ptr := func(i int64) *int64 { return &i }

	// Simulate a 100KB base64 image payload
	largeBase64 := make([]byte, 100*1024)
	for i := range largeBase64 {
		largeBase64[i] = 'A'
	}
	imageData := string(largeBase64)

	msgs := make([]types.Message, 10)
	for i := range msgs {
		if i%2 == 0 {
			// User message with multimodal content
			msgs[i] = types.Message{
				Role:      "user",
				Timestamp: time.Now(),
				Parts: []types.ContentPart{
					{Type: "text", Text: strPtr("Please analyze this image")},
					{
						Type: "image",
						Media: &types.MediaContent{
							Data:     strPtr(imageData),
							MIMEType: "image/jpeg",
							Detail:   strPtr("high"),
							SizeKB:   int64Ptr(100),
							Width:    intPtr(1920),
							Height:   intPtr(1080),
							Caption:  strPtr("An example image for analysis"),
						},
					},
				},
			}
		} else {
			// Assistant response
			msgs[i] = types.Message{
				Role:      "assistant",
				Content:   "I can see the image. It shows a landscape with mountains.",
				Timestamp: time.Now(),
				CostInfo: &types.CostInfo{
					InputTokens:   500,
					OutputTokens:  100,
					InputCostUSD:  0.005,
					OutputCostUSD: 0.001,
					TotalCost:     0.006,
				},
			}
		}
	}
	return &ConversationState{
		ID:             "conv-bench-multimodal",
		UserID:         "user-alice",
		SystemPrompt:   "You are a multimodal assistant that can analyze images.",
		Messages:       msgs,
		TokenCount:     5000,
		LastAccessedAt: time.Now(),
		Metadata:       map[string]interface{}{"mode": "multimodal"},
	}
}

func BenchmarkDeepCopyState_Multimodal_Structural(b *testing.B) {
	state := buildMultimodalState()
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		deepCopyState(state)
	}
}

func BenchmarkCloneMediaContent(b *testing.B) {
	strPtr := func(s string) *string { return &s }
	intPtr := func(i int) *int { return &i }
	int64Ptr := func(i int64) *int64 { return &i }

	mc := &types.MediaContent{
		Data:     strPtr("base64encodedimagedata"),
		MIMEType: "image/jpeg",
		Detail:   strPtr("high"),
		SizeKB:   int64Ptr(512),
		Width:    intPtr(1920),
		Height:   intPtr(1080),
		Caption:  strPtr("A scenic photo"),
		Format:   strPtr("jpeg"),
	}

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		cloneMediaContent(mc)
	}
}

func BenchmarkCloneContentPart_TextOnly(b *testing.B) {
	strPtr := func(s string) *string { return &s }
	cp := &types.ContentPart{
		Type: "text",
		Text: strPtr("This is a text content part with some content."),
	}

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		cloneContentPart(cp)
	}
}

func BenchmarkCloneContentPart_WithMedia(b *testing.B) {
	strPtr := func(s string) *string { return &s }
	intPtr := func(i int) *int { return &i }
	int64Ptr := func(i int64) *int64 { return &i }

	cp := &types.ContentPart{
		Type: "image",
		Media: &types.MediaContent{
			Data:     strPtr("base64encodedimagedata"),
			MIMEType: "image/jpeg",
			Detail:   strPtr("high"),
			SizeKB:   int64Ptr(512),
			Width:    intPtr(1920),
			Height:   intPtr(1080),
		},
	}

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		cloneContentPart(cp)
	}
}

func BenchmarkCloneMessage_Simple(b *testing.B) {
	msg := &types.Message{
		Role:      "user",
		Content:   "Hello, how are you?",
		Timestamp: time.Now(),
	}

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		cloneMessage(msg)
	}
}

func BenchmarkCloneMessage_WithMeta(b *testing.B) {
	msg := &types.Message{
		Role:      "assistant",
		Content:   "I can help with that!",
		Timestamp: time.Now(),
		Meta: map[string]interface{}{
			"assertions": map[string]interface{}{"passed": true},
			"score":      0.95,
		},
		CostInfo: &types.CostInfo{
			InputTokens:   100,
			OutputTokens:  50,
			TotalCost:     0.003,
		},
	}

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		cloneMessage(msg)
	}
}
