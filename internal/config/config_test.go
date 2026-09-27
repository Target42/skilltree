package config_test

import (
	"os"
	"testing"

	"skilltree/internal/config"
)

func TestValidateProductionRequiresSecret(t *testing.T) {
	cfg := config.Config{
		HTTPAddr:      "127.0.0.1:8088",
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

func TestValidateSMTP(t *testing.T) {
	base := config.Config{
		HTTPAddr:     "127.0.0.1:8088",
		DatabaseURL:  "postgres://x",
		CookieSecure: "auto",
		SMTPTLSMode:  "starttls",
		SMTPPort:     587,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("empty smtp should be ok: %v", err)
	}
	partial := base
	partial.SMTPHost = "smtp.example.com"
	if err := partial.Validate(); err == nil {
		t.Fatal("host without from should fail")
	}
	ok := base
	ok.SMTPHost = "smtp.example.com"
	ok.SMTPFrom = "noreply@example.com"
	if err := ok.Validate(); err != nil {
		t.Fatalf("complete smtp should pass: %v", err)
	}
	if !ok.MailEnabled() {
		t.Fatal("expected mail enabled")
	}
	badTLS := ok
	badTLS.SMTPTLSMode = "ssl"
	if err := badTLS.Validate(); err == nil {
		t.Fatal("invalid tls mode should fail")
	}
}

func TestLoadDotEnv(t *testing.T) {
	dir := t.TempDir()
	path := dir + string(os.PathSeparator) + "test.env"
	content := "# comment\nFOO=bar\nexport BAZ=\"qux\"\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FOO", "keep")
	t.Setenv("BAZ", "")
	_ = os.Unsetenv("BAZ")
	if err := config.LoadDotEnv(path); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("FOO"); got != "keep" {
		t.Fatalf("must not override existing: %q", got)
	}
	if got := os.Getenv("BAZ"); got != "qux" {
		t.Fatalf("BAZ=%q", got)
	}
	if err := config.LoadDotEnv(dir + string(os.PathSeparator) + "missing.env"); err != nil {
		t.Fatalf("missing file should be ignored: %v", err)
	}
}
