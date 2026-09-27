package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var slugPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

type Tenant struct {
	ID        int64
	Slug      string
	Name      string
	IsActive  bool
	CreatedAt time.Time
}

func NormalizeSlug(slug string) string {
	return strings.ToLower(strings.TrimSpace(slug))
}

func ValidateSlug(slug string) error {
	slug = NormalizeSlug(slug)
	if slug == "" {
		return fmt.Errorf("slug required")
	}
	if len(slug) > 64 {
		return fmt.Errorf("slug too long")
	}
	if !slugPattern.MatchString(slug) {
		return fmt.Errorf("slug must be lowercase letters, digits, and hyphens")
	}
	return nil
}

func (s *Store) ListTenants(ctx context.Context) ([]Tenant, error) {
	const q = `
SELECT id, slug, name, is_active, created_at
FROM tenants
ORDER BY name`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Tenant
	for rows.Next() {
		var t Tenant
		if err := rows.Scan(&t.ID, &t.Slug, &t.Name, &t.IsActive, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) GetTenantByID(ctx context.Context, id int64) (*Tenant, error) {
	const q = `
SELECT id, slug, name, is_active, created_at
FROM tenants WHERE id = $1`
	var t Tenant
	err := s.db.QueryRowContext(ctx, q, id).Scan(&t.ID, &t.Slug, &t.Name, &t.IsActive, &t.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *Store) GetTenantBySlug(ctx context.Context, slug string) (*Tenant, error) {
	const q = `
SELECT id, slug, name, is_active, created_at
FROM tenants WHERE slug = $1`
	var t Tenant
	err := s.db.QueryRowContext(ctx, q, NormalizeSlug(slug)).Scan(&t.ID, &t.Slug, &t.Name, &t.IsActive, &t.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// CreateTenantWithAdmin creates a tenant and its first tenant admin in one transaction.
// adminEmail may be empty (platform-admin provisioning); self-service signup always sets it.
func (s *Store) CreateTenantWithAdmin(ctx context.Context, slug, name, adminUsername, adminDisplayName, adminEmail, adminPassword string) (tenantID, userID int64, err error) {
	slug = NormalizeSlug(slug)
	if err := ValidateSlug(slug); err != nil {
		return 0, 0, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, 0, fmt.Errorf("name required")
	}
	adminUsername = strings.TrimSpace(adminUsername)
	if adminUsername == "" {
		return 0, 0, fmt.Errorf("admin username required")
	}
	if strings.TrimSpace(adminPassword) == "" {
		return 0, 0, fmt.Errorf("admin password required")
	}
	if err := ValidatePassword(adminPassword); err != nil {
		return 0, 0, err
	}
	email := NormalizeEmail(adminEmail)
	if email != "" {
		if err := ValidateEmail(email); err != nil {
			return 0, 0, err
		}
	}

	hash, err := bcryptHash(adminPassword)
	if err != nil {
		return 0, 0, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = tx.Rollback() }()

	err = tx.QueryRowContext(ctx, `
INSERT INTO tenants (slug, name)
VALUES ($1, $2)
RETURNING id`, slug, name).Scan(&tenantID)
	if err != nil {
		if isUniqueViolation(err) {
			return 0, 0, ErrDuplicate
		}
		return 0, 0, fmt.Errorf("create tenant: %w", err)
	}

	err = tx.QueryRowContext(ctx, `
INSERT INTO users (tenant_id, username, display_name, email, password_hash, is_admin, is_deletion_officer, is_platform_admin, is_active)
VALUES ($1, $2, $3, $4, $5, TRUE, FALSE, FALSE, TRUE)
RETURNING id`, tenantID, adminUsername, strings.TrimSpace(adminDisplayName), email, hash).Scan(&userID)
	if err != nil {
		if isUniqueViolation(err) {
			return 0, 0, ErrDuplicate
		}
		return 0, 0, fmt.Errorf("create admin user: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return tenantID, userID, nil
}

func (s *Store) SetTenantActive(ctx context.Context, id int64, active bool) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE tenants SET is_active = $2 WHERE id = $1`, id, active)
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

func (s *Store) UpdateTenant(ctx context.Context, id int64, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("name required")
	}
	res, err := s.db.ExecContext(ctx, `
UPDATE tenants SET name = $2 WHERE id = $1`, id, name)
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

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "duplicate key") || strings.Contains(msg, "unique constraint")
}

var ErrDuplicate = errors.New("duplicate")

// DeleteTenant removes a tenant and all of its data.
func (s *Store) DeleteTenant(ctx context.Context, tenantID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tenants WHERE id = $1)`, tenantID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return sql.ErrNoRows
	}

	steps := []string{
		`DELETE FROM notifications WHERE user_id IN (SELECT id FROM users WHERE tenant_id = $1)`,
		`DELETE FROM group_watches WHERE user_id IN (SELECT id FROM users WHERE tenant_id = $1)
		   OR group_id IN (SELECT id FROM groups WHERE tenant_id = $1)`,
		`DELETE FROM skill_ratings WHERE user_id IN (SELECT id FROM users WHERE tenant_id = $1)
		   OR skill_id IN (SELECT id FROM skills WHERE tenant_id = $1)`,
		`DELETE FROM deletion_requests WHERE tenant_id = $1`,
		`DELETE FROM group_skills WHERE group_id IN (SELECT id FROM groups WHERE tenant_id = $1)
		   OR skill_id IN (SELECT id FROM skills WHERE tenant_id = $1)`,
		`DELETE FROM groups WHERE tenant_id = $1`,
		`DELETE FROM skills WHERE tenant_id = $1`,
		`DELETE FROM users WHERE tenant_id = $1`,
		`DELETE FROM tenants WHERE id = $1`,
	}
	for _, q := range steps {
		if _, err := tx.ExecContext(ctx, q, tenantID); err != nil {
			return fmt.Errorf("delete tenant data: %w", err)
		}
	}
	return tx.Commit()
}
