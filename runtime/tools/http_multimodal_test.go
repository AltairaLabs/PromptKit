package tools

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestIsBinaryContentType_DefaultPrefixes(t *testing.T) {
	tests := []struct {
		ct   string
		want bool
	}{
		{"image/png", true},
		{"image/jpeg", true},
		{"audio/wav", true},
		{"audio/mpeg", true},
		{"video/mp4", true},
		{"application/json", false},
		{"text/html", false},
		{"text/plain", false},
		{"image/png; charset=utf-8", true},
	}

	for _, tt := range tests {
		got := IsBinaryContentType(tt.ct, nil)
		if got != tt.want {
			t.Errorf("IsBinaryContentType(%q, nil) = %v, want %v", tt.ct, got, tt.want)
		}
	}
}

func TestIsBinaryContentType_CustomAcceptTypes(t *testing.T) {
	accept := []string{"image/png", "audio/wav"}

	tests := []struct {
		ct   string
		want bool
	}{
		{"image/png", true},
		{"audio/wav", true},
		{"image/jpeg", false}, // not in accept list
		{"video/mp4", false},  // not in accept list
	}

	for _, tt := range tests {
		got := IsBinaryContentType(tt.ct, accept)
		if got != tt.want {
			t.Errorf("IsBinaryContentType(%q, %v) = %v, want %v", tt.ct, accept, got, tt.want)
		}
	}
}

func TestContentTypeToMediaType(t *testing.T) {
	tests := []struct {
		ct   string
		want string
	}{
		{"image/png", "image"},
		{"image/jpeg; charset=utf-8", "image"},
		{"audio/wav", "audio"},
		{"video/mp4", "video"},
		{"application/pdf", "document"},
	}

	for _, tt := range tests {
		got := ContentTypeToMediaType(tt.ct)
		if got != tt.want {
			t.Errorf("ContentTypeToMediaType(%q) = %q, want %q", tt.ct, got, tt.want)
		}
	}
}

func TestReadMultimodalResponse_Success(t *testing.T) {
	imageData := []byte{0x89, 0x50, 0x4E, 0x47} // PNG magic bytes
	resp := &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"image/png"}},
		Body:       io.NopCloser(strings.NewReader(string(imageData))),
	}

	var agg atomic.Int64
	jsonResult, parts, err := ReadMultimodalResponse(resp, &agg, DefaultMaxAggregateSize)
	if err != nil {
		t.Fatal(err)
	}

	// Check JSON summary
	var summary map[string]any
	if err := json.Unmarshal(jsonResult, &summary); err != nil {
		t.Fatal(err)
	}
	if summary["type"] != "image" {
		t.Errorf("expected type=image, got %v", summary["type"])
	}
	if summary["content_type"] != "image/png" {
		t.Errorf("expected content_type=image/png, got %v", summary["content_type"])
	}

	// Check content parts
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	if parts[0].Type != "image" {
		t.Errorf("expected part type=image, got %q", parts[0].Type)
	}
	if parts[0].Media == nil {
		t.Fatal("expected media content")
	}
	if parts[0].Media.MIMEType != "image/png" {
		t.Errorf("expected MIME=image/png, got %q", parts[0].Media.MIMEType)
	}

	// Verify base64 encoding
	decoded, err := base64.StdEncoding.DecodeString(*parts[0].Media.Data)
	if err != nil {
		t.Fatal(err)
	}
	if string(decoded) != string(imageData) {
		t.Error("decoded data does not match original")
	}

	// Check aggregate tracking
	if agg.Load() != int64(len(imageData)) {
		t.Errorf("expected aggregate=%d, got %d", len(imageData), agg.Load())
	}
}

func TestReadMultimodalResponse_HTTPError(t *testing.T) {
	resp := &http.Response{
		StatusCode: 404,
		Header:     http.Header{"Content-Type": []string{"image/png"}},
		Body:       io.NopCloser(strings.NewReader("not found")),
	}

	var agg atomic.Int64
	_, _, err := ReadMultimodalResponse(resp, &agg, DefaultMaxAggregateSize)
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
}

func TestReadMultimodalResponse_AggregateLimitExceeded(t *testing.T) {
	resp := &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"image/png"}},
		Body:       io.NopCloser(strings.NewReader("data")),
	}

	var agg atomic.Int64
	agg.Store(100) // already at limit
	_, _, err := ReadMultimodalResponse(resp, &agg, 100)
	if err == nil {
		t.Fatal("expected aggregate limit error")
	}
}
