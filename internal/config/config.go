package config

import (
	"fmt"
	"net/netip"
	"os"
	"strconv"
	"strings"
)

const DefaultSessionSecret = "dev-only-change-me-skilltree-session"

// DefaultTrustedProxies suits nginx on the same host terminating TLS.
const DefaultTrustedProxies = "127.0.0.1,::1"

const DefaultSMTPPort = 587

type Config struct {
	HTTPAddr      string
	DatabaseURL   string
	ServiceName   string
	DisplayName   string
	SessionSecret string
	// CookieSecure: "auto" (default), "true", or "false".
	CookieSecure string
	// TrustedProxies are IPs/CIDRs allowed to set X-Forwarded-* (e.g. local nginx).
	TrustedProxies []netip.Prefix
	// Production enables stricter checks (required strong session secret).
	Production bool

	SMTPHost     string
	SMTPPort     int
	SMTPUser     string
	SMTPPassword string
	// SMTPFrom is the envelope/header From address (e.g. skilltree@example.com).
	SMTPFrom string
	// SMTPTLSMode: starttls (default), tls (implicit, port 465), or none.
	SMTPTLSMode string

	trustedProxiesErr error
	smtpPortErr       error
}

func Load() Config {
	rawProxies := env("SKILLTREE_TRUSTED_PROXIES", DefaultTrustedProxies)
	proxies, err := ParseTrustedProxies(rawProxies)
	port, portErr := envInt("SKILLTREE_SMTP_PORT", DefaultSMTPPort)
	return Config{
		HTTPAddr:          env("SKILLTREE_HTTP_ADDR", "127.0.0.1:8088"),
		DatabaseURL:       env("SKILLTREE_DATABASE_URL", "postgres://skilltree:skilltree@127.0.0.1:55432/skilltree?sslmode=disable"),
		ServiceName:       env("SKILLTREE_SERVICE_NAME", "skilltree"),
		DisplayName:       env("SKILLTREE_DISPLAY_NAME", "Skilltree"),
		SessionSecret:     env("SKILLTREE_SESSION_SECRET", DefaultSessionSecret),
		CookieSecure:      strings.ToLower(env("SKILLTREE_COOKIE_SECURE", "auto")),
		TrustedProxies:    proxies,
		Production:        strings.EqualFold(os.Getenv("SKILLTREE_ENV"), "production"),
		SMTPHost:          strings.TrimSpace(os.Getenv("SKILLTREE_SMTP_HOST")),
		SMTPPort:          port,
		SMTPUser:          os.Getenv("SKILLTREE_SMTP_USER"),
		SMTPPassword:      os.Getenv("SKILLTREE_SMTP_PASSWORD"),
		SMTPFrom:          strings.TrimSpace(os.Getenv("SKILLTREE_SMTP_FROM")),
		SMTPTLSMode:      strings.ToLower(env("SKILLTREE_SMTP_TLS", "starttls")),
		trustedProxiesErr: err,
		smtpPortErr:       portErr,
	}
}

// MailEnabled reports whether outbound mail is configured (host + from).
func (c Config) MailEnabled() bool {
	return c.SMTPHost != "" && c.SMTPFrom != ""
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.DatabaseURL) == "" {
		return fmt.Errorf("SKILLTREE_DATABASE_URL is required")
	}
	if strings.TrimSpace(c.HTTPAddr) == "" {
		return fmt.Errorf("SKILLTREE_HTTP_ADDR is required")
	}
	switch c.CookieSecure {
	case "auto", "true", "false", "1", "0", "always", "never":
	default:
		return fmt.Errorf("SKILLTREE_COOKIE_SECURE must be auto, true, or false")
	}
	if c.trustedProxiesErr != nil {
		return fmt.Errorf("SKILLTREE_TRUSTED_PROXIES: %w", c.trustedProxiesErr)
	}
	if c.smtpPortErr != nil {
		return fmt.Errorf("SKILLTREE_SMTP_PORT: %w", c.smtpPortErr)
	}
	if err := c.validateSMTP(); err != nil {
		return err
	}
	if c.Production {
		if c.SessionSecret == "" || c.SessionSecret == DefaultSessionSecret {
			return fmt.Errorf("SKILLTREE_SESSION_SECRET must be set to a non-default value when SKILLTREE_ENV=production")
		}
		if len(c.SessionSecret) < 32 {
			return fmt.Errorf("SKILLTREE_SESSION_SECRET must be at least 32 characters when SKILLTREE_ENV=production")
		}
	}
	return nil
}

func (c Config) validateSMTP() error {
	if c.SMTPHost == "" && c.SMTPFrom == "" && c.SMTPUser == "" && c.SMTPPassword == "" {
		return nil
	}
	if c.SMTPHost == "" {
		return fmt.Errorf("SKILLTREE_SMTP_HOST is required when SMTP is configured")
	}
	if c.SMTPFrom == "" {
		return fmt.Errorf("SKILLTREE_SMTP_FROM is required when SMTP is configured")
	}
	if c.SMTPPort < 1 || c.SMTPPort > 65535 {
		return fmt.Errorf("SKILLTREE_SMTP_PORT must be between 1 and 65535")
	}
	switch c.SMTPTLSMode {
	case "starttls", "tls", "none":
	default:
		return fmt.Errorf("SKILLTREE_SMTP_TLS must be starttls, tls, or none")
	}
	return nil
}

func (c Config) UsingDevSessionSecret() bool {
	return c.SessionSecret == "" || c.SessionSecret == DefaultSessionSecret
}

// CookieSecureForRequest reports whether Set-Cookie should include Secure.
func (c Config) CookieSecureForRequest(tls bool, forwardedProto string) bool {
	switch c.CookieSecure {
	case "true", "1", "always":
		return true
	case "false", "0", "never":
		return false
	default: // auto
		if tls {
			return true
		}
		return strings.EqualFold(forwardedProto, "https")
	}
}

// IsTrustedProxy reports whether addr (host without port) is a configured proxy.
func (c Config) IsTrustedProxy(ipStr string) bool {
	ip, err := netip.ParseAddr(ipStr)
	if err != nil {
		return false
	}
	ip = ip.Unmap()
	for _, p := range c.TrustedProxies {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// ParseTrustedProxies parses a comma-separated list of IPs or CIDRs.
// Bare IPs become /32 or /128 prefixes.
func ParseTrustedProxies(raw string) ([]netip.Prefix, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "-" || strings.EqualFold(raw, "none") {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	out := make([]netip.Prefix, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if prefix, err := netip.ParsePrefix(p); err == nil {
			out = append(out, prefix)
			continue
		}
		ip, err := netip.ParseAddr(p)
		if err != nil {
			return nil, fmt.Errorf("invalid entry %q", p)
		}
		bits := 32
		if ip.Is6() {
			bits = 128
		}
		out = append(out, netip.PrefixFrom(ip, bits))
	}
	return out, nil
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fallback, fmt.Errorf("invalid integer %q", raw)
	}
	return n, nil
}
