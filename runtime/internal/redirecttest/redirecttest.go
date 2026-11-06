// Package redirecttest checks, from tests, that an HTTP client keeps
// credential headers on their origin when a server redirects to another host.
package redirecttest

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// ProbeHeader is the credential header the checks send. http.Client itself
// copies it to any host, so only a credential redirect policy keeps it off.
const ProbeHeader = "X-Goog-Api-Key"

// CallerClient returns a client like one a caller passes in, with its own
// CheckRedirect that counts the hops it sees and lets them through.
func CallerClient() (*http.Client, *atomic.Int32) {
	hops := &atomic.Int32{}
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		hops.Add(1)
		return nil
	}}, hops
}

// SendThroughCrossHostRedirect sends a request carrying ProbeHeader through
// client to an origin that answers with a 307 to another host. It returns the
// headers the origin and the other host received.
func SendThroughCrossHostRedirect(t *testing.T, client *http.Client) (atOrigin, atTarget http.Header) {
	t.Helper()
	got := make(chan http.Header, 1)
	target := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got <- r.Header.Clone()
	}))
	t.Cleanup(target.Close)
	// 127.0.0.1 and localhost are different hosts to http.Client.
	targetURL := strings.Replace(target.URL, "127.0.0.1", "localhost", 1)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atOrigin = r.Header.Clone()
		http.Redirect(w, r, targetURL, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(origin.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, origin.URL, http.NoBody)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set(ProbeHeader, "secret")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request through redirect: %v", err)
	}
	_ = resp.Body.Close()
	select {
	case atTarget = <-got:
	default:
		t.Fatal("the redirect never reached the other host")
	}
	return atOrigin, atTarget
}

// Result is what Probe saw.
type Result struct {
	// AtOrigin and AtTarget are the ProbeHeader values the origin and the
	// other host received through the client under test.
	AtOrigin, AtTarget string
	// CallerHops counts the hops the caller's own CheckRedirect saw.
	CallerHops int32
	// CallerDirect is the ProbeHeader value the other host received when the
	// request went through the caller's own client: still "secret" if the
	// caller's client was not modified.
	CallerDirect string
}

// Probe sends ProbeHeader through used (the client a provider built from
// caller) and then through caller itself, each to an origin that redirects to
// another host, and reports what each host received.
func Probe(t *testing.T, used, caller *http.Client, hops *atomic.Int32) Result {
	t.Helper()
	atOrigin, atTarget := SendThroughCrossHostRedirect(t, used)
	r := Result{
		AtOrigin:   atOrigin.Get(ProbeHeader),
		AtTarget:   atTarget.Get(ProbeHeader),
		CallerHops: hops.Load(),
	}
	_, direct := SendThroughCrossHostRedirect(t, caller)
	r.CallerDirect = direct.Get(ProbeHeader)
	return r
}
