package partner

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// webhookResolver is deliberately narrower than net.Resolver so the URL and
// dial policies can be exercised against deterministic DNS answers in tests.
type webhookResolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// webhookURLPolicy is the application-layer half of the webhook egress
// boundary. Infrastructure egress filtering remains required as defense in
// depth, but this policy makes a merchant-supplied URL incapable of selecting
// an internal address even when DNS changes between registration and delivery.
type webhookURLPolicy struct {
	resolver webhookResolver
}

func newWebhookURLPolicy() *webhookURLPolicy {
	return &webhookURLPolicy{resolver: net.DefaultResolver}
}

// validate parses and resolves a webhook URL. It is called both when the URL
// is registered and immediately before every delivery. The validating dialer
// below repeats the resolution at connection time and pins the connection to
// one of the addresses it checked, closing the DNS-rebinding window between a
// preflight lookup and net/http's dial.
func (p *webhookURLPolicy) validate(ctx context.Context, raw string) (*url.URL, error) {
	if raw == "" || strings.TrimSpace(raw) != raw {
		return nil, errors.New("URL must not be empty or contain surrounding whitespace")
	}
	if strings.Contains(raw, "#") {
		return nil, errors.New("URL fragments are not allowed")
	}
	u, err := url.ParseRequestURI(raw)
	if err != nil || u == nil || !u.IsAbs() || u.Opaque != "" {
		return nil, errors.New("URL must be an absolute HTTPS URL")
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return nil, errors.New("URL scheme must be HTTPS")
	}
	if u.User != nil {
		return nil, errors.New("URL user information is not allowed")
	}
	if u.Fragment != "" {
		return nil, errors.New("URL fragments are not allowed")
	}
	host := u.Hostname()
	if host == "" {
		return nil, errors.New("URL must contain a hostname")
	}
	if port := u.Port(); port != "" && port != "443" {
		return nil, errors.New("only the standard HTTPS port 443 is allowed")
	}
	if _, err := p.resolvePublic(ctx, host); err != nil {
		return nil, err
	}
	return u, nil
}

func (p *webhookURLPolicy) resolvePublic(ctx context.Context, host string) ([]netip.Addr, error) {
	host = strings.TrimSpace(host)
	if host == "" || strings.Contains(host, "%") {
		return nil, errors.New("hostname is invalid")
	}

	if literal, err := netip.ParseAddr(host); err == nil {
		literal = literal.Unmap()
		if err := validatePublicWebhookIP(literal); err != nil {
			return nil, err
		}
		return []netip.Addr{literal}, nil
	}

	host = strings.ToLower(host)
	if err := validateWebhookHostname(host); err != nil {
		return nil, err
	}
	addrs, err := p.resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("hostname could not be resolved: %w", err)
	}
	if len(addrs) == 0 {
		return nil, errors.New("hostname resolved to no addresses")
	}

	// Validate the complete answer before dialing any candidate. A hostname
	// with one public and one private answer is rejected rather than gambling
	// on resolver order.
	validated := make([]netip.Addr, 0, len(addrs))
	for _, addr := range addrs {
		addr = addr.Unmap()
		if err := validatePublicWebhookIP(addr); err != nil {
			return nil, err
		}
		validated = append(validated, addr)
	}
	return validated, nil
}

func validateWebhookHostname(host string) error {
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".") {
		return errors.New("local or ambiguous hostnames are not allowed")
	}
	switch host {
	case "metadata", "metadata.google.internal", "instance-data", "instance-data.ec2.internal":
		return errors.New("cloud metadata hostnames are not allowed")
	}
	if len(host) > 253 || !strings.Contains(host, ".") {
		return errors.New("hostname must be a fully qualified public DNS name")
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return errors.New("hostname is invalid")
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
				return errors.New("hostname must use ASCII DNS labels")
			}
		}
	}
	return nil
}

var nonPublicWebhookPrefixes = []netip.Prefix{
	// IPv4 special-use space not fully covered by IsPrivate/IsGlobalUnicast.
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	// IPv6 special-use, translation and tunnelling ranges. Rejecting the
	// translation/tunnel forms avoids reaching a private IPv4 target through
	// an address that appears globally routable at this layer.
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/32"),
	netip.MustParsePrefix("2001:2::/48"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
}

func validatePublicWebhookIP(addr netip.Addr) error {
	if !addr.IsValid() || !addr.IsGlobalUnicast() || addr.IsUnspecified() || addr.IsLoopback() ||
		addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsMulticast() {
		return fmt.Errorf("destination address %s is not public", addr)
	}
	for _, prefix := range nonPublicWebhookPrefixes {
		if prefix.Contains(addr) {
			return fmt.Errorf("destination address %s is not public", addr)
		}
	}
	return nil
}

type webhookDialer struct {
	policy *webhookURLPolicy
	dial   func(context.Context, string, string) (net.Conn, error)
}

func (d *webhookDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("invalid webhook dial address: %w", err)
	}
	if port != "443" {
		return nil, errors.New("webhook connection attempted a non-HTTPS port")
	}
	addrs, err := d.policy.resolvePublic(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("webhook destination rejected: %w", err)
	}

	var lastErr error
	for _, addr := range addrs {
		conn, dialErr := d.dial(ctx, network, net.JoinHostPort(addr.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		lastErr = dialErr
	}
	if lastErr == nil {
		lastErr = errors.New("hostname resolved to no dialable addresses")
	}
	return nil, lastErr
}

func newWebhookHTTPClient(policy *webhookURLPolicy) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	safeDialer := &webhookDialer{policy: policy, dial: dialer.DialContext}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// A forward proxy would resolve the merchant hostname outside this
	// validating dialer and reopen the SSRF path, so webhook egress never uses
	// HTTP(S)_PROXY. Deployments enforce their own explicit network egress rule.
	transport.Proxy = nil
	transport.DialContext = safeDialer.DialContext
	transport.ForceAttemptHTTP2 = true

	return &http.Client{
		Transport:     transport,
		Timeout:       30 * time.Second,
		CheckRedirect: rejectWebhookRedirect,
	}
}

func rejectWebhookRedirect(_ *http.Request, _ []*http.Request) error {
	return http.ErrUseLastResponse
}
