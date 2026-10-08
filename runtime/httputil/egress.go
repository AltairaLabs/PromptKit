package httputil

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

// ErrNonPublicDestination is returned when a guarded client is asked to
// connect to an address that is not on the public internet.
var ErrNonPublicDestination = errors.New("destination is not a public address")

// Address ranges that are not reachable as ordinary public destinations but
// that netip's Is* predicates do not cover.
var nonPublicPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),     // "this network"
	netip.MustParsePrefix("100.64.0.0/10"), // carrier-grade NAT; Alibaba's metadata service
	netip.MustParsePrefix("192.0.0.0/24"),  // IETF protocol assignments
	netip.MustParsePrefix("198.18.0.0/15"), // benchmarking
	netip.MustParsePrefix("240.0.0.0/4"),   // reserved, and the broadcast address
	netip.MustParsePrefix("64:ff9b::/96"),  // NAT64: embeds an IPv4 address of any kind
	netip.MustParsePrefix("2002::/16"),     // 6to4: likewise
}

// IsPublicAddress reports whether ip is an ordinary public address: not
// loopback, private, link-local (which is where cloud metadata services
// live), multicast, unspecified or otherwise reserved.
func IsPublicAddress(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return false
	}
	for _, p := range nonPublicPrefixes {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}

// publicOnlyControl refuses a connection whose resolved address is not public.
//
// It runs after DNS resolution, on the address actually being dialed, so a
// hostname that resolves to a private address is refused, as is one that
// re-resolves between a check and the connection (DNS rebinding), and as is
// every hop of a redirect, since each hop dials anew.
func publicOnlyControl(_, address string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil || !dialAllowed(ap) {
		return fmt.Errorf("%w: %s", ErrNonPublicDestination, address)
	}
	return nil
}

// dialAllowed decides each dial. A variable so tests can stand a loopback
// server in for a public one.
var dialAllowed = func(ap netip.AddrPort) bool { return IsPublicAddress(ap.Addr()) }

// NewPublicHTTPClient returns an HTTP client that connects only to public
// addresses. Use it to fetch a URL that came from a model, a remote client or
// any other input the host does not control: without it, such a URL can point
// the host at its own cloud metadata service or internal network.
//
// It never uses a proxy, since a proxy would make the dialed address the
// proxy's and hide the real destination from the check.
func NewPublicHTTPClient(timeout time.Duration) *http.Client {
	transport := newDefaultTransport()
	transport.DialContext = (&net.Dialer{
		Timeout:   defaultDialTimeout,
		KeepAlive: defaultDialKeepAlive,
		Control:   publicOnlyControl,
	}).DialContext
	return &http.Client{Timeout: timeout, Transport: transport}
}
