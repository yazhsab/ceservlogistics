package httpx

import (
	"net"
	"net/http"
	"testing"
)

// clientIP decides the identity that rate limiting and login throttling budget
// against. If a caller can choose it, both controls become decorative: an
// attacker sends a fresh X-Forwarded-For per request and never exhausts a
// bucket. This file exists because that is a one-line regression away and the
// package had no tests.

func mustCIDRs(t *testing.T, cidrs ...string) []*net.IPNet {
	t.Helper()
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			t.Fatalf("parse %s: %v", c, err)
		}
		out = append(out, n)
	}
	return out
}

func request(remoteAddr string, headers map[string]string) *http.Request {
	r, _ := http.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = remoteAddr
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	return r
}

func TestClientIPIgnoresForwardingHeadersFromAnUntrustedPeer(t *testing.T) {
	trusted := mustCIDRs(t, "127.0.0.1/32", "10.0.0.0/8")

	for _, tc := range []struct {
		name, remoteAddr, xff, want string
	}{
		{"direct connection, forged header", "203.0.113.9:44321", "1.2.3.4", "203.0.113.9"},
		{"forged chain", "203.0.113.9:44321", "1.1.1.1, 2.2.2.2, 3.3.3.3", "203.0.113.9"},
		{"forged loopback to look trusted", "203.0.113.9:44321", "127.0.0.1", "203.0.113.9"},
		{"empty header", "198.51.100.7:1", "", "198.51.100.7"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := clientIP(request(tc.remoteAddr, map[string]string{"X-Forwarded-For": tc.xff}), trusted)
			if got != tc.want {
				t.Errorf("clientIP = %q, want %q — a caller chose its own rate-limit bucket", got, tc.want)
			}
		})
	}
}

func TestClientIPTrustsTheProxyOnlyForTheRealClient(t *testing.T) {
	trusted := mustCIDRs(t, "127.0.0.1/32", "10.0.0.0/8")

	for _, tc := range []struct {
		name, remoteAddr, xff, want string
	}{
		// Nginx on the same host forwards the real client.
		{"single hop", "127.0.0.1:9999", "203.0.113.9", "203.0.113.9"},
		// Two trusted hops: the right-most non-trusted entry is the client.
		{"through two trusted proxies", "10.0.0.5:9999", "203.0.113.9, 10.0.0.4", "203.0.113.9"},
		// A client that forges a chain before reaching the proxy cannot make
		// itself look like an earlier hop: the right-most untrusted entry wins,
		// and that is the address the proxy actually observed.
		{"client-forged prefix is ignored", "127.0.0.1:9999", "9.9.9.9, 203.0.113.9", "203.0.113.9"},
		{"garbage entries are skipped", "127.0.0.1:9999", "not-an-ip, 203.0.113.9", "203.0.113.9"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := clientIP(request(tc.remoteAddr, map[string]string{"X-Forwarded-For": tc.xff}), trusted)
			if got != tc.want {
				t.Errorf("clientIP = %q, want %q", got, tc.want)
			}
		})
	}
}

// With no configured proxies nothing is trusted, which is the safe default for
// a process exposed directly.
func TestClientIPWithNoTrustedProxiesNeverReadsAHeader(t *testing.T) {
	got := clientIP(request("203.0.113.9:1", map[string]string{"X-Forwarded-For": "1.2.3.4"}), nil)
	if got != "203.0.113.9" {
		t.Errorf("clientIP = %q, want the peer address", got)
	}
}

// A malformed RemoteAddr must still produce a stable non-empty key rather than
// collapsing every caller into one shared bucket.
func TestClientIPHandlesAMalformedPeerAddress(t *testing.T) {
	if got := clientIP(request("garbage", nil), mustCIDRs(t, "127.0.0.1/32")); got != "garbage" {
		t.Errorf("clientIP = %q, want the raw value preserved as a key", got)
	}
	if got := clientIP(request("", nil), nil); got != "" {
		t.Errorf("clientIP = %q for an empty peer", got)
	}
}
