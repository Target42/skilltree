package config

import (
	"fmt"
	"net/netip"
	"os"
	"strings"
)

const DefaultSessionSecret = "dev-only-change-me-skilltree-session"

// DefaultTrustedProxies suits nginx on the same host terminating TLS.
const DefaultTrustedProxies = "127.0.0.1,::1"

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

	trustedProxiesErr error
}

func Load() Config {
	rawProxies := env("SKILLTREE_TRUSTED_PROXIES", DefaultTrustedProxies)
	proxies, err := ParseTrustedProxies(rawProxies)
	return Config{
		HTTPAddr:          env("SKILLTREE_HTTP_ADDR", "127.0.0.1:8080"),
		DatabaseURL:       env("SKILLTREE_DATABASE_URL", "postgres://skilltree:skilltree@127.0.0.1:55432/skilltree?sslmode=disable"),
		ServiceName:       env("SKILLTREE_SERVICE_NAME", "skilltree"),
		DisplayName:       env("SKILLTREE_DISPLAY_NAME", "Skilltree"),
		SessionSecret:     env("SKILLTREE_SESSION_SECRET", DefaultSessionSecret),
		CookieSecure:      strings.ToLower(env("SKILLTREE_COOKIE_SECURE", "auto")),
		TrustedProxies:    proxies,
		Production:        strings.EqualFold(os.Getenv("SKILLTREE_ENV"), "production"),
		trustedProxiesErr: err,
	}
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
