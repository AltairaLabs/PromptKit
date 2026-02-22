package providers

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream"
)

const bedrockExceptionType = "exception"

// BedrockEventScanner decodes AWS binary event-stream frames from Bedrock's
// invoke-with-response-stream endpoint. Each frame's payload is JSON like
// {"bytes":"<base64>"} where the decoded bytes are a standard Claude JSON event
// (identical to the SSE data: payloads from the direct API).
type BedrockEventScanner struct {
	decoder *eventstream.Decoder
	reader  io.Reader
	buf     []byte
	data    string
	err     error
}

// bedrockChunkPayload represents the JSON payload inside each binary event frame.
type bedrockChunkPayload struct {
	Bytes string `json:"bytes"`
}

// NewBedrockEventScanner creates a scanner that reads AWS binary event-stream frames.
func NewBedrockEventScanner(r io.Reader) *BedrockEventScanner {
	return &BedrockEventScanner{
		decoder: eventstream.NewDecoder(),
		reader:  r,
		buf:     make([]byte, 0, 4096), //nolint:mnd // initial buffer capacity
	}
}

// Scan reads the next event-stream frame. Returns true if a data event was
// successfully decoded, false on EOF or error.
func (s *BedrockEventScanner) Scan() bool {
	for {
		msg, err := s.decoder.Decode(s.reader, s.buf)
		if err != nil {
			if err != io.EOF {
				s.err = fmt.Errorf("failed to decode event-stream frame: %w", err)
			}
			return false
		}

		if s.isExceptionEvent(msg) {
			s.err = fmt.Errorf("bedrock stream exception: %s", string(msg.Payload))
			return false
		}

		data, ok := s.decodePayload(msg)
		if !ok {
			continue
		}
		s.data = data
		return true
	}
}

// isExceptionEvent checks if the message is an exception event.
func (s *BedrockEventScanner) isExceptionEvent(msg eventstream.Message) bool {
	if val := msg.Headers.Get(":event-type"); val != nil {
		if str, ok := val.(eventstream.StringValue); ok && string(str) == bedrockExceptionType {
			return true
		}
	}
	if val := msg.Headers.Get(":message-type"); val != nil {
		if str, ok := val.(eventstream.StringValue); ok && string(str) == bedrockExceptionType {
			return true
		}
	}
	return false
}

// decodePayload extracts the Claude JSON event from a Bedrock event-stream frame.
// Returns the decoded string and true if successful, or empty and false to skip.
func (s *BedrockEventScanner) decodePayload(msg eventstream.Message) (string, bool) {
	var payload bedrockChunkPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		return "", false
	}
	if payload.Bytes == "" {
		return "", false
	}

	decoded, err := base64.StdEncoding.DecodeString(payload.Bytes)
	if err != nil {
		s.err = fmt.Errorf("failed to decode base64 payload: %w", err)
		return "", false
	}
	return string(decoded), true
}

// Data returns the decoded Claude JSON event from the last scanned frame.
func (s *BedrockEventScanner) Data() string {
	return s.data
}

// Err returns any error encountered during scanning.
func (s *BedrockEventScanner) Err() error {
	return s.err
}
