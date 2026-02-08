package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/a2a"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

// a2aExecutor implements tools.Executor for A2A agent tools within Arena.
// It dispatches tool calls to remote A2A agents via the A2A client.
type a2aExecutor struct {
	mu      sync.RWMutex
	clients map[string]*a2a.Client
}

// Name returns "a2a" to match the Mode on A2A tool descriptors.
func (e *a2aExecutor) Name() string { return "a2a" }

// Execute calls a remote A2A agent with the tool arguments and returns the response.
func (e *a2aExecutor) Execute(descriptor *tools.ToolDescriptor, args json.RawMessage) (json.RawMessage, error) {
	if descriptor.A2AConfig == nil {
		return nil, fmt.Errorf("a2a executor: tool %q has no A2AConfig", descriptor.Name)
	}

	cfg := descriptor.A2AConfig
	client := e.getOrCreateClient(cfg.AgentURL)

	// Parse arguments.
	var input struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, fmt.Errorf("a2a executor: parse args: %w", err)
	}

	// Build message with skill metadata for mock server routing.
	text := input.Query
	parts := []a2a.Part{{Text: &text}}

	req := &a2a.SendMessageRequest{
		Message: a2a.Message{
			Role:  a2a.RoleUser,
			Parts: parts,
			Metadata: map[string]any{
				"skillId": cfg.SkillID,
			},
		},
	}

	// Apply timeout.
	ctx := context.Background()
	if cfg.TimeoutMs > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(cfg.TimeoutMs)*time.Millisecond)
		defer cancel()
	}

	task, err := client.SendMessage(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("a2a executor: send message: %w", err)
	}

	responseText := extractA2AResponseText(task)
	result := map[string]string{"response": responseText}
	return json.Marshal(result)
}

// getOrCreateClient returns a cached client or creates a new one.
func (e *a2aExecutor) getOrCreateClient(agentURL string) *a2a.Client {
	e.mu.RLock()
	if c, ok := e.clients[agentURL]; ok {
		e.mu.RUnlock()
		return c
	}
	e.mu.RUnlock()

	e.mu.Lock()
	defer e.mu.Unlock()
	if c, ok := e.clients[agentURL]; ok {
		return c
	}
	if e.clients == nil {
		e.clients = make(map[string]*a2a.Client)
	}
	c := a2a.NewClient(agentURL)
	e.clients[agentURL] = c
	return c
}

// extractA2AResponseText extracts text from a completed A2A task.
// It checks the status message first, then artifacts.
func extractA2AResponseText(task *a2a.Task) string {
	if task.Status.Message != nil {
		for _, part := range task.Status.Message.Parts {
			if part.Text != nil {
				return *part.Text
			}
		}
	}

	var texts []string
	for _, artifact := range task.Artifacts {
		for _, part := range artifact.Parts {
			if part.Text != nil {
				texts = append(texts, *part.Text)
			}
		}
	}
	if len(texts) > 0 {
		return strings.Join(texts, "\n")
	}

	return ""
}
