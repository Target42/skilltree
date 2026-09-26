package server

import (
	"net"
	"net/http"
	"strings"
)

const maxRequestBody = 1 << 20 // 1 MiB

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) fromTrustedProxy(r *http.Request) bool {
	return s.cfg.IsTrustedProxy(remoteIP(r))
}

// clientIP returns the client address for rate limiting and logging.
// X-Forwarded-For / X-Real-IP are honored only when the direct peer is trusted.
func (s *Server) clientIP(r *http.Request) string {
	direct := remoteIP(r)
	if !s.cfg.IsTrustedProxy(direct) {
		return direct
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		// Prefer rightmost non-trusted hop (resists client-spoofed prepended IPs
		// when a proxy appends). With nginx overwriting XFF to $remote_addr,
		// there is typically a single entry — the real client.
		for i := len(parts) - 1; i >= 0; i-- {
			ip := strings.TrimSpace(parts[i])
			if ip == "" {
				continue
			}
			if !s.cfg.IsTrustedProxy(ip) {
				return ip
			}
		}
		if ip := strings.TrimSpace(parts[0]); ip != "" {
			return ip
		}
	}
	if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" {
		return xri
	}
	return direct
}

func (s *Server) forwardedProto(r *http.Request) string {
	if !s.fromTrustedProxy(r) {
		return ""
	}
	return r.Header.Get("X-Forwarded-Proto")
}

func (s *Server) cookieSecure(r *http.Request) bool {
	return s.cfg.CookieSecureForRequest(r.TLS != nil, s.forwardedProto(r))
}

func setSecurityHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "same-origin")
	h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
	// Full CSP is set at nginx; keep a moderate baseline if the app is hit directly.
	h.Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self' https://unpkg.com; img-src 'self' data:; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
}
