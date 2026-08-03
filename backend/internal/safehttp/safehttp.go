package safehttp

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const maxRedirects = 10

var blockedDownloadPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.100.100.200/32"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/128"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/32"),
	netip.MustParsePrefix("2001:2::/48"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2001:20::/28"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("fd00:ec2::254/128"),
	netip.MustParsePrefix("fec0::/10"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("ff00::/8"),
}

// ValidateURL performs the URL checks that do not require DNS. Host addresses
// are checked again after resolution and immediately before every connection.
func ValidateURL(rawURL string) (*url.URL, error) {
	if len(rawURL) == 0 || len(rawURL) > 4096 {
		return nil, fmt.Errorf("download URL must be between 1 and 4096 characters")
	}
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid download URL: %v", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("download URL must use HTTP or HTTPS")
	}
	if parsed.Host == "" || parsed.Hostname() == "" {
		return nil, fmt.Errorf("download URL must include a host")
	}
	if parsed.User != nil {
		return nil, fmt.Errorf("download URL must not include credentials")
	}
	if parsed.Fragment != "" {
		return nil, fmt.Errorf("download URL must not include a fragment")
	}
	if port := parsed.Port(); port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return nil, fmt.Errorf("download URL contains an invalid port")
		}
	}
	if addr, err := netip.ParseAddr(parsed.Hostname()); err == nil && !isAllowedDownloadAddress(addr) {
		return nil, fmt.Errorf("download URL resolves to a blocked address")
	}
	return parsed, nil
}

// Get retrieves a resource only when every resolved destination is safe for
// image downloads. Private network image mirrors are allowed; loopback,
// link-local, metadata, multicast and reserved destinations remain blocked.
func Get(ctx context.Context, rawURL, userAgent string, timeout time.Duration) (*http.Response, error) {
	parsed, err := ValidateURL(rawURL)
	if err != nil {
		return nil, err
	}
	if err := validateHost(ctx, net.DefaultResolver, parsed.Hostname()); err != nil {
		return nil, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", userAgent)

	client := &http.Client{
		Timeout:   timeout,
		Transport: restrictedTransport(net.DefaultResolver),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("too many redirects")
			}
			redirect, err := ValidateURL(req.URL.String())
			if err != nil {
				return err
			}
			if err := validateHost(req.Context(), net.DefaultResolver, redirect.Hostname()); err != nil {
				return err
			}
			if len(via) > 0 {
				req.Header.Set("User-Agent", via[0].Header.Get("User-Agent"))
			}
			return nil
		},
	}

	// The URL, redirects, DNS answers and dial destinations are constrained
	// above and in restrictedTransport. CodeQL cannot infer those checks across
	// the custom transport boundary.
	// codeql[go/request-forgery]
	return client.Do(request)
}

func restrictedTransport(resolver *net.Resolver) *http.Transport {
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	return &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, fmt.Errorf("invalid download destination: %v", err)
			}
			addresses, err := resolveAllowedHost(ctx, resolver, host)
			if err != nil {
				return nil, err
			}
			var lastErr error
			for _, addr := range addresses {
				conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(addr.String(), port))
				if err == nil {
					return conn, nil
				}
				lastErr = err
			}
			if lastErr == nil {
				lastErr = fmt.Errorf("host has no usable public addresses")
			}
			return nil, lastErr
		},
		ForceAttemptHTTP2:   true,
		TLSHandshakeTimeout: 30 * time.Second,
		IdleConnTimeout:     90 * time.Second,
	}
}

func validateHost(ctx context.Context, resolver *net.Resolver, host string) error {
	_, err := resolveAllowedHost(ctx, resolver, host)
	return err
}

func resolveAllowedHost(ctx context.Context, resolver *net.Resolver, host string) ([]netip.Addr, error) {
	host = strings.TrimSpace(strings.TrimSuffix(host, "."))
	if host == "" {
		return nil, fmt.Errorf("download URL host is empty")
	}
	if strings.EqualFold(host, "localhost") || strings.HasSuffix(strings.ToLower(host), ".localhost") {
		return nil, fmt.Errorf("download URL host is not public")
	}

	if addr, err := netip.ParseAddr(host); err == nil {
		addr = addr.Unmap()
		if !isAllowedDownloadAddress(addr) {
			return nil, fmt.Errorf("download URL resolves to a blocked address")
		}
		return []netip.Addr{addr}, nil
	}

	addresses, err := resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve download host: %v", err)
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("download host has no IP addresses")
	}
	result := make([]netip.Addr, 0, len(addresses))
	for _, address := range addresses {
		address = address.Unmap()
		if !isAllowedDownloadAddress(address) {
			return nil, fmt.Errorf("download host resolves to a blocked address")
		}
		result = append(result, address)
	}
	return result, nil
}

func isAllowedDownloadAddress(address netip.Addr) bool {
	if !address.IsValid() || address.Zone() != "" || !address.IsGlobalUnicast() ||
		address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() ||
		address.IsMulticast() || address.IsUnspecified() {
		return false
	}
	address = address.Unmap()
	for _, prefix := range blockedDownloadPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}
