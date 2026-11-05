package tools

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"
)

// capturedRequest is what a test server saw for one request.
type capturedRequest struct {
	path   string
	query  map[string][]string
	header http.Header
	body   string
}

// newCapturingServer starts a server that records the last request it received.
func newCapturingServer(t *testing.T) (url string, last func() capturedRequest) {
	t.Helper()
	var got capturedRequest
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = capturedRequest{path: r.URL.Path, query: r.URL.Query(), header: r.Header.Clone(), body: string(b)}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	return server.URL, func() capturedRequest { return got }
}

// A URL path template renders even when the tool has no request block, and the
// argument it consumes is not repeated as a query parameter.
func TestHTTPExecutor_URLTemplateRendersWithoutRequestBlock(t *testing.T) {
	url, last := newCapturingServer(t)
	desc := newTestDescriptor(url+"/users/{{.user_id}}", "GET")

	mustExecute(t, NewHTTPExecutor(), desc, json.RawMessage(`{"user_id":"u-42","limit":5}`))

	got := last()
	if got.path != "/users/u-42" {
		t.Errorf("path = %q, want /users/u-42", got.path)
	}
	if _, ok := got.query["user_id"]; ok {
		t.Errorf("user_id was consumed by the path and must not also be a query param: %v", got.query)
	}
	if v := got.query["limit"]; len(v) != 1 || v[0] != "5" {
		t.Errorf("limit = %v, want [5]: args the template does not use still go to the query", v)
	}
}

// For a body method with no request block, the args the template does not use
// still form the JSON body.
func TestHTTPExecutor_URLTemplateWithoutRequestBlock_BodyMethod(t *testing.T) {
	url, last := newCapturingServer(t)
	desc := newTestDescriptor(url+"/users/{{.user_id}}/notes", "POST")

	mustExecute(t, NewHTTPExecutor(), desc, json.RawMessage(`{"user_id":"u-42","text":"hi"}`))

	got := last()
	if got.path != "/users/u-42/notes" {
		t.Errorf("path = %q, want /users/u-42/notes", got.path)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(got.body), &body); err != nil {
		t.Fatalf("body %q is not JSON: %v", got.body, err)
	}
	if body["text"] != "hi" {
		t.Errorf("body text = %v, want hi", body["text"])
	}
	if _, ok := body["user_id"]; ok {
		t.Errorf("user_id was consumed by the path and must not be in the body: %v", body)
	}
}

// A templated host is refused on the no-request-block path too.
func TestHTTPExecutor_URLTemplateWithoutRequestBlock_RefusesTemplatedHost(t *testing.T) {
	desc := newTestDescriptor("https://{{.region}}.api.example.com/v1", "GET")
	_, err := NewHTTPExecutor().Execute(t.Context(), desc, json.RawMessage(`{"region":"x"}`))
	if !errors.Is(err, ErrTemplatedURLHost) {
		t.Fatalf("err = %v, want ErrTemplatedURLHost", err)
	}
}

// Static query, static headers and header params apply when the call has no
// arguments. Static auth headers must not depend on the model passing an arg.
func TestHTTPExecutor_RequestBlockAppliesWithoutArgs(t *testing.T) {
	for _, args := range []json.RawMessage{nil, json.RawMessage(`null`), json.RawMessage(`{}`)} {
		t.Run(string(args), func(t *testing.T) {
			url, last := newCapturingServer(t)
			desc := newTestDescriptor(url+"/status", "GET")
			desc.HTTPConfig.Request = &RequestMapping{
				StaticQuery:   map[string]string{"format": "json"},
				StaticHeaders: map[string]string{"Authorization": "Bearer static-token"},
				HeaderParams:  map[string]string{"X-Api-Version": "2024-01"},
			}

			mustExecute(t, NewHTTPExecutor(), desc, args)

			got := last()
			if v := got.query["format"]; len(v) != 1 || v[0] != "json" {
				t.Errorf("format query = %v, want [json]", v)
			}
			if h := got.header.Get("Authorization"); h != "Bearer static-token" {
				t.Errorf("Authorization = %q, want the static header", h)
			}
			if h := got.header.Get("X-Api-Version"); h != "2024-01" {
				t.Errorf("X-Api-Version = %q, want 2024-01", h)
			}
		})
	}
}

// PreRequest runs on every request after it is built, and sees the final URL.
func TestHTTPExecutor_PreRequestModifiesRequest(t *testing.T) {
	url, last := newCapturingServer(t)
	exec := NewHTTPExecutor()
	var sawPath string
	exec.PreRequest = func(req *http.Request) error {
		sawPath = req.URL.Path
		req.Header.Set("Authorization", "Bearer dynamic")
		return nil
	}
	desc := newTestDescriptor(url+"/items/{{.id}}", "GET")

	mustExecute(t, exec, desc, json.RawMessage(`{"id":"7"}`))

	if sawPath != "/items/7" {
		t.Errorf("PreRequest saw path %q, want the rendered /items/7", sawPath)
	}
	if h := last().header.Get("Authorization"); h != "Bearer dynamic" {
		t.Errorf("Authorization = %q, want the header PreRequest set", h)
	}
}

// An error from PreRequest fails the call and nothing is sent.
func TestHTTPExecutor_PreRequestErrorFailsTheCall(t *testing.T) {
	calls := 0
	server := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{}`))
	})
	exec := NewHTTPExecutor()
	sentinel := errors.New("token refresh failed")
	exec.PreRequest = func(*http.Request) error { return sentinel }

	_, err := exec.Execute(t.Context(), newTestDescriptor(server.URL, "GET"), nil)
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want the PreRequest error", err)
	}
	if calls != 0 {
		t.Errorf("server received %d requests, want 0", calls)
	}
}

// PreRequest also runs on the multimodal path.
func TestHTTPExecutor_PreRequestRunsOnMultimodalPath(t *testing.T) {
	url, last := newCapturingServer(t)
	exec := NewHTTPExecutor()
	exec.PreRequest = func(req *http.Request) error {
		req.Header.Set("X-Signed", "yes")
		return nil
	}
	desc := newTestDescriptor(url, "GET")
	desc.HTTPConfig.Multimodal = &MultimodalConfig{Enabled: true}

	if _, _, err := exec.ExecuteMultimodal(t.Context(), desc, nil); err != nil {
		t.Fatal(err)
	}
	if h := last().header.Get("X-Signed"); h != "yes" {
		t.Errorf("X-Signed = %q, want yes", h)
	}
}
