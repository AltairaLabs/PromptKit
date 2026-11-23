// Package stage provides the reactive streams architecture for pipeline execution.
//
// A pipeline is a directed acyclic graph of stages. Each stage runs in its own
// goroutine, reads StreamElement values from an input channel, and writes
// StreamElement values to an output channel. A StreamElement carries one unit
// of data (text, audio, video, an image, a message, or a tool call) together
// with metadata and an optional error.
//
// # Building and running
//
// PipelineBuilder assembles stages into a graph with Chain, Connect, Branch and
// Merge, and Build validates it. StreamPipeline.Execute starts the stages and
// returns the output channel. StreamPipeline.ExecuteSync runs the pipeline to
// completion and returns an ExecutionResult. PipelineConfig sets channel
// buffering, the execution and idle timeouts, and the graceful shutdown timeout.
//
// # Stage kinds
//
// Every stage implements Stage. A stage with several upstream edges also
// implements MultiInputStage, and a stage that routes elements to named
// downstream outputs implements MultiOutputStage. StageType describes whether a
// stage transforms, accumulates, generates, sinks or is bidirectional.
//
// The package supplies stages for prompt assembly and templating, context
// building and compaction, provider calls and tool execution, state store load
// and save, recording, speech, text-to-speech and voice activity handling,
// audio pacing and resampling, and media extraction, conversion and
// composition.
//
// # Failure
//
// The first stage error ends the turn. The pipeline does not cancel the other
// stages. It sends the error in-band to the failed stage's downstream stages as
// an error element, and they pass it on instead of acting on the incomplete
// turn. The error reaches the caller as the last element on the output channel.
// A provider error arrives as the provider stage emitted it. The error of any
// other stage arrives as a *StageError, which unwraps to the stage's own error.
// ExecuteSync returns the error alongside the result.
//
// # Package layout
//
// Most stages are implemented in packages under stage/internal, one per
// subsystem (core, provider, duplex, voice, media, persist, cue and others).
// This package re-exports their API under the names documented here: types as
// aliases, functions as wrappers, and errors and constants as variables and
// constants of the same value. Import this package; the internal packages
// cannot be imported from outside it.
package stage
