package gemini

import (
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
)

// geminiPricingCorrectAtYear/Month/Day pin the capture date stamped onto
// every table entry below. Named instead of inlined into time.Date so the
// magic-number linter doesn't fire on the call site.
const (
	geminiPricingCorrectAtYear  = 2026
	geminiPricingCorrectAtMonth = time.November
	geminiPricingCorrectAtDay   = 4
)

// geminiPricingCorrectAt is the date the 2.5 and 3.x rates below were checked
// against https://aistudio.google.com/docs/pricing (Gemini Developer API, paid
// tier). The retired 1.5 and 2.0 rows are no longer on that page and keep their
// last published rates. These tables are fallbacks: a configured descriptor
// or flat rate always wins.
//
// Where a model tiers by context length, the SHORT-CONTEXT rate is used
// (2.5 Pro and 3.1 Pro charge more above 200K tokens); long-context requests
// are understated until a dimensioned price item is added.
var geminiPricingCorrectAt = time.Date(
	geminiPricingCorrectAtYear, geminiPricingCorrectAtMonth, geminiPricingCorrectAtDay,
	0, 0, 0, 0, time.UTC,
)

// perMillionScale converts a published per-1M-token USD rate into a per-unit rate.
const perMillionScale = 1_000_000.0

// geminiCacheReadDiscount: every model on the pricing page charges a cache hit
// (implicit or explicit) at 10% of its text input rate.
const geminiCacheReadDiscount = 0.1

// geminiRates is one model's published per-1M-token USD rates. Zero audioIn,
// imageIn or videoIn means that modality bills at the text input rate; zero
// audioOut or imageOut means the model does not produce that modality, so it
// stays unpriced and is warned about if it ever appears.
type geminiRates struct {
	in, audioIn, imageIn, videoIn float64
	out, audioOut, imageOut       float64
	// thinking gates whether the model bills "thinking" tokens (at the text
	// output rate). Earlier non-thinking models never emit thoughtsTokenCount,
	// but pricing it unconditionally would hide a future thinking backport
	// behind a stale table, so it stays explicit.
	thinking bool
}

func (r geminiRates) descriptor() *base.PricingDescriptor {
	orText := func(rate float64) float64 {
		if rate == 0 {
			return r.in
		}
		return rate
	}
	items := []base.PriceItem{
		{Unit: base.UnitInputToken, Rate: r.in / perMillionScale},
		{Unit: base.UnitAudioInputToken, Rate: orText(r.audioIn) / perMillionScale},
		{Unit: base.UnitImageInputToken, Rate: orText(r.imageIn) / perMillionScale},
		{Unit: base.UnitVideoInputToken, Rate: orText(r.videoIn) / perMillionScale},
		{Unit: base.UnitOutputToken, Rate: r.out / perMillionScale},
		{Unit: base.UnitCacheReadToken, Rate: r.in * geminiCacheReadDiscount / perMillionScale},
	}
	if r.audioOut > 0 {
		items = append(items, base.PriceItem{Unit: base.UnitAudioOutputToken, Rate: r.audioOut / perMillionScale})
	}
	if r.imageOut > 0 {
		items = append(items, base.PriceItem{Unit: base.UnitImageOutputToken, Rate: r.imageOut / perMillionScale})
	}
	if r.thinking {
		items = append(items, base.PriceItem{Unit: base.UnitReasoningToken, Rate: r.out / perMillionScale})
	}
	return &base.PricingDescriptor{
		Source:           base.PricingSourceInline,
		PricingCorrectAt: geminiPricingCorrectAt,
		Items:            items,
	}
}

// geminiPerM builds a descriptor for a model that prices every input modality
// at its text rate.
func geminiPerM(input, output float64, thinking bool) *base.PricingDescriptor {
	return geminiRates{in: input, out: output, thinking: thinking}.descriptor()
}

// Per-1M-token USD rates (input, output) for every Gemini model family.
const (
	gemini15ProIn, gemini15ProOut     = 1.25, 5.00  // Gemini 1.5 Pro (retired; short-context tier)
	gemini15FlashIn, gemini15FlashOut = 0.075, 0.30 // Gemini 1.5 Flash (retired; short-context tier)

	gemini20FlashIn, gemini20FlashOut         = 0.10, 0.40  // Gemini 2.0 Flash (retired)
	gemini20FlashLiteIn, gemini20FlashLiteOut = 0.075, 0.30 // Gemini 2.0 Flash-Lite (retired)

	gemini25ProIn, gemini25ProOut             = 1.25, 10.0 // Gemini 2.5 Pro, prompts <= 200k (>200k: 2.50/15.00)
	gemini25FlashIn, gemini25FlashOut         = 0.30, 2.50 // Gemini 2.5 Flash, text/image/video
	gemini25FlashAudioIn                      = 1.00       // Gemini 2.5 Flash, audio in
	gemini25FlashLiteIn, gemini25FlashLiteOut = 0.10, 0.40 // Gemini 2.5 Flash-Lite, text/image/video
	gemini25FlashLiteAudioIn                  = 0.30       // Gemini 2.5 Flash-Lite, audio in

	gemini31ProIn, gemini31ProOut = 2.00, 12.00 // Gemini 3.1 Pro, prompts <= 200k (>200k: 4.00/18.00)

	// 3.8, 3.7 and 3.6 Flash carry an INTRODUCTORY rate that expires. From
	// 2027-01-01 they become 1.50 / 7.50 — double. This table has no time
	// dimension, so that is a diary entry, not something the code will notice.
	gemini38FlashIn, gemini38FlashOut = 0.75, 3.75 // Gemini 3.8 Flash, introductory through 2026-12-31
	gemini37FlashIn, gemini37FlashOut = 0.75, 3.75 // Gemini 3.7 Flash, introductory through 2026-12-31
	gemini36FlashIn, gemini36FlashOut = 0.75, 3.75 // Gemini 3.6 Flash, introductory through 2026-12-31

	gemini35FlashIn, gemini35FlashOut = 1.50, 9.00 // Gemini 3.5 Flash
	gemini3FlashIn, gemini3FlashOut   = 0.50, 3.00 // Gemini 3 Flash preview, text/image/video
	gemini3FlashAudioIn               = 1.00       // Gemini 3 Flash preview, audio in

	gemini35FlashLiteIn, gemini35FlashLiteOut = 0.30, 2.50 // Gemini 3.5 Flash-Lite, every modality
	gemini31FlashLiteIn, gemini31FlashLiteOut = 0.25, 1.50 // Gemini 3.1 Flash-Lite, text/image/video
	gemini31FlashLiteAudioIn                  = 0.50       // Gemini 3.1 Flash-Lite, audio in
)

// Live API models price every modality separately, and audio dominates a
// voice session's bill.
const (
	// Gemini 3.8 Live, 3.8 Live Extended Thinking and 3.1 Flash Live Preview.
	gemini38LiveTextIn, gemini38LiveAudioIn, gemini38LiveVisualIn = 0.75, 3.00, 1.00 // image/video share a rate
	gemini38LiveTextOut, gemini38LiveAudioOut                     = 4.50, 12.00

	// Gemini 2.5 Flash Native Audio. The page prices "audio / video" input
	// together and lists no separate image rate; Live receives images as video
	// frames, so image takes the video rate.
	gemini25NativeAudioTextIn, gemini25NativeAudioAVIn    = 0.50, 3.00
	gemini25NativeAudioTextOut, gemini25NativeAudioAudOut = 2.00, 12.00
)

// Image-generation models bill generated images as output tokens at their own
// rate, many times the text output rate; text and thinking bill at the text
// rate. 2.5 Flash Image prices text like 2.5 Flash and does not think.
const (
	nanoBanana21In, nanoBanana21Out, nanoBanana21ImgOut                   = 1.50, 7.50, 30.00   // Nano Banana 2.1
	gemini31FlashImgIn, gemini31FlashImgOut, gemini31FlashImgImgOut       = 0.50, 3.00, 60.00   // 3.1 Flash Image
	gemini31FlashLiteImgIn, gemini31FlashLiteImgOut, gemini31FlashLiteImg = 0.25, 1.50, 30.00   // 3.1 Flash Lite Image
	gemini3ProImgIn, gemini3ProImgOut, gemini3ProImgImgOut                = 2.00, 12.00, 120.00 // 3 Pro Image
	gemini25FlashImgImgOut                                                = 30.00               // 2.5 Flash Image
)

// Shared descriptors, one per rate tier, reused across dated snapshot IDs and
// bare family aliases in geminiPricingTable so every alias of a model prices
// identically.
var (
	gemini15Pro       = geminiPerM(gemini15ProIn, gemini15ProOut, false)
	gemini15Flash     = geminiPerM(gemini15FlashIn, gemini15FlashOut, false)
	gemini20Flash     = geminiPerM(gemini20FlashIn, gemini20FlashOut, false)
	gemini20FlashLite = geminiPerM(gemini20FlashLiteIn, gemini20FlashLiteOut, false)
	gemini25Pro       = geminiPerM(gemini25ProIn, gemini25ProOut, true)
	gemini25Flash     = geminiRates{
		in: gemini25FlashIn, audioIn: gemini25FlashAudioIn, out: gemini25FlashOut, thinking: true,
	}.descriptor()
	gemini25FlashLite = geminiRates{
		in: gemini25FlashLiteIn, audioIn: gemini25FlashLiteAudioIn, out: gemini25FlashLiteOut, thinking: true,
	}.descriptor()
	gemini31Pro   = geminiPerM(gemini31ProIn, gemini31ProOut, true)
	gemini38Flash = geminiPerM(gemini38FlashIn, gemini38FlashOut, true)
	gemini37Flash = geminiPerM(gemini37FlashIn, gemini37FlashOut, true)
	gemini36Flash = geminiPerM(gemini36FlashIn, gemini36FlashOut, true)
	gemini35Flash = geminiPerM(gemini35FlashIn, gemini35FlashOut, true)
	gemini3Flash  = geminiRates{
		in: gemini3FlashIn, audioIn: gemini3FlashAudioIn, out: gemini3FlashOut, thinking: true,
	}.descriptor()
	gemini35FlashLite = geminiPerM(gemini35FlashLiteIn, gemini35FlashLiteOut, true)
	gemini31FlashLite = geminiRates{
		in: gemini31FlashLiteIn, audioIn: gemini31FlashLiteAudioIn, out: gemini31FlashLiteOut, thinking: true,
	}.descriptor()

	gemini38Live = geminiRates{
		in: gemini38LiveTextIn, audioIn: gemini38LiveAudioIn,
		imageIn: gemini38LiveVisualIn, videoIn: gemini38LiveVisualIn,
		out: gemini38LiveTextOut, audioOut: gemini38LiveAudioOut, thinking: true,
	}.descriptor()
	gemini25NativeAudio = geminiRates{
		in: gemini25NativeAudioTextIn, audioIn: gemini25NativeAudioAVIn,
		imageIn: gemini25NativeAudioAVIn, videoIn: gemini25NativeAudioAVIn,
		out: gemini25NativeAudioTextOut, audioOut: gemini25NativeAudioAudOut, thinking: true,
	}.descriptor()

	nanoBanana21 = geminiRates{
		in: nanoBanana21In, out: nanoBanana21Out, imageOut: nanoBanana21ImgOut, thinking: true,
	}.descriptor()
	gemini31FlashImage = geminiRates{
		in: gemini31FlashImgIn, out: gemini31FlashImgOut, imageOut: gemini31FlashImgImgOut, thinking: true,
	}.descriptor()
	gemini31FlashLiteImage = geminiRates{
		in: gemini31FlashLiteImgIn, out: gemini31FlashLiteImgOut, imageOut: gemini31FlashLiteImg, thinking: true,
	}.descriptor()
	gemini3ProImage = geminiRates{
		in: gemini3ProImgIn, out: gemini3ProImgOut, imageOut: gemini3ProImgImgOut, thinking: true,
	}.descriptor()
	gemini25FlashImage = geminiRates{
		in: gemini25FlashIn, out: gemini25FlashOut, imageOut: gemini25FlashImgImgOut,
	}.descriptor()
)

// Model IDs named so each id string is spelled once (goconst).
const (
	idGemini15Pro   = "gemini-1.5-pro"
	idGemini15Flash = "gemini-1.5-flash"
	idGemini25Pro   = "gemini-2.5-pro"
	idGemini25Flash = "gemini-2.5-flash"
)

// geminiPricingTable maps both dated/versioned model IDs and bare family
// aliases to their pricing descriptor. Looked up via base.ResolveLLMPricing,
// which normalizes a vendor-qualified model id down to the bare id before
// matching — there is no substring/heuristic fallback, so a model absent here
// prices as $0 with a loud warning rather than silently guessing a wrong
// constant (see PriceUsage).
var geminiPricingTable = map[string]*base.PricingDescriptor{
	// Gemini 1.5
	idGemini15Pro:          gemini15Pro,
	"gemini-1.5-pro-002":   gemini15Pro,
	idGemini15Flash:        gemini15Flash,
	"gemini-1.5-flash-002": gemini15Flash,

	// Gemini 2.0
	"gemini-2.0-flash":      gemini20Flash,
	"gemini-2.0-flash-001":  gemini20Flash,
	"gemini-2.0-flash-exp":  gemini20Flash,
	"gemini-2.0-flash-lite": gemini20FlashLite,

	// Gemini 2.5
	idGemini25Pro:           gemini25Pro,
	idGemini25Flash:         gemini25Flash,
	"gemini-2.5-flash-lite": gemini25FlashLite,

	// Gemini 3.x — keyed by the IDs the API actually serves.
	//
	// The previous rows were "gemini-3-pro", "gemini-3-flash" and
	// "gemini-3-flash-lite", which match NO released model: every real 3.x id
	// carries a minor version ("gemini-3.7-flash"). ResolveLLMPricing has no
	// substring fallback by design, so each of those models resolved to nothing
	// and billed at ZERO with a warning. Verified live before this change:
	// gemini-3.7-flash, -3.6-flash, -3.5-flash and -3.1-pro-preview all
	// returned priced=false.
	"gemini-3.8-flash":       gemini38Flash,
	"gemini-3.7-flash":       gemini37Flash,
	"gemini-3.6-flash":       gemini36Flash,
	"gemini-3.5-flash":       gemini35Flash,
	"gemini-3-flash-preview": gemini3Flash,
	"gemini-3.5-flash-lite":  gemini35FlashLite,
	"gemini-3.1-flash-lite":  gemini31FlashLite,
	"gemini-3.1-pro-preview": gemini31Pro,
	"gemini-3.1-pro":         gemini31Pro,

	// Live API (bidirectional streaming) models.
	"gemini-3.8-live":                               gemini38Live,
	"gemini-3.8-live-extended-thinking":             gemini38Live,
	"gemini-3.1-flash-live-preview":                 gemini38Live,
	"gemini-2.5-flash-native-audio-preview-12-2025": gemini25NativeAudio,

	// Image-generation models.
	"gemini-nano-banana-2.1":      nanoBanana21,
	"gemini-3.1-flash-image":      gemini31FlashImage,
	"gemini-3.1-flash-lite-image": gemini31FlashLiteImage,
	"gemini-3-pro-image":          gemini3ProImage,
	"gemini-2.5-flash-image":      gemini25FlashImage,
}
