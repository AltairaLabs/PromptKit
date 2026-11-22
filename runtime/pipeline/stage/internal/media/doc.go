// Package media holds the stages that prepare a turn's media: extracting
// media from a message and composing it back (MediaExtractStage,
// MediaComposeStage), converting media to the formats a provider accepts
// (MediaConvertStage), resizing and preprocessing images (ImageResizeStage, ImagePreprocessStage), turning
// video into frames and frames into a message (VideoToFramesStage,
// FramesToMessageStage), and capping a stream's frame rate
// (FrameRateLimitStage).
//
// It is internal to stage (#2876): stage re-exports these stages, their
// configs and their constants under the same names, so callers keep importing
// stage.
package media
