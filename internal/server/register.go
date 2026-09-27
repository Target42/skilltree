package server

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"skilltree/internal/store"
)

func (s *Server) registrationEnabled() bool {
	return s.cfg.MailEnabled() && s.mail != nil
}

func (s *Server) registerForm(w http.ResponseWriter, r *http.Request) {
	if _, err := s.sessions.UserID(r); err == nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	data := map[string]any{
		"Title":            "Firma registrieren · Skilltree",
		"Enabled":          s.registrationEnabled(),
		"Error":            r.URL.Query().Get("error"),
		"Ok":               r.URL.Query().Get("ok"),
		"Slug":             r.URL.Query().Get("slug"),
		"Name":             r.URL.Query().Get("name"),
		"AdminUsername":    r.URL.Query().Get("username"),
		"AdminDisplayName": r.URL.Query().Get("display_name"),
		"AdminEmail":       r.URL.Query().Get("email"),
	}
	s.render(w, r, "register", data)
}

func (s *Server) registerSubmit(w http.ResponseWriter, r *http.Request) {
	if !s.registrationEnabled() {
		http.Redirect(w, r, "/register?error=mail", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	slug := strings.TrimSpace(r.FormValue("slug"))
	name := strings.TrimSpace(r.FormValue("name"))
	username := strings.TrimSpace(r.FormValue("admin_username"))
	displayName := strings.TrimSpace(r.FormValue("admin_display_name"))
	email := strings.TrimSpace(r.FormValue("admin_email"))

	redirectErr := func(code string) {
		q := url.Values{}
		q.Set("error", code)
		if slug != "" {
			q.Set("slug", slug)
		}
		if name != "" {
			q.Set("name", name)
		}
		if username != "" {
			q.Set("username", username)
		}
		if displayName != "" {
			q.Set("display_name", displayName)
		}
		if email != "" {
			q.Set("email", email)
		}
		http.Redirect(w, r, "/register?"+q.Encode(), http.StatusSeeOther)
	}

	limitKey := "reg|" + s.clientIP(r)
	if !s.logins.Allow(limitKey) {
		redirectErr("rate")
		return
	}

	token, err := s.store.CreateTenantRegistration(r.Context(), store.CreateRegistrationInput{
		Slug:             slug,
		Name:             name,
		AdminUsername:    username,
		AdminDisplayName: displayName,
		AdminEmail:       email,
		Password:         r.FormValue("admin_password"),
	})
	if err != nil {
		s.logins.Fail(limitKey)
		switch {
		case errors.Is(err, store.ErrDuplicate):
			redirectErr("duplicate")
		case errors.Is(err, store.ErrWeakPassword):
			redirectErr("password")
		case errors.Is(err, store.ErrInvalidEmail):
			redirectErr("email")
		case store.ValidateSlug(store.NormalizeSlug(slug)) != nil:
			redirectErr("slug")
		default:
			s.log.Error("create registration", "err", err)
			redirectErr("create")
		}
		return
	}

	confirmURL := s.publicURL(r, "/register/confirm?token="+url.QueryEscape(token))
	body := fmt.Sprintf(`Hallo,

bitte bestätigen Sie die Registrierung Ihrer Firma „%s“ (Kürzel: %s) bei Skilltree:

%s

Der Link ist 24 Stunden gültig. Falls Sie diese Registrierung nicht angefordert haben, ignorieren Sie diese E-Mail.

Skilltree
`, name, store.NormalizeSlug(slug), confirmURL)

	if err := s.mail.Send(store.NormalizeEmail(email), "Skilltree: Firma bestätigen", body); err != nil {
		_ = s.store.DeleteTenantRegistrationBySlug(r.Context(), slug)
		s.logins.Fail(limitKey)
		s.log.Error("send registration mail", "err", err)
		redirectErr("send")
		return
	}

	s.logins.Success(limitKey)
	http.Redirect(w, r, "/register?ok=sent", http.StatusSeeOther)
}

func (s *Server) registerConfirm(w http.ResponseWriter, r *http.Request) {
	if _, err := s.sessions.UserID(r); err == nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	tenant, err := s.store.ConfirmTenantRegistration(r.Context(), token)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrRegistrationExpired):
			http.Redirect(w, r, "/register?error=expired", http.StatusSeeOther)
		case errors.Is(err, store.ErrDuplicate):
			http.Redirect(w, r, "/register?error=duplicate", http.StatusSeeOther)
		default:
			http.Redirect(w, r, "/register?error=token", http.StatusSeeOther)
		}
		return
	}
	q := url.Values{}
	q.Set("ok", "registered")
	q.Set("tenant", tenant.Slug)
	http.Redirect(w, r, "/login?"+q.Encode(), http.StatusSeeOther)
}

func (s *Server) publicURL(r *http.Request, pathQuery string) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if p := s.forwardedProto(r); p != "" {
		scheme = strings.ToLower(p)
	}
	host := r.Host
	if s.fromTrustedProxy(r) {
		if xh := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); xh != "" {
			host = strings.Split(xh, ",")[0]
			host = strings.TrimSpace(host)
		}
	}
	if host == "" {
		host = "localhost"
	}
	if _, _, err := net.SplitHostPort(host); err == nil {
		// keep host:port
	}
	if !strings.HasPrefix(pathQuery, "/") {
		pathQuery = "/" + pathQuery
	}
	return scheme + "://" + host + pathQuery
}
