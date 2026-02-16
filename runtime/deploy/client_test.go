package deploy

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"testing"
)

// mockServer reads JSON-RPC requests from r and writes responses to w,
// simulating an adapter binary without importing adaptersdk (which would
// cause an import cycle).
func mockServer(r io.Reader, w io.Writer, handler func(method string, params json.RawMessage) (any, *rpcError)) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), maxScanSize)
	enc := json.NewEncoder(w)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var req struct {
			JSONRPC string          `json:"jsonrpc"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params,omitempty"`
			ID      int             `json:"id"`
		}
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}

		result, rpcErr := handler(req.Method, req.Params)

		resp := struct {
			JSONRPC string   `json:"jsonrpc"`
			Result  any      `json:"result,omitempty"`
			Error   *rpcError `json:"error,omitempty"`
			ID      int      `json:"id"`
		}{
			JSONRPC: "2.0",
			Result:  result,
			Error:   rpcErr,
			ID:      req.ID,
		}
		_ = enc.Encode(resp)
	}
}

// defaultHandler responds to all standard adapter methods.
func defaultHandler(method string, params json.RawMessage) (any, *rpcError) {
	switch method {
	case methodProviderInfo:
		return &ProviderInfo{
			Name:         "test",
			Version:      "0.1.0",
			Capabilities: []string{"plan", "apply"},
		}, nil

	case methodValidate:
		var req ValidateRequest
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, &rpcError{Code: -32700, Message: err.Error()}
		}
		if req.Config == "" {
			return &ValidateResponse{Valid: false, Errors: []string{"empty config"}}, nil
		}
		return &ValidateResponse{Valid: true}, nil

	case methodPlan:
		return &PlanResponse{
			Summary: "2 resources to create",
			Changes: []ResourceChange{
				{Type: "agent_runtime", Name: "agent1", Action: ActionCreate},
				{Type: "a2a_endpoint", Name: "ep1", Action: ActionCreate},
			},
		}, nil

	case methodApply:
		return &applyResult{AdapterState: "state-abc"}, nil

	case methodDestroy:
		return map[string]string{"status": "destroyed"}, nil

	case methodStatus:
		return &StatusResponse{
			Status: "deployed",
			Resources: []ResourceStatus{
				{Type: "agent_runtime", Name: "agent1", Status: "healthy"},
			},
		}, nil

	default:
		return nil, &rpcError{Code: -32601, Message: "method not found: " + method}
	}
}

// startTestClient creates a client ↔ mock server pair connected via pipes.
func startTestClient(t *testing.T, handler func(string, json.RawMessage) (any, *rpcError)) *AdapterClient {
	t.Helper()

	// clientWriter → serverReader (client sends requests)
	serverReader, clientWriter := io.Pipe()
	// serverWriter → clientReader (server sends responses)
	clientReader, serverWriter := io.Pipe()

	go func() {
		mockServer(serverReader, serverWriter, handler)
		serverWriter.Close()
	}()

	client := NewAdapterClientIO(clientReader, clientWriter)
	t.Cleanup(func() { client.Close() })
	return client
}

func TestClientGetProviderInfo(t *testing.T) {
	client := startTestClient(t, defaultHandler)
	ctx := context.Background()

	info, err := client.GetProviderInfo(ctx)
	if err != nil {
		t.Fatalf("GetProviderInfo: %v", err)
	}
	if info.Name != "test" {
		t.Errorf("Name = %q, want %q", info.Name, "test")
	}
	if info.Version != "0.1.0" {
		t.Errorf("Version = %q, want %q", info.Version, "0.1.0")
	}
	if len(info.Capabilities) != 2 {
		t.Errorf("Capabilities = %v, want 2 items", info.Capabilities)
	}
}

func TestClientValidateConfig(t *testing.T) {
	client := startTestClient(t, defaultHandler)
	ctx := context.Background()

	resp, err := client.ValidateConfig(ctx, &ValidateRequest{Config: `{"region":"us-east-1"}`})
	if err != nil {
		t.Fatalf("ValidateConfig: %v", err)
	}
	if !resp.Valid {
		t.Error("expected valid config")
	}

	resp, err = client.ValidateConfig(ctx, &ValidateRequest{Config: ""})
	if err != nil {
		t.Fatalf("ValidateConfig: %v", err)
	}
	if resp.Valid {
		t.Error("expected invalid config for empty string")
	}
	if len(resp.Errors) == 0 {
		t.Error("expected validation errors")
	}
}

func TestClientPlan(t *testing.T) {
	client := startTestClient(t, defaultHandler)
	ctx := context.Background()

	plan, err := client.Plan(ctx, &PlanRequest{
		PackJSON:     `{"name":"test"}`,
		DeployConfig: `{"region":"us-east-1"}`,
		Environment:  "staging",
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.Summary != "2 resources to create" {
		t.Errorf("Summary = %q", plan.Summary)
	}
	if len(plan.Changes) != 2 {
		t.Fatalf("Changes len = %d, want 2", len(plan.Changes))
	}
	if plan.Changes[0].Action != ActionCreate {
		t.Errorf("Changes[0].Action = %q, want CREATE", plan.Changes[0].Action)
	}
}

func TestClientApply(t *testing.T) {
	client := startTestClient(t, defaultHandler)
	ctx := context.Background()

	state, err := client.Apply(ctx, &PlanRequest{
		PackJSON:     `{"name":"test"}`,
		DeployConfig: `{"region":"us-east-1"}`,
	}, nil)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if state != "state-abc" {
		t.Errorf("state = %q, want %q", state, "state-abc")
	}
}

func TestClientDestroy(t *testing.T) {
	client := startTestClient(t, defaultHandler)
	ctx := context.Background()

	err := client.Destroy(ctx, &DestroyRequest{
		DeployConfig: `{"region":"us-east-1"}`,
		Environment:  "staging",
	}, nil)
	if err != nil {
		t.Fatalf("Destroy: %v", err)
	}
}

func TestClientStatus(t *testing.T) {
	client := startTestClient(t, defaultHandler)
	ctx := context.Background()

	status, err := client.Status(ctx, &StatusRequest{
		DeployConfig: `{"region":"us-east-1"}`,
	})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.Status != "deployed" {
		t.Errorf("Status = %q, want %q", status.Status, "deployed")
	}
	if len(status.Resources) != 1 {
		t.Fatalf("Resources len = %d, want 1", len(status.Resources))
	}
	if status.Resources[0].Status != "healthy" {
		t.Errorf("Resources[0].Status = %q", status.Resources[0].Status)
	}
}

func TestClientMultipleCalls(t *testing.T) {
	client := startTestClient(t, defaultHandler)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		info, err := client.GetProviderInfo(ctx)
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if info.Name != "test" {
			t.Fatalf("call %d: Name = %q", i, info.Name)
		}
	}
}

func TestClientCloseIdempotent(t *testing.T) {
	client := startTestClient(t, defaultHandler)

	if err := client.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestClientCallAfterClose(t *testing.T) {
	client := startTestClient(t, defaultHandler)
	_ = client.Close()

	_, err := client.GetProviderInfo(context.Background())
	if err == nil {
		t.Fatal("expected error calling closed client")
	}
}

func TestClientAdapterError(t *testing.T) {
	client := startTestClient(t, defaultHandler)

	// Call a method that doesn't exist to trigger an error response.
	var result json.RawMessage
	err := client.call("nonexistent", nil, &result)
	if err == nil {
		t.Fatal("expected error for unknown method")
	}
	rpcErr, ok := err.(*rpcError)
	if !ok {
		t.Fatalf("expected *rpcError, got %T", err)
	}
	if rpcErr.Code != -32601 {
		t.Errorf("error code = %d, want -32601", rpcErr.Code)
	}
}

func TestNewAdapterClient_BinaryNotFound(t *testing.T) {
	_, err := NewAdapterClient("/nonexistent/binary/path")
	if err == nil {
		t.Fatal("expected error for nonexistent binary")
	}
}

func TestNewAdapterClient_WithCatBinary(t *testing.T) {
	// Use 'cat' as a real subprocess that echoes stdin to stdout.
	// This tests the full newAdapterClient path with real pipes.
	cmd := exec.CommandContext(context.Background(), "cat")
	client, err := newAdapterClient(cmd)
	if err != nil {
		t.Fatalf("newAdapterClient: %v", err)
	}
	defer client.Close()

	// Write a valid JSON-RPC response line to stdin; cat echoes it to stdout.
	resp := `{"jsonrpc":"2.0","result":{"name":"cat-test","version":"1.0.0"},"id":1}`
	_, err = client.stdin.Write([]byte(resp + "\n"))
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	// Read the echoed response.
	if !client.stdout.Scan() {
		t.Fatal("expected scan to succeed")
	}
	if string(client.stdout.Bytes()) != resp {
		t.Errorf("echoed = %q, want %q", client.stdout.Bytes(), resp)
	}
}

func TestRPCError_Error(t *testing.T) {
	e := &rpcError{Code: -32601, Message: "method not found"}
	got := e.Error()
	want := "adapter error -32601: method not found"
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}
