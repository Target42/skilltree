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
u.id, u.tenant_id, u.username, u.display_name, u.is_admin, u.is_deletion_officer,
u.is_platform_admin, u.is_active, u.password_hash, u.created_at,
t.slug, t.name, t.is_active`

func scanUser(scanner interface {
	Scan(dest ...any) error
}) (*User, error) {
	var u User
	err := scanner.Scan(
		&u.ID, &u.TenantID, &u.Username, &u.DisplayName, &u.IsAdmin, &u.IsDeletionOfficer,
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

func (s *Store) ListUsersByTenant(ctx context.Context, tenantID int64) ([]User, error) {
	q := `
SELECT ` + userSelectCols + `
FROM users u
JOIN tenants t ON t.id = u.tenant_id
WHERE u.tenant_id = $1
ORDER BY u.username`
	rows, err := s.db.QueryContext(ctx, q, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
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
	hash, err := bcryptHash(in.Password)
	if err != nil {
		return 0, err
	}
	var id int64
	err = s.db.QueryRowContext(ctx, `
INSERT INTO users (tenant_id, username, display_name, password_hash, is_admin, is_deletion_officer, is_platform_admin, is_active)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING id`,
		in.TenantID, username, strings.TrimSpace(in.DisplayName), hash,
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
	IsAdmin           bool
	IsDeletionOfficer bool
	IsActive          bool
	NewPassword       string // empty = leave unchanged
}

func (s *Store) UpdateUser(ctx context.Context, in UpdateUserInput) error {
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
SET display_name = $3, is_admin = $4, is_deletion_officer = $5, is_active = $6, password_hash = $7
WHERE id = $1 AND tenant_id = $2`,
			in.UserID, in.TenantID, strings.TrimSpace(in.DisplayName),
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
SET display_name = $3, is_admin = $4, is_deletion_officer = $5, is_active = $6
WHERE id = $1 AND tenant_id = $2`,
		in.UserID, in.TenantID, strings.TrimSpace(in.DisplayName),
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
