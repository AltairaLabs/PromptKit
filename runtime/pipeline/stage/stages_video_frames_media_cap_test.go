package stage

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// A frames message composed at the default limit must fit the per-message
// media item cap every provider enforces. The default used to be 30, above the
// cap of 20, so a long video's frames message always failed at the provider.
func TestFramesToMessageStage_DefaultFitsPerMessageMediaCap(t *testing.T) {
	stg := NewFramesToMessageStage(DefaultFramesToMessageConfig())

	const extracted = 30 // VideoToFramesStage's DefaultMaxFrames
	pf := &pendingFrames{frames: make(map[int]*ImageData, extracted), totalFrames: extracted}
	for i := 0; i < extracted; i++ {
		pf.frames[i] = &ImageData{Data: createTestFrameData(8, 8), MIMEType: "image/jpeg", Width: 8, Height: 8}
	}

	msg, err := stg.composeMessage(pf)
	if err != nil {
		t.Fatalf("composeMessage: %v", err)
	}
	if len(msg.Parts) != providers.MaxMediaItems {
		t.Fatalf("want %d frames, the per-message cap, got %d", providers.MaxMediaItems, len(msg.Parts))
	}

	// Load the parts the way a provider does: one loader for the message.
	loader := providers.NewMediaLoader(providers.MediaLoaderConfig{})
	for i := range msg.Parts {
		if _, err := loader.GetBase64Data(context.Background(), msg.Parts[i].Media); err != nil {
			t.Fatalf("part %d: a provider would fail this message: %v", i, err)
		}
	}
}
