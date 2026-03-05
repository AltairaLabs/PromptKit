package statestore

import (
	"context"
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

// =============================================================================
// LRU eviction benchmarks — heap-based vs linear scan
// =============================================================================

// linearEvictLRU is the old O(N) implementation kept for benchmarking comparison.
func linearEvictLRU(states map[string]*ConversationState) string {
	var oldestID string
	var oldestTime time.Time
	first := true

	for id, state := range states {
		if first || state.LastAccessedAt.Before(oldestTime) {
			oldestID = id
			oldestTime = state.LastAccessedAt
			first = false
		}
	}
	return oldestID
}

// setupStoreForEviction creates a MemoryStore with n entries for eviction benchmarking.
func setupStoreForEviction(n int) *MemoryStore {
	store := NewMemoryStore(WithNoTTL(), WithMemoryMaxEntries(n+1))
	ctx := context.Background()
	base := time.Now()
	for i := 0; i < n; i++ {
		state := &ConversationState{
			ID:             "conv-" + string(rune(i)),
			LastAccessedAt: base.Add(time.Duration(i) * time.Millisecond),
		}
		_ = store.Save(ctx, state)
	}
	return store
}

func BenchmarkEvictLRU_Heap_100(b *testing.B) {
	benchmarkHeapEvict(b, 100)
}

func BenchmarkEvictLRU_Heap_1000(b *testing.B) {
	benchmarkHeapEvict(b, 1000)
}

func BenchmarkEvictLRU_Heap_10000(b *testing.B) {
	benchmarkHeapEvict(b, 10000)
}

func BenchmarkEvictLRU_Linear_100(b *testing.B) {
	benchmarkLinearEvict(b, 100)
}

func BenchmarkEvictLRU_Linear_1000(b *testing.B) {
	benchmarkLinearEvict(b, 1000)
}

func BenchmarkEvictLRU_Linear_10000(b *testing.B) {
	benchmarkLinearEvict(b, 10000)
}

func benchmarkHeapEvict(b *testing.B, n int) {
	b.Helper()
	store := setupStoreForEviction(n)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		// Pop from heap (O(log N))
		store.mu.Lock()
		store.evictLRULocked()
		store.mu.Unlock()

		// Re-add an entry to keep the store at size n
		store.mu.Lock()
		now := time.Now()
		id := "refill"
		state := &ConversationState{ID: id, LastAccessedAt: now}
		store.states[id] = state
		store.touchLRULocked(id, now)
		store.mu.Unlock()
	}
}

func benchmarkLinearEvict(b *testing.B, n int) {
	b.Helper()
	store := setupStoreForEviction(n)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		// Linear scan (O(N))
		store.mu.Lock()
		oldestID := linearEvictLRU(store.states)
		if state, ok := store.states[oldestID]; ok {
			store.deleteStateLocked(oldestID, state)
		}
		store.mu.Unlock()

		// Re-add an entry to keep the store at size n
		store.mu.Lock()
		now := time.Now()
		id := "refill"
		state := &ConversationState{ID: id, LastAccessedAt: now}
		store.states[id] = state
		store.touchLRULocked(id, now)
		store.mu.Unlock()
	}
}

// =============================================================================
// Background expiration benchmarks — two-phase RLock vs single write lock
// =============================================================================

// setupStoreForExpiration creates a MemoryStore with n entries, half of which are expired.
func setupStoreForExpiration(n int) *MemoryStore {
	store := NewMemoryStore(WithMemoryTTL(50*time.Millisecond), WithNoMaxEntries())
	now := time.Now()
	store.mu.Lock()
	for i := 0; i < n; i++ {
		id := "conv-" + string(rune(i))
		accessTime := now
		if i%2 == 0 {
			// Make half of the entries expired
			accessTime = now.Add(-1 * time.Second)
		}
		state := &ConversationState{
			ID:             id,
			LastAccessedAt: accessTime,
		}
		store.states[id] = state
		store.touchLRULocked(id, accessTime)
	}
	store.mu.Unlock()
	return store
}

func BenchmarkExpiration_TwoPhase_1000(b *testing.B) {
	benchmarkTwoPhaseExpire(b, 1000)
}

func BenchmarkExpiration_TwoPhase_10000(b *testing.B) {
	benchmarkTwoPhaseExpire(b, 10000)
}

func BenchmarkExpiration_WriteLock_1000(b *testing.B) {
	benchmarkWriteLockExpire(b, 1000)
}

func BenchmarkExpiration_WriteLock_10000(b *testing.B) {
	benchmarkWriteLockExpire(b, 10000)
}

func benchmarkTwoPhaseExpire(b *testing.B, n int) {
	b.Helper()
	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		store := setupStoreForExpiration(n)
		b.StartTimer()

		expired := store.collectExpiredKeys()
		store.deleteExpiredKeys(expired)
	}
}

// evictExpiredWriteLocked is the old single-lock implementation kept for benchmarking comparison.
func evictExpiredWriteLocked(s *MemoryStore) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, state := range s.states {
		if s.isExpired(state) {
			s.deleteStateLocked(id, state)
		}
	}
}

func benchmarkWriteLockExpire(b *testing.B, n int) {
	b.Helper()
	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		store := setupStoreForExpiration(n)
		b.StartTimer()

		evictExpiredWriteLocked(store)
	}
}
