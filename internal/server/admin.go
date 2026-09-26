package server

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"skilltree/internal/store"
)

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
	users, err := s.store.ListUsersByTenant(r.Context(), user.TenantID)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, r, "admin_users", map[string]any{
		"Title": "Benutzer · Skilltree",
		"User":  user,
		"Users": users,
		"Error": r.URL.Query().Get("error"),
		"Ok":    r.URL.Query().Get("ok"),
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
	_, err = s.store.CreateUser(r.Context(), store.CreateUserInput{
		TenantID:          user.TenantID,
		Username:          strings.TrimSpace(r.FormValue("username")),
		DisplayName:       strings.TrimSpace(r.FormValue("display_name")),
		Password:          r.FormValue("password"),
		IsAdmin:           formBool(r, "is_admin"),
		IsDeletionOfficer: formBool(r, "is_deletion_officer"),
		IsActive:          true,
	})
	if err != nil {
		if errors.Is(err, store.ErrDuplicate) {
			http.Redirect(w, r, "/admin/users?error=duplicate", http.StatusSeeOther)
			return
		}
		if errors.Is(err, store.ErrWeakPassword) {
			http.Redirect(w, r, "/admin/users?error=password", http.StatusSeeOther)
			return
		}
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/admin/users?ok=created", http.StatusSeeOther)
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
			http.Redirect(w, r, "/admin/users?error=password", http.StatusSeeOther)
			return
		}
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/admin/users?ok=updated", http.StatusSeeOther)
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
