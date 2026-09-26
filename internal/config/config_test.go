package config_test

import (
	"testing"

	"skilltree/internal/config"
)

func TestValidateProductionRequiresSecret(t *testing.T) {
	cfg := config.Config{
		HTTPAddr:      "127.0.0.1:8080",
		DatabaseURL:   "postgres://x",
		SessionSecret: config.DefaultSessionSecret,
		CookieSecure:  "auto",
		Production:    true,
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for default secret in production")
	}
	cfg.SessionSecret = "abcdefghijklmnopqrstuvwxyz012345"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("strong secret should pass: %v", err)
	}
}

func TestCookieSecureForRequest(t *testing.T) {
	cfg := config.Config{CookieSecure: "auto"}
	if !cfg.CookieSecureForRequest(true, "") {
		t.Fatal("TLS should be secure")
	}
	if !cfg.CookieSecureForRequest(false, "https") {
		t.Fatal("forwarded https should be secure")
	}
	if cfg.CookieSecureForRequest(false, "http") {
		t.Fatal("http should not be secure in auto mode")
	}
	cfg.CookieSecure = "true"
	if !cfg.CookieSecureForRequest(false, "http") {
		t.Fatal("always secure")
	}
	cfg.CookieSecure = "false"
	if cfg.CookieSecureForRequest(true, "https") {
		t.Fatal("never secure")
	}
}

func TestParseTrustedProxies(t *testing.T) {
	list, err := config.ParseTrustedProxies("127.0.0.1, ::1, 10.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("got %d", len(list))
	}
	cfg := config.Config{TrustedProxies: list}
	if !cfg.IsTrustedProxy("127.0.0.1") || !cfg.IsTrustedProxy("10.1.2.3") {
		t.Fatal("expected trusted")
	}
	if cfg.IsTrustedProxy("203.0.113.1") {
		t.Fatal("public IP must not be trusted")
	}
	none, err := config.ParseTrustedProxies("none")
	if err != nil || none != nil {
		t.Fatalf("none: %v %#v", err, none)
	}
}
