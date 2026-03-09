package tools

import (
	"encoding/json"
	"testing"
)

func TestRenderURL_NoTemplate(t *testing.T) {
	m := &DefaultRequestMapper{}
	got, err := m.RenderURL("https://api.example.com/v1/items", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://api.example.com/v1/items" {
		t.Errorf("got %q", got)
	}
}

func TestRenderURL_WithTemplate(t *testing.T) {
	m := &DefaultRequestMapper{}
	args := map[string]any{"user_id": "abc123", "repo": "myrepo"}
	got, err := m.RenderURL("https://api.example.com/users/{{.user_id}}/repos/{{.repo}}", args)
	if err != nil {
		t.Fatal(err)
	}
	want := "https://api.example.com/users/abc123/repos/myrepo"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderURL_MissingVar(t *testing.T) {
	m := &DefaultRequestMapper{}
	args := map[string]any{"user_id": "abc123"}
	// missingkey=zero renders missing vars as "<no value>"
	got, err := m.RenderURL("https://api.example.com/users/{{.user_id}}/repos/{{.repo}}", args)
	if err != nil {
		t.Fatal(err)
	}
	want := "https://api.example.com/users/abc123/repos/<no value>"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPartitionArgs_NilConfig(t *testing.T) {
	m := &DefaultRequestMapper{}
	args := map[string]any{"a": 1, "b": 2}
	q, h, b := m.PartitionArgs(args, nil, "")
	if q != nil || h != nil {
		t.Error("expected nil query and header")
	}
	if len(b) != 2 {
		t.Errorf("expected 2 body args, got %d", len(b))
	}
}

func TestPartitionArgs_WithQueryParams(t *testing.T) {
	m := &DefaultRequestMapper{}
	cfg := &RequestMapping{
		QueryParams: []string{"page", "limit"},
	}
	args := map[string]any{"page": 1, "limit": 10, "name": "test"}
	q, _, b := m.PartitionArgs(args, cfg, "")

	if len(q) != 2 {
		t.Errorf("expected 2 query params, got %d", len(q))
	}
	if len(b) != 1 {
		t.Errorf("expected 1 body arg, got %d", len(b))
	}
	if b["name"] != "test" {
		t.Errorf("expected body[name]=test, got %v", b["name"])
	}
}

func TestPartitionArgs_URLTemplateExclusion(t *testing.T) {
	m := &DefaultRequestMapper{}
	cfg := &RequestMapping{}
	args := map[string]any{"user_id": "abc", "name": "test"}
	_, _, b := m.PartitionArgs(args, cfg, "https://api.example.com/users/{{.user_id}}")

	if _, ok := b["user_id"]; ok {
		t.Error("user_id should be excluded from body (consumed by URL template)")
	}
	if b["name"] != "test" {
		t.Errorf("expected body[name]=test, got %v", b["name"])
	}
}

func TestPartitionArgs_HeaderTemplateExclusion(t *testing.T) {
	m := &DefaultRequestMapper{}
	cfg := &RequestMapping{
		HeaderParams: map[string]string{
			"Authorization": "Bearer {{.token}}",
		},
	}
	args := map[string]any{"token": "secret", "data": "value"}
	_, _, b := m.PartitionArgs(args, cfg, "")

	if _, ok := b["token"]; ok {
		t.Error("token should be excluded from body (consumed by header template)")
	}
	if b["data"] != "value" {
		t.Errorf("expected body[data]=value, got %v", b["data"])
	}
}

func TestPartitionArgs_ExplicitExclude(t *testing.T) {
	m := &DefaultRequestMapper{}
	cfg := &RequestMapping{
		Exclude: []string{"internal_id"},
	}
	args := map[string]any{"internal_id": "xyz", "name": "test"}
	_, _, b := m.PartitionArgs(args, cfg, "")

	if _, ok := b["internal_id"]; ok {
		t.Error("internal_id should be excluded")
	}
}

func TestBuildBody_Empty(t *testing.T) {
	m := &DefaultRequestMapper{}
	result, err := m.BuildBody(nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Error("expected nil for empty body args")
	}
}

func TestBuildBody_NoJMESPath(t *testing.T) {
	m := &DefaultRequestMapper{}
	args := map[string]any{"name": "test", "value": 42}
	result, err := m.BuildBody(args, "")
	if err != nil {
		t.Fatal(err)
	}

	var got map[string]any
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatal(err)
	}
	if got["name"] != "test" {
		t.Errorf("expected name=test, got %v", got["name"])
	}
}

func TestBuildBody_WithJMESPath(t *testing.T) {
	m := &DefaultRequestMapper{}
	args := map[string]any{
		"first_name": "John",
		"last_name":  "Doe",
		"age":        float64(30),
	}
	// Reshape: pick only first_name and last_name
	result, err := m.BuildBody(args, "{name: first_name, surname: last_name}")
	if err != nil {
		t.Fatal(err)
	}

	var got map[string]any
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatal(err)
	}
	if got["name"] != "John" {
		t.Errorf("expected name=John, got %v", got["name"])
	}
	if got["surname"] != "Doe" {
		t.Errorf("expected surname=Doe, got %v", got["surname"])
	}
	if _, ok := got["age"]; ok {
		t.Error("age should not be in reshaped body")
	}
}

func TestRenderHeaders_Empty(t *testing.T) {
	m := &DefaultRequestMapper{}
	result, err := m.RenderHeaders(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Error("expected nil for empty templates")
	}
}

func TestRenderHeaders_StaticValue(t *testing.T) {
	m := &DefaultRequestMapper{}
	templates := map[string]string{
		"Content-Type": "application/json",
	}
	result, err := m.RenderHeaders(templates, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result["Content-Type"] != "application/json" {
		t.Errorf("got %q", result["Content-Type"])
	}
}

func TestRenderHeaders_WithTemplate(t *testing.T) {
	m := &DefaultRequestMapper{}
	templates := map[string]string{
		"Authorization": "Bearer {{.token}}",
	}
	args := map[string]any{"token": "my-secret-token"}
	result, err := m.RenderHeaders(templates, args)
	if err != nil {
		t.Fatal(err)
	}
	want := "Bearer my-secret-token"
	if result["Authorization"] != want {
		t.Errorf("got %q, want %q", result["Authorization"], want)
	}
}

func TestMapResponse_NoExpr(t *testing.T) {
	m := &DefaultResponseMapper{}
	input := json.RawMessage(`{"a": 1, "b": 2}`)
	got, err := m.MapResponse(input, "")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(input) {
		t.Errorf("expected passthrough, got %s", got)
	}
}

func TestMapResponse_WithJMESPath(t *testing.T) {
	m := &DefaultResponseMapper{}
	input := json.RawMessage(`{"results": [{"name": "a"}, {"name": "b"}], "meta": {"total": 2}}`)
	got, err := m.MapResponse(input, "results[*].name")
	if err != nil {
		t.Fatal(err)
	}

	var names []string
	if err := json.Unmarshal(got, &names); err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || names[0] != "a" || names[1] != "b" {
		t.Errorf("got %v", names)
	}
}

func TestMapResponse_NonJSON(t *testing.T) {
	m := &DefaultResponseMapper{}
	input := json.RawMessage(`not json`)
	got, err := m.MapResponse(input, "some.path")
	if err != nil {
		t.Fatal(err)
	}
	// Non-JSON should pass through
	if string(got) != "not json" {
		t.Errorf("expected passthrough for non-JSON, got %s", got)
	}
}

func TestMapResponse_NullResult(t *testing.T) {
	m := &DefaultResponseMapper{}
	input := json.RawMessage(`{"a": 1}`)
	got, err := m.MapResponse(input, "nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "null" {
		t.Errorf("expected null, got %s", got)
	}
}

func TestExtractTemplateVars(t *testing.T) {
	vars := extractTemplateVars("https://api.example.com/{{.user_id}}/repos/{{ .repo }}")
	if !vars["user_id"] {
		t.Error("expected user_id")
	}
	if !vars["repo"] {
		t.Error("expected repo")
	}
	if len(vars) != 2 {
		t.Errorf("expected 2 vars, got %d", len(vars))
	}
}

func TestExtractTemplateVars_NoVars(t *testing.T) {
	vars := extractTemplateVars("https://api.example.com/v1/items")
	if len(vars) != 0 {
		t.Errorf("expected 0 vars, got %d", len(vars))
	}
}

func TestBuildMappedRequest_StaticQuery(t *testing.T) {
	exec := NewHTTPExecutor()
	cfg := &HTTPConfig{
		URL:    "https://api.example.com/search",
		Method: "GET",
		Request: &RequestMapping{
			StaticQuery: map[string]string{
				"count":  "1",
				"format": "json",
			},
		},
	}

	args := json.RawMessage(`{"name": "London"}`)
	req, err := exec.buildMappedRequest(t.Context(), cfg, "GET", args)
	if err != nil {
		t.Fatal(err)
	}

	q := req.URL.Query()
	if q.Get("count") != "1" {
		t.Errorf("expected count=1, got %q", q.Get("count"))
	}
	if q.Get("format") != "json" {
		t.Errorf("expected format=json, got %q", q.Get("format"))
	}
	// LLM arg should also be present (in body for non-query-routed args,
	// but since it's GET, name goes to body bucket which is ignored for GET).
}

func TestBuildMappedRequest_StaticQueryWithQueryParams(t *testing.T) {
	exec := NewHTTPExecutor()
	cfg := &HTTPConfig{
		URL:    "https://api.example.com/search",
		Method: "GET",
		Request: &RequestMapping{
			QueryParams: []string{"name"},
			StaticQuery: map[string]string{
				"count": "1",
			},
		},
	}

	args := json.RawMessage(`{"name": "London"}`)
	req, err := exec.buildMappedRequest(t.Context(), cfg, "GET", args)
	if err != nil {
		t.Fatal(err)
	}

	q := req.URL.Query()
	if q.Get("name") != "London" {
		t.Errorf("expected name=London, got %q", q.Get("name"))
	}
	if q.Get("count") != "1" {
		t.Errorf("expected count=1, got %q", q.Get("count"))
	}
}

func TestBuildMappedRequest_StaticHeaders(t *testing.T) {
	exec := NewHTTPExecutor()
	cfg := &HTTPConfig{
		URL:    "https://api.example.com/data",
		Method: "GET",
		Request: &RequestMapping{
			StaticHeaders: map[string]string{
				"X-Api-Version": "2024-01",
				"Accept":        "application/json",
			},
		},
	}

	args := json.RawMessage(`{}`)
	req, err := exec.buildMappedRequest(t.Context(), cfg, "GET", args)
	if err != nil {
		t.Fatal(err)
	}

	if req.Header.Get("X-Api-Version") != "2024-01" {
		t.Errorf("expected X-Api-Version=2024-01, got %q", req.Header.Get("X-Api-Version"))
	}
	if req.Header.Get("Accept") != "application/json" {
		t.Errorf("expected Accept=application/json, got %q", req.Header.Get("Accept"))
	}
}

func TestBuildMappedRequest_StaticBody(t *testing.T) {
	exec := NewHTTPExecutor()
	cfg := &HTTPConfig{
		URL:    "https://api.example.com/data",
		Method: "POST",
		Request: &RequestMapping{
			StaticBody: map[string]any{
				"api_version": "v2",
				"format":      "json",
			},
		},
	}

	args := json.RawMessage(`{"query": "test"}`)
	req, err := exec.buildMappedRequest(t.Context(), cfg, "POST", args)
	if err != nil {
		t.Fatal(err)
	}

	// Read body and verify static fields are merged
	var body map[string]any
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["query"] != "test" {
		t.Errorf("expected query=test, got %v", body["query"])
	}
	if body["api_version"] != "v2" {
		t.Errorf("expected api_version=v2, got %v", body["api_version"])
	}
	if body["format"] != "json" {
		t.Errorf("expected format=json, got %v", body["format"])
	}
}
