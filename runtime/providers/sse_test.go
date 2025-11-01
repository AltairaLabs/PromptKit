package providers

import (
	"bytes"
	"strings"
	"testing"
)

func TestSSEScanner_BasicEvents(t *testing.T) {
	input := `data: {"delta": "hello"}

data: {"delta": " world"}

data: [DONE]

`
	scanner := NewSSEScanner(strings.NewReader(input))

	// First event
	if !scanner.Scan() {
		t.Fatal("Expected first event")
	}
	if got := scanner.Data(); got != `{"delta": "hello"}` {
		t.Errorf("First event: got %q, want %q", got, `{"delta": "hello"}`)
	}

	// Second event
	if !scanner.Scan() {
		t.Fatal("Expected second event")
	}
	if got := scanner.Data(); got != `{"delta": " world"}` {
		t.Errorf("Second event: got %q, want %q", got, `{"delta": " world"}`)
	}

	// Third event
	if !scanner.Scan() {
		t.Fatal("Expected third event")
	}
	if got := scanner.Data(); got != "[DONE]" {
		t.Errorf("Third event: got %q, want %q", got, "[DONE]")
	}

	// No more events
	if scanner.Scan() {
		t.Error("Expected no more events")
	}

	if err := scanner.Err(); err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestSSEScanner_EmptyLines(t *testing.T) {
	input := `data: first


data: second


`
	scanner := NewSSEScanner(strings.NewReader(input))

	events := []string{}
	for scanner.Scan() {
		events = append(events, scanner.Data())
	}

	if len(events) != 2 {
		t.Fatalf("Expected 2 events, got %d", len(events))
	}

	if events[0] != "first" {
		t.Errorf("First event: got %q, want %q", events[0], "first")
	}

	if events[1] != "second" {
		t.Errorf("Second event: got %q, want %q", events[1], "second")
	}
}

func TestSSEScanner_WithoutDataPrefix(t *testing.T) {
	input := `id: 1
event: message
data: actual data
: comment line

data: another event
`
	scanner := NewSSEScanner(strings.NewReader(input))

	events := []string{}
	for scanner.Scan() {
		events = append(events, scanner.Data())
	}

	// Should only capture lines with "data:" prefix
	if len(events) != 2 {
		t.Fatalf("Expected 2 events, got %d", len(events))
	}

	if events[0] != "actual data" {
		t.Errorf("First event: got %q, want %q", events[0], "actual data")
	}

	if events[1] != "another event" {
		t.Errorf("Second event: got %q, want %q", events[1], "another event")
	}
}

func TestSSEScanner_EmptyInput(t *testing.T) {
	scanner := NewSSEScanner(strings.NewReader(""))

	if scanner.Scan() {
		t.Error("Expected no events from empty input")
	}

	if err := scanner.Err(); err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestSSEScanner_OnlyEmptyLines(t *testing.T) {
	input := "\n\n\n\n"
	scanner := NewSSEScanner(strings.NewReader(input))

	if scanner.Scan() {
		t.Error("Expected no events from empty lines")
	}

	if err := scanner.Err(); err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestSSEScanner_MultilineData(t *testing.T) {
	input := `data: {"content": "line 1"}
data: {"content": "line 2"}
data: {"content": "line 3"}

`
	scanner := NewSSEScanner(strings.NewReader(input))

	events := []string{}
	for scanner.Scan() {
		events = append(events, scanner.Data())
	}

	if len(events) != 3 {
		t.Fatalf("Expected 3 events, got %d", len(events))
	}
}

func TestSSEScanner_LargeBuffer(t *testing.T) {
	// Create a large SSE event (but within bufio.Scanner limits ~64KB)
	largeContent := strings.Repeat("x", 50000)
	input := "data: " + largeContent + "\n\n"

	scanner := NewSSEScanner(strings.NewReader(input))

	if !scanner.Scan() {
		t.Fatal("Expected to scan large event")
	}

	if got := scanner.Data(); got != largeContent {
		t.Errorf("Large event length: got %d, want %d", len(got), len(largeContent))
	}
}

func TestSSEScanner_BinaryData(t *testing.T) {
	// Test with binary/non-UTF8 data
	input := []byte("data: \x00\x01\x02\xff\xfe\n\n")
	scanner := NewSSEScanner(bytes.NewReader(input))

	if !scanner.Scan() {
		t.Fatal("Expected to scan binary event")
	}

	// Just verify it doesn't crash
	data := scanner.Data()
	if len(data) == 0 {
		t.Error("Expected non-empty data")
	}
}

func TestSSEScanner_TrailingWhitespace(t *testing.T) {
	input := "data: content with spaces   \n\n"
	scanner := NewSSEScanner(strings.NewReader(input))

	if !scanner.Scan() {
		t.Fatal("Expected event")
	}

	// Should preserve trailing whitespace
	if got := scanner.Data(); got != "content with spaces   " {
		t.Errorf("Got %q, want %q", got, "content with spaces   ")
	}
}

func TestSSEScanner_ConsecutiveDataLines(t *testing.T) {
	// Multiple data lines without blank line separator
	// Each "data:" line should be treated as separate event boundary
	input := `data: event1
data: event2
data: event3

`
	scanner := NewSSEScanner(strings.NewReader(input))

	count := 0
	for scanner.Scan() {
		count++
	}

	// Should get all 3 events
	if count != 3 {
		t.Errorf("Expected 3 events, got %d", count)
	}
}
