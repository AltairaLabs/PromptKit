package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

var toolResponse = map[string]interface{}{
	"order_id": "12345",
	"status":   "shipped",
	"eta":      "2026-04-10",
}

func NewToolHandler(cfg ToolProfile) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/tool", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if cfg.ExecutionDelay > 0 {
			time.Sleep(cfg.ExecutionDelay)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(toolResponse)
	})

	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})

	return mux
}
