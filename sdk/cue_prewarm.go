package sdk

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	rtprompt "github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
	"github.com/AltairaLabs/PromptKit/sdk/v2/internal/pack"
)

// cuePrewarmRate is the output rate PrewarmCueClips renders for: the rate
// OpenAI Realtime and Gemini Live speak at, and the cue stage's default.
const cuePrewarmRate = 24000

// PrewarmCueClips renders a pack's static guardrail messages with the TTS
// service svc into the clip cache realtime conversations play cues from, so
// a conversation opened later speaks them with no render. A host calls it at
// startup or deploy.
//
// The messages are every prompt's validator messages, as an open of that
// prompt compiles them, the messages of guardrails given as options
// (WithGuardrail), and the default blocked message. voice is the session's
// voice name (for example "alloy"): clips are keyed by the service voice whose
// ID or name matches it, ignoring case, exactly as a session in that voice
// looks them up. Clips render as PCM16 mono at 24 kHz, the rate OpenAI
// Realtime and Gemini Live speak at.
//
// The cache is WithCueClipCache's, else the process-wide default every
// conversation uses. Any [stage.ClipCache] works: a writable
// [stage.NewDirClipCache] fills a directory offline, which a conversation
// opened on it with [stage.DirCacheReadOnly] and no TTS service plays from.
//
// Only a TTS service can be pre-warmed: a provider's out-of-band audio needs a
// live session, so a nil svc, or a session passed as one, returns
// [stage.ErrSessionBoundSource]. A message the service fails is named in the
// returned error; the others are still cached. opts are Open's; those that
// shape the guardrails (WithEvalRegistry, WithGuardrail, WithJudgeProvider,
// WithSkipSchemaValidation) apply here as they do there.
func PrewarmCueClips(ctx context.Context, packPath string, svc tts.Service, voice string, opts ...Option) error {
	cfg, err := applyOptions("", opts)
	if err != nil {
		return err
	}
	p, err := loadPackFile(packPath, cfg)
	if err != nil {
		return err
	}
	texts, err := packBlockMessages(p.Prompts, cfg)
	if err != nil {
		return err
	}
	format := stage.ClipFormat{Encoding: stage.AudioFormatPCM16, SampleRate: cuePrewarmRate, Channels: 1}
	return stage.PrewarmClips(ctx, cfg.cueClipCache, svc, voice, format, texts)
}

// packBlockMessages is the block messages of every prompt's guardrails, as an
// open of each prompt builds them, and the default blocked message.
// The host's own guardrails (cfg.providerHooks) count once.
func packBlockMessages(prompts map[string]*pack.Prompt, cfg *config) ([]string, error) {
	var opts []hooks.Option
	for _, name := range slices.Sorted(maps.Keys(prompts)) {
		packHooks, err := compilePackValidators(prompts[name], cfg)
		if err != nil {
			return nil, fmt.Errorf("prompt %q: %w", name, err)
		}
		for _, h := range packHooks {
			opts = append(opts, hooks.WithProviderHook(h))
		}
	}
	for _, h := range cfg.providerHooks {
		opts = append(opts, hooks.WithProviderHook(h))
	}
	return append(hooks.NewRegistry(opts...).BlockMessages(), rtprompt.DefaultBlockedMessage), nil
}
