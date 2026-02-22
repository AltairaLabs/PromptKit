package providers

import (
	"bytes"
	"encoding/base64"
	"io"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream"
)

// encodeBedrockEvent creates a single binary event-stream frame with a base64-encoded JSON payload.
func encodeBedrockEvent(t *testing.T, data string) []byte {
	t.Helper()
	encoded := base64.StdEncoding.EncodeToString([]byte(data))
	payload := []byte(`{"bytes":"` + encoded + `"}`)

	msg := eventstream.Message{
		Headers: eventstream.Headers{
			{Name: ":event-type", Value: eventstream.StringValue("chunk")},
			{Name: ":content-type", Value: eventstream.StringValue("application/json")},
			{Name: ":message-type", Value: eventstream.StringValue("event")},
		},
		Payload: payload,
	}

	var buf bytes.Buffer
	encoder := eventstream.NewEncoder()
	if err := encoder.Encode(&buf, msg); err != nil {
		t.Fatalf("failed to encode event: %v", err)
	}
	return buf.Bytes()
}

func TestBedrockEventScanner_SingleEvent(t *testing.T) {
	event := `{"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello"}}`
	data := encodeBedrockEvent(t, event)

	scanner := NewBedrockEventScanner(bytes.NewReader(data))

	if !scanner.Scan() {
		t.Fatalf("expected Scan to return true, got false; err: %v", scanner.Err())
	}
	if scanner.Data() != event {
		t.Errorf("expected data %q, got %q", event, scanner.Data())
	}
	if scanner.Scan() {
		t.Error("expected Scan to return false after last event")
	}
	if scanner.Err() != nil {
		t.Errorf("expected no error, got %v", scanner.Err())
	}
}

func TestBedrockEventScanner_MultipleEvents(t *testing.T) {
	events := []string{
		`{"type":"message_start","message":{"usage":{"input_tokens":10}}}`,
		`{"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello"}}`,
		`{"type":"content_block_delta","delta":{"type":"text_delta","text":" world"}}`,
		`{"type":"message_stop"}`,
	}

	var buf bytes.Buffer
	for _, event := range events {
		buf.Write(encodeBedrockEvent(t, event))
	}

	scanner := NewBedrockEventScanner(bytes.NewReader(buf.Bytes()))

	var scanned []string
	for scanner.Scan() {
		scanned = append(scanned, scanner.Data())
	}

	if scanner.Err() != nil {
		t.Fatalf("unexpected error: %v", scanner.Err())
	}
	if len(scanned) != len(events) {
		t.Fatalf("expected %d events, got %d", len(events), len(scanned))
	}
	for i, expected := range events {
		if scanned[i] != expected {
			t.Errorf("event %d: expected %q, got %q", i, expected, scanned[i])
		}
	}
}

func TestBedrockEventScanner_EmptyReader(t *testing.T) {
	scanner := NewBedrockEventScanner(bytes.NewReader(nil))

	if scanner.Scan() {
		t.Error("expected Scan to return false on empty reader")
	}
	if scanner.Err() != nil {
		t.Errorf("expected no error on empty reader, got %v", scanner.Err())
	}
}

func TestBedrockEventScanner_ExceptionEvent(t *testing.T) {
	msg := eventstream.Message{
		Headers: eventstream.Headers{
			{Name: ":event-type", Value: eventstream.StringValue("exception")},
			{Name: ":message-type", Value: eventstream.StringValue("exception")},
		},
		Payload: []byte(`{"message":"throttling"}`),
	}

	var buf bytes.Buffer
	encoder := eventstream.NewEncoder()
	if err := encoder.Encode(&buf, msg); err != nil {
		t.Fatalf("failed to encode exception: %v", err)
	}

	scanner := NewBedrockEventScanner(bytes.NewReader(buf.Bytes()))

	if scanner.Scan() {
		t.Error("expected Scan to return false on exception event")
	}
	if scanner.Err() == nil {
		t.Fatal("expected an error for exception event")
	}
	if got := scanner.Err().Error(); got == "" {
		t.Error("expected non-empty error message")
	}
}

func TestBedrockEventScanner_EmptyBytesPayload(t *testing.T) {
	// Event with empty "bytes" field should be skipped
	msg := eventstream.Message{
		Headers: eventstream.Headers{
			{Name: ":event-type", Value: eventstream.StringValue("chunk")},
			{Name: ":message-type", Value: eventstream.StringValue("event")},
		},
		Payload: []byte(`{"bytes":""}`),
	}

	var buf bytes.Buffer
	encoder := eventstream.NewEncoder()
	if err := encoder.Encode(&buf, msg); err != nil {
		t.Fatalf("failed to encode: %v", err)
	}

	// Append a real event after the empty one
	realEvent := `{"type":"message_stop"}`
	buf.Write(encodeBedrockEvent(t, realEvent))

	scanner := NewBedrockEventScanner(bytes.NewReader(buf.Bytes()))

	if !scanner.Scan() {
		t.Fatalf("expected Scan to return true, skipping empty event; err: %v", scanner.Err())
	}
	if scanner.Data() != realEvent {
		t.Errorf("expected %q, got %q", realEvent, scanner.Data())
	}
}

// Verify StreamScanner interface compliance at compile time.
var _ StreamScanner = (*BedrockEventScanner)(nil)
var _ StreamScanner = (*SSEScanner)(nil)

func TestBedrockEventScanner_MalformedPayload(t *testing.T) {
	// Payload that isn't valid JSON — should be skipped
	msg := eventstream.Message{
		Headers: eventstream.Headers{
			{Name: ":event-type", Value: eventstream.StringValue("chunk")},
			{Name: ":message-type", Value: eventstream.StringValue("event")},
		},
		Payload: []byte(`not-json`),
	}

	var buf bytes.Buffer
	encoder := eventstream.NewEncoder()
	if err := encoder.Encode(&buf, msg); err != nil {
		t.Fatalf("failed to encode: %v", err)
	}

	scanner := NewBedrockEventScanner(bytes.NewReader(buf.Bytes()))

	// Should return false (no valid events) with no error (malformed payloads are skipped)
	if scanner.Scan() {
		t.Error("expected Scan to return false for malformed payload")
	}
	if scanner.Err() != nil {
		t.Errorf("expected no error for malformed payload, got %v", scanner.Err())
	}
}

func TestBedrockEventScanner_TruncatedFrame(t *testing.T) {
	// Create a valid frame and truncate it
	event := `{"type":"message_stop"}`
	data := encodeBedrockEvent(t, event)

	// Truncate to half
	truncated := data[:len(data)/2]

	scanner := NewBedrockEventScanner(bytes.NewReader(truncated))

	if scanner.Scan() {
		t.Error("expected Scan to return false for truncated frame")
	}
	if scanner.Err() == nil {
		t.Error("expected an error for truncated frame")
	}
}

func TestBedrockEventScanner_InvalidBase64(t *testing.T) {
	msg := eventstream.Message{
		Headers: eventstream.Headers{
			{Name: ":event-type", Value: eventstream.StringValue("chunk")},
			{Name: ":message-type", Value: eventstream.StringValue("event")},
		},
		Payload: []byte(`{"bytes":"!!!not-base64!!!"}`),
	}

	var buf bytes.Buffer
	encoder := eventstream.NewEncoder()
	if err := encoder.Encode(&buf, msg); err != nil {
		t.Fatalf("failed to encode: %v", err)
	}

	scanner := NewBedrockEventScanner(bytes.NewReader(buf.Bytes()))

	if scanner.Scan() {
		t.Error("expected Scan to return false for invalid base64")
	}
	if scanner.Err() == nil {
		t.Error("expected an error for invalid base64")
	}
}

// ReaderFunc is a simple io.Reader that calls a function for each Read.
type readerFunc func([]byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }

func TestBedrockEventScanner_ReadError(t *testing.T) {
	errReader := readerFunc(func([]byte) (int, error) {
		return 0, io.ErrUnexpectedEOF
	})

	scanner := NewBedrockEventScanner(errReader)

	if scanner.Scan() {
		t.Error("expected Scan to return false on read error")
	}
	if scanner.Err() == nil {
		t.Error("expected an error on read error")
	}
}
