package adaptersdk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/deploy"
)

// fakeProvider is a minimal deploy.Provider for testing.
type fakeProvider struct {
	info      *deploy.ProviderInfo
	planResp  *deploy.PlanResponse
	status    *deploy.StatusResponse
	applyErr  error
	importErr error
}

func (f *fakeProvider) GetProviderInfo(_ context.Context) (*deploy.ProviderInfo, error) {
	return f.info, nil
}

func (f *fakeProvider) ValidateConfig(
	_ context.Context, req *deploy.ValidateRequest,
) (*deploy.ValidateResponse, error) {
	if req.Config == "" {
		return &deploy.ValidateResponse{Valid: false, Errors: []string{"empty config"}}, nil
	}
	return &deploy.ValidateResponse{Valid: true}, nil
}

func (f *fakeProvider) Plan(
	_ context.Context, _ *deploy.PlanRequest,
) (*deploy.PlanResponse, error) {
	return f.planResp, nil
}

func (f *fakeProvider) Apply(
	_ context.Context, _ *deploy.PlanRequest, cb deploy.ApplyCallback,
) (string, error) {
	if f.applyErr != nil {
		return "", f.applyErr
	}
	_ = cb(&deploy.ApplyEvent{Type: "progress", Message: "deploying"})
	return "state-123", nil
}

func (f *fakeProvider) Destroy(
	_ context.Context, _ *deploy.DestroyRequest, cb deploy.DestroyCallback,
) error {
	_ = cb(&deploy.DestroyEvent{Type: "progress", Message: "destroying"})
	return nil
}

func (f *fakeProvider) Status(
	_ context.Context, _ *deploy.StatusRequest,
) (*deploy.StatusResponse, error) {
	return f.status, nil
}

func (f *fakeProvider) Import(
	_ context.Context, req *deploy.ImportRequest,
) (*deploy.ImportResponse, error) {
	if f.importErr != nil {
		return nil, f.importErr
	}
	return &deploy.ImportResponse{
		Resource: deploy.ResourceStatus{
			Type:   req.ResourceType,
			Name:   req.ResourceName,
			Status: "healthy",
		},
		State: "imported-state",
	}, nil
}

func newFakeProvider() *fakeProvider {
	return &fakeProvider{
		info: &deploy.ProviderInfo{
			Name:    "test-adapter",
			Version: "1.0.0",
		},
		planResp: &deploy.PlanResponse{
			Summary: "1 resource to create",
			Changes: []deploy.ResourceChange{
				{Type: "agent_runtime", Name: "main", Action: deploy.ActionCreate},
			},
		},
		status: &deploy.StatusResponse{
			Status: "deployed",
		},
	}
}

func makeRequest(method string, params any, id int) string {
	p, _ := json.Marshal(params)
	req := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  json.RawMessage(p),
		"id":      id,
	}
	b, _ := json.Marshal(req)
	return string(b)
}

func TestServeIO_GetProviderInfo(t *testing.T) {
	provider := newFakeProvider()
	input := makeRequest("get_provider_info", nil, 1) + "\n"
	var out bytes.Buffer

	err := ServeIO(provider, strings.NewReader(input), &out)
	if err != nil {
		t.Fatalf("ServeIO error: %v", err)
	}

	var resp response
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	if resp.JSONRPC != "2.0" {
		t.Errorf("expected jsonrpc 2.0, got %s", resp.JSONRPC)
	}
	result, _ := json.Marshal(resp.Result)
	if !strings.Contains(string(result), "test-adapter") {
		t.Errorf("expected provider name in result, got %s", string(result))
	}
}

func TestServeIO_ValidateConfig(t *testing.T) {
	provider := newFakeProvider()
	input := makeRequest("validate_config", deploy.ValidateRequest{Config: "ok"}, 2) + "\n"
	var out bytes.Buffer

	err := ServeIO(provider, strings.NewReader(input), &out)
	if err != nil {
		t.Fatalf("ServeIO error: %v", err)
	}

	var resp response
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
}

func TestServeIO_Plan(t *testing.T) {
	provider := newFakeProvider()
	params := deploy.PlanRequest{PackJSON: "{}", DeployConfig: "{}"}
	input := makeRequest("plan", params, 3) + "\n"
	var out bytes.Buffer

	err := ServeIO(provider, strings.NewReader(input), &out)
	if err != nil {
		t.Fatalf("ServeIO error: %v", err)
	}

	var resp response
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	result, _ := json.Marshal(resp.Result)
	if !strings.Contains(string(result), "agent_runtime") {
		t.Errorf("expected resource change in result, got %s", string(result))
	}
}

func TestServeIO_Apply(t *testing.T) {
	provider := newFakeProvider()
	params := deploy.PlanRequest{PackJSON: "{}", DeployConfig: "{}"}
	input := makeRequest("apply", params, 4) + "\n"
	var out bytes.Buffer

	err := ServeIO(provider, strings.NewReader(input), &out)
	if err != nil {
		t.Fatalf("ServeIO error: %v", err)
	}

	var resp response
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	result, _ := json.Marshal(resp.Result)
	if !strings.Contains(string(result), "state-123") {
		t.Errorf("expected adapter_state in result, got %s", string(result))
	}
}

func TestServeIO_ApplyError(t *testing.T) {
	provider := newFakeProvider()
	provider.applyErr = fmt.Errorf("deploy failed")
	params := deploy.PlanRequest{PackJSON: "{}", DeployConfig: "{}"}
	input := makeRequest("apply", params, 5) + "\n"
	var out bytes.Buffer

	err := ServeIO(provider, strings.NewReader(input), &out)
	if err != nil {
		t.Fatalf("ServeIO error: %v", err)
	}

	var resp response
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error response")
	}
	if resp.Error.Code != CodeInternalError {
		t.Errorf("expected code %d, got %d", CodeInternalError, resp.Error.Code)
	}
}

func TestServeIO_Destroy(t *testing.T) {
	provider := newFakeProvider()
	params := deploy.DestroyRequest{DeployConfig: "{}"}
	input := makeRequest("destroy", params, 6) + "\n"
	var out bytes.Buffer

	err := ServeIO(provider, strings.NewReader(input), &out)
	if err != nil {
		t.Fatalf("ServeIO error: %v", err)
	}

	var resp response
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
}

func TestServeIO_Status(t *testing.T) {
	provider := newFakeProvider()
	params := deploy.StatusRequest{DeployConfig: "{}"}
	input := makeRequest("status", params, 7) + "\n"
	var out bytes.Buffer

	err := ServeIO(provider, strings.NewReader(input), &out)
	if err != nil {
		t.Fatalf("ServeIO error: %v", err)
	}

	var resp response
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	result, _ := json.Marshal(resp.Result)
	if !strings.Contains(string(result), "deployed") {
		t.Errorf("expected deployed status, got %s", string(result))
	}
}

func TestServeIO_MethodNotFound(t *testing.T) {
	provider := newFakeProvider()
	input := makeRequest("unknown_method", nil, 8) + "\n"
	var out bytes.Buffer

	err := ServeIO(provider, strings.NewReader(input), &out)
	if err != nil {
		t.Fatalf("ServeIO error: %v", err)
	}

	var resp response
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error response")
	}
	if resp.Error.Code != CodeMethodNotFound {
		t.Errorf("expected code %d, got %d", CodeMethodNotFound, resp.Error.Code)
	}
	if !strings.Contains(resp.Error.Message, "unknown_method") {
		t.Errorf("expected method name in error, got %s", resp.Error.Message)
	}
}

func TestServeIO_ParseError(t *testing.T) {
	provider := newFakeProvider()
	input := "not valid json\n"
	var out bytes.Buffer

	err := ServeIO(provider, strings.NewReader(input), &out)
	if err != nil {
		t.Fatalf("ServeIO error: %v", err)
	}

	var resp response
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error response")
	}
	if resp.Error.Code != CodeParseError {
		t.Errorf("expected code %d, got %d", CodeParseError, resp.Error.Code)
	}
}

func TestServeIO_InvalidParams(t *testing.T) {
	provider := newFakeProvider()
	// Send plan with invalid params (string instead of object)
	req := `{"jsonrpc":"2.0","method":"plan","params":"bad","id":9}` + "\n"
	var out bytes.Buffer

	err := ServeIO(provider, strings.NewReader(req), &out)
	if err != nil {
		t.Fatalf("ServeIO error: %v", err)
	}

	var resp response
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error response for invalid params")
	}
	if resp.Error.Code != CodeParseError {
		t.Errorf("expected code %d, got %d", CodeParseError, resp.Error.Code)
	}
}

func TestServeIO_MultipleRequests(t *testing.T) {
	provider := newFakeProvider()
	input := makeRequest("get_provider_info", nil, 1) + "\n" +
		makeRequest("status", deploy.StatusRequest{DeployConfig: "{}"}, 2) + "\n"
	var out bytes.Buffer

	err := ServeIO(provider, strings.NewReader(input), &out)
	if err != nil {
		t.Fatalf("ServeIO error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 response lines, got %d", len(lines))
	}

	for i, line := range lines {
		var resp response
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			t.Fatalf("line %d: unmarshal error: %v", i, err)
		}
		if resp.Error != nil {
			t.Errorf("line %d: unexpected error: %+v", i, resp.Error)
		}
	}
}

func TestServeIO_EmptyLines(t *testing.T) {
	provider := newFakeProvider()
	input := "\n\n" + makeRequest("get_provider_info", nil, 1) + "\n\n"
	var out bytes.Buffer

	err := ServeIO(provider, strings.NewReader(input), &out)
	if err != nil {
		t.Fatalf("ServeIO error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 response line, got %d", len(lines))
	}
}

func TestServeIO_NilReaderWriter(t *testing.T) {
	provider := newFakeProvider()
	err := ServeIO(provider, nil, nil)
	if err == nil {
		t.Fatal("expected error for nil reader/writer")
	}
}

func TestServeIO_Import(t *testing.T) {
	provider := newFakeProvider()
	params := deploy.ImportRequest{
		ResourceType: "agent_runtime",
		ResourceName: "my-agent",
		Identifier:   "container-abc",
		DeployConfig: "{}",
	}
	input := makeRequest("import", params, 10) + "\n"
	var out bytes.Buffer

	err := ServeIO(provider, strings.NewReader(input), &out)
	if err != nil {
		t.Fatalf("ServeIO error: %v", err)
	}

	var resp response
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	result, _ := json.Marshal(resp.Result)
	if !strings.Contains(string(result), "imported-state") {
		t.Errorf("expected imported-state in result, got %s", string(result))
	}
	if !strings.Contains(string(result), "my-agent") {
		t.Errorf("expected resource name in result, got %s", string(result))
	}
}

func TestServeIO_Import_InvalidParams(t *testing.T) {
	provider := newFakeProvider()
	req := `{"jsonrpc":"2.0","method":"import","params":"bad","id":11}` + "\n"
	var out bytes.Buffer

	err := ServeIO(provider, strings.NewReader(req), &out)
	if err != nil {
		t.Fatalf("ServeIO error: %v", err)
	}

	var resp response
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error response for invalid params")
	}
	if resp.Error.Code != CodeParseError {
		t.Errorf("expected code %d, got %d", CodeParseError, resp.Error.Code)
	}
}

func TestServe_ReturnsErrorForNilIO(t *testing.T) {
	// Serve() calls ServeIO with nil, which should return error.
	// We can't easily test Serve() itself since it uses os.Stdin/Stdout,
	// but we verify ServeIO nil handling.
	err := ServeIO(newFakeProvider(), nil, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error for nil reader")
	}
}
