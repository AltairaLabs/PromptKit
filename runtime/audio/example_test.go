package audio

import (
	"context"
	"fmt"
)

// ExampleNewSimpleVAD shows the RMS-based voice activity detector analyzing
// silent PCM16 audio: below the default MinVolume threshold, the voice
// probability is zero and the detector stays in the quiet state. No
// external model or network access is required.
func ExampleNewSimpleVAD() {
	vad, err := NewSimpleVAD(DefaultVADParams())
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	// 100ms of silence at 16kHz, 16-bit mono PCM.
	silence := make([]byte, 2*1600)
	probability, err := vad.Analyze(context.Background(), silence)
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	fmt.Printf("probability=%.1f state=%s\n", probability, vad.State())
	// Output: probability=0.0 state=quiet
}
