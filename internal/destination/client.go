// Package destination restricts webhook egress at URL validation and dial time.
package destination

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// denied contains reserved, private, or local network prefixes (IPv4 and IPv6)
// that must be blocked to prevent Server-Side Request Forgery (SSRF) attacks.
var denied = []netip.Prefix{
	// IPv4: Local host and loopback networks
	netip.MustParsePrefix("0.0.0.0/8"),   // RFC 1122: Local network "this host on this network"
	netip.MustParsePrefix("127.0.0.0/8"), // RFC 1122: Loopback (localhost)

	// IPv4: Private network address spaces (RFC 1918)
	netip.MustParsePrefix("10.0.0.0/8"),     // Private Class A
	netip.MustParsePrefix("172.16.0.0/12"),  // Private Class B
	netip.MustParsePrefix("192.168.0.0/16"), // Private Class C

	// IPv4: Link-Local and Cloud metadata
	netip.MustParsePrefix("169.254.0.0/16"), // RFC 3927: Link-Local (AWS/GCP/Azure metadata)

	// IPv4: Shared space and benchmarking
	netip.MustParsePrefix("100.64.0.0/10"), // RFC 6598: Carrier-Grade NAT (CGNAT)
	netip.MustParsePrefix("198.18.0.0/15"), // RFC 2544: Inter-network benchmark

	// IPv4: Documentation and examples
	netip.MustParsePrefix("192.0.0.0/24"),    // RFC 6890: IETF protocol assignments
	netip.MustParsePrefix("192.0.2.0/24"),    // RFC 5737: TEST-NET-1 documentation
	netip.MustParsePrefix("198.51.100.0/24"), // RFC 5737: TEST-NET-2 documentation
	netip.MustParsePrefix("203.0.113.0/24"),  // RFC 5737: TEST-NET-3 documentation

	// IPv4: Special and reserved protocols
	netip.MustParsePrefix("192.88.99.0/24"), // RFC 7526: 6to4 Anycast (deprecated)
	netip.MustParsePrefix("224.0.0.0/4"),    // RFC 5771: Multicast
	netip.MustParsePrefix("240.0.0.0/4"),    // RFC 1112: Reserved for future use

	// IPv6: Special ranges and documentation
	netip.MustParsePrefix("2001::/23"),     // RFC 2928: IETF special protocols
	netip.MustParsePrefix("2001:db8::/32"), // RFC 3849: IPv6 documentation
	netip.MustParsePrefix("2002::/16"),     // RFC 3056: 6to4 transition
	netip.MustParsePrefix("3fff::/20"),     // RFC 9637: IPv6 documentation
}

func public(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || ip.Zone() != "" || !ip.IsGlobalUnicast() {
		return false
	}
	if ip.Is6() && !netip.MustParsePrefix("2000::/3").Contains(ip) {
		return false
	}
	for _, prefix := range denied {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}

// Validate checks that a raw destination URL is safe and syntactically valid.
// In standard mode (loopback == false), destinations require HTTPS on port 443
// and must not resolve to private/reserved IP spaces.
// In test mode (loopback == true), literal loopback addresses (127.0.0.1) are accepted.
func Validate(raw string, loopback bool) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Fragment != "" || u.Opaque != "" || u.Hostname() == "" || len(raw) > 2048 {
		return nil, errors.New("invalid destination URL")
	}
	ip, ipErr := netip.ParseAddr(u.Hostname())
	if loopback {
		if ipErr != nil || ip.Zone() != "" || !ip.Unmap().IsLoopback() || (u.Scheme != "http" && u.Scheme != "https") {
			return nil, errors.New("test destination requires literal loopback")
		}
		if u.Port() != "" {
			port, err := strconv.Atoi(u.Port())
			if err != nil || port < 1 || port > 65535 {
				return nil, errors.New("invalid destination port")
			}
		}
	} else {
		if u.Scheme != "https" || (u.Port() != "" && u.Port() != "443") {
			return nil, errors.New("external destinations require HTTPS port 443")
		}
		if ipErr == nil && !public(ip) {
			return nil, errors.New("destination address blocked")
		}
		if strings.Contains(u.Hostname(), "%") || strings.ContainsAny(u.Hostname(), " \t\r\n") {
			return nil, errors.New("invalid destination host")
		}
	}
	return u, nil
}

type lookupFunc func(context.Context, string) ([]netip.Addr, error)
type dialFunc func(context.Context, string, string) (net.Conn, error)

func guardedDial(loopback bool, lookup lookupFunc, dial dialFunc) dialFunc {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, errors.New("invalid dial address")
		}
		if !loopback && port != "443" {
			return nil, errors.New("destination port blocked")
		}
		ips := []netip.Addr{}
		ip, err := netip.ParseAddr(host)
		if err == nil {
			ips = append(ips, ip)
		} else {
			if loopback {
				return nil, errors.New("test destination must be literal loopback")
			}
			ips, err = lookup(ctx, host)
			if err != nil {
				return nil, errors.New("destination resolution failed")
			}
		}
		if len(ips) == 0 {
			return nil, errors.New("no destination addresses")
		}
		// Reject the whole response if any address is internal; dial validated IP directly.
		for _, ip := range ips {
			if ip.Zone() != "" || (loopback && !ip.Unmap().IsLoopback()) || (!loopback && !public(ip)) {
				return nil, errors.New("destination address blocked")
			}
		}
		for _, ip := range ips {
			conn, err := dial(ctx, network, net.JoinHostPort(ip.Unmap().String(), port))
			if err == nil {
				return conn, nil
			}
		}
		return nil, errors.New("destination connection failed")
	}
}
func Client(raw string, loopback bool) (*http.Client, error) {
	if _, err := Validate(raw, loopback); err != nil {
		return nil, err
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	transport := &http.Transport{Proxy: nil, DialContext: guardedDial(loopback, func(ctx context.Context, host string) ([]netip.Addr, error) {
		return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	}, dialer.DialContext), DisableKeepAlives: true, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 5 * time.Second}
	// Resolve at every connection. TLS keeps the original hostname for certificate validation.
	return &http.Client{Timeout: 5 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, nil
}
