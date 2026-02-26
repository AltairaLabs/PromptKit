package stage

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/pkg/testutil"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestGetElement(t *testing.T) {
	elem := GetElement()
	if elem == nil {
		t.Fatal("GetElement returned nil")
	}
	if elem.Metadata == nil {
		t.Error("GetElement should initialize Metadata map")
	}
	// Clean up
	PutElement(elem)
}

func TestPutElement(t *testing.T) {
	// Test that PutElement handles nil gracefully
	PutElement(nil) // Should not panic

	// Test that element is reset after put
	elem := GetElement()
	elem.Text = testutil.Ptr("test")
	elem.Sequence = 42
	elem.Source = "test-source"
	elem.Metadata["key"] = "value"

	PutElement(elem)

	// Get a new element - it should be reset
	// Note: We can't guarantee we get the same element back from the pool,
	// but if we do, it should be reset
	elem2 := GetElement()
	if elem2.Text != nil {
		t.Error("Element should be reset - Text should be nil")
	}
	if elem2.Sequence != 0 {
		t.Error("Element should be reset - Sequence should be 0")
	}
	if elem2.Source != "" {
		t.Error("Element should be reset - Source should be empty")
	}
	if len(elem2.Metadata) != 0 {
		t.Error("Element should be reset - Metadata should be empty")
	}
	PutElement(elem2)
}

func TestReset(t *testing.T) {
	elem := &StreamElement{
		Text:        testutil.Ptr("test text"),
		Audio:       &AudioData{Samples: []byte{1, 2, 3}},
		Video:       &VideoData{Data: []byte{4, 5, 6}},
		Image:       &ImageData{Data: []byte{7, 8, 9}},
		Message:     &types.Message{Role: "user"},
		ToolCall:    &types.MessageToolCall{ID: "tool-1"},
		Part:        &types.ContentPart{},
		MediaData:   &types.MediaContent{},
		Sequence:    100,
		Timestamp:   time.Now(),
		Source:      "test-source",
		Priority:    PriorityHigh,
		Metadata:    map[string]interface{}{"key": "value", "num": 42},
		EndOfStream: true,
		Error:       errors.New("test error"),
	}

	elem.Reset()

	// Verify all fields are reset
	if elem.Text != nil {
		t.Error("Text should be nil after Reset")
	}
	if elem.Audio != nil {
		t.Error("Audio should be nil after Reset")
	}
	if elem.Video != nil {
		t.Error("Video should be nil after Reset")
	}
	if elem.Image != nil {
		t.Error("Image should be nil after Reset")
	}
	if elem.Message != nil {
		t.Error("Message should be nil after Reset")
	}
	if elem.ToolCall != nil {
		t.Error("ToolCall should be nil after Reset")
	}
	if elem.Part != nil {
		t.Error("Part should be nil after Reset")
	}
	if elem.MediaData != nil {
		t.Error("MediaData should be nil after Reset")
	}
	if elem.Sequence != 0 {
		t.Error("Sequence should be 0 after Reset")
	}
	if !elem.Timestamp.IsZero() {
		t.Error("Timestamp should be zero after Reset")
	}
	if elem.Source != "" {
		t.Error("Source should be empty after Reset")
	}
	if elem.Priority != PriorityNormal {
		t.Error("Priority should be PriorityNormal after Reset")
	}
	if len(elem.Metadata) != 0 {
		t.Error("Metadata should be empty after Reset")
	}
	if elem.EndOfStream {
		t.Error("EndOfStream should be false after Reset")
	}
	if elem.Error != nil {
		t.Error("Error should be nil after Reset")
	}
}

func TestGetTextElement(t *testing.T) {
	text := "hello world"
	elem := GetTextElement(text)

	if elem.Text == nil || *elem.Text != text {
		t.Errorf("Expected Text to be %q, got %v", text, elem.Text)
	}
	if elem.Timestamp.IsZero() {
		t.Error("Timestamp should be set")
	}
	if elem.Priority != PriorityNormal {
		t.Errorf("Expected Priority %d, got %d", PriorityNormal, elem.Priority)
	}
	if elem.Metadata == nil {
		t.Error("Metadata should be initialized")
	}
	PutElement(elem)
}

func TestGetMessageElement(t *testing.T) {
	msg := &types.Message{Role: "assistant", Content: "hello"}
	elem := GetMessageElement(msg)

	if elem.Message != msg {
		t.Error("Message should be set to the provided value")
	}
	if elem.Timestamp.IsZero() {
		t.Error("Timestamp should be set")
	}
	if elem.Priority != PriorityNormal {
		t.Errorf("Expected Priority %d, got %d", PriorityNormal, elem.Priority)
	}
	PutElement(elem)
}

func TestGetAudioElement(t *testing.T) {
	audio := &AudioData{
		Samples:    []byte{1, 2, 3},
		SampleRate: 16000,
		Channels:   1,
	}
	elem := GetAudioElement(audio)

	if elem.Audio != audio {
		t.Error("Audio should be set to the provided value")
	}
	if elem.Timestamp.IsZero() {
		t.Error("Timestamp should be set")
	}
	if elem.Priority != PriorityHigh {
		t.Errorf("Expected Priority %d, got %d", PriorityHigh, elem.Priority)
	}
	PutElement(elem)
}

func TestGetVideoElement(t *testing.T) {
	video := &VideoData{
		Data:     []byte{1, 2, 3},
		MIMEType: "video/mp4",
	}
	elem := GetVideoElement(video)

	if elem.Video != video {
		t.Error("Video should be set to the provided value")
	}
	if elem.Priority != PriorityHigh {
		t.Errorf("Expected Priority %d, got %d", PriorityHigh, elem.Priority)
	}
	PutElement(elem)
}

func TestGetImageElement(t *testing.T) {
	image := &ImageData{
		Data:     []byte{1, 2, 3},
		MIMEType: "image/png",
	}
	elem := GetImageElement(image)

	if elem.Image != image {
		t.Error("Image should be set to the provided value")
	}
	if elem.Priority != PriorityNormal {
		t.Errorf("Expected Priority %d, got %d", PriorityNormal, elem.Priority)
	}
	PutElement(elem)
}

func TestGetErrorElement(t *testing.T) {
	testErr := errors.New("test error")
	elem := GetErrorElement(testErr)

	if elem.Error != testErr {
		t.Error("Error should be set to the provided value")
	}
	if elem.Priority != PriorityCritical {
		t.Errorf("Expected Priority %d, got %d", PriorityCritical, elem.Priority)
	}
	PutElement(elem)
}

func TestGetEndOfStreamElement(t *testing.T) {
	elem := GetEndOfStreamElement()

	if !elem.EndOfStream {
		t.Error("EndOfStream should be true")
	}
	if elem.Priority != PriorityCritical {
		t.Errorf("Expected Priority %d, got %d", PriorityCritical, elem.Priority)
	}
	PutElement(elem)
}

func TestPoolConcurrency(t *testing.T) {
	const numGoroutines = 100
	const numOperations = 1000

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				elem := GetElement()
				elem.Text = testutil.Ptr("test")
				elem.Sequence = int64(id*numOperations + j)
				elem.Metadata["id"] = id
				elem.Metadata["op"] = j
				PutElement(elem)
			}
		}(i)
	}

	wg.Wait()
}

func TestPoolReuse(t *testing.T) {
	// Get and put multiple elements to test pool reuse
	elements := make([]*StreamElement, 10)

	// Get elements
	for i := 0; i < len(elements); i++ {
		elements[i] = GetElement()
		elements[i].Sequence = int64(i)
	}

	// Put them back
	for i := 0; i < len(elements); i++ {
		PutElement(elements[i])
	}

	// Get them again - they should be reset
	for i := 0; i < len(elements); i++ {
		elem := GetElement()
		if elem.Sequence != 0 {
			t.Error("Reused element should have Sequence reset to 0")
		}
		PutElement(elem)
	}
}

// BenchmarkGetElement benchmarks getting an element from the pool.
func BenchmarkGetElement(b *testing.B) {
	for i := 0; i < b.N; i++ {
		elem := GetElement()
		PutElement(elem)
	}
}

// BenchmarkNewTextElement benchmarks creating a new text element without pooling.
func BenchmarkNewTextElement(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = NewTextElement("test")
	}
}

// BenchmarkGetTextElement benchmarks getting a text element from the pool.
func BenchmarkGetTextElement(b *testing.B) {
	for i := 0; i < b.N; i++ {
		elem := GetTextElement("test")
		PutElement(elem)
	}
}

// BenchmarkPoolParallel benchmarks pool operations in parallel.
func BenchmarkPoolParallel(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			elem := GetElement()
			elem.Text = testutil.Ptr("test")
			elem.Metadata["key"] = "value"
			PutElement(elem)
		}
	})
}
