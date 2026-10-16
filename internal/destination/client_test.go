package destination

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func TestURLPolicy(t *testing.T) {
	for _, raw := range []string{"http://example.com", "https://example.com:8443", "https://user:pass@example.com", "https://example.com/#secret", "https://127.0.0.1", "https://10.0.0.1", "https://169.254.169.254", "https://100.64.0.1", "https://[::1]", "https://[::ffff:127.0.0.1]", "https://[64:ff9b::7f00:1]", "https://[2002:7f00:1::]", "https://192.0.2.1", "file:///tmp/test"} {
		if _, err := Validate(raw, false); err == nil {
			t.Errorf("allowed %s", raw)
		}
	}
	for _, raw := range []string{"https://example.com/webhook", "https://8.8.8.8:443", "https://[2606:4700:4700::1111]/"} {
		if _, err := Validate(raw, false); err != nil {
			t.Errorf("blocked %s: %v", raw, err)
		}
	}
	if _, err := Validate("http://127.0.0.1:9090", true); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"http://localhost:9090", "http://10.0.0.1:9090", "https://example.com"} {
		if _, err := Validate(raw, true); err == nil {
			t.Fatal("test flag broadened policy")
		}
	}
}
func TestDNSValidationAndPinnedDial(t *testing.T) {
	calls := 0
	var dialed string
	dial := func(_ context.Context, _, address string) (net.Conn, error) {
		calls++
		dialed = address
		return nil, errors.New("simulated network failure")
	}
	answer := []netip.Addr{netip.MustParseAddr("8.8.8.8")}
	lookup := func(context.Context, string) ([]netip.Addr, error) { return answer, nil }
	guarded := guardedDial(false, lookup, dial)
	guarded(context.Background(), "tcp", "example.com:443")
	if calls != 1 || dialed != "8.8.8.8:443" {
		t.Fatal("hostname re-resolved by dial", dialed)
	}
	answer = []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("127.0.0.1")}
	guarded(context.Background(), "tcp", "example.com:443")
	if calls != 1 {
		t.Fatal("mixed DNS reached dial")
	}
	answer = []netip.Addr{netip.MustParseAddr("169.254.169.254")}
	guarded(context.Background(), "tcp", "example.com:443")
	if calls != 1 {
		t.Fatal("DNS rebinding reached dial")
	}
	guarded(context.Background(), "tcp", "example.com:80")
	if calls != 1 {
		t.Fatal("unsafe port reached dial")
	}
}
func TestNoRedirectOrProxy(t *testing.T) {
	var requests int
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests++; w.WriteHeader(204) }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(302)
	}))
	defer source.Close()
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	client, err := Client(source.URL, true)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Post(source.URL, "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 302 || requests != 0 {
		t.Fatal("followed redirect")
	}
}

func TestTLSCertificateIsVerified(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer server.Close()
	client, err := Client(server.URL, true)
	if err != nil {
		t.Fatal(err)
	}
	if response, err := client.Post(server.URL, "application/json", strings.NewReader("{}")); err == nil {
		response.Body.Close()
		t.Fatal("untrusted certificate accepted")
	}
}
