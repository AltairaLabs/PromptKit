package providers

import (
	"bufio"
	"bytes"
	"io"
)

// SSEScanner scans Server-Sent Events (SSE) streams
type SSEScanner struct {
	scanner *bufio.Scanner
	data    string
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

		// Look for "data:" prefix
		if bytes.HasPrefix(line, []byte("data: ")) {
			s.data = string(bytes.TrimPrefix(line, []byte("data: ")))
			return true
		}
	}

	s.err = s.scanner.Err()
	return false
}

// Data returns the current event data
func (s *SSEScanner) Data() string {
	return s.data
}

// Err returns any scanning error
func (s *SSEScanner) Err() error {
	return s.err
}
