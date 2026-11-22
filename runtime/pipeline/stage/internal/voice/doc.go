// Package voice holds the stages of a cascaded voice pipeline: turn detection
// on the caller's audio (AudioTurnStage), speech-to-text (STTStage),
// text-to-speech (TTSStage, and TTSStageWithInterruption, which speaks a
// streamed reply segment by segment and stops when the caller barges in),
// detection of the end of a spoken reply (ResponseVADStage), real-time
// pacing of audio (AudioPacingStage) and resampling (AudioResampleStage).
//
// It is internal to stage (#2876): stage re-exports these stages and their
// configs under the same names, so callers keep importing stage.
package voice
