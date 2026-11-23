// Package cue holds the cue stage, which plays a pre-rendered clip for each
// element carrying a Cue (a guardrail's message), and what it plays from: the
// clip bank, the clip sources (the session's own out-of-band audio, a TTS
// service, pre-warmed clips, the earcon) and PrewarmClips.
//
// It is internal to stage (#2876): stage re-exports CueOptions, PrewarmClips
// and its errors under the same names, and keeps NewDuplexCueStage, which
// wires the stage to the provider stage, so callers keep importing stage.
package cue
