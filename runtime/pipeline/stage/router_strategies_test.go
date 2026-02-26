package stage

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/pkg/testutil"
)

func TestContentRouter_RoutesBasedOnPredicate(t *testing.T) {
	router := NewContentRouter("test-router",
		RouteWhen("text-output", func(e StreamElement) bool {
			return e.Text != nil
		}),
		RouteWhen("audio-output", func(e StreamElement) bool {
			return e.Audio != nil
		}),
	)

	textOutput := make(chan StreamElement, 10)
	audioOutput := make(chan StreamElement, 10)
	router.RegisterOutput("text-output", textOutput)
	router.RegisterOutput("audio-output", audioOutput)

	input := make(chan StreamElement, 10)
	output := make(chan StreamElement, 10)

	// Send test elements
	text := "hello"
	input <- StreamElement{Text: &text}
	input <- StreamElement{Audio: &AudioData{Samples: []byte{1, 2, 3}}}
	close(input)

	ctx := context.Background()
	err := router.Process(ctx, input, output)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Check text went to text output
	select {
	case elem := <-textOutput:
		if elem.Text == nil || *elem.Text != "hello" {
			t.Error("Expected text element in text output")
		}
	default:
		t.Error("Expected element in text output")
	}

	// Check audio went to audio output
	select {
	case elem := <-audioOutput:
		if elem.Audio == nil {
			t.Error("Expected audio element in audio output")
		}
	default:
		t.Error("Expected element in audio output")
	}
}

func TestContentRouter_DropsUnmatchedElements(t *testing.T) {
	router := NewContentRouter("test-router",
		RouteWhen("text-only", func(e StreamElement) bool {
			return e.Text != nil
		}),
	)

	textOutput := make(chan StreamElement, 10)
	router.RegisterOutput("text-only", textOutput)

	input := make(chan StreamElement, 10)
	output := make(chan StreamElement, 10)

	// Send audio element (will be dropped)
	input <- StreamElement{Audio: &AudioData{Samples: []byte{1, 2, 3}}}
	close(input)

	ctx := context.Background()
	err := router.Process(ctx, input, output)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Text output should be empty
	select {
	case <-textOutput:
		t.Error("Should not have received element in text output")
	default:
		// Expected
	}
}

func TestRouteAudio(t *testing.T) {
	rule := RouteAudio("pcm-output", AudioFormatPCM16)

	// Should match PCM16
	if !rule.Predicate(StreamElement{Audio: &AudioData{Format: AudioFormatPCM16}}) {
		t.Error("Should match PCM16 audio")
	}

	// Should not match Opus
	if rule.Predicate(StreamElement{Audio: &AudioData{Format: AudioFormatOpus}}) {
		t.Error("Should not match Opus audio")
	}

	// Should not match non-audio
	text := "hello"
	if rule.Predicate(StreamElement{Text: &text}) {
		t.Error("Should not match text element")
	}
}

func TestRouteContentType(t *testing.T) {
	tests := []struct {
		name        string
		routeType   ContentType
		element     StreamElement
		shouldMatch bool
	}{
		{"text matches text", ContentTypeText, StreamElement{Text: testutil.Ptr("hello")}, true},
		{"text doesn't match audio", ContentTypeText, StreamElement{Audio: &AudioData{}}, false},
		{"audio matches audio", ContentTypeAudio, StreamElement{Audio: &AudioData{}}, true},
		{"video matches video", ContentTypeVideo, StreamElement{Video: &VideoData{}}, true},
		{"image matches image", ContentTypeImage, StreamElement{Image: &ImageData{}}, true},
		{"any matches everything", ContentTypeAny, StreamElement{Text: testutil.Ptr("x")}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := RouteContentType("output", tt.routeType)
			if got := rule.Predicate(tt.element); got != tt.shouldMatch {
				t.Errorf("RouteContentType(%v) predicate = %v, want %v", tt.routeType, got, tt.shouldMatch)
			}
		})
	}
}

func TestRoundRobinRouter(t *testing.T) {
	router := NewRoundRobinRouter("rr-router", []string{"a", "b", "c"})

	outputA := make(chan StreamElement, 10)
	outputB := make(chan StreamElement, 10)
	outputC := make(chan StreamElement, 10)
	router.RegisterOutput("a", outputA)
	router.RegisterOutput("b", outputB)
	router.RegisterOutput("c", outputC)

	input := make(chan StreamElement, 10)
	output := make(chan StreamElement, 10)

	// Send 6 elements
	for i := 0; i < 6; i++ {
		input <- StreamElement{Sequence: int64(i)}
	}
	close(input)

	ctx := context.Background()
	err := router.Process(ctx, input, output)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Each output should have 2 elements
	countA := len(outputA)
	countB := len(outputB)
	countC := len(outputC)

	if countA != 2 || countB != 2 || countC != 2 {
		t.Errorf("Expected 2 elements each, got A=%d, B=%d, C=%d", countA, countB, countC)
	}
}

func TestWeightedRouter(t *testing.T) {
	router := NewWeightedRouter("weighted-router", map[string]float64{
		"heavy": 0.9,
		"light": 0.1,
	})

	heavyOutput := make(chan StreamElement, 1000)
	lightOutput := make(chan StreamElement, 1000)
	router.RegisterOutput("heavy", heavyOutput)
	router.RegisterOutput("light", lightOutput)

	input := make(chan StreamElement, 1000)
	output := make(chan StreamElement, 1000)

	// Send 1000 elements
	for i := 0; i < 1000; i++ {
		input <- StreamElement{Sequence: int64(i)}
	}
	close(input)

	ctx := context.Background()
	err := router.Process(ctx, input, output)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	heavyCount := len(heavyOutput)
	lightCount := len(lightOutput)

	// With 90/10 split, heavy should have significantly more
	// Allow some variance due to randomness
	if heavyCount < 700 {
		t.Errorf("Heavy output should have ~900 elements, got %d", heavyCount)
	}
	if lightCount > 300 {
		t.Errorf("Light output should have ~100 elements, got %d", lightCount)
	}
}

func TestHashRouter(t *testing.T) {
	router := NewHashRouter("hash-router",
		[]string{"a", "b", "c"},
		func(e StreamElement) string {
			if s, ok := e.Metadata["session_id"].(string); ok {
				return s
			}
			return ""
		},
	)

	outputA := make(chan StreamElement, 100)
	outputB := make(chan StreamElement, 100)
	outputC := make(chan StreamElement, 100)
	router.RegisterOutput("a", outputA)
	router.RegisterOutput("b", outputB)
	router.RegisterOutput("c", outputC)

	input := make(chan StreamElement, 100)
	output := make(chan StreamElement, 100)

	// Send elements with same session - should all go to same output
	for i := 0; i < 10; i++ {
		elem := StreamElement{
			Sequence: int64(i),
			Metadata: map[string]interface{}{"session_id": "session-123"},
		}
		input <- elem
	}
	close(input)

	ctx := context.Background()
	err := router.Process(ctx, input, output)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// All 10 should go to exactly one output
	totalA := len(outputA)
	totalB := len(outputB)
	totalC := len(outputC)

	nonZeroCount := 0
	if totalA > 0 {
		nonZeroCount++
		if totalA != 10 {
			t.Errorf("Expected all 10 elements in one output, got %d in A", totalA)
		}
	}
	if totalB > 0 {
		nonZeroCount++
		if totalB != 10 {
			t.Errorf("Expected all 10 elements in one output, got %d in B", totalB)
		}
	}
	if totalC > 0 {
		nonZeroCount++
		if totalC != 10 {
			t.Errorf("Expected all 10 elements in one output, got %d in C", totalC)
		}
	}

	if nonZeroCount != 1 {
		t.Errorf("Expected exactly one output to have elements, got %d", nonZeroCount)
	}
}

func TestRandomRouter(t *testing.T) {
	router := NewRandomRouter("random-router", []string{"a", "b"})

	outputA := make(chan StreamElement, 1000)
	outputB := make(chan StreamElement, 1000)
	router.RegisterOutput("a", outputA)
	router.RegisterOutput("b", outputB)

	input := make(chan StreamElement, 1000)
	output := make(chan StreamElement, 1000)

	for i := 0; i < 1000; i++ {
		input <- StreamElement{Sequence: int64(i)}
	}
	close(input)

	ctx := context.Background()
	err := router.Process(ctx, input, output)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	countA := len(outputA)
	countB := len(outputB)

	// With random 50/50, each should be roughly half
	// Allow variance of 200 (20%)
	if countA < 300 || countA > 700 {
		t.Errorf("Random router should distribute roughly 50/50, got A=%d", countA)
	}
	if countB < 300 || countB > 700 {
		t.Errorf("Random router should distribute roughly 50/50, got B=%d", countB)
	}
}

func TestBroadcastRouter(t *testing.T) {
	router := NewBroadcastRouter("broadcast-router")

	outputA := make(chan StreamElement, 10)
	outputB := make(chan StreamElement, 10)
	outputC := make(chan StreamElement, 10)
	router.RegisterOutput("a", outputA)
	router.RegisterOutput("b", outputB)
	router.RegisterOutput("c", outputC)

	input := make(chan StreamElement, 10)
	output := make(chan StreamElement, 10)

	// Send 3 elements
	for i := 0; i < 3; i++ {
		input <- StreamElement{Sequence: int64(i)}
	}
	close(input)

	ctx := context.Background()
	err := router.Process(ctx, input, output)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Each output should have all 3 elements
	if len(outputA) != 3 {
		t.Errorf("Expected 3 elements in A, got %d", len(outputA))
	}
	if len(outputB) != 3 {
		t.Errorf("Expected 3 elements in B, got %d", len(outputB))
	}
	if len(outputC) != 3 {
		t.Errorf("Expected 3 elements in C, got %d", len(outputC))
	}
}

func TestRouterContextCancellation(t *testing.T) {
	router := NewContentRouter("test-router",
		RouteWhen("output", func(e StreamElement) bool { return true }),
	)

	// Don't register output - will block when trying to send
	slowOutput := make(chan StreamElement) // unbuffered
	router.RegisterOutput("output", slowOutput)

	input := make(chan StreamElement, 10)
	output := make(chan StreamElement, 10)

	input <- StreamElement{Sequence: 1}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	var processErr error
	go func() {
		defer wg.Done()
		processErr = router.Process(ctx, input, output)
	}()

	wg.Wait()

	if processErr != context.DeadlineExceeded {
		t.Errorf("Expected DeadlineExceeded, got %v", processErr)
	}
}

func TestRouterConcurrency(t *testing.T) {
	router := NewRoundRobinRouter("concurrent-router", []string{"a", "b", "c", "d"})

	outputA := make(chan StreamElement, 1000)
	outputB := make(chan StreamElement, 1000)
	outputC := make(chan StreamElement, 1000)
	outputD := make(chan StreamElement, 1000)
	router.RegisterOutput("a", outputA)
	router.RegisterOutput("b", outputB)
	router.RegisterOutput("c", outputC)
	router.RegisterOutput("d", outputD)

	input := make(chan StreamElement, 1000)
	output := make(chan StreamElement, 1000)

	// Send many elements
	go func() {
		for i := 0; i < 1000; i++ {
			input <- StreamElement{Sequence: int64(i)}
		}
		close(input)
	}()

	ctx := context.Background()
	err := router.Process(ctx, input, output)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	total := len(outputA) + len(outputB) + len(outputC) + len(outputD)
	if total != 1000 {
		t.Errorf("Expected 1000 total elements, got %d", total)
	}
}
