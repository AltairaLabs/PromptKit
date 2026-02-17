// Package main demonstrates external orchestration mode for workflows.
//
// In external orchestration mode, state transitions are triggered by outside
// callers (HTTP handlers, message queues, etc.) rather than from within the
// conversation loop. The WorkflowConversation is thread-safe for concurrent
// Send() and Transition() calls from different goroutines.
//
// Usage:
//
//	go run . -pack ./support.pack.json
//
// Then interact via HTTP:
//
//	# Send a message to the current state's conversation
//	curl -X POST localhost:8080/send -d '{"message":"I need help with billing"}'
//
//	# Trigger a state transition
//	curl -X POST localhost:8080/transition -d '{"event":"Escalate"}'
//
//	# Check current state
//	curl localhost:8080/state
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/AltairaLabs/PromptKit/sdk"
)

func main() {
	packPath := flag.String("pack", "", "path to pack file with workflow")
	addr := flag.String("addr", ":8080", "listen address")
	flag.Parse()

	if *packPath == "" {
		log.Fatal("usage: workflow-external -pack <path>")
	}

	wc, err := sdk.OpenWorkflow(*packPath, sdk.WithContextCarryForward())
	if err != nil {
		log.Fatalf("failed to open workflow: %v", err)
	}
	defer wc.Close()

	// GET /state — returns the current workflow state and orchestration mode
	http.HandleFunc("/state", func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"state":         wc.CurrentState(),
			"prompt_task":   wc.CurrentPromptTask(),
			"orchestration": string(wc.OrchestrationMode()),
			"complete":      wc.IsComplete(),
			"events":        wc.AvailableEvents(),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	// POST /send — sends a user message to the active conversation
	http.HandleFunc("/send", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			Message string `json:"message"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		response, err := wc.Send(r.Context(), body.Message)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		resp := map[string]interface{}{
			"response": response.Text(),
			"state":    wc.CurrentState(),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	// POST /transition — triggers a workflow state transition
	http.HandleFunc("/transition", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			Event string `json:"event"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		newState, err := wc.Transition(body.Event)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		resp := map[string]interface{}{
			"state":    newState,
			"complete": wc.IsComplete(),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	fmt.Printf("Workflow server starting on %s (state: %s)\n", *addr, wc.CurrentState())
	log.Fatal(http.ListenAndServe(*addr, nil))
}
