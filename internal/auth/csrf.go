package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

const csrfCookieName = "skilltree_csrf"
const csrfFieldName = "csrf_token"

var (
	ErrCSRFMissing = errors.New("csrf token missing")
	ErrCSRFInvalid = errors.New("csrf token invalid")
)

// CSRF protects state-changing requests via a double-submit cookie + form field.
type CSRF struct{}

func NewCSRF() *CSRF { return &CSRF{} }

func (c *CSRF) Ensure(w http.ResponseWriter, r *http.Request, secure bool) string {
	if cookie, err := r.Cookie(csrfCookieName); err == nil {
		if token, ok := decodeCSRFToken(cookie.Value); ok {
			return token
		}
	}
	return c.Rotate(w, secure)
}

func (c *CSRF) Rotate(w http.ResponseWriter, secure bool) string {
	token := newCSRFToken()
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
	})
	return token
}

func (c *CSRF) Verify(r *http.Request) error {
	cookie, err := r.Cookie(csrfCookieName)
	if err != nil {
		return ErrCSRFMissing
	}
	cookieToken, ok := decodeCSRFToken(cookie.Value)
	if !ok {
		return ErrCSRFInvalid
	}
	if err := r.ParseForm(); err != nil {
		return ErrCSRFMissing
	}
	formToken := strings.TrimSpace(r.FormValue(csrfFieldName))
	if formToken == "" {
		return ErrCSRFMissing
	}
	if subtle.ConstantTimeCompare([]byte(cookieToken), []byte(formToken)) != 1 {
		return ErrCSRFInvalid
	}
	if err := checkOrigin(r); err != nil {
		return err
	}
	return nil
}

func (c *CSRF) Clear(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
		MaxAge:   -1,
	})
}

func FieldName() string { return csrfFieldName }

func newCSRFToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// Extremely unlikely; fall back to zeros would be unsafe — panic like crypto/rand docs suggest for apps.
		panic("csrf: crypto/rand failed: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCSRFToken(v string) (string, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(v)
	if err != nil || len(decoded) < 16 {
		return "", false
	}
	return v, true
}

func checkOrigin(r *http.Request) error {
	origin := r.Header.Get("Origin")
	if origin == "" {
		// Some browsers omit Origin on same-site POSTs; Referer is a weaker fallback.
		ref := r.Header.Get("Referer")
		if ref == "" {
			return nil
		}
		u, err := url.Parse(ref)
		if err != nil {
			return ErrCSRFInvalid
		}
		if !hostMatches(r.Host, u.Host) {
			return ErrCSRFInvalid
		}
		return nil
	}
	u, err := url.Parse(origin)
	if err != nil {
		return ErrCSRFInvalid
	}
	if !hostMatches(r.Host, u.Host) {
		return ErrCSRFInvalid
	}
	return nil
}

func hostMatches(reqHost, otherHost string) bool {
	reqHost = strings.ToLower(reqHost)
	otherHost = strings.ToLower(otherHost)
	if reqHost == otherHost {
		return true
	}
	// Strip port from both if only one side includes it inconsistently.
	reqH, reqP, _ := strings.Cut(reqHost, ":")
	othH, othP, _ := strings.Cut(otherHost, ":")
	if reqH == othH {
		if reqP == "" || othP == "" || reqP == othP {
			return true
		}
	}
	return false
}
