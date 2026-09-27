package server

import (
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"skilltree/internal/auth"
	"skilltree/internal/store"
)

func parseUsersFilter(r *http.Request) store.ListUsersFilter {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	return store.ListUsersFilter{
		Query:  r.URL.Query().Get("q"),
		Letter: r.URL.Query().Get("letter"),
		Page:   page,
	}
}

func parseUsersFilterFromForm(r *http.Request) store.ListUsersFilter {
	page, _ := strconv.Atoi(r.FormValue("list_page"))
	return store.ListUsersFilter{
		Query:  r.FormValue("list_q"),
		Letter: r.FormValue("list_letter"),
		Page:   page,
	}.Normalized()
}

func usersListPath(f store.ListUsersFilter, extra url.Values) string {
	f = f.Normalized()
	q := url.Values{}
	if f.Query != "" {
		q.Set("q", f.Query)
	}
	if f.Letter != "" {
		q.Set("letter", strings.ToUpper(f.Letter))
	}
	if f.Page > 1 {
		q.Set("page", strconv.Itoa(f.Page))
	}
	for k, vals := range extra {
		for _, v := range vals {
			q.Set(k, v)
		}
	}
	if len(q) == 0 {
		return "/admin/users"
	}
	return "/admin/users?" + q.Encode()
}

func alphabetLetters() []string {
	out := make([]string, 26)
	for i := 0; i < 26; i++ {
		out[i] = string(rune('A' + i))
	}
	return out
}

func (s *Server) adminUsers(w http.ResponseWriter, r *http.Request) {
	user, err := s.currentUser(r)
	if err != nil {
		s.fail(w, err)
		return
	}
	if !user.IsAdmin {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	filter := parseUsersFilter(r)
	result, err := s.store.ListUsersByTenant(r.Context(), user.TenantID, filter)
	if err != nil {
		s.fail(w, err)
		return
	}
	filter.Page = result.Page
	filter.PageSize = result.PageSize

	s.render(w, r, "admin_users", map[string]any{
		"Title":      "Benutzer · Skilltree",
		"User":       user,
		"Users":      result.Users,
		"Total":      result.Total,
		"Page":       result.Page,
		"PageSize":   result.PageSize,
		"TotalPages": result.TotalPages,
		"Query":      filter.Query,
		"Letter":     strings.ToUpper(filter.Letter),
		"Letters":    alphabetLetters(),
		"PrevPage":   result.Page - 1,
		"NextPage":   result.Page + 1,
		"HasPrev":    result.Page > 1,
		"HasNext":    result.Page < result.TotalPages,
		"Error":      r.URL.Query().Get("error"),
		"Ok":         r.URL.Query().Get("ok"),
		"ListQ":      filter.Query,
		"ListLetter": strings.ToUpper(filter.Letter),
		"ListPage":   result.Page,
	})
}

func (s *Server) adminUserCreate(w http.ResponseWriter, r *http.Request) {
	user, err := s.currentUser(r)
	if err != nil {
		s.fail(w, err)
		return
	}
	if !user.IsAdmin {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	filter := parseUsersFilterFromForm(r)
	_, err = s.store.CreateUser(r.Context(), store.CreateUserInput{
		TenantID:          user.TenantID,
		Username:          strings.TrimSpace(r.FormValue("username")),
		DisplayName:       strings.TrimSpace(r.FormValue("display_name")),
		Email:             r.FormValue("email"),
		Password:          r.FormValue("password"),
		IsAdmin:           formBool(r, "is_admin"),
		IsDeletionOfficer: formBool(r, "is_deletion_officer"),
		IsActive:          true,
	})
	if err != nil {
		if errors.Is(err, store.ErrDuplicate) {
			http.Redirect(w, r, usersListPath(filter, url.Values{"error": {"duplicate"}}), http.StatusSeeOther)
			return
		}
		if errors.Is(err, store.ErrWeakPassword) {
			http.Redirect(w, r, usersListPath(filter, url.Values{"error": {"password"}}), http.StatusSeeOther)
			return
		}
		if errors.Is(err, store.ErrInvalidEmail) {
			http.Redirect(w, r, usersListPath(filter, url.Values{"error": {"email"}}), http.StatusSeeOther)
			return
		}
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, usersListPath(filter, url.Values{"ok": {"created"}}), http.StatusSeeOther)
}

func (s *Server) adminUserUpdate(w http.ResponseWriter, r *http.Request) {
	user, err := s.currentUser(r)
	if err != nil {
		s.fail(w, err)
		return
	}
	if !user.IsAdmin {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	targetID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	filter := parseUsersFilterFromForm(r)

	target, err := s.store.GetUserInTenant(r.Context(), user.TenantID, targetID)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	isAdmin := formBool(r, "is_admin")
	isActive := formBool(r, "is_active")
	// Prevent locking yourself out of tenant admin.
	if target.ID == user.ID {
		isAdmin = true
		isActive = true
	}

	err = s.store.UpdateUser(r.Context(), store.UpdateUserInput{
		TenantID:          user.TenantID,
		UserID:            targetID,
		DisplayName:       strings.TrimSpace(r.FormValue("display_name")),
		Email:             r.FormValue("email"),
		IsAdmin:           isAdmin,
		IsDeletionOfficer: formBool(r, "is_deletion_officer"),
		IsActive:          isActive,
		NewPassword:       r.FormValue("password"),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		if errors.Is(err, store.ErrWeakPassword) {
			http.Redirect(w, r, usersListPath(filter, url.Values{"error": {"password"}}), http.StatusSeeOther)
			return
		}
		if errors.Is(err, store.ErrInvalidEmail) {
			http.Redirect(w, r, usersListPath(filter, url.Values{"error": {"email"}}), http.StatusSeeOther)
			return
		}
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, usersListPath(filter, url.Values{"ok": {"updated"}}), http.StatusSeeOther)
}

func (s *Server) platformTenants(w http.ResponseWriter, r *http.Request) {
	user, err := s.currentUser(r)
	if err != nil {
		s.fail(w, err)
		return
	}
	if !user.IsPlatformAdmin {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	tenants, err := s.store.ListTenants(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, r, "platform_tenants", map[string]any{
		"Title":   "Mandanten · Skilltree",
		"User":    user,
		"Tenants": tenants,
		"Error":   r.URL.Query().Get("error"),
		"Ok":      r.URL.Query().Get("ok"),
	})
}

func (s *Server) platformTenantCreate(w http.ResponseWriter, r *http.Request) {
	user, err := s.currentUser(r)
	if err != nil {
		s.fail(w, err)
		return
	}
	if !user.IsPlatformAdmin {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	_, _, err = s.store.CreateTenantWithAdmin(
		r.Context(),
		r.FormValue("slug"),
		r.FormValue("name"),
		r.FormValue("admin_username"),
		r.FormValue("admin_display_name"),
		r.FormValue("admin_email"),
		r.FormValue("admin_password"),
	)
	if err != nil {
		if errors.Is(err, store.ErrDuplicate) {
			http.Redirect(w, r, "/platform/tenants?error=duplicate", http.StatusSeeOther)
			return
		}
		if errors.Is(err, store.ErrWeakPassword) {
			http.Redirect(w, r, "/platform/tenants?error=password", http.StatusSeeOther)
			return
		}
		if store.ValidateSlug(store.NormalizeSlug(r.FormValue("slug"))) != nil {
			http.Redirect(w, r, "/platform/tenants?error=slug", http.StatusSeeOther)
			return
		}
		s.log.Error("create tenant", "err", err)
		http.Redirect(w, r, "/platform/tenants?error=create", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/platform/tenants?ok=created", http.StatusSeeOther)
}

func (s *Server) platformTenantUpdate(w http.ResponseWriter, r *http.Request) {
	user, err := s.currentUser(r)
	if err != nil {
		s.fail(w, err)
		return
	}
	if !user.IsPlatformAdmin {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if name := strings.TrimSpace(r.FormValue("name")); name != "" {
		if err := s.store.UpdateTenant(r.Context(), id, name); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				http.NotFound(w, r)
				return
			}
			s.fail(w, err)
			return
		}
	}
	if r.FormValue("set_active") != "" {
		if err := s.store.SetTenantActive(r.Context(), id, formBool(r, "is_active")); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				http.NotFound(w, r)
				return
			}
			s.fail(w, err)
			return
		}
	}
	http.Redirect(w, r, "/platform/tenants?ok=updated", http.StatusSeeOther)
}

func (s *Server) platformTenantDelete(w http.ResponseWriter, r *http.Request) {
	user, err := s.currentUser(r)
	if err != nil {
		s.fail(w, err)
		return
	}
	if !user.IsPlatformAdmin {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	own := id == user.TenantID
	err = s.store.DeleteTenant(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		s.log.Error("delete tenant", "err", err)
		http.Redirect(w, r, "/platform/tenants?error=delete", http.StatusSeeOther)
		return
	}
	if own {
		secure := auth.CookieSecureFromContext(r.Context())
		s.sessions.ClearSession(w, secure)
		s.csrf.Rotate(w, secure)
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/platform/tenants?ok=deleted", http.StatusSeeOther)
}

func (s *Server) adminFirma(w http.ResponseWriter, r *http.Request) {
	user, err := s.currentUser(r)
	if err != nil {
		s.fail(w, err)
		return
	}
	if !user.IsAdmin {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	tenant, err := s.store.GetTenantByID(r.Context(), user.TenantID)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, r, "admin_firma", map[string]any{
		"Title":  "Firma · Skilltree",
		"User":   user,
		"Tenant": tenant,
		"Error":  r.URL.Query().Get("error"),
		"Ok":     r.URL.Query().Get("ok"),
	})
}

func (s *Server) adminFirmaSubmit(w http.ResponseWriter, r *http.Request) {
	user, err := s.currentUser(r)
	if err != nil {
		s.fail(w, err)
		return
	}
	if !user.IsAdmin {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	switch r.FormValue("action") {
	case "rename":
		name := strings.TrimSpace(r.FormValue("name"))
		if name == "" {
			http.Redirect(w, r, "/admin/firma?error=name", http.StatusSeeOther)
			return
		}
		if err := s.store.UpdateTenant(r.Context(), user.TenantID, name); err != nil {
			s.fail(w, err)
			return
		}
		http.Redirect(w, r, "/admin/firma?ok=updated", http.StatusSeeOther)
	case "delete":
		tenant, err := s.store.GetTenantByID(r.Context(), user.TenantID)
		if err != nil {
			s.fail(w, err)
			return
		}
		if store.NormalizeSlug(r.FormValue("confirm_slug")) != tenant.Slug {
			http.Redirect(w, r, "/admin/firma?error=confirm", http.StatusSeeOther)
			return
		}
		if err := s.store.DeleteTenant(r.Context(), user.TenantID); err != nil {
			s.log.Error("delete own tenant", "err", err)
			http.Redirect(w, r, "/admin/firma?error=delete", http.StatusSeeOther)
			return
		}
		secure := auth.CookieSecureFromContext(r.Context())
		s.sessions.ClearSession(w, secure)
		s.csrf.Rotate(w, secure)
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	default:
		http.Redirect(w, r, "/admin/firma", http.StatusSeeOther)
	}
}
