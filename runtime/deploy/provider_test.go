package deploy

import (
	"encoding/json"
	"testing"
)

func TestActionConstants(t *testing.T) {
	tests := []struct {
		action Action
		want   string
	}{
		{ActionCreate, "CREATE"},
		{ActionUpdate, "UPDATE"},
		{ActionDelete, "DELETE"},
		{ActionNoChange, "NO_CHANGE"},
		{ActionDrift, "DRIFT"},
	}

	for _, tt := range tests {
		if string(tt.action) != tt.want {
			t.Errorf("Action = %q, want %q", tt.action, tt.want)
		}
	}
}

func TestProviderInfoJSONRoundtrip(t *testing.T) {
	info := ProviderInfo{
		Name:         "docker-compose",
		Version:      "0.1.0",
		Capabilities: []string{"plan", "apply", "destroy", "status"},
		ConfigSchema: `{"type":"object"}`,
	}

	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got ProviderInfo
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got.Name != info.Name {
		t.Errorf("Name = %q, want %q", got.Name, info.Name)
	}
	if got.Version != info.Version {
		t.Errorf("Version = %q, want %q", got.Version, info.Version)
	}
	if len(got.Capabilities) != len(info.Capabilities) {
		t.Errorf("Capabilities len = %d, want %d", len(got.Capabilities), len(info.Capabilities))
	}
	if got.ConfigSchema != info.ConfigSchema {
		t.Errorf("ConfigSchema = %q, want %q", got.ConfigSchema, info.ConfigSchema)
	}
}

func TestPlanResponseJSONRoundtrip(t *testing.T) {
	resp := PlanResponse{
		Changes: []ResourceChange{
			{
				Type:   "agent_runtime",
				Name:   "my-agent",
				Action: ActionCreate,
				Detail: "New container will be created",
			},
			{
				Type:   "a2a_endpoint",
				Name:   "my-agent-a2a",
				Action: ActionUpdate,
				Detail: "Port mapping changed",
			},
			{
				Type:   "volume",
				Name:   "data-vol",
				Action: ActionNoChange,
			},
		},
		Summary: "2 changes, 1 unchanged",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got PlanResponse
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if len(got.Changes) != 3 {
		t.Fatalf("Changes len = %d, want 3", len(got.Changes))
	}
	if got.Changes[0].Action != ActionCreate {
		t.Errorf("Changes[0].Action = %q, want %q", got.Changes[0].Action, ActionCreate)
	}
	if got.Changes[1].Action != ActionUpdate {
		t.Errorf("Changes[1].Action = %q, want %q", got.Changes[1].Action, ActionUpdate)
	}
	if got.Changes[2].Action != ActionNoChange {
		t.Errorf("Changes[2].Action = %q, want %q", got.Changes[2].Action, ActionNoChange)
	}
	if got.Summary != resp.Summary {
		t.Errorf("Summary = %q, want %q", got.Summary, resp.Summary)
	}
}

func TestApplyEventTypes(t *testing.T) {
	tests := []struct {
		name  string
		event ApplyEvent
	}{
		{
			name: "progress event",
			event: ApplyEvent{
				Type:    "progress",
				Message: "Deploying agent runtime...",
			},
		},
		{
			name: "resource event",
			event: ApplyEvent{
				Type: "resource",
				Resource: &ResourceResult{
					Type:   "agent_runtime",
					Name:   "my-agent",
					Action: ActionCreate,
					Status: "created",
				},
			},
		},
		{
			name: "error event",
			event: ApplyEvent{
				Type:    "error",
				Message: "Failed to create container",
			},
		},
		{
			name: "complete event",
			event: ApplyEvent{
				Type:    "complete",
				Message: "Deployment finished",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.event)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}

			var got ApplyEvent
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}

			if got.Type != tt.event.Type {
				t.Errorf("Type = %q, want %q", got.Type, tt.event.Type)
			}
			if got.Message != tt.event.Message {
				t.Errorf("Message = %q, want %q", got.Message, tt.event.Message)
			}
			if tt.event.Resource != nil {
				if got.Resource == nil {
					t.Fatal("Resource is nil, want non-nil")
				}
				if got.Resource.Status != tt.event.Resource.Status {
					t.Errorf("Resource.Status = %q, want %q", got.Resource.Status, tt.event.Resource.Status)
				}
			}
		})
	}
}

func TestImportRequestJSONRoundtrip(t *testing.T) {
	req := ImportRequest{
		ResourceType: "agent_runtime",
		ResourceName: "my-agent",
		Identifier:   "container-abc123",
		DeployConfig: `{"region":"us-east-1"}`,
		Environment:  "production",
		PriorState:   "prior-state-data",
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got ImportRequest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got.ResourceType != req.ResourceType {
		t.Errorf("ResourceType = %q, want %q", got.ResourceType, req.ResourceType)
	}
	if got.ResourceName != req.ResourceName {
		t.Errorf("ResourceName = %q, want %q", got.ResourceName, req.ResourceName)
	}
	if got.Identifier != req.Identifier {
		t.Errorf("Identifier = %q, want %q", got.Identifier, req.Identifier)
	}
	if got.DeployConfig != req.DeployConfig {
		t.Errorf("DeployConfig = %q, want %q", got.DeployConfig, req.DeployConfig)
	}
	if got.Environment != req.Environment {
		t.Errorf("Environment = %q, want %q", got.Environment, req.Environment)
	}
	if got.PriorState != req.PriorState {
		t.Errorf("PriorState = %q, want %q", got.PriorState, req.PriorState)
	}
}

func TestImportResponseJSONRoundtrip(t *testing.T) {
	resp := ImportResponse{
		Resource: ResourceStatus{
			Type:   "agent_runtime",
			Name:   "my-agent",
			Status: "healthy",
			Detail: "Running on port 8080",
		},
		State: "new-adapter-state",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got ImportResponse
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got.Resource.Type != resp.Resource.Type {
		t.Errorf("Resource.Type = %q, want %q", got.Resource.Type, resp.Resource.Type)
	}
	if got.Resource.Name != resp.Resource.Name {
		t.Errorf("Resource.Name = %q, want %q", got.Resource.Name, resp.Resource.Name)
	}
	if got.Resource.Status != resp.Resource.Status {
		t.Errorf("Resource.Status = %q, want %q", got.Resource.Status, resp.Resource.Status)
	}
	if got.Resource.Detail != resp.Resource.Detail {
		t.Errorf("Resource.Detail = %q, want %q", got.Resource.Detail, resp.Resource.Detail)
	}
	if got.State != resp.State {
		t.Errorf("State = %q, want %q", got.State, resp.State)
	}
}

func TestStatusResponseJSONRoundtrip(t *testing.T) {
	resp := StatusResponse{
		Status: "deployed",
		Resources: []ResourceStatus{
			{
				Type:   "agent_runtime",
				Name:   "my-agent",
				Status: "healthy",
			},
			{
				Type:   "a2a_endpoint",
				Name:   "my-agent-a2a",
				Status: "unhealthy",
				Detail: "Connection refused on port 8080",
			},
		},
		State: "base64-opaque-state",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got StatusResponse
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got.Status != "deployed" {
		t.Errorf("Status = %q, want %q", got.Status, "deployed")
	}
	if len(got.Resources) != 2 {
		t.Fatalf("Resources len = %d, want 2", len(got.Resources))
	}
	if got.Resources[0].Status != "healthy" {
		t.Errorf("Resources[0].Status = %q, want %q", got.Resources[0].Status, "healthy")
	}
	if got.Resources[1].Detail != "Connection refused on port 8080" {
		t.Errorf("Resources[1].Detail = %q, want %q", got.Resources[1].Detail, "Connection refused on port 8080")
	}
	if got.State != "base64-opaque-state" {
		t.Errorf("State = %q, want %q", got.State, "base64-opaque-state")
	}
}
