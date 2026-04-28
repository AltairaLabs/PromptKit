// Package stage provides the reactive streams architecture for pipeline execution.
package stage

import (
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// elementPool is a sync.Pool for reusing StreamElement instances.
// This reduces GC pressure in high-throughput pipeline scenarios.
//
// TODO: Wire elementPool into hot paths (e.g. collectOutput, runStage,
// ProviderStage streaming loops) so that elements are obtained via
// GetElement/PutElement instead of direct allocation. The pool is defined
// and ready to use but currently unused by the pipeline internals.
var elementPool = sync.Pool{
	New: func() interface{} {
		return &StreamElement{}
	},
}

// GetElement retrieves a StreamElement from the pool or creates a new one.
// The returned element is reset to its zero state.
// Callers should use PutElement when the element is no longer needed.
func GetElement() *StreamElement {
	return elementPool.Get().(*StreamElement)
}

// PutElement returns a StreamElement to the pool for reuse.
// The element is reset before being returned to the pool to prevent data leaks.
// After calling PutElement, the caller must not use the element again.
func PutElement(elem *StreamElement) {
	if elem == nil {
		return
	}
	elem.Reset()
	elementPool.Put(elem)
}

// Reset clears all fields of the StreamElement to their zero values.
// This is called automatically by PutElement before returning to the pool.
func (e *StreamElement) Reset() {
	// Clear content types
	e.Text = nil
	e.Audio = nil
	e.Video = nil
	e.Image = nil
	e.Message = nil
	e.ToolCall = nil
	e.Part = nil
	e.MediaData = nil

	// Clear metadata fields
	e.Sequence = 0
	e.Timestamp = time.Time{}
	e.Source = ""
	e.Priority = PriorityNormal

	// Clear typed metadata
	e.Meta = ElementMetadata{}

	// Clear control signals
	e.EndOfStream = false
	e.Error = nil
}

// GetTextElement retrieves a StreamElement from the pool and initializes it with text content.
// This is a pooled alternative to NewTextElement.
func GetTextElement(text string) *StreamElement {
	elem := GetElement()
	elem.Text = &text
	elem.Timestamp = time.Now()
	elem.Priority = PriorityNormal
	return elem
}

// GetMessageElement retrieves a StreamElement from the pool and initializes it with a message.
// This is a pooled alternative to NewMessageElement.
func GetMessageElement(msg *types.Message) *StreamElement {
	elem := GetElement()
	elem.Message = msg
	elem.Timestamp = time.Now()
	elem.Priority = PriorityNormal
	return elem
}

// GetAudioElement retrieves a StreamElement from the pool and initializes it with audio data.
// This is a pooled alternative to NewAudioElement.
func GetAudioElement(audio *AudioData) *StreamElement {
	elem := GetElement()
	elem.Audio = audio
	elem.Timestamp = time.Now()
	elem.Priority = PriorityHigh
	return elem
}

// GetVideoElement retrieves a StreamElement from the pool and initializes it with video data.
// This is a pooled alternative to NewVideoElement.
func GetVideoElement(video *VideoData) *StreamElement {
	elem := GetElement()
	elem.Video = video
	elem.Timestamp = time.Now()
	elem.Priority = PriorityHigh
	return elem
}

// GetImageElement retrieves a StreamElement from the pool and initializes it with image data.
// This is a pooled alternative to NewImageElement.
func GetImageElement(image *ImageData) *StreamElement {
	elem := GetElement()
	elem.Image = image
	elem.Timestamp = time.Now()
	elem.Priority = PriorityNormal
	return elem
}

// GetErrorElement retrieves a StreamElement from the pool and initializes it with an error.
// This is a pooled alternative to NewErrorElement.
func GetErrorElement(err error) *StreamElement {
	elem := GetElement()
	elem.Error = err
	elem.Timestamp = time.Now()
	elem.Priority = PriorityCritical
	return elem
}

// GetEndOfStreamElement retrieves a StreamElement from the pool and marks it as end-of-stream.
// This is a pooled alternative to NewEndOfStreamElement.
func GetEndOfStreamElement() *StreamElement {
	elem := GetElement()
	elem.EndOfStream = true
	elem.Timestamp = time.Now()
	elem.Priority = PriorityCritical
	return elem
}
