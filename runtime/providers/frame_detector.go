package providers

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
)

// FrameDetector extracts the bytes of the first complete "frame" from a
// streaming response body. The concept of a frame is protocol-specific —
// SSE uses `data: ...\n\n` boundaries, NDJSON uses line terminators,
// Gemini's JSON-array streaming uses balanced-brace JSON objects inside
// a top-level array.
//
// The returned bytes MUST be exactly a prefix of the underlying stream:
// the detector reads from r, returns everything it has consumed so far,
// and leaves r positioned immediately after the returned bytes. This is
// what lets the retry driver wrap the returned bytes + remaining body
// in a replayReadCloser so downstream parsers see a contiguous stream.
//
// Detectors MUST drain any internal bufio lookahead into the returned
// slice before returning — if they use a bufio.Reader for efficiency,
// bytes buffered past the frame boundary belong in the replay slice,
// not stuck in a throwaway bufio buffer.
//
// Detectors SHOULD NOT wrap r with an idle-timeout reader themselves;
// the retry driver applies the caller's StreamIdleTimeout to r before
// handing it to the detector, so each detector gets uniform idle
// protection for free.
type FrameDetector interface {
	// Name identifies the detector in logs and errors. Should be a
	// short lowercase token: "sse", "ndjson", "json-array", etc.
	Name() string

	// PeekFirstFrame reads from r until at least one complete frame has
	// been seen, then returns the bytes consumed so far. Returns an
	// error if r fails before a complete frame is found, or if the
	// stream ends cleanly before any frame has been observed.
	//
	// On success, the returned slice is non-empty and r is positioned
	// immediately after the last returned byte.
	PeekFirstFrame(r io.Reader) ([]byte, error)
}

// defaultFrameDetector returns the detector to use when a caller did
// not specify one on StreamRetryRequest. SSE is the most common
// streaming format across providers, so it's the default.
func defaultFrameDetector() FrameDetector { return SSEFrameDetector{} }

// --- SSE ---

// SSEFrameDetector detects server-sent event boundaries. A complete
// frame is one or more `data: ...` lines terminated by a blank line,
// optionally preceded by `:` comments or other SSE directive lines
// that get passed through as part of the frame bytes.
//
// This is the framing used by OpenAI Chat Completions, OpenAI Responses
// API, Claude Messages, VLLM, and most SSE-based LLM providers.
type SSEFrameDetector struct{}

// Name implements FrameDetector.
func (SSEFrameDetector) Name() string { return "sse" }

// PeekFirstFrame reads until a `data: ...` line has been seen and then
// a terminating blank line. Comments and other directives before the
// first data line are included in the returned bytes.
//
// If the stream closes cleanly right after the first event without a
// trailing blank line, this is still treated as a complete frame so
// downstream can decide what to do.
func (SSEFrameDetector) PeekFirstFrame(r io.Reader) ([]byte, error) {
	br := bufio.NewReader(r)
	var buf bytes.Buffer
	sawData := false
	for {
		line, err := br.ReadString('\n')
		if line != "" {
			buf.WriteString(line)
			if done, drainErr := processSSELine(br, &buf, line, &sawData); done {
				return buf.Bytes(), drainErr
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) && buf.Len() > 0 && sawData {
				return buf.Bytes(), nil
			}
			return nil, err
		}
	}
}

// processSSELine classifies a single SSE framing line and, when the end
// of the first event is reached, drains any lookahead the bufio.Reader
// has pre-buffered so downstream reads resume contiguously. Returns
// done=true exactly when the first event boundary has been observed.
func processSSELine(br *bufio.Reader, buf *bytes.Buffer, line string, sawData *bool) (done bool, err error) {
	trimmed := strings.TrimRight(line, "\r\n")
	if strings.HasPrefix(trimmed, "data: ") || trimmed == "data:" {
		*sawData = true
		return false, nil
	}
	if trimmed == "" && *sawData {
		// Blank line after a data: line terminates the event. Drain
		// any bytes bufio has read past our boundary into the replay
		// buffer so downstream sees a contiguous stream.
		if drainErr := drainBufferedInto(br, buf); drainErr != nil {
			return true, drainErr
		}
		return true, nil
	}
	return false, nil
}

// --- NDJSON ---

// NDJSONFrameDetector detects newline-delimited JSON frames. Each frame
// is one complete JSON object terminated by a literal `\n`. This is
// the framing used by Ollama's streaming API (`{"response":"..."}\n`)
// and several other non-SSE providers that stream raw JSON.
//
// This detector does NOT validate that the line is parseable JSON —
// it only looks for the line terminator. Upstream producers that
// emit partial JSON would fail at the downstream parser, not here.
// This matches the SSE detector's behavior of not interpreting the
// payload.
type NDJSONFrameDetector struct{}

// Name implements FrameDetector.
func (NDJSONFrameDetector) Name() string { return "ndjson" }

// PeekFirstFrame reads until the first `\n` and returns the line
// (including the newline) plus any bufio lookahead. Blank lines are
// skipped so leading whitespace or keepalive newlines from certain
// producers don't count as a "frame".
//
//nolint:gocognit // Line reader with blank-skip + EOF fallback
func (NDJSONFrameDetector) PeekFirstFrame(r io.Reader) ([]byte, error) {
	br := bufio.NewReader(r)
	var buf bytes.Buffer
	for {
		line, err := br.ReadString('\n')
		if line != "" {
			buf.WriteString(line)
			// A "frame" in NDJSON is a non-empty line terminated by \n.
			// Blank or whitespace-only lines are treated as keepalives
			// (loosely — the JSON RFC doesn't permit them but some
			// producers emit them) and we keep reading until we find
			// a real frame.
			trimmed := strings.TrimSpace(line)
			if trimmed != "" {
				if drainErr := drainBufferedInto(br, &buf); drainErr != nil {
					return nil, drainErr
				}
				return buf.Bytes(), nil
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) && buf.Len() > 0 {
				// Last line without trailing \n — treat as a complete
				// frame if it has any content.
				trimmed := strings.TrimSpace(buf.String())
				if trimmed != "" {
					return buf.Bytes(), nil
				}
			}
			return nil, err
		}
	}
}

// --- JSON array ---

// JSONArrayFrameDetector detects the first complete top-level object
// inside a streaming JSON array. This is the framing used by Gemini's
// `streamGenerateContent` endpoint, which returns
//
//	[
//	  {"candidates": [...], "usageMetadata": {...}},
//	  {"candidates": [...], "usageMetadata": {...}},
//	  ...
//	]
//
// parsed incrementally by a downstream `json.Decoder`. The detector
// reads past leading whitespace and the opening `[`, then scans bytes
// until it finds the end of the first `{...}` at depth 0 (respecting
// JSON string escapes).
//
// Byte-level parsing is deliberate — a `json.Decoder` would work but
// it buffers aggressively and makes it harder to track exactly how
// many bytes have been consumed from the underlying reader.
//
// On success the returned bytes form a prefix of the stream ending at
// the closing brace of the first object. The downstream `json.Decoder`
// continues from there and expects either `,` or `]` next, which is
// exactly what remains in the stream.
type JSONArrayFrameDetector struct{}

// Name implements FrameDetector.
func (JSONArrayFrameDetector) Name() string { return "json-array" }

// PeekFirstFrame reads bytes from r tracking JSON state until the
// first top-level object inside the array is complete.
func (JSONArrayFrameDetector) PeekFirstFrame(r io.Reader) ([]byte, error) {
	br := bufio.NewReader(r)
	var buf bytes.Buffer

	// Phase 1: skip leading whitespace and consume the opening `[`.
	if err := consumeJSONArrayOpen(br, &buf); err != nil {
		return nil, err
	}

	// Phase 2: scan until we've read a complete object at depth 0.
	if err := consumeFirstObject(br, &buf); err != nil {
		return nil, err
	}

	// Phase 3: drain any bufio lookahead into the replay slice.
	if err := drainBufferedInto(br, &buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// consumeJSONArrayOpen skips whitespace and reads the opening `[` into
// buf. Returns an error if the stream does not start with a valid
// array prefix.
func consumeJSONArrayOpen(br *bufio.Reader, buf *bytes.Buffer) error {
	for {
		b, err := br.ReadByte()
		if err != nil {
			return fmt.Errorf("reading JSON array open: %w", err)
		}
		buf.WriteByte(b)
		if b == '[' {
			return nil
		}
		if !isJSONWhitespace(b) {
			return fmt.Errorf("expected '[' at start of JSON array, got %q", b)
		}
	}
}

// consumeFirstObject reads bytes into buf until a complete `{...}` at
// brace depth 0 has been seen, respecting JSON string literals and
// escapes. Leading whitespace between `[` and the first `{` is
// consumed. Returns an error on EOF before the object is complete, or
// on a non-whitespace/non-brace byte before the object opens.
//
//nolint:gocognit // JSON byte-level state machine tracks string/escape/depth/started dimensions
func consumeFirstObject(br *bufio.Reader, buf *bytes.Buffer) error {
	depth := 0
	inString := false
	escaped := false
	started := false

	for {
		b, err := br.ReadByte()
		if err != nil {
			return fmt.Errorf("reading JSON array element: %w", err)
		}
		buf.WriteByte(b)

		if escaped {
			escaped = false
			continue
		}
		if inString {
			switch b {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}

		// Once we've entered an object (depth > 0), any character
		// except a string delimiter or a matching brace is just a
		// payload byte we pass through. Nested arrays, nested
		// objects, and JSON primitives all fall into this bucket.
		if started && depth > 0 {
			switch b {
			case '"':
				inString = true
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					return nil
				}
			}
			continue
		}

		// At depth 0 (haven't opened the first object yet): only
		// whitespace, commas, a closing `]` for an empty array, or
		// the opening `{` of the first object are valid. Anything
		// else is malformed.
		switch b {
		case '{':
			depth++
			started = true
		case ']':
			// Empty array — no frames available. Return EOF so
			// classifyStreamAttempt treats the stream as a terminal
			// failure (no content to forward downstream).
			return io.EOF
		default:
			if !isJSONWhitespace(b) && b != ',' {
				return fmt.Errorf("unexpected %q before object opened", b)
			}
		}
	}
}

// isJSONWhitespace reports whether b is a JSON insignificant
// whitespace character (RFC 8259 §2).
func isJSONWhitespace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// --- AWS Bedrock event-stream ---

// BedrockEventStreamFrameDetector reads one complete AWS binary
// event-stream message. The format is:
//
//	[4-byte total_length BE][4-byte headers_length BE][4-byte prelude_crc]
//	[headers...][payload...][4-byte message_crc]
//
// The detector reads the 4-byte total_length prefix, then reads
// exactly (total_length - 4) more bytes to complete the message.
// This gives the retry driver one full binary frame for replay,
// confirming the stream is "live" before handing it to the consumer.
//
// Used by the Claude provider when running on AWS Bedrock
// (Content-Type: application/vnd.amazon.eventstream).
type BedrockEventStreamFrameDetector struct{}

// Name implements FrameDetector.
func (BedrockEventStreamFrameDetector) Name() string { return "bedrock-eventstream" }

// PeekFirstFrame reads one complete event-stream message from r and
// returns the raw bytes. The reader must be positioned at the start
// of a message boundary.
func (BedrockEventStreamFrameDetector) PeekFirstFrame(r io.Reader) ([]byte, error) {
	// Read the 4-byte total message length (big-endian uint32).
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return nil, fmt.Errorf("bedrock eventstream: reading message length: %w", err)
	}
	totalLen := uint32(lenBuf[0])<<24 | uint32(lenBuf[1])<<16 | uint32(lenBuf[2])<<8 | uint32(lenBuf[3])

	// Sanity: messages must be at least 16 bytes (12-byte prelude + 4-byte CRC).
	const minMessageSize = 16
	if totalLen < minMessageSize {
		return nil, fmt.Errorf("bedrock eventstream: message length %d too small (min %d)", totalLen, minMessageSize)
	}

	// Cap at a reasonable max to guard against corrupted length fields.
	const maxMessageSize = 16 * 1024 * 1024 // 16 MB
	if totalLen > maxMessageSize {
		return nil, fmt.Errorf("bedrock eventstream: message length %d exceeds max %d", totalLen, maxMessageSize)
	}

	// Allocate and fill the complete message (including the length prefix).
	msg := make([]byte, totalLen)
	copy(msg[:4], lenBuf[:])
	if _, err := io.ReadFull(r, msg[4:]); err != nil {
		return nil, fmt.Errorf("bedrock eventstream: reading message body: %w", err)
	}
	return msg, nil
}

// --- Shared helpers ---

// drainBufferedInto copies any bytes sitting in the bufio.Reader's
// internal buffer (but not yet surfaced to callers) into dst, then
// advances the bufio.Reader past them. Used by every frame detector
// to transfer pre-read lookahead bytes into the replay slice so
// downstream reads resume contiguously from the underlying stream.
func drainBufferedInto(br *bufio.Reader, dst *bytes.Buffer) error {
	n := br.Buffered()
	if n == 0 {
		return nil
	}
	remaining, peekErr := br.Peek(n)
	if peekErr != nil {
		return peekErr
	}
	dst.Write(remaining)
	if _, discardErr := br.Discard(n); discardErr != nil {
		return discardErr
	}
	return nil
}
