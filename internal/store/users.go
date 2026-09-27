package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const MinPasswordLen = 8

var ErrWeakPassword = errors.New("password too weak")

func ValidatePassword(password string) error {
	if len([]rune(password)) < MinPasswordLen {
		return ErrWeakPassword
	}
	return nil
}

type User struct {
	ID                int64
	TenantID          int64
	Username          string
	DisplayName       string
	Email             string
	IsAdmin           bool
	IsDeletionOfficer bool
	IsPlatformAdmin   bool
	IsActive          bool
	PasswordHash      string
	CreatedAt         time.Time
	TenantSlug        string
	TenantName        string
	TenantActive      bool
}

func (u User) Label() string {
	if strings.TrimSpace(u.DisplayName) != "" {
		return u.DisplayName
	}
	return u.Username
}

func bcryptHash(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

const userSelectCols = `
u.id, u.tenant_id, u.username, u.display_name, u.email, u.is_admin, u.is_deletion_officer,
u.is_platform_admin, u.is_active, u.password_hash, u.created_at,
t.slug, t.name, t.is_active`

func scanUser(scanner interface {
	Scan(dest ...any) error
}) (*User, error) {
	var u User
	err := scanner.Scan(
		&u.ID, &u.TenantID, &u.Username, &u.DisplayName, &u.Email, &u.IsAdmin, &u.IsDeletionOfficer,
		&u.IsPlatformAdmin, &u.IsActive, &u.PasswordHash, &u.CreatedAt,
		&u.TenantSlug, &u.TenantName, &u.TenantActive,
	)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *Store) GetUserByID(ctx context.Context, id int64) (*User, error) {
	q := `
SELECT ` + userSelectCols + `
FROM users u
JOIN tenants t ON t.id = u.tenant_id
WHERE u.id = $1`
	u, err := scanUser(s.db.QueryRowContext(ctx, q, id))
	if err != nil {
		return nil, err
	}
	return u, nil
}

// Authenticate looks up an active user in an active tenant by slug + username.
func (s *Store) Authenticate(ctx context.Context, tenantSlug, username, password string) (*User, error) {
	q := `
SELECT ` + userSelectCols + `
FROM users u
JOIN tenants t ON t.id = u.tenant_id
WHERE t.slug = $1 AND u.username = $2`
	u, err := scanUser(s.db.QueryRowContext(ctx, q, NormalizeSlug(tenantSlug), strings.TrimSpace(username)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, sql.ErrNoRows
		}
		return nil, err
	}
	if !u.IsActive || !u.TenantActive {
		return nil, sql.ErrNoRows
	}
	if u.PasswordHash == "" || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return nil, sql.ErrNoRows
	}
	return u, nil
}

const DefaultUsersPageSize = 20

type ListUsersFilter struct {
	Query    string
	Letter   string // "" or a-z
	Page     int
	PageSize int
}

type ListUsersResult struct {
	Users      []User
	Total      int64
	Page       int
	PageSize   int
	TotalPages int
}

func (f ListUsersFilter) Normalized() ListUsersFilter {
	f.Query = strings.TrimSpace(f.Query)
	if len(f.Query) > 100 {
		f.Query = f.Query[:100]
	}
	f.Letter = strings.ToLower(strings.TrimSpace(f.Letter))
	if len(f.Letter) != 1 || f.Letter[0] < 'a' || f.Letter[0] > 'z' {
		f.Letter = ""
	}
	if f.PageSize <= 0 {
		f.PageSize = DefaultUsersPageSize
	}
	if f.PageSize > 100 {
		f.PageSize = 100
	}
	if f.Page <= 0 {
		f.Page = 1
	}
	return f
}

func likeContains(q string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + replacer.Replace(q) + "%"
}

func (s *Store) ListUsersByTenant(ctx context.Context, tenantID int64, f ListUsersFilter) (ListUsersResult, error) {
	f = f.Normalized()
	search := ""
	if f.Query != "" {
		search = likeContains(f.Query)
	}

	const countQ = `
SELECT COUNT(*)
FROM users u
WHERE u.tenant_id = $1
  AND ($2 = '' OR u.username ILIKE $2 ESCAPE '\' OR u.display_name ILIKE $2 ESCAPE '\' OR u.email ILIKE $2 ESCAPE '\')
  AND ($3 = '' OR lower(left(u.username, 1)) = $3)`

	var total int64
	if err := s.db.QueryRowContext(ctx, countQ, tenantID, search, f.Letter).Scan(&total); err != nil {
		return ListUsersResult{}, err
	}

	page := f.Page
	totalPages := 1
	if total > 0 {
		totalPages = int((total + int64(f.PageSize) - 1) / int64(f.PageSize))
	}
	if page > totalPages {
		page = totalPages
	}
	offset := (page - 1) * f.PageSize

	q := `
SELECT ` + userSelectCols + `
FROM users u
JOIN tenants t ON t.id = u.tenant_id
WHERE u.tenant_id = $1
  AND ($2 = '' OR u.username ILIKE $2 ESCAPE '\' OR u.display_name ILIKE $2 ESCAPE '\' OR u.email ILIKE $2 ESCAPE '\')
  AND ($3 = '' OR lower(left(u.username, 1)) = $3)
ORDER BY u.username
LIMIT $4 OFFSET $5`
	rows, err := s.db.QueryContext(ctx, q, tenantID, search, f.Letter, f.PageSize, offset)
	if err != nil {
		return ListUsersResult{}, err
	}
	defer rows.Close()

	out := make([]User, 0, f.PageSize)
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return ListUsersResult{}, err
		}
		out = append(out, *u)
	}
	if err := rows.Err(); err != nil {
		return ListUsersResult{}, err
	}
	return ListUsersResult{
		Users:      out,
		Total:      total,
		Page:       page,
		PageSize:   f.PageSize,
		TotalPages: totalPages,
	}, nil
}

func (s *Store) GetUserInTenant(ctx context.Context, tenantID, userID int64) (*User, error) {
	q := `
SELECT ` + userSelectCols + `
FROM users u
JOIN tenants t ON t.id = u.tenant_id
WHERE u.id = $1 AND u.tenant_id = $2`
	u, err := scanUser(s.db.QueryRowContext(ctx, q, userID, tenantID))
	if err != nil {
		return nil, err
	}
	return u, nil
}

type CreateUserInput struct {
	TenantID          int64
	Username          string
	DisplayName       string
	Email             string
	Password          string
	IsAdmin           bool
	IsDeletionOfficer bool
	IsPlatformAdmin   bool
	IsActive          bool
}

func (s *Store) CreateUser(ctx context.Context, in CreateUserInput) (int64, error) {
	username := strings.TrimSpace(in.Username)
	if username == "" {
		return 0, fmt.Errorf("username required")
	}
	if strings.TrimSpace(in.Password) == "" {
		return 0, fmt.Errorf("password required")
	}
	if err := ValidatePassword(in.Password); err != nil {
		return 0, err
	}
	email := NormalizeEmail(in.Email)
	if email != "" {
		if err := ValidateEmail(email); err != nil {
			return 0, err
		}
	}
	hash, err := bcryptHash(in.Password)
	if err != nil {
		return 0, err
	}
	var id int64
	err = s.db.QueryRowContext(ctx, `
INSERT INTO users (tenant_id, username, display_name, email, password_hash, is_admin, is_deletion_officer, is_platform_admin, is_active)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING id`,
		in.TenantID, username, strings.TrimSpace(in.DisplayName), email, hash,
		in.IsAdmin, in.IsDeletionOfficer, in.IsPlatformAdmin, in.IsActive,
	).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			return 0, ErrDuplicate
		}
		return 0, err
	}
	return id, nil
}

type UpdateUserInput struct {
	TenantID          int64
	UserID            int64
	DisplayName       string
	Email             string
	IsAdmin           bool
	IsDeletionOfficer bool
	IsActive          bool
	NewPassword       string // empty = leave unchanged
}

func (s *Store) UpdateUser(ctx context.Context, in UpdateUserInput) error {
	email := NormalizeEmail(in.Email)
	if email != "" {
		if err := ValidateEmail(email); err != nil {
			return err
		}
	}

	if strings.TrimSpace(in.NewPassword) != "" {
		if err := ValidatePassword(in.NewPassword); err != nil {
			return err
		}
		hash, err := bcryptHash(in.NewPassword)
		if err != nil {
			return err
		}
		res, err := s.db.ExecContext(ctx, `
UPDATE users
SET display_name = $3, email = $4, is_admin = $5, is_deletion_officer = $6, is_active = $7, password_hash = $8
WHERE id = $1 AND tenant_id = $2`,
			in.UserID, in.TenantID, strings.TrimSpace(in.DisplayName), email,
			in.IsAdmin, in.IsDeletionOfficer, in.IsActive, hash,
		)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return sql.ErrNoRows
		}
		return nil
	}

	res, err := s.db.ExecContext(ctx, `
UPDATE users
SET display_name = $3, email = $4, is_admin = $5, is_deletion_officer = $6, is_active = $7
WHERE id = $1 AND tenant_id = $2`,
		in.UserID, in.TenantID, strings.TrimSpace(in.DisplayName), email,
		in.IsAdmin, in.IsDeletionOfficer, in.IsActive,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
