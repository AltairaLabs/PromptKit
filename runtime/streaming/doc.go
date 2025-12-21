// Package streaming provides generic utilities for bidirectional streaming
// communication with LLM providers.
//
// This package extracts common patterns used in duplex (bidirectional) streaming
// conversations, including:
//   - Response processing state machine for handling provider responses
//   - Tool execution interface for streaming tool calls
//   - Audio streaming utilities for sending audio chunks to providers
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
// # Audio Streaming
//
// AudioStreamer provides utilities for streaming audio data in either burst mode
// (all at once) or real-time mode (paced to match playback speed).
//
// # Response Collection
//
// ResponseCollector manages the goroutine pattern for collecting streaming
// responses from a provider session, with optional tool call handling.
package streaming
