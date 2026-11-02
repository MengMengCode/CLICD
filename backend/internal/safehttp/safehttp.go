package safehttp

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxRedirects = 10
const imageMirrorOriginsEnv = "CLICD_IMAGE_MIRROR_ORIGINS"

var sharedAddressPrefix = netip.MustParsePrefix("100.64.0.0/10")

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
	if strings.Contains(rawURL, "#") {
		return nil, fmt.Errorf("download URL must not include a fragment")
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

// Get downloads an image through approved, pinned network endpoints. Private
// mirrors require a server-approved origin and are limited to its initially
// resolved addresses; redirects and DNS changes cannot grant new private access.
func Get(ctx context.Context, rawURL, userAgent string, timeout time.Duration) (*http.Response, error) {
	trustedOrigins, err := parseImageMirrorOrigins(os.Getenv(imageMirrorOriginsEnv))
	if err != nil {
		return nil, err
	}
	dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	return (downloader{resolver: net.DefaultResolver, dialContext: dialer.DialContext, trustedOrigins: trustedOrigins}).get(ctx, rawURL, userAgent, timeout)
}

func parseImageMirrorOrigins(value string) (map[string]bool, error) {
	origins := make(map[string]bool)
	if strings.TrimSpace(value) == "" {
		return origins, nil
	}
	for _, raw := range strings.Split(value, ",") {
		parsed, err := ValidateURL(strings.TrimSpace(raw))
		if err != nil || (parsed != nil && (parsed.Path != "" && parsed.Path != "/" || parsed.RawQuery != "" || parsed.ForceQuery)) {
			return nil, fmt.Errorf("%s must contain comma-separated HTTP(S) origins without paths, queries, fragments or credentials", imageMirrorOriginsEnv)
		}
		origins[downloadOrigin(parsed)] = true
	}
	return origins, nil
}

type hostResolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

type downloader struct {
	resolver       hostResolver
	dialContext    func(context.Context, string, string) (net.Conn, error)
	tlsConfig      *tls.Config
	trustedOrigins map[string]bool
}

type downloadPolicy struct {
	privateOrigin    string
	privateAddresses map[netip.Addr]bool
}

func newDownloadPolicy(source *url.URL, addresses []netip.Addr, trustedOrigins map[string]bool) (downloadPolicy, error) {
	policy := downloadPolicy{privateAddresses: make(map[netip.Addr]bool)}
	privateCount := 0
	for _, address := range addresses {
		if isPrivateDownloadAddress(address) {
			privateCount++
			policy.privateAddresses[address] = true
		}
	}
	if len(policy.privateAddresses) > 0 {
		if !trustedOrigins[downloadOrigin(source)] {
			return downloadPolicy{}, fmt.Errorf("private image mirror must be explicitly approved in %s", imageMirrorOriginsEnv)
		}
		if privateCount != len(addresses) {
			return downloadPolicy{}, fmt.Errorf("download host mixes public and private addresses")
		}
		policy.privateOrigin = downloadOrigin(source)
	}
	return policy, nil
}

func (policy downloadPolicy) approve(target *url.URL, addresses []netip.Addr) error {
	origin := downloadOrigin(target)
	for _, address := range addresses {
		if policy.privateOrigin != "" && origin == policy.privateOrigin {
			if !policy.privateAddresses[address] {
				return fmt.Errorf("private image mirror address changed during download")
			}
		} else if isPrivateDownloadAddress(address) {
			return fmt.Errorf("download redirect or DNS change targets an unapproved private address")
		}
	}
	return nil
}

func isPrivateDownloadAddress(address netip.Addr) bool {
	address = address.Unmap()
	return address.IsPrivate() || sharedAddressPrefix.Contains(address)
}

func downloadPort(target *url.URL) int {
	if value := target.Port(); value != "" {
		port, _ := strconv.Atoi(value) // ValidateURL has already checked the range.
		return port
	}
	if target.Scheme == "https" {
		return 443
	}
	return 80
}

func downloadOrigin(target *url.URL) string {
	host := strings.ToLower(strings.TrimSuffix(target.Hostname(), "."))
	if address, err := netip.ParseAddr(host); err == nil {
		host = address.String()
	}
	return target.Scheme + "://" + net.JoinHostPort(host, strconv.Itoa(downloadPort(target)))
}

func (d downloader) get(ctx context.Context, rawURL, userAgent string, timeout time.Duration) (*http.Response, error) {
	var cancel context.CancelFunc
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}
	logicalURL, err := ValidateURL(rawURL)
	if err != nil {
		cancel()
		return nil, err
	}
	addresses, err := resolveAllowedHost(ctx, d.resolver, logicalURL.Hostname())
	if err != nil {
		cancel()
		return nil, err
	}
	policy, err := newDownloadPolicy(logicalURL, addresses, d.trustedOrigins)
	if err != nil {
		cancel()
		return nil, err
	}
	for hop := 0; hop < maxRedirects; hop++ {
		if err := policy.approve(logicalURL, addresses); err != nil {
			cancel()
			return nil, err
		}
		// The request authority is constructed from an approved IP and numeric
		// port. User-controlled path and query are appended only after that
		// complete authority; the transport dials this same frozen address set.
		scheme := "http"
		if logicalURL.Scheme == "https" {
			scheme = "https"
		}
		endpoint := scheme + "://" + net.JoinHostPort(addresses[0].String(), strconv.Itoa(downloadPort(logicalURL)))
		requestURL := endpoint + "/" + strings.TrimPrefix(logicalURL.EscapedPath(), "/")
		if logicalURL.RawQuery != "" || logicalURL.ForceQuery {
			requestURL += "?" + logicalURL.RawQuery
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
		if err != nil {
			cancel()
			return nil, err
		}
		request.Host = logicalURL.Host
		request.Header.Set("User-Agent", userAgent)
		transport := d.transport(logicalURL, addresses)
		client := &http.Client{
			Transport: transport,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
		response, err := client.Do(request)
		if err != nil {
			transport.CloseIdleConnections()
			cancel()
			return nil, err
		}
		// Relative Location values and downstream response validation refer to
		// the logical URL, while the actual connection used the pinned endpoint.
		logicalCopy := *logicalURL
		response.Request = request.Clone(ctx)
		response.Request.URL = &logicalCopy
		if isDownloadRedirect(response.StatusCode) && response.Header.Get("Location") != "" {
			next, redirectErr := response.Location()
			_ = response.Body.Close()
			transport.CloseIdleConnections()
			if redirectErr != nil {
				cancel()
				return nil, redirectErr
			}
			if hop+1 >= maxRedirects {
				cancel()
				return nil, fmt.Errorf("too many redirects")
			}
			logicalURL, err = ValidateURL(next.String())
			if err != nil {
				cancel()
				return nil, err
			}
			addresses, err = resolveAllowedHost(ctx, d.resolver, logicalURL.Hostname())
			if err != nil {
				cancel()
				return nil, err
			}
			continue
		}
		response.Body = &downloadBody{ReadCloser: response.Body, close: func() {
			cancel()
			transport.CloseIdleConnections()
		}}
		return response, nil
	}
	cancel()
	return nil, fmt.Errorf("too many redirects")
}

func isDownloadRedirect(status int) bool {
	return status == http.StatusMovedPermanently || status == http.StatusFound ||
		status == http.StatusSeeOther || status == http.StatusTemporaryRedirect || status == http.StatusPermanentRedirect
}

type downloadBody struct {
	io.ReadCloser
	close func()
	once  sync.Once
}

func (body *downloadBody) Close() error {
	err := body.ReadCloser.Close()
	body.once.Do(body.close)
	return err
}

func (d downloader) transport(logicalURL *url.URL, addresses []netip.Addr) *http.Transport {
	port := strconv.Itoa(downloadPort(logicalURL))
	tlsConfig := &tls.Config{}
	if d.tlsConfig != nil {
		tlsConfig = d.tlsConfig.Clone()
	}
	tlsConfig.ServerName = strings.TrimSuffix(logicalURL.Hostname(), ".")
	return &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var lastErr error
			for _, address := range addresses {
				conn, err := d.dialContext(ctx, network, net.JoinHostPort(address.String(), port))
				if err == nil {
					return conn, nil
				}
				lastErr = err
			}
			return nil, lastErr
		},
		TLSClientConfig:     tlsConfig,
		ForceAttemptHTTP2:   true,
		TLSHandshakeTimeout: 30 * time.Second,
		IdleConnTimeout:     90 * time.Second,
	}
}

func resolveAllowedHost(ctx context.Context, resolver hostResolver, host string) ([]netip.Addr, error) {
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
