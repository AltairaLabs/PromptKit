package tokenizer

import (
	"sync"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHeuristicTokenCounter_CountTokens(t *testing.T) {
	tests := []struct {
		name    string
		family  ModelFamily
		text    string
		wantMin int // Minimum expected tokens
		wantMax int // Maximum expected tokens
	}{
		{
			name:    "empty text",
			family:  ModelFamilyDefault,
			text:    "",
			wantMin: 0,
			wantMax: 0,
		},
		{
			name:    "single word GPT",
			family:  ModelFamilyGPT,
			text:    "hello",
			wantMin: 1,
			wantMax: 2,
		},
		{
			name:    "sentence GPT",
			family:  ModelFamilyGPT,
			text:    "The quick brown fox jumps over the lazy dog",
			wantMin: 10, // 9 words * 1.3 = 11.7 -> 11
			wantMax: 13,
		},
		{
			name:    "sentence Gemini",
			family:  ModelFamilyGemini,
			text:    "The quick brown fox jumps over the lazy dog",
			wantMin: 11, // 9 words * 1.4 = 12.6 -> 12
			wantMax: 14,
		},
		{
			name:    "whitespace only",
			family:  ModelFamilyDefault,
			text:    "   \t\n  ",
			wantMin: 0,
			wantMax: 0,
		},
		{
			name:    "multiple spaces between words",
			family:  ModelFamilyDefault,
			text:    "hello    world",
			wantMin: 2, // 2 words
			wantMax: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			counter := NewHeuristicTokenCounter(tt.family)
			got := counter.CountTokens(tt.text)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("CountTokens() = %d, want between %d and %d", got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestHeuristicTokenCounter_CountMultiple(t *testing.T) {
	counter := NewHeuristicTokenCounter(ModelFamilyGPT)

	texts := []string{
		"Hello world",
		"This is a test",
		"",
		"One more sentence here",
	}

	got := counter.CountMultiple(texts)

	// 2 + 4 + 0 + 4 = 10 words, * 1.3 = 13 tokens
	if got < 10 || got > 15 {
		t.Errorf("CountMultiple() = %d, want between 10 and 15", got)
	}

	// Verify it equals sum of individual counts
	sum := 0
	for _, text := range texts {
		sum += counter.CountTokens(text)
	}
	if got != sum {
		t.Errorf("CountMultiple() = %d, but sum of individual counts = %d", got, sum)
	}
}

func TestNewHeuristicTokenCounterWithRatio(t *testing.T) {
	tests := []struct {
		name      string
		ratio     float64
		wantRatio float64
	}{
		{
			name:      "valid ratio",
			ratio:     1.5,
			wantRatio: 1.5,
		},
		{
			name:      "zero ratio defaults",
			ratio:     0,
			wantRatio: tokenRatios[ModelFamilyDefault],
		},
		{
			name:      "negative ratio defaults",
			ratio:     -1.0,
			wantRatio: tokenRatios[ModelFamilyDefault],
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			counter := NewHeuristicTokenCounterWithRatio(tt.ratio)
			if counter.Ratio() != tt.wantRatio {
				t.Errorf("Ratio() = %v, want %v", counter.Ratio(), tt.wantRatio)
			}
		})
	}
}

func TestHeuristicTokenCounter_SetRatio(t *testing.T) {
	counter := NewHeuristicTokenCounter(ModelFamilyDefault)
	originalRatio := counter.Ratio()

	// Set new ratio
	counter.SetRatio(2.0)
	if counter.Ratio() != 2.0 {
		t.Errorf("Ratio() after SetRatio(2.0) = %v, want 2.0", counter.Ratio())
	}

	// Setting invalid ratio should be ignored
	counter.SetRatio(-1.0)
	if counter.Ratio() != 2.0 {
		t.Errorf("Ratio() after SetRatio(-1.0) = %v, want 2.0 (unchanged)", counter.Ratio())
	}

	counter.SetRatio(0)
	if counter.Ratio() != 2.0 {
		t.Errorf("Ratio() after SetRatio(0) = %v, want 2.0 (unchanged)", counter.Ratio())
	}

	// Restore and verify
	counter.SetRatio(originalRatio)
	if counter.Ratio() != originalRatio {
		t.Errorf("Ratio() after restore = %v, want %v", counter.Ratio(), originalRatio)
	}
}

func TestGetModelFamily(t *testing.T) {
	tests := []struct {
		modelName string
		want      ModelFamily
	}{
		{"gpt-4", ModelFamilyGPT},
		{"gpt-3.5-turbo", ModelFamilyGPT},
		{"GPT-4-turbo", ModelFamilyGPT},
		{"text-davinci-003", ModelFamilyGPT},
		{"text-embedding-ada-002", ModelFamilyGPT},
		{"claude-3-opus", ModelFamilyClaude},
		{"claude-3-5-sonnet-20241022", ModelFamilyClaude},
		{"CLAUDE-3-haiku", ModelFamilyClaude},
		{"gemini-pro", ModelFamilyGemini},
		{"gemini-1.5-flash", ModelFamilyGemini},
		{"models/gemini-pro", ModelFamilyGemini},
		{"llama-2-70b", ModelFamilyLlama},
		{"meta-llama/Llama-2-7b", ModelFamilyLlama},
		{"unknown-model", ModelFamilyDefault},
		{"", ModelFamilyDefault},
		{"mistral-7b", ModelFamilyDefault},
	}

	for _, tt := range tests {
		t.Run(tt.modelName, func(t *testing.T) {
			got := GetModelFamily(tt.modelName)
			if got != tt.want {
				t.Errorf("GetModelFamily(%q) = %v, want %v", tt.modelName, got, tt.want)
			}
		})
	}
}

func TestNewTokenCounterForModel(t *testing.T) {
	tests := []struct {
		modelName    string
		expectedType ModelFamily
	}{
		{"gpt-4", ModelFamilyGPT},
		{"claude-3-opus", ModelFamilyClaude},
		{"gemini-pro", ModelFamilyGemini},
		{"unknown", ModelFamilyDefault},
	}

	for _, tt := range tests {
		t.Run(tt.modelName, func(t *testing.T) {
			counter := NewTokenCounterForModel(tt.modelName)
			if counter == nil {
				t.Fatal("NewTokenCounterForModel returned nil")
			}

			// Verify it's a HeuristicTokenCounter with the expected ratio
			heuristic, ok := counter.(*HeuristicTokenCounter)
			if !ok {
				t.Fatal("NewTokenCounterForModel did not return HeuristicTokenCounter")
			}

			expectedRatio := tokenRatios[tt.expectedType]
			if heuristic.Ratio() != expectedRatio {
				t.Errorf("Counter ratio = %v, want %v for model %s",
					heuristic.Ratio(), expectedRatio, tt.modelName)
			}
		})
	}
}

func TestDefaultTokenCounter(t *testing.T) {
	// Verify DefaultTokenCounter is initialized
	if DefaultTokenCounter == nil {
		t.Fatal("DefaultTokenCounter is nil")
	}

	// Verify convenience function works
	got := CountTokens("hello world")
	if got < 2 || got > 4 {
		t.Errorf("CountTokens(\"hello world\") = %d, want between 2 and 4", got)
	}
}

func TestHeuristicTokenCounter_ThreadSafety(t *testing.T) {
	counter := NewHeuristicTokenCounter(ModelFamilyDefault)
	var wg sync.WaitGroup

	// Concurrent reads
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			counter.CountTokens("test text for counting")
			counter.Ratio()
		}()
	}

	// Concurrent writes
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(ratio float64) {
			defer wg.Done()
			counter.SetRatio(ratio)
		}(float64(i) + 1.0)
	}

	wg.Wait()
	// If we get here without a race condition, the test passes
}

func TestTokenRatios(t *testing.T) {
	// Verify all model families have defined ratios
	families := []ModelFamily{
		ModelFamilyGPT,
		ModelFamilyClaude,
		ModelFamilyGemini,
		ModelFamilyLlama,
		ModelFamilyDefault,
	}

	for _, family := range families {
		ratio, ok := tokenRatios[family]
		if !ok {
			t.Errorf("ModelFamily %s has no defined ratio", family)
		}
		if ratio <= 0 {
			t.Errorf("ModelFamily %s has invalid ratio: %v", family, ratio)
		}
	}
}

func BenchmarkHeuristicTokenCounter_CountTokens(b *testing.B) {
	counter := NewHeuristicTokenCounter(ModelFamilyGPT)
	text := "The quick brown fox jumps over the lazy dog. " +
		"This is a longer text to benchmark token counting performance. " +
		"It includes multiple sentences and various words."

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		counter.CountTokens(text)
	}
}

func BenchmarkHeuristicTokenCounter_CountMultiple(b *testing.B) {
	counter := NewHeuristicTokenCounter(ModelFamilyGPT)
	texts := []string{
		"First message with some content",
		"Second message that is a bit longer than the first",
		"Third message",
		"Fourth message with additional context and details",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		counter.CountMultiple(texts)
	}
}

// =============================================================================
// Content-aware token counting tests
// =============================================================================

func TestDetectContentType_RegularText(t *testing.T) {
	multiplier := DetectContentType("The quick brown fox jumps over the lazy dog")
	assert.Equal(t, 1.0, multiplier)
}

func TestDetectContentType_Code(t *testing.T) {
	code := `func main() { fmt.Println("hello") } if (x > 0) { return x; }`
	multiplier := DetectContentType(code)
	assert.Equal(t, codeRatioMultiplier, multiplier)
}

func TestDetectContentType_CJK(t *testing.T) {
	cjk := "这是一个测试文本用于验证字符检测功能的准确性"
	multiplier := DetectContentType(cjk)
	assert.Equal(t, cjkRatioMultiplier, multiplier)
}

func TestDetectContentType_Empty(t *testing.T) {
	assert.Equal(t, 1.0, DetectContentType(""))
}

func TestDetectContentType_CodeTakesPrecedence(t *testing.T) {
	// Mix code chars into text — code detection comes first
	mixed := "函数() { return 值; } if (条件) { break; }"
	multiplier := DetectContentType(mixed)
	assert.Equal(t, codeRatioMultiplier, multiplier)
}

func TestCountTokensContentAware_Empty(t *testing.T) {
	counter := NewHeuristicTokenCounter(ModelFamilyDefault)
	assert.Equal(t, 0, counter.CountTokensContentAware(""))
}

func TestCountTokensContentAware_Code(t *testing.T) {
	counter := NewHeuristicTokenCounterWithRatio(1.0)
	code := `if (x > 0) { return x; } else { return -x; }`
	tokens := counter.CountTokensContentAware(code)
	plainTokens := counter.CountTokens(code)
	assert.Greater(t, tokens, plainTokens)
}

func TestCountTokensContentAware_RegularText(t *testing.T) {
	counter := NewHeuristicTokenCounterWithRatio(1.0)
	text := "This is a regular sentence with no special characters"
	tokens := counter.CountTokensContentAware(text)
	plainTokens := counter.CountTokens(text)
	// For regular text, content-aware should equal plain
	assert.Equal(t, plainTokens, tokens)
}

// =============================================================================
// CountMessageTokens tests
// =============================================================================

func TestCountMessageTokens_TextOnly(t *testing.T) {
	counter := NewHeuristicTokenCounter(ModelFamilyDefault)
	messages := []types.Message{
		{Role: "user", Content: "Hello, how are you?"},
		{Role: "assistant", Content: "I am fine, thank you!"},
	}
	tokens := counter.CountMessageTokens(messages)
	assert.Greater(t, tokens, 0)
	assert.GreaterOrEqual(t, tokens, perMessageOverhead*2)
}

func TestCountMessageTokens_Empty(t *testing.T) {
	counter := NewHeuristicTokenCounter(ModelFamilyDefault)
	tokens := counter.CountMessageTokens(nil)
	assert.Equal(t, 0, tokens)
}

func TestCountMessageTokens_EmptySlice(t *testing.T) {
	counter := NewHeuristicTokenCounter(ModelFamilyDefault)
	tokens := counter.CountMessageTokens([]types.Message{})
	assert.Equal(t, 0, tokens)
}

func TestCountMessageTokens_Multimodal_TextParts(t *testing.T) {
	counter := NewHeuristicTokenCounter(ModelFamilyDefault)
	text1 := "Hello world"
	text2 := "Goodbye world"
	messages := []types.Message{
		{
			Role: "user",
			Parts: []types.ContentPart{
				{Type: types.ContentTypeText, Text: &text1},
				{Type: types.ContentTypeText, Text: &text2},
			},
		},
	}
	tokens := counter.CountMessageTokens(messages)
	assert.Greater(t, tokens, perMessageOverhead)
}

func TestCountMessageTokens_ImageLowDetail(t *testing.T) {
	counter := NewHeuristicTokenCounter(ModelFamilyDefault)
	low := "low"
	messages := []types.Message{
		{
			Role: "user",
			Parts: []types.ContentPart{
				{
					Type:  types.ContentTypeImage,
					Media: &types.MediaContent{MIMEType: "image/jpeg", Detail: &low},
				},
			},
		},
	}
	tokens := counter.CountMessageTokens(messages)
	assert.Equal(t, perMessageOverhead+imageTokensLowDetail, tokens)
}

func TestCountMessageTokens_ImageHighDetail(t *testing.T) {
	counter := NewHeuristicTokenCounter(ModelFamilyDefault)
	high := "high"
	messages := []types.Message{
		{
			Role: "user",
			Parts: []types.ContentPart{
				{
					Type:  types.ContentTypeImage,
					Media: &types.MediaContent{MIMEType: "image/png", Detail: &high},
				},
			},
		},
	}
	tokens := counter.CountMessageTokens(messages)
	assert.Equal(t, perMessageOverhead+imageTokensHighDetail, tokens)
}

func TestCountMessageTokens_ImageAutoDetail(t *testing.T) {
	counter := NewHeuristicTokenCounter(ModelFamilyDefault)
	messages := []types.Message{
		{
			Role: "user",
			Parts: []types.ContentPart{
				{Type: types.ContentTypeImage, Media: &types.MediaContent{MIMEType: "image/jpeg"}},
			},
		},
	}
	tokens := counter.CountMessageTokens(messages)
	assert.Equal(t, perMessageOverhead+imageTokensAutoDetail, tokens)
}

func TestCountMessageTokens_ImageNilMedia(t *testing.T) {
	counter := NewHeuristicTokenCounter(ModelFamilyDefault)
	messages := []types.Message{
		{
			Role:  "user",
			Parts: []types.ContentPart{{Type: types.ContentTypeImage}},
		},
	}
	tokens := counter.CountMessageTokens(messages)
	assert.Equal(t, perMessageOverhead+imageTokensAutoDetail, tokens)
}

func TestCountMessageTokens_AudioWithCaption(t *testing.T) {
	counter := NewHeuristicTokenCounter(ModelFamilyDefault)
	caption := "A bird singing in the morning"
	messages := []types.Message{
		{
			Role: "user",
			Parts: []types.ContentPart{
				{
					Type:  types.ContentTypeAudio,
					Media: &types.MediaContent{MIMEType: "audio/mp3", Caption: &caption},
				},
			},
		},
	}
	tokens := counter.CountMessageTokens(messages)
	assert.Greater(t, tokens, perMessageOverhead)
}

func TestCountMessageTokens_AudioWithoutCaption(t *testing.T) {
	counter := NewHeuristicTokenCounter(ModelFamilyDefault)
	messages := []types.Message{
		{
			Role: "user",
			Parts: []types.ContentPart{
				{Type: types.ContentTypeAudio, Media: &types.MediaContent{MIMEType: "audio/mp3"}},
			},
		},
	}
	tokens := counter.CountMessageTokens(messages)
	assert.Equal(t, perMessageOverhead, tokens)
}

func TestCountMessageTokens_ToolCalls(t *testing.T) {
	counter := NewHeuristicTokenCounter(ModelFamilyDefault)
	messages := []types.Message{
		{
			Role:    "assistant",
			Content: "Let me check that for you.",
			ToolCalls: []types.MessageToolCall{
				{
					ID:   "call_1",
					Name: "get_weather",
					Args: []byte(`{"city": "San Francisco", "units": "celsius"}`),
				},
			},
		},
	}
	tokens := counter.CountMessageTokens(messages)
	assert.Greater(t, tokens, perMessageOverhead)
}

func TestCountMessageTokens_ToolResult(t *testing.T) {
	counter := NewHeuristicTokenCounter(ModelFamilyDefault)
	messages := []types.Message{
		{
			Role: "tool",
			ToolResult: &types.MessageToolResult{
				ID:      "call_1",
				Name:    "get_weather",
				Content: "The temperature in San Francisco is 18C",
			},
		},
	}
	tokens := counter.CountMessageTokens(messages)
	assert.Greater(t, tokens, perMessageOverhead)
}

func TestCountMessageTokens_TextPartNilText(t *testing.T) {
	counter := NewHeuristicTokenCounter(ModelFamilyDefault)
	messages := []types.Message{
		{
			Role:  "user",
			Parts: []types.ContentPart{{Type: types.ContentTypeText, Text: nil}},
		},
	}
	tokens := counter.CountMessageTokens(messages)
	assert.Equal(t, perMessageOverhead, tokens)
}

func TestCountMessageTokensDefault(t *testing.T) {
	messages := []types.Message{
		{Role: "user", Content: "Hello"},
	}
	tokens := CountMessageTokensDefault(messages)
	assert.Greater(t, tokens, 0)
}

func TestCountMessageTokens_LargeConversation(t *testing.T) {
	counter := NewHeuristicTokenCounter(ModelFamilyDefault)
	messages := make([]types.Message, 200)
	for i := range messages {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		messages[i] = types.Message{
			Role:    role,
			Content: "This is a test message with some content for counting tokens.",
		}
	}
	tokens := counter.CountMessageTokens(messages)
	require.Greater(t, tokens, 0)
	assert.Greater(t, tokens, 200*perMessageOverhead)
}

// =============================================================================
// Helper function tests
// =============================================================================

func TestIsCJK(t *testing.T) {
	assert.True(t, isCJK('中'))  // Han
	assert.True(t, isCJK('あ'))  // Hiragana
	assert.True(t, isCJK('ア'))  // Katakana
	assert.True(t, isCJK('한'))  // Hangul
	assert.False(t, isCJK('A')) // Latin
	assert.False(t, isCJK('1')) // Digit
}

func TestIsCodeChar(t *testing.T) {
	codeChars := []rune{'{', '}', '(', ')', '[', ']', ';', '=', '<', '>',
		'|', '&', '!', '~', '^', '%', '#', '@', '\\', '`'}
	for _, ch := range codeChars {
		assert.True(t, isCodeChar(ch), "expected %c to be code char", ch)
	}
	assert.False(t, isCodeChar('a'))
	assert.False(t, isCodeChar(' '))
	assert.False(t, isCodeChar('.'))
}

func TestEstimateImageTokens(t *testing.T) {
	tests := []struct {
		name     string
		media    *types.MediaContent
		expected int
	}{
		{"nil media", nil, imageTokensAutoDetail},
		{"no detail", &types.MediaContent{MIMEType: "image/jpeg"}, imageTokensAutoDetail},
		{"low detail", &types.MediaContent{Detail: strPtr("low")}, imageTokensLowDetail},
		{"high detail", &types.MediaContent{Detail: strPtr("high")}, imageTokensHighDetail},
		{"auto detail", &types.MediaContent{Detail: strPtr("auto")}, imageTokensAutoDetail},
		{"unknown detail", &types.MediaContent{Detail: strPtr("medium")}, imageTokensAutoDetail},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := estimateImageTokens(tt.media)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func strPtr(s string) *string {
	return &s
}
