package httputil

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestIsPublicAddress(t *testing.T) {
	for _, tc := range []struct {
		addr string
		want bool
	}{
		{"8.8.8.8", true},
		{"2606:4700:4700::1111", true},
		{"127.0.0.1", false},
		{"::1", false},
		{"10.1.2.3", false},
		{"172.16.0.1", false},
		{"192.168.1.1", false},
		{"169.254.169.254", false}, // AWS, GCP and Azure metadata
		{"100.100.100.200", false}, // Alibaba metadata
		{"fd00:ec2::254", false},   // AWS IPv6 metadata
		{"fe80::1", false},
		{"0.0.0.0", false},
		{"::", false},
		{"224.0.0.1", false},
		{"255.255.255.255", false},
		{"::ffff:127.0.0.1", false}, // IPv4-mapped loopback
		{"::ffff:169.254.169.254", false},
		{"64:ff9b::a9fe:a9fe", false}, // NAT64 of 169.254.169.254
	} {
		if got := IsPublicAddress(netip.MustParseAddr(tc.addr)); got != tc.want {
			t.Errorf("IsPublicAddress(%s) = %v, want %v", tc.addr, got, tc.want)
		}
	}
}

func TestNewPublicHTTPClient_RefusesALoopbackServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("secret"))
	}))
	defer srv.Close()

	resp, err := NewPublicHTTPClient(5 * time.Second).Get(srv.URL)
	if err == nil {
		resp.Body.Close()
		t.Fatal("the guarded client reached a loopback server")
	}
	if !errors.Is(err, ErrNonPublicDestination) {
		t.Errorf("err = %v, want ErrNonPublicDestination", err)
	}

	// The same server is reachable with an ordinary client, so the refusal
	// above is the guard and not a broken server.
	resp, err = NewHTTPClient(5 * time.Second).Get(srv.URL)
	if err != nil {
		t.Fatalf("ordinary client: %v", err)
	}
	resp.Body.Close()
}

func TestNewPublicHTTPClient_RefusesAHostnameResolvingToLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	_, port, _ := splitPort(srv.URL)

	resp, err := NewPublicHTTPClient(5 * time.Second).Get("http://localhost:" + port)
	if err == nil {
		resp.Body.Close()
		t.Fatal("the guarded client reached localhost by name")
	}
	if !errors.Is(err, ErrNonPublicDestination) {
		t.Errorf("err = %v, want ErrNonPublicDestination", err)
	}
}

// Every hop of a redirect dials anew, so a public URL that redirects to a
// non-public one is refused at the redirect. The redirector stands in for a
// public server by being the one port the predicate allows.
func TestNewPublicHTTPClient_RefusesARedirectToANonPublicAddress(t *testing.T) {
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("secret"))
	}))
	defer internal.Close()
	redirector := httptest.NewServer(http.RedirectHandler(internal.URL, http.StatusFound))
	defer redirector.Close()

	public := netip.MustParseAddrPort(redirector.Listener.Addr().String())
	orig := dialAllowed
	dialAllowed = func(ap netip.AddrPort) bool { return ap == public }
	defer func() { dialAllowed = orig }()

	client := NewPublicHTTPClient(5 * time.Second)
	resp, err := client.Get(redirector.URL)
	if err == nil {
		resp.Body.Close()
		t.Fatal("the guarded client followed a redirect to a non-public address")
	}
	if !errors.Is(err, ErrNonPublicDestination) {
		t.Errorf("err = %v, want ErrNonPublicDestination", err)
	}
	if !strings.Contains(err.Error(), internal.Listener.Addr().String()) {
		t.Errorf("err = %v, want it to name the redirect target", err)
	}
}

func TestPublicOnlyControl_MalformedAddress(t *testing.T) {
	if err := publicOnlyControl("tcp", "no-port", nil); !errors.Is(err, ErrNonPublicDestination) {
		t.Errorf("err = %v, want ErrNonPublicDestination", err)
	}
	if err := publicOnlyControl("tcp", "not-an-ip:80", nil); !errors.Is(err, ErrNonPublicDestination) {
		t.Errorf("err = %v, want ErrNonPublicDestination", err)
	}
}

func splitPort(rawURL string) (host, port string, err error) {
	u, err := http.NewRequest(http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return "", "", err
	}
	return u.URL.Hostname(), u.URL.Port(), nil
}
