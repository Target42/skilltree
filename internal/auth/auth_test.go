package auth_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"skilltree/internal/auth"
)

func TestLoginLimiter(t *testing.T) {
	l := auth.NewLoginLimiter(3, time.Minute)
	key := "1.2.3.4|demo|alice"
	for i := 0; i < 3; i++ {
		if !l.Allow(key) {
			t.Fatalf("attempt %d should be allowed", i+1)
		}
		l.Fail(key)
	}
	if l.Allow(key) {
		t.Fatal("4th attempt should be blocked")
	}
	l.Success(key)
	if !l.Allow(key) {
		t.Fatal("after success, attempts should reset")
	}
}

func TestCSRFRoundTrip(t *testing.T) {
	c := auth.NewCSRF()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	token := c.Ensure(rec, req, false)
	if token == "" {
		t.Fatal("expected token")
	}
	cookies := rec.Result().Cookies()
	var csrfCookie *http.Cookie
	for _, ck := range cookies {
		if ck.Name == "skilltree_csrf" {
			csrfCookie = ck
		}
	}
	if csrfCookie == nil {
		t.Fatal("csrf cookie not set")
	}

	body := strings.NewReader("csrf_token=" + token)
	post := httptest.NewRequest(http.MethodPost, "/login", body)
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.Header.Set("Origin", "http://example.com")
	post.Host = "example.com"
	post.AddCookie(csrfCookie)
	if err := c.Verify(post); err != nil {
		t.Fatalf("verify: %v", err)
	}

	bad := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader("csrf_token=nope"))
	bad.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	bad.AddCookie(csrfCookie)
	if err := c.Verify(bad); err == nil {
		t.Fatal("expected verify failure")
	}
}
