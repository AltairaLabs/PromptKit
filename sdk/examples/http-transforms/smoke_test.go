package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// The pre-request pattern only demonstrates anything if its headers reach the
// server, and the path template only if user_id lands in the path.
func TestSmoke_OrdersToolSendsPreRequestHeadersAndRendersPath(t *testing.T) {
	var gotPath, gotQuery string
	var gotHeader http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery, gotHeader = r.URL.Path, r.URL.RawQuery, r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"orders":[]}`))
	}))
	defer srv.Close()

	handler := ordersToolConfig(srv.URL).HandlerCtx()
	for range 2 {
		if _, err := handler(t.Context(), map[string]any{"user_id": "U-42"}); err != nil {
			t.Fatalf("get_orders: %v", err)
		}
	}

	if gotPath != "/users/U-42/orders" {
		t.Errorf("path = %q, want /users/U-42/orders", gotPath)
	}
	if gotQuery != "" {
		t.Errorf("query = %q, want none: user_id is consumed by the path", gotQuery)
	}
	if got := gotHeader.Get("Authorization"); got != "Bearer dynamic-token-here" {
		t.Errorf("Authorization = %q, want the token the pre-request hook sets", got)
	}
	if got := gotHeader.Get("X-Correlation-ID"); got != "req-0002" {
		t.Errorf("X-Correlation-ID = %q, want req-0002 on the second call", got)
	}
}
