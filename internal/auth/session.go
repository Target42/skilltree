package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const cookieName = "skilltree_session"

type ctxKey int

const (
	userIDKey   ctxKey = 1
	tenantIDKey ctxKey = 2
	csrfKey     ctxKey = 3
	secureKey   ctxKey = 4
)

var ErrInvalidSession = errors.New("invalid session")

type Manager struct {
	secret []byte
	ttl    time.Duration
}

func NewManager(secret string) *Manager {
	if secret == "" {
		secret = "dev-only-change-me-skilltree-session"
	}
	return &Manager{secret: []byte(secret), ttl: 30 * 24 * time.Hour}
}

func (m *Manager) SetSession(w http.ResponseWriter, userID int64, secure bool) {
	exp := time.Now().Add(m.ttl).Unix()
	payload := strconv.FormatInt(userID, 10) + "|" + strconv.FormatInt(exp, 10)
	mac := hmac.New(sha256.New, m.secret)
	_, _ = mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	val := base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + sig
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    val,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
		Expires:  time.Unix(exp, 0),
	})
}

func (m *Manager) ClearSession(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}

func (m *Manager) UserID(r *http.Request) (int64, error) {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return 0, ErrInvalidSession
	}
	parts := strings.Split(c.Value, ".")
	if len(parts) != 2 {
		return 0, ErrInvalidSession
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return 0, ErrInvalidSession
	}
	payload := string(raw)
	mac := hmac.New(sha256.New, m.secret)
	_, _ = mac.Write([]byte(payload))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(parts[1])) {
		return 0, ErrInvalidSession
	}
	fields := strings.Split(payload, "|")
	if len(fields) != 2 {
		return 0, ErrInvalidSession
	}
	uid, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return 0, ErrInvalidSession
	}
	exp, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return 0, ErrInvalidSession
	}
	return uid, nil
}

func WithUserID(ctx context.Context, userID int64) context.Context {
	return context.WithValue(ctx, userIDKey, userID)
}

func UserIDFromContext(ctx context.Context) (int64, bool) {
	v, ok := ctx.Value(userIDKey).(int64)
	return v, ok
}

func WithTenantID(ctx context.Context, tenantID int64) context.Context {
	return context.WithValue(ctx, tenantIDKey, tenantID)
}

func TenantIDFromContext(ctx context.Context) (int64, bool) {
	v, ok := ctx.Value(tenantIDKey).(int64)
	return v, ok
}

func WithCSRF(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, csrfKey, token)
}

func CSRFFromContext(ctx context.Context) string {
	v, _ := ctx.Value(csrfKey).(string)
	return v
}

func WithCookieSecure(ctx context.Context, secure bool) context.Context {
	return context.WithValue(ctx, secureKey, secure)
}

func CookieSecureFromContext(ctx context.Context) bool {
	v, _ := ctx.Value(secureKey).(bool)
	return v
}
