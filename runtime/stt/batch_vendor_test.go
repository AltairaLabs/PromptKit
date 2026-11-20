package stt_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// capturedUpload is what a vendor batch endpoint received.
type capturedUpload struct {
	method   string
	path     string
	query    map[string]string
	headers  http.Header
	fields   map[string]string
	filename string
	file     []byte
}

// uploadRecorder is an httptest server that records every multipart request
// and answers each with the next scripted response.
type uploadRecorder struct {
	t         *testing.T
	srv       *httptest.Server
	mu        sync.Mutex
	got       []capturedUpload
	responses []scriptedResponse
}

type scriptedResponse struct {
	status int
	body   string
}

func newUploadRecorder(t *testing.T, responses ...scriptedResponse) *uploadRecorder {
	t.Helper()
	r := &uploadRecorder{t: t, responses: responses}
	r.srv = httptest.NewServer(http.HandlerFunc(r.handle))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *uploadRecorder) handle(w http.ResponseWriter, req *http.Request) {
	c := capturedUpload{
		method:  req.Method,
		path:    req.URL.Path,
		query:   map[string]string{},
		headers: req.Header.Clone(),
		fields:  map[string]string{},
	}
	for k := range req.URL.Query() {
		c.query[k] = req.URL.Query().Get(k)
	}
	if err := req.ParseMultipartForm(1 << 20); err != nil {
		r.t.Errorf("request is not multipart: %v", err)
	} else {
		for k, v := range req.MultipartForm.Value {
			c.fields[k] = v[0]
		}
		if fh := req.MultipartForm.File["file"]; len(fh) == 1 {
			c.filename = fh[0].Filename
			f, err := fh[0].Open()
			if err == nil {
				c.file, _ = io.ReadAll(f)
				_ = f.Close()
			}
		}
	}

	r.mu.Lock()
	n := len(r.got)
	r.got = append(r.got, c)
	r.mu.Unlock()

	resp := r.responses[len(r.responses)-1]
	if n < len(r.responses) {
		resp = r.responses[n]
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.status)
	_, _ = w.Write([]byte(resp.body))
}

func (r *uploadRecorder) requests() []capturedUpload {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]capturedUpload(nil), r.got...)
}

// only returns the single request the server received.
func (r *uploadRecorder) only() capturedUpload {
	r.t.Helper()
	got := r.requests()
	if len(got) != 1 {
		r.t.Fatalf("server received %d requests, want 1", len(got))
	}
	return got[0]
}
