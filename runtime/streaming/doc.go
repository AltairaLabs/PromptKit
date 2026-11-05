// Package streaming provides generic utilities for bidirectional streaming
// communication with LLM providers.
//
// This package extracts common patterns used in duplex (bidirectional) streaming
// conversations, including:
//   - Response processing state machine for handling provider responses
//   - Tool execution interface for streaming tool calls
//   - Audio, image and video streaming utilities for sending input to providers
//   - Response collection patterns for managing streaming responses
//
// The package is designed to be provider-agnostic, working with any provider
// that implements the runtime/providers streaming interfaces.
//
// # Response Processing
//
// The response state machine (ProcessResponseElement) analyzes stream elements
// and determines appropriate actions:
//   - Continue: informational element, keep waiting
//   - Complete: turn finished with valid response
//   - Error: error or unexpected empty response
//   - ToolCalls: tool calls need execution
//
// # Tool Execution
//
// The ToolExecutor interface allows custom tool registry implementations to be
// plugged in. The package provides helpers for sending tool results back through
// the streaming pipeline.
//
// # Audio, Image and Video Streaming
//
// SendEndOfStream signals the end of audio input for a turn. ImageStreamer and
// VideoStreamer send frames or encoded chunks into the pipeline, either in burst
// mode (all at once) or real-time mode (paced to the target frame rate or chunk
// duration). SendImageEndOfStream and SendVideoEndOfStream signal the end of
// image and video input.
//
// # Response Collection
//
// ResponseCollector manages the goroutine pattern for collecting streaming
// responses from a provider session, with optional tool call handling.
package streaming
