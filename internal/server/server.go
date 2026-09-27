package server

import (
	"database/sql"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"skilltree/internal/auth"
	"skilltree/internal/config"
	"skilltree/internal/mail"
	"skilltree/internal/store"
	"skilltree/internal/web"
)

type Server struct {
	store    *store.Store
	tmpl     *template.Template
	log      *slog.Logger
	cfg      config.Config
	sessions *auth.Manager
	csrf     *auth.CSRF
	logins   *auth.LoginLimiter
	mail     *mail.Sender
}

func New(st *store.Store, cfg config.Config, log *slog.Logger) (*Server, error) {
	tmpl, err := web.Templates()
	if err != nil {
		return nil, err
	}
	srv := &Server{
		store:    st,
		tmpl:     tmpl,
		log:      log,
		cfg:      cfg,
		sessions: auth.NewManager(cfg.SessionSecret),
		csrf:     auth.NewCSRF(),
		logins:   auth.NewLoginLimiter(10, 15*time.Minute),
	}
	if cfg.MailEnabled() {
		srv.mail = &mail.Sender{
			Host:     cfg.SMTPHost,
			Port:     cfg.SMTPPort,
			Username: cfg.SMTPUser,
			Password: cfg.SMTPPassword,
			From:     cfg.SMTPFrom,
			TLSMode:  cfg.SMTPTLSMode,
		}
	}
	return srv, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", web.StaticHandler()))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /login", s.loginForm)
	mux.HandleFunc("POST /login", s.loginSubmit)
	mux.HandleFunc("POST /logout", s.logout)
	mux.HandleFunc("GET /register", s.registerForm)
	mux.HandleFunc("POST /register", s.registerSubmit)
	mux.HandleFunc("GET /register/confirm", s.registerConfirm)

	protected := http.NewServeMux()
	protected.HandleFunc("GET /{$}", s.workspace)
	protected.HandleFunc("GET /groups/{id}", s.workspace)
	protected.HandleFunc("GET /groups/{id}/skills/{skillID}", s.workspace)
	protected.HandleFunc("GET /groups/{id}/skills/{skillID}/manage", s.skillManage)
	protected.HandleFunc("POST /groups", s.createGroup)
	protected.HandleFunc("POST /groups/{id}", s.updateGroup)
	protected.HandleFunc("POST /groups/{id}/delete", s.deleteGroup)
	protected.HandleFunc("POST /groups/{id}/skills", s.createSkill)
	protected.HandleFunc("POST /skills/{id}", s.updateSkill)
	protected.HandleFunc("POST /skills/{id}/delete", s.deleteSkill)
	protected.HandleFunc("POST /skills/{id}/rating", s.setRating)
	protected.HandleFunc("GET /deletion-requests", s.deletionList)
	protected.HandleFunc("POST /deletion-requests/{id}/decide", s.deletionDecide)

	protected.HandleFunc("GET /admin/users", s.adminUsers)
	protected.HandleFunc("POST /admin/users", s.adminUserCreate)
	protected.HandleFunc("POST /admin/users/{id}", s.adminUserUpdate)
	protected.HandleFunc("GET /admin/firma", s.adminFirma)
	protected.HandleFunc("POST /admin/firma", s.adminFirmaSubmit)

	protected.HandleFunc("GET /platform/tenants", s.platformTenants)
	protected.HandleFunc("POST /platform/tenants", s.platformTenantCreate)
	protected.HandleFunc("POST /platform/tenants/{id}", s.platformTenantUpdate)
	protected.HandleFunc("POST /platform/tenants/{id}/delete", s.platformTenantDelete)

	mux.Handle("/", s.requireLogin(protected))
	return s.withSecurity(mux)
}

func (s *Server) withSecurity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setSecurityHeaders(w)

		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}

		if r.Body != nil && r.Body != http.NoBody {
			r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
		}

		if strings.HasPrefix(r.URL.Path, "/static/") {
			next.ServeHTTP(w, r)
			return
		}

		secure := s.cookieSecure(r)
		ctx := auth.WithCookieSecure(r.Context(), secure)
		token := s.csrf.Ensure(w, r, secure)
		ctx = auth.WithCSRF(ctx, token)
		r = r.WithContext(ctx)

		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			if err := s.csrf.Verify(r); err != nil {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireLogin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uid, err := s.sessions.UserID(r)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		user, err := s.store.GetUserByID(r.Context(), uid)
		if err != nil || !user.IsActive || !user.TenantActive {
			s.sessions.ClearSession(w, auth.CookieSecureFromContext(r.Context()))
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		ctx := auth.WithUserID(r.Context(), uid)
		ctx = auth.WithTenantID(ctx, user.TenantID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) currentUser(r *http.Request) (*store.User, error) {
	uid, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		return nil, sql.ErrNoRows
	}
	user, err := s.store.GetUserByID(r.Context(), uid)
	if err != nil {
		return nil, err
	}
	if !user.IsActive || !user.TenantActive {
		return nil, sql.ErrNoRows
	}
	return user, nil
}

func (s *Server) loginForm(w http.ResponseWriter, r *http.Request) {
	if _, err := s.sessions.UserID(r); err == nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.render(w, r, "login", map[string]any{
		"Title":              "Anmelden · Skilltree",
		"Error":              r.URL.Query().Get("error"),
		"Ok":                 r.URL.Query().Get("ok"),
		"Tenant":             r.URL.Query().Get("tenant"),
		"Username":           r.URL.Query().Get("username"),
		"RegistrationEnabled": s.registrationEnabled(),
	})
}

func (s *Server) loginSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	tenant := strings.TrimSpace(r.FormValue("tenant"))
	username := strings.TrimSpace(r.FormValue("username"))
	limitKey := s.clientIP(r) + "|" + strings.ToLower(tenant) + "|" + strings.ToLower(username)
	if !s.logins.Allow(limitKey) {
		q := "/login?error=rate"
		if tenant != "" {
			q += "&tenant=" + url.QueryEscape(tenant)
		}
		if username != "" {
			q += "&username=" + url.QueryEscape(username)
		}
		http.Redirect(w, r, q, http.StatusSeeOther)
		return
	}
	user, err := s.store.Authenticate(r.Context(), tenant, username, r.FormValue("password"))
	if err != nil {
		s.logins.Fail(limitKey)
		q := "/login?error=1"
		if tenant != "" {
			q += "&tenant=" + url.QueryEscape(tenant)
		}
		if username != "" {
			q += "&username=" + url.QueryEscape(username)
		}
		http.Redirect(w, r, q, http.StatusSeeOther)
		return
	}
	s.logins.Success(limitKey)
	secure := auth.CookieSecureFromContext(r.Context())
	s.sessions.SetSession(w, user.ID, secure)
	s.csrf.Rotate(w, secure)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	secure := auth.CookieSecureFromContext(r.Context())
	s.sessions.ClearSession(w, secure)
	s.csrf.Rotate(w, secure)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) workspace(w http.ResponseWriter, r *http.Request) {
	user, err := s.currentUser(r)
	if err != nil {
		s.fail(w, err)
		return
	}

	var selectedID int64
	if raw := r.PathValue("id"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		selectedID = id
	}

	var selectedSkillID int64
	if raw := r.PathValue("skillID"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		selectedSkillID = id
	}

	groups, err := s.store.ListAllGroups(r.Context(), user.TenantID)
	if err != nil {
		s.fail(w, err)
		return
	}
	tree := store.BuildTree(groups, selectedID)

	data := map[string]any{
		"Title": "Skilltree",
		"Tree":  tree,
		"User":  user,
	}

	if selectedID > 0 {
		g, err := s.store.GetGroup(r.Context(), user.TenantID, selectedID)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		skills, err := s.store.ListSkillsInGroup(r.Context(), user.TenantID, selectedID, user.ID)
		if err != nil {
			s.fail(w, err)
			return
		}
		for i := range skills {
			skills[i].Active = skills[i].ID == selectedSkillID
		}

		empty, err := s.store.GroupIsEmpty(r.Context(), selectedID)
		if err != nil {
			s.fail(w, err)
			return
		}
		pendingGroup, err := s.store.HasPendingDeletion(r.Context(), store.EntityGroup, selectedID)
		if err != nil {
			s.fail(w, err)
			return
		}
		isCreator := g.CreatedBy.Valid && g.CreatedBy.Int64 == user.ID

		data["Title"] = g.Name + " · Skilltree"
		data["Group"] = g
		data["Skills"] = skills
		data["GroupEmpty"] = empty
		data["GroupPendingDeletion"] = pendingGroup
		data["CanHardDeleteGroup"] = isCreator && empty && !pendingGroup
		data["CanRequestDeleteGroup"] = !pendingGroup && !(isCreator && empty)

		if selectedSkillID > 0 {
			sk, err := s.store.GetSkillWithRatings(r.Context(), user.TenantID, selectedSkillID, user.ID)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			data["Title"] = sk.Name + " · " + g.Name
			data["Skill"] = sk
		}
	}

	s.render(w, r, "workspace", data)
}

func (s *Server) skillManage(w http.ResponseWriter, r *http.Request) {
	user, err := s.currentUser(r)
	if err != nil {
		s.fail(w, err)
		return
	}
	groupID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	skillID, err := strconv.ParseInt(r.PathValue("skillID"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	g, err := s.store.GetGroup(r.Context(), user.TenantID, groupID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	sk, err := s.store.GetSkill(r.Context(), user.TenantID, skillID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	pending, err := s.store.HasPendingDeletion(r.Context(), store.EntitySkill, skillID)
	if err != nil {
		s.fail(w, err)
		return
	}
	others, err := s.store.SkillOtherRatingCount(r.Context(), skillID, sk.CreatedByID())
	if err != nil {
		s.fail(w, err)
		return
	}
	isCreator := sk.CreatedByID() == user.ID
	sk.PendingDeletion = pending
	sk.CanHardDelete = isCreator && others == 0 && !pending
	sk.CanRequestDelete = !pending && !sk.CanHardDelete

	s.render(w, r, "skill_manage", map[string]any{
		"Title": sk.Name + " · verwalten",
		"User":  user,
		"Group": g,
		"Skill": sk,
	})
}

func (s *Server) createGroup(w http.ResponseWriter, r *http.Request) {
	user, err := s.currentUser(r)
	if err != nil {
		s.fail(w, err)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	desc := strings.TrimSpace(r.FormValue("description"))
	if name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}

	var parent *int64
	if raw := r.FormValue("parent_id"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			http.Error(w, "bad parent_id", http.StatusBadRequest)
			return
		}
		parent = &id
	}

	id, err := s.store.CreateGroup(r.Context(), user.TenantID, parent, name, desc, user.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/groups/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *Server) updateGroup(w http.ResponseWriter, r *http.Request) {
	user, err := s.currentUser(r)
	if err != nil {
		s.fail(w, err)
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
	name := strings.TrimSpace(r.FormValue("name"))
	desc := strings.TrimSpace(r.FormValue("description"))
	if name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	if err := s.store.UpdateGroup(r.Context(), user.TenantID, id, name, desc); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/groups/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *Server) deleteGroup(w http.ResponseWriter, r *http.Request) {
	user, err := s.currentUser(r)
	if err != nil {
		s.fail(w, err)
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
	g, err := s.store.GetGroup(r.Context(), user.TenantID, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	reason := strings.TrimSpace(r.FormValue("reason"))
	mode := r.FormValue("mode")

	empty, err := s.store.GroupIsEmpty(r.Context(), id)
	if err != nil {
		s.fail(w, err)
		return
	}
	isCreator := g.CreatedBy.Valid && g.CreatedBy.Int64 == user.ID

	if mode == "hard" {
		if !isCreator || !empty {
			http.Error(w, "hard delete not allowed", http.StatusForbidden)
			return
		}
		if err := s.store.DeleteGroup(r.Context(), user.TenantID, id); err != nil {
			s.fail(w, err)
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	pending, err := s.store.HasPendingDeletion(r.Context(), store.EntityGroup, id)
	if err != nil {
		s.fail(w, err)
		return
	}
	if pending {
		http.Error(w, "deletion already requested", http.StatusConflict)
		return
	}
	if _, err := s.store.CreateDeletionRequest(r.Context(), user.TenantID, store.EntityGroup, id, g.Name, user.ID, reason); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/groups/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *Server) createSkill(w http.ResponseWriter, r *http.Request) {
	user, err := s.currentUser(r)
	if err != nil {
		s.fail(w, err)
		return
	}
	groupID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	desc := strings.TrimSpace(r.FormValue("description"))
	if name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	id, err := s.store.CreateSkillInGroup(r.Context(), user.TenantID, groupID, name, desc, user.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/groups/"+strconv.FormatInt(groupID, 10)+"/skills/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *Server) updateSkill(w http.ResponseWriter, r *http.Request) {
	user, err := s.currentUser(r)
	if err != nil {
		s.fail(w, err)
		return
	}
	skillID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	groupID, err := strconv.ParseInt(r.FormValue("group_id"), 10, 64)
	if err != nil {
		http.Error(w, "group_id required", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	desc := strings.TrimSpace(r.FormValue("description"))
	if name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	if err := s.store.UpdateSkill(r.Context(), user.TenantID, skillID, name, desc); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/groups/"+strconv.FormatInt(groupID, 10)+"/skills/"+strconv.FormatInt(skillID, 10)+"/manage", http.StatusSeeOther)
}

func (s *Server) deleteSkill(w http.ResponseWriter, r *http.Request) {
	user, err := s.currentUser(r)
	if err != nil {
		s.fail(w, err)
		return
	}
	skillID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	groupID, err := strconv.ParseInt(r.FormValue("group_id"), 10, 64)
	if err != nil {
		http.Error(w, "group_id required", http.StatusBadRequest)
		return
	}
	sk, err := s.store.GetSkill(r.Context(), user.TenantID, skillID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	reason := strings.TrimSpace(r.FormValue("reason"))
	mode := r.FormValue("mode")

	creatorID := sk.CreatedByID()
	others, err := s.store.SkillOtherRatingCount(r.Context(), skillID, creatorID)
	if err != nil {
		s.fail(w, err)
		return
	}
	isCreator := creatorID == user.ID

	if mode == "hard" {
		if !isCreator || others > 0 {
			http.Error(w, "hard delete not allowed", http.StatusForbidden)
			return
		}
		if err := s.store.DeleteSkill(r.Context(), user.TenantID, skillID); err != nil {
			s.fail(w, err)
			return
		}
		http.Redirect(w, r, "/groups/"+strconv.FormatInt(groupID, 10), http.StatusSeeOther)
		return
	}

	pending, err := s.store.HasPendingDeletion(r.Context(), store.EntitySkill, skillID)
	if err != nil {
		s.fail(w, err)
		return
	}
	if pending {
		http.Error(w, "deletion already requested", http.StatusConflict)
		return
	}
	if _, err := s.store.CreateDeletionRequest(r.Context(), user.TenantID, store.EntitySkill, skillID, sk.Name, user.ID, reason); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/groups/"+strconv.FormatInt(groupID, 10)+"/skills/"+strconv.FormatInt(skillID, 10)+"/manage", http.StatusSeeOther)
}

func (s *Server) setRating(w http.ResponseWriter, r *http.Request) {
	user, err := s.currentUser(r)
	if err != nil {
		s.fail(w, err)
		return
	}
	skillID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	groupID, err := strconv.ParseInt(r.FormValue("group_id"), 10, 64)
	if err != nil {
		http.Error(w, "group_id required", http.StatusBadRequest)
		return
	}

	raw := strings.TrimSpace(r.FormValue("level"))
	clear := raw == ""
	level := 0
	if !clear {
		level, err = strconv.Atoi(raw)
		if err != nil || level < 0 || level > 5 {
			http.Error(w, "level must be empty or 0–5", http.StatusBadRequest)
			return
		}
	}

	if err := s.store.SetSkillRating(r.Context(), user.TenantID, user.ID, skillID, level, clear); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/groups/"+strconv.FormatInt(groupID, 10)+"/skills/"+strconv.FormatInt(skillID, 10), http.StatusSeeOther)
}

func (s *Server) deletionList(w http.ResponseWriter, r *http.Request) {
	user, err := s.currentUser(r)
	if err != nil {
		s.fail(w, err)
		return
	}
	if !user.IsDeletionOfficer && !user.IsAdmin {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	reqs, err := s.store.ListPendingDeletions(r.Context(), user.TenantID)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, r, "deletions", map[string]any{
		"Title":    "Löschanträge · Skilltree",
		"User":     user,
		"Requests": reqs,
	})
}

func (s *Server) deletionDecide(w http.ResponseWriter, r *http.Request) {
	user, err := s.currentUser(r)
	if err != nil {
		s.fail(w, err)
		return
	}
	if !user.IsDeletionOfficer && !user.IsAdmin {
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
	approve := r.FormValue("decision") == "approve"
	note := strings.TrimSpace(r.FormValue("review_note"))
	if err := s.store.DecideDeletion(r.Context(), user.TenantID, id, user.ID, approve, note); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/deletion-requests", http.StatusSeeOther)
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, name string, data map[string]any) {
	if data == nil {
		data = map[string]any{}
	}
	data["CSRF"] = auth.CSRFFromContext(r.Context())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, name, data); err != nil {
		s.log.Error("render template", "name", name, "err", err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

func (s *Server) fail(w http.ResponseWriter, err error) {
	s.log.Error("request failed", "err", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}

func formBool(r *http.Request, name string) bool {
	v := r.FormValue(name)
	return v == "1" || v == "on" || v == "true"
}
