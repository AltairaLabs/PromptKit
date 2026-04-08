package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestToolEndpoint_ReturnsJSON(t *testing.T) {
	handler := NewToolHandler(ToolProfile{ExecutionDelay: 0})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/tool", "application/json",
		strings.NewReader(`{"order_id":"12345"}`))
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result["order_id"] != "12345" {
		t.Errorf("order_id = %v, want 12345", result["order_id"])
	}
	if result["status"] != "shipped" {
		t.Errorf("status = %v, want shipped", result["status"])
	}
}

func TestToolEndpoint_RespectsDelay(t *testing.T) {
	handler := NewToolHandler(ToolProfile{ExecutionDelay: 100 * time.Millisecond})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	start := time.Now()
	resp, err := http.Post(srv.URL+"/tool", "application/json",
		strings.NewReader(`{"order_id":"1"}`))
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	resp.Body.Close()
	elapsed := time.Since(start)

	if elapsed < 90*time.Millisecond {
		t.Errorf("elapsed = %v, want >= 90ms (delay=100ms)", elapsed)
	}
}

func TestToolEndpoint_Health(t *testing.T) {
	handler := NewToolHandler(ToolProfile{ExecutionDelay: 0})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}
