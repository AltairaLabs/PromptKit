package markup

// Persona rubric constants are system-prompt fragments that consumer code
// (self-play personas, scripted-text turns, SDK extractors) splices into
// an LLM's system prompt so the LLM emits canonical bracket tags. Each
// provider variant lists only the tags that provider can actually act on
// — sending a Cartesia persona a [whispers] suggestion would just waste
// tokens, because the Cartesia adapter drops it.
//
// Each provider's adapter returns one of these via PersonaRubricProvider
// (declared in runtime/tts/service.go). The strings are public so SDK
// consumers can re-use them for non-self-play surfaces (custom personas,
// scripted authoring tools, docs).

// RubricExpressiveFull lists every tag in the canonical taxonomy. Provider
// adapters that consume the full vocabulary (OpenAI gpt-4o-mini-tts via the
// instructions field, ElevenLabs v3 inline tags) return this string.
const RubricExpressiveFull = `When you want to add emotional or vocal characterization
to spoken output, wrap text in bracket tags. Use at most one or two per turn
— sparingly, only when the directive genuinely matters. Available tags:

  [whispers]  ...  [/]   speak the wrapped span as a whisper
  [shouts]    ...  [/]   speak the wrapped span at high volume
  [laughs]    ...  [/]   render the wrapped span with a chuckle
  [sighs]     ...  [/]   render the wrapped span with a sigh
  [excited]   ...  [/]   warm, upbeat delivery
  [sad]       ...  [/]   downbeat, slower delivery
  [smile]     ...  [/]   a smile in the voice
  [calm]      ...  [/]   slower, measured pace
  [pause:Ns]              insert a pause of N (e.g. 300ms, 1s)

A new tag implicitly closes the previous span. [/] closes the current span
explicitly. Tags you do not need can be omitted; do not invent new tag names.`

// RubricEmotionOnly lists only the tags Cartesia maps to its emotion array.
// Cartesia's vocabulary is narrow (positivity / sadness / anger), so any
// other tag we include here would be silently dropped by the adapter.
const RubricEmotionOnly = `When you want to color spoken output emotionally, wrap text
in bracket tags. Use at most one or two per turn. Available tags:

  [excited]   ...  [/]   upbeat, energetic delivery
  [laughs]    ...  [/]   render the wrapped span with a chuckle
  [smile]     ...  [/]   a smile in the voice
  [sad]       ...  [/]   downbeat, slower delivery
  [sighs]     ...  [/]   render the wrapped span with a sigh
  [shouts]    ...  [/]   render the wrapped span as if shouting

A new tag implicitly closes the previous span. [/] closes the current span
explicitly. Do not invent new tag names.`
