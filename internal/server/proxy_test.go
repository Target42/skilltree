package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"skilltree/internal/config"
)

func TestClientIPTrustedProxy(t *testing.T) {
	proxies, err := config.ParseTrustedProxies("127.0.0.1,::1")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: config.Config{TrustedProxies: proxies, CookieSecure: "auto"}}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	if got := s.clientIP(req); got != "203.0.113.9" {
		t.Fatalf("trusted proxy XFF: got %q", got)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.RemoteAddr = "198.51.100.1:1234"
	req2.Header.Set("X-Forwarded-For", "203.0.113.9")
	if got := s.clientIP(req2); got != "198.51.100.1" {
		t.Fatalf("untrusted peer must ignore XFF: got %q", got)
	}
}

func TestForwardedProtoOnlyFromTrusted(t *testing.T) {
	proxies, err := config.ParseTrustedProxies("127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: config.Config{TrustedProxies: proxies, CookieSecure: "auto"}}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:1"
	req.Header.Set("X-Forwarded-Proto", "https")
	if !s.cookieSecure(req) {
		t.Fatal("expected Secure from trusted X-Forwarded-Proto")
	}

	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.RemoteAddr = "198.51.100.1:1"
	req2.Header.Set("X-Forwarded-Proto", "https")
	if s.cookieSecure(req2) {
		t.Fatal("untrusted peer must not force Secure via spoofed proto")
	}
}

func TestClientIPSpoofedAppend(t *testing.T) {
	proxies, err := config.ParseTrustedProxies("127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: config.Config{TrustedProxies: proxies}}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:1"
	// Client prepended fake IP; nginx appended real client (or we overwrite — both cases).
	req.Header.Set("X-Forwarded-For", "1.2.3.4, 203.0.113.50")
	if got := s.clientIP(req); got != "203.0.113.50" {
		t.Fatalf("expected rightmost untrusted IP, got %q", got)
	}
}
