package providers

import (
	"bufio"
	"bytes"
	"io"
)

// StreamScanner is the interface for scanning streaming responses.
// Both SSE (Server-Sent Events) and binary event-stream formats implement this.
type StreamScanner interface {
	Scan() bool
	Data() string
	Err() error
}

// SSEScanner scans Server-Sent Events (SSE) streams
type SSEScanner struct {
	scanner *bufio.Scanner
	data    string // lazy: only materialized on first Data() call per Scan
	rawData []byte // zero-copy reference into scanner buffer, valid until next Scan
	hasData bool   // whether data has been materialized from rawData
	err     error
}

// NewSSEScanner creates a new SSE scanner
func NewSSEScanner(r io.Reader) *SSEScanner {
	scanner := bufio.NewScanner(r)
	return &SSEScanner{
		scanner: scanner,
	}
}

// Scan advances to the next SSE event
func (s *SSEScanner) Scan() bool {
	for s.scanner.Scan() {
		line := s.scanner.Bytes()

		// Skip empty lines (event boundaries)
		if len(line) == 0 {
			continue
		}

		// Look for "data:" prefix — per SSE spec, strip one leading space if present.
		if bytes.HasPrefix(line, []byte("data:")) {
			payload := bytes.TrimPrefix(line, []byte("data:"))
			payload = bytes.TrimPrefix(payload, []byte(" "))
			s.rawData = payload // valid until next Scan call
			s.hasData = false   // reset lazy string
			s.data = ""
			return true
		}
	}

	s.err = s.scanner.Err()
	return false
}

// Data returns the current event data as a string.
// The string is lazily allocated on first call per Scan to avoid
// unnecessary heap allocations when only DataBytes is needed.
func (s *SSEScanner) Data() string {
	if !s.hasData {
		s.data = string(s.rawData)
		s.hasData = true
	}
	return s.data
}

// DataBytes returns the current event data as a byte slice.
// The returned slice is only valid until the next call to Scan.
// Use this to avoid the string→[]byte conversion in json.Unmarshal.
func (s *SSEScanner) DataBytes() []byte {
	return s.rawData
}

// Err returns any scanning error
func (s *SSEScanner) Err() error {
	return s.err
}
