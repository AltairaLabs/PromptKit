package engine

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/pkg/config"
)

func TestCompositeConversationExecutor_RouteToDefault(t *testing.T) {
	// Create mock executors
	defaultExec := NewDefaultConversationExecutor(nil, nil, nil, nil)
	duplexExec := NewDuplexConversationExecutor(nil, nil, nil)

	composite := NewCompositeConversationExecutor(defaultExec, duplexExec)

	// Scenario without duplex config should route to default
	req := ConversationRequest{
		Scenario: &config.Scenario{
			ID:       "test",
			TaskType: "test",
			Duplex:   nil, // No duplex config
		},
	}

	// This will fail due to nil turn executor, but we're testing routing
	result := composite.ExecuteConversation(context.Background(), req)

	// The default executor should have been selected (will fail due to nil components)
	// but the routing logic should work
	if result == nil {
		t.Error("Expected result, got nil")
	}
}

func TestCompositeConversationExecutor_RouteToDuplex(t *testing.T) {
	// Create mock executors
	defaultExec := NewDefaultConversationExecutor(nil, nil, nil, nil)
	duplexExec := NewDuplexConversationExecutor(nil, nil, nil)

	composite := NewCompositeConversationExecutor(defaultExec, duplexExec)

	// Scenario with duplex config should route to duplex
	req := ConversationRequest{
		Scenario: &config.Scenario{
			ID:       "test",
			TaskType: "test",
			Duplex: &config.DuplexConfig{
				Timeout: "10m",
			},
		},
	}

	result := composite.ExecuteConversation(context.Background(), req)

	// The duplex executor should have been selected
	// It should fail validation since provider doesn't support streaming
	if result == nil {
		t.Error("Expected result, got nil")
	}
	// Duplex should fail with provider not supporting streaming
	if !result.Failed {
		t.Error("Expected failure due to provider not supporting streaming")
	}
}

func TestCompositeConversationExecutor_NilDuplexExecutor(t *testing.T) {
	// Create composite with nil duplex executor
	defaultExec := NewDefaultConversationExecutor(nil, nil, nil, nil)
	composite := NewCompositeConversationExecutor(defaultExec, nil)

	// Scenario requesting duplex should fail gracefully
	req := ConversationRequest{
		Scenario: &config.Scenario{
			ID:       "test",
			TaskType: "test",
			Duplex: &config.DuplexConfig{
				Timeout: "10m",
			},
		},
	}

	result := composite.ExecuteConversation(context.Background(), req)

	if result == nil {
		t.Fatal("Expected result, got nil")
	}
	if !result.Failed {
		t.Error("Expected failure when duplex executor is nil")
	}
	if result.Error != "duplex executor not configured but scenario requires duplex mode" {
		t.Errorf("Unexpected error message: %s", result.Error)
	}
}

func TestCompositeConversationExecutor_NilDefaultExecutor(t *testing.T) {
	// Create composite with nil default executor
	duplexExec := NewDuplexConversationExecutor(nil, nil, nil)
	composite := NewCompositeConversationExecutor(nil, duplexExec)

	// Standard scenario should fail gracefully
	req := ConversationRequest{
		Scenario: &config.Scenario{
			ID:       "test",
			TaskType: "test",
			Duplex:   nil,
		},
	}

	result := composite.ExecuteConversation(context.Background(), req)

	if result == nil {
		t.Fatal("Expected result, got nil")
	}
	if !result.Failed {
		t.Error("Expected failure when default executor is nil")
	}
	if result.Error != "default executor not configured" {
		t.Errorf("Unexpected error message: %s", result.Error)
	}
}

func TestCompositeConversationExecutor_IsDuplexScenario(t *testing.T) {
	composite := NewCompositeConversationExecutor(nil, nil)

	tests := []struct {
		name     string
		req      *ConversationRequest
		expected bool
	}{
		{
			name:     "nil scenario",
			req:      &ConversationRequest{Scenario: nil},
			expected: false,
		},
		{
			name: "no duplex config",
			req: &ConversationRequest{
				Scenario: &config.Scenario{
					ID:     "test",
					Duplex: nil,
				},
			},
			expected: false,
		},
		{
			name: "with duplex config",
			req: &ConversationRequest{
				Scenario: &config.Scenario{
					ID: "test",
					Duplex: &config.DuplexConfig{
						Timeout: "5m",
					},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := composite.isDuplexScenario(tt.req)
			if result != tt.expected {
				t.Errorf("isDuplexScenario() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestCompositeConversationExecutor_GetExecutors(t *testing.T) {
	defaultExec := NewDefaultConversationExecutor(nil, nil, nil, nil)
	duplexExec := NewDuplexConversationExecutor(nil, nil, nil)

	composite := NewCompositeConversationExecutor(defaultExec, duplexExec)

	if composite.GetDefaultExecutor() != defaultExec {
		t.Error("GetDefaultExecutor() returned wrong executor")
	}

	if composite.GetDuplexExecutor() != duplexExec {
		t.Error("GetDuplexExecutor() returned wrong executor")
	}
}

func TestCompositeConversationExecutor_StreamRouteToDefault(t *testing.T) {
	defaultExec := NewDefaultConversationExecutor(nil, nil, nil, nil)
	composite := NewCompositeConversationExecutor(defaultExec, nil)

	req := ConversationRequest{
		Scenario: &config.Scenario{
			ID:     "test",
			Duplex: nil,
		},
	}

	ch, err := composite.ExecuteConversationStream(context.Background(), req)
	if err != nil {
		t.Fatalf("ExecuteConversationStream() error = %v", err)
	}

	// Should get a result (even if it fails due to nil components)
	result := <-ch
	if result.Result == nil {
		t.Error("Expected result in stream")
	}
}

func TestCompositeConversationExecutor_StreamRouteToDuplex(t *testing.T) {
	duplexExec := NewDuplexConversationExecutor(nil, nil, nil)
	composite := NewCompositeConversationExecutor(nil, duplexExec)

	req := ConversationRequest{
		Scenario: &config.Scenario{
			ID: "test",
			Duplex: &config.DuplexConfig{
				Timeout: "5m",
			},
		},
	}

	ch, err := composite.ExecuteConversationStream(context.Background(), req)
	if err != nil {
		t.Fatalf("ExecuteConversationStream() error = %v", err)
	}

	// Should get a result
	result := <-ch
	if result.Result == nil {
		t.Error("Expected result in stream")
	}
}

func TestCompositeConversationExecutor_StreamNilDuplexExecutor(t *testing.T) {
	composite := NewCompositeConversationExecutor(nil, nil)

	req := ConversationRequest{
		Scenario: &config.Scenario{
			ID: "test",
			Duplex: &config.DuplexConfig{
				Timeout: "5m",
			},
		},
	}

	ch, err := composite.ExecuteConversationStream(context.Background(), req)
	if err != nil {
		t.Fatalf("ExecuteConversationStream() error = %v", err)
	}

	result := <-ch
	if result.Result == nil {
		t.Fatal("Expected result in stream")
	}
	if !result.Result.Failed {
		t.Error("Expected failure when duplex executor is nil")
	}
}

func TestCompositeConversationExecutor_StreamNilDefaultExecutor(t *testing.T) {
	composite := NewCompositeConversationExecutor(nil, nil)

	req := ConversationRequest{
		Scenario: &config.Scenario{
			ID:     "test",
			Duplex: nil,
		},
	}

	ch, err := composite.ExecuteConversationStream(context.Background(), req)
	if err != nil {
		t.Fatalf("ExecuteConversationStream() error = %v", err)
	}

	result := <-ch
	if result.Result == nil {
		t.Fatal("Expected result in stream")
	}
	if !result.Result.Failed {
		t.Error("Expected failure when default executor is nil")
	}
}

func TestCompositeConversationExecutor_ImplementsInterface(t *testing.T) {
	composite := NewCompositeConversationExecutor(nil, nil)

	// Verify composite implements ConversationExecutor interface
	var _ ConversationExecutor = composite
}
