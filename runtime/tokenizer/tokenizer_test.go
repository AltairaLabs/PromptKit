package tokenizer

import (
	"sync"
	"testing"
)

func TestHeuristicTokenCounter_CountTokens(t *testing.T) {
	tests := []struct {
		name     string
		family   ModelFamily
		text     string
		wantMin  int // Minimum expected tokens
		wantMax  int // Maximum expected tokens
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
