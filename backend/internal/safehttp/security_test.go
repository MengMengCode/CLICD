package safehttp

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"
)

type lookupFunc func(context.Context, string, string) ([]netip.Addr, error)

func (lookup lookupFunc) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	return lookup(ctx, network, host)
}

func TestParseImageMirrorOrigins(t *testing.T) {
	for _, invalid := range []string{
		"mirror.test", "ftp://mirror.test", "http://user:pass@mirror.test",
		"http://mirror.test/images", "http://mirror.test?key=value", "http://mirror.test?",
		"http://mirror.test/#fragment", "http://mirror.test:65536", "http://mirror.test,",
		"http://127.0.0.1", "http://169.254.169.254",
	} {
		if _, err := parseImageMirrorOrigins(invalid); err == nil || !strings.Contains(err.Error(), imageMirrorOriginsEnv) {
			t.Errorf("parseImageMirrorOrigins(%q) = %v, want configuration error", invalid, err)
		}
	}
	origins, err := parseImageMirrorOrigins(" HTTP://Mirror.test:80/, https://[fd00::10]:443 ")
	if err != nil {
		t.Fatal(err)
	}
	if !origins["http://mirror.test:80"] || !origins["https://[fd00::10]:443"] || len(origins) != 2 {
		t.Fatalf("unexpected normalized origins: %v", origins)
	}
}

func TestGetPrivateMirrorRequiresServerApproval(t *testing.T) {
	t.Setenv(imageMirrorOriginsEnv, "")
	_, err := Get(context.Background(), "http://10.0.0.10/image", "test", time.Second)
	if err == nil || !strings.Contains(err.Error(), imageMirrorOriginsEnv) {
		t.Fatalf("unapproved private mirror error = %v", err)
	}
	t.Setenv(imageMirrorOriginsEnv, "http://mirror.test/not-an-origin")
	if _, err := Get(context.Background(), "http://10.0.0.10/image", "test", time.Second); err == nil || !strings.Contains(err.Error(), imageMirrorOriginsEnv) {
		t.Fatalf("invalid mirror configuration error = %v", err)
	}
}

func TestPrivateMirrorPolicyAllowsRepeatedDNSAnswers(t *testing.T) {
	source, err := ValidateURL("http://mirror.test/image")
	if err != nil {
		t.Fatal(err)
	}
	address := netip.MustParseAddr("10.0.0.10")
	addresses := []netip.Addr{address, address}
	policy, err := newDownloadPolicy(source, addresses, map[string]bool{"http://mirror.test:80": true})
	if err != nil {
		t.Fatalf("duplicate private answers were treated as mixed addresses: %v", err)
	}
	if err := policy.approve(source, addresses); err != nil {
		t.Fatalf("approved repeated answers were rejected: %v", err)
	}
}

func TestPrivateMirrorApprovalMatchesExactOrigin(t *testing.T) {
	for _, allowed := range []string{
		"http://other.test:8080", "http://mirror.test:8081", "https://mirror.test:8080",
	} {
		t.Run(allowed, func(t *testing.T) {
			d := downloader{
				resolver: lookupFunc(func(context.Context, string, string) ([]netip.Addr, error) {
					return []netip.Addr{netip.MustParseAddr("10.0.0.10")}, nil
				}),
				trustedOrigins: map[string]bool{allowed: true},
				dialContext: func(context.Context, string, string) (net.Conn, error) {
					t.Error("unapproved private origin was dialed")
					return nil, errors.New("unexpected connection")
				},
			}
			if _, err := d.get(context.Background(), "http://mirror.test:8080/image", "test", time.Second); err == nil {
				t.Fatal("private origin accepted a different host, scheme or port")
			}
		})
	}
}

func TestDownloadRejectsPrivateRedirectsAndDNSChanges(t *testing.T) {
	tests := []struct {
		name      string
		source    string
		redirect  string
		initialIP string
		nextIP    string
		allowlist string
	}{
		{"public to private", "http://public.test/image", "http://10.0.0.10/secret", "8.8.8.8", "", ""},
		{"public to CGNAT", "http://public.test/image", "http://100.64.0.10/secret", "8.8.8.8", "", ""},
		{"public to ULA", "http://public.test/image", "http://[fd00::10]/secret", "8.8.8.8", "", ""},
		{"public DNS becomes private", "http://public.test/image", "/next", "8.8.8.8", "10.0.0.10", "http://public.test:80"},
		{"private DNS address changes", "http://mirror.test/image", "/next", "10.0.0.10", "10.0.0.11", "http://mirror.test:80"},
		{"private mirror changes host", "http://mirror.test/image", "http://other.test/secret", "10.0.0.10", "10.0.0.10", "http://mirror.test:80"},
		{"private mirror changes port", "http://mirror.test/image", "http://mirror.test:81/secret", "10.0.0.10", "10.0.0.10", "http://mirror.test:80"},
		{"private mirror changes scheme", "http://mirror.test/image", "https://mirror.test:80/secret", "10.0.0.10", "10.0.0.10", "http://mirror.test:80"},
		{"loopback redirect", "http://public.test/image", "http://127.0.0.1/secret", "8.8.8.8", "", ""},
		{"mapped loopback redirect", "http://public.test/image", "http://[::ffff:127.0.0.1]/secret", "8.8.8.8", "", ""},
		{"metadata redirect", "http://public.test/image", "http://169.254.169.254/secret", "8.8.8.8", "", ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, test.redirect, http.StatusFound)
			}))
			defer server.Close()
			lookups, dials := 0, 0
			d := downloader{
				resolver: lookupFunc(func(context.Context, string, string) ([]netip.Addr, error) {
					lookups++
					address := test.initialIP
					if lookups > 1 && test.nextIP != "" {
						address = test.nextIP
					}
					return []netip.Addr{netip.MustParseAddr(address)}, nil
				}),
				dialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
					dials++
					if address != net.JoinHostPort(test.initialIP, "80") {
						return nil, fmt.Errorf("unapproved dial endpoint: %s", address)
					}
					return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
				},
				trustedOrigins: map[string]bool{test.allowlist: true},
			}
			response, err := d.get(context.Background(), test.source, "test", time.Second)
			if response != nil {
				response.Body.Close()
			}
			if err == nil || dials != 1 {
				t.Fatalf("redirect was not stopped before a second connection: err=%v dials=%d", err, dials)
			}
		})
	}
}

func TestDownloadPinsInitialDNSAnswerAndRejectsMixedAnswers(t *testing.T) {
	for _, addresses := range [][]netip.Addr{
		{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("127.0.0.1")},
		{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("10.0.0.10")},
	} {
		d := downloader{
			resolver:       lookupFunc(func(context.Context, string, string) ([]netip.Addr, error) { return addresses, nil }),
			trustedOrigins: map[string]bool{"http://mirror.test:80": true},
			dialContext: func(context.Context, string, string) (net.Conn, error) {
				t.Error("unsafe DNS answer was dialed")
				return nil, errors.New("unexpected connection")
			},
		}
		if _, err := d.get(context.Background(), "http://mirror.test/image", "test", time.Second); err == nil {
			t.Fatalf("mixed DNS answers accepted: %v", addresses)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok") }))
	defer server.Close()
	lookups := 0
	d := downloader{
		resolver: lookupFunc(func(context.Context, string, string) ([]netip.Addr, error) {
			lookups++
			if lookups != 1 {
				t.Error("transport re-resolved the hostname instead of dialing the approved address")
			}
			return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
		}),
		dialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if address != "8.8.8.8:80" {
				return nil, fmt.Errorf("dial endpoint = %q, want frozen IP", address)
			}
			return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
		},
	}
	response, err := d.get(context.Background(), "http://public.test/image", "test", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if lookups != 1 {
		t.Fatalf("initial request performed %d DNS resolutions", lookups)
	}
}

func TestDownloadRedirectPreservesLogicalURLHostAndEscaping(t *testing.T) {
	for _, private := range []bool{false, true} {
		t.Run(fmt.Sprintf("private=%v", private), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/start" {
					http.Redirect(w, r, "/image%2Fpart?token=a%2Fb&next=%2F%2Fevil.test", http.StatusTemporaryRedirect)
					return
				}
				_, _ = fmt.Fprintf(w, "%s %s %s", r.Host, r.RequestURI, r.UserAgent())
			}))
			defer server.Close()
			address := "8.8.8.8"
			if private {
				address = "10.0.0.10"
			}
			d := downloader{
				resolver: lookupFunc(func(context.Context, string, string) ([]netip.Addr, error) {
					return []netip.Addr{netip.MustParseAddr(address)}, nil
				}),
				dialContext: func(ctx context.Context, network, endpoint string) (net.Conn, error) {
					if endpoint != net.JoinHostPort(address, "8080") {
						return nil, fmt.Errorf("unexpected endpoint %s", endpoint)
					}
					return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
				},
				trustedOrigins: map[string]bool{"http://mirror.test:8080": true},
			}
			response, err := d.get(context.Background(), "http://mirror.test:8080/start", "image-agent", time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			path := "/image%2Fpart?token=a%2Fb&next=%2F%2Fevil.test"
			if string(body) != "mirror.test:8080 "+path+" image-agent" || response.Request.URL.String() != "http://mirror.test:8080"+path {
				t.Fatalf("logical request semantics changed: body=%q responseURL=%s", body, response.Request.URL)
			}
		})
	}
}

func TestDownloadIgnoresEnvironmentProxy(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	t.Setenv("ALL_PROXY", "http://127.0.0.1:1")
	t.Setenv("NO_PROXY", "")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok") }))
	defer server.Close()
	d := downloader{
		dialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if address != "8.8.8.8:80" {
				return nil, fmt.Errorf("proxy used as endpoint: %s", address)
			}
			return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
		},
	}
	response, err := d.get(context.Background(), "http://8.8.8.8/image", "test", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
}

func TestDownloadHTTPSUsesLogicalSNIAndVerifiesCertificate(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		DNSNames: []string{"mirror.test"}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(certificate)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, r.Host) }))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	server.StartTLS()
	defer server.Close()
	d := downloader{
		resolver: lookupFunc(func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
		}),
		dialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if address != "8.8.8.8:443" {
				return nil, fmt.Errorf("unexpected TLS endpoint: %s", address)
			}
			return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
		},
		tlsConfig: &tls.Config{RootCAs: roots},
	}
	response, err := d.get(context.Background(), "https://mirror.test/image", "test", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || string(body) != "mirror.test" || response.TLS.ServerName != "mirror.test" || len(response.TLS.VerifiedChains) == 0 {
		t.Fatalf("logical HTTPS identity lost: body=%q TLS=%+v err=%v", body, response.TLS, err)
	}
	if response, err := d.get(context.Background(), "https://wrong.test/image", "test", time.Second); err == nil {
		response.Body.Close()
		t.Fatal("HTTPS accepted a certificate for a different logical hostname")
	}
}

func TestDownloadTimeoutCoversRedirectsAndReturnedBody(t *testing.T) {
	var handlers sync.WaitGroup
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlers.Add(1)
		defer handlers.Done()
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/body", http.StatusFound)
			return
		}
		_, _ = io.WriteString(w, "started")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	d := downloader{dialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}}
	response, err := d.get(context.Background(), "http://8.8.8.8/start", "test", 300*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if string(body) != "started" || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("response body lost the total download deadline: body=%q err=%v", body, err)
	}
	handlers.Wait()
}
