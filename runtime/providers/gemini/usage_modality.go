package gemini

import "github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"

// ModalityTokenCount is one entry of the per-modality token breakdown Gemini
// reports in usageMetadata (promptTokensDetails, responseTokensDetails, ...).
type ModalityTokenCount struct {
	Modality   string `json:"modality"` // TEXT, IMAGE, VIDEO, AUDIO, DOCUMENT
	TokenCount int    `json:"tokenCount"`
}

// Modality names Gemini uses in ModalityTokenCount.
const (
	modalityAudio = "AUDIO"
	modalityImage = "IMAGE"
	modalityVideo = "VIDEO"
)

// usageCounts is the provider-neutral shape of Gemini's usageMetadata, shared
// by generateContent (candidatesTokenCount) and the Live API
// (responseTokenCount), which name the output count differently.
type usageCounts struct {
	prompt, cached, toolUse, response, thoughts         int
	promptDetails, cacheDetails, toolUseDetails, output []ModalityTokenCount
}

// modalitySum totals the tokens reported for one modality.
func modalitySum(details []ModalityTokenCount, modality string) int {
	n := 0
	for _, d := range details {
		if d.Modality == modality {
			n += d.TokenCount
		}
	}
	return n
}

// nonNegative clamps a derived count, so an inconsistent breakdown on the
// wire can never produce a negative charge.
func nonNegative(n int) int {
	return max(n, 0)
}

// tokens maps Gemini's usage into canonical units.
//
// The prompt count is cache-INCLUSIVE and the cached tokens are priced as
// cache reads, so they are taken out of full-price input. Audio, image and
// video are priced apart from text where a model charges differently for them
// (audio input costs up to ~4x text), so each modality's uncached tokens get
// their own unit and the rest (text, documents, anything unbroken-down) stays
// input. Tool-use prompt tokens bill as input too. Thoughts bill on top of the
// response at the output rate, so they become the reasoning unit. Without a
// breakdown on the wire everything prices as text, as before.
func (u usageCounts) tokens() base.TokenUsage {
	uncached := func(modality string) int {
		return nonNegative(modalitySum(u.promptDetails, modality) - modalitySum(u.cacheDetails, modality) +
			modalitySum(u.toolUseDetails, modality))
	}
	audioIn, imageIn, videoIn := uncached(modalityAudio), uncached(modalityImage), uncached(modalityVideo)
	audioOut := modalitySum(u.output, modalityAudio)

	return base.TokenUsage{
		Input:       nonNegative(u.prompt + u.toolUse - u.cached - audioIn - imageIn - videoIn),
		CacheRead:   u.cached,
		AudioInput:  audioIn,
		ImageInput:  imageIn,
		VideoInput:  videoIn,
		Output:      nonNegative(u.response - audioOut),
		AudioOutput: audioOut,
		Reasoning:   u.thoughts,
	}
}
