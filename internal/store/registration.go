package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
)

const RegistrationTTL = 24 * time.Hour

var (
	ErrInvalidEmail        = errors.New("invalid email")
	ErrRegistrationExpired = errors.New("registration expired")
	ErrInvalidToken        = errors.New("invalid token")
)

type TenantRegistration struct {
	ID               int64
	Slug             string
	Name             string
	AdminUsername    string
	AdminDisplayName string
	AdminEmail       string
	PasswordHash     string
	ExpiresAt        time.Time
	CreatedAt        time.Time
}

type CreateRegistrationInput struct {
	Slug             string
	Name             string
	AdminUsername    string
	AdminDisplayName string
	AdminEmail       string
	Password         string
}

func ValidateEmail(email string) error {
	email = strings.TrimSpace(email)
	if email == "" {
		return ErrInvalidEmail
	}
	if len(email) > 254 {
		return ErrInvalidEmail
	}
	addr, err := mail.ParseAddress(email)
	if err != nil {
		return ErrInvalidEmail
	}
	if addr.Address != email || !strings.Contains(addr.Address, "@") {
		return ErrInvalidEmail
	}
	return nil
}

func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func hashRegistrationToken(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}

func newRegistrationToken() (raw string, hash []byte, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", nil, err
	}
	raw = base64.RawURLEncoding.EncodeToString(buf)
	return raw, hashRegistrationToken(raw), nil
}

// CreateTenantRegistration stores a pending self-service signup and returns the
// raw confirmation token (shown once in the email link).
func (s *Store) CreateTenantRegistration(ctx context.Context, in CreateRegistrationInput) (token string, err error) {
	slug := NormalizeSlug(in.Slug)
	if err := ValidateSlug(slug); err != nil {
		return "", err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return "", fmt.Errorf("name required")
	}
	username := strings.TrimSpace(in.AdminUsername)
	if username == "" {
		return "", fmt.Errorf("admin username required")
	}
	email := NormalizeEmail(in.AdminEmail)
	if err := ValidateEmail(email); err != nil {
		return "", err
	}
	if err := ValidatePassword(in.Password); err != nil {
		return "", err
	}
	hash, err := bcryptHash(in.Password)
	if err != nil {
		return "", err
	}
	rawToken, tokenHash, err := newRegistrationToken()
	if err != nil {
		return "", err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM tenant_registrations WHERE expires_at < now()`); err != nil {
		return "", err
	}

	var exists bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tenants WHERE slug = $1)`, slug).Scan(&exists)
	if err != nil {
		return "", err
	}
	if exists {
		return "", ErrDuplicate
	}

	expires := time.Now().Add(RegistrationTTL)
	_, err = tx.ExecContext(ctx, `
INSERT INTO tenant_registrations (
  slug, name, admin_username, admin_display_name, admin_email, password_hash, token_hash, expires_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		slug, name, username, strings.TrimSpace(in.AdminDisplayName), email, hash, tokenHash, expires,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return "", ErrDuplicate
		}
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return rawToken, nil
}

func (s *Store) DeleteTenantRegistrationBySlug(ctx context.Context, slug string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM tenant_registrations WHERE slug = $1`, NormalizeSlug(slug))
	return err
}

// ConfirmTenantRegistration activates a pending signup (creates tenant + admin).
func (s *Store) ConfirmTenantRegistration(ctx context.Context, rawToken string) (*Tenant, error) {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return nil, ErrInvalidToken
	}
	tokenHash := hashRegistrationToken(rawToken)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var reg TenantRegistration
	err = tx.QueryRowContext(ctx, `
SELECT id, slug, name, admin_username, admin_display_name, admin_email, password_hash, expires_at, created_at
FROM tenant_registrations
WHERE token_hash = $1
FOR UPDATE`, tokenHash).Scan(
		&reg.ID, &reg.Slug, &reg.Name, &reg.AdminUsername, &reg.AdminDisplayName,
		&reg.AdminEmail, &reg.PasswordHash, &reg.ExpiresAt, &reg.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrInvalidToken
		}
		return nil, err
	}
	if time.Now().After(reg.ExpiresAt) {
		_, _ = tx.ExecContext(ctx, `DELETE FROM tenant_registrations WHERE id = $1`, reg.ID)
		_ = tx.Commit()
		return nil, ErrRegistrationExpired
	}

	var tenantID int64
	err = tx.QueryRowContext(ctx, `
INSERT INTO tenants (slug, name)
VALUES ($1, $2)
RETURNING id`, reg.Slug, reg.Name).Scan(&tenantID)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrDuplicate
		}
		return nil, fmt.Errorf("create tenant: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
INSERT INTO users (tenant_id, username, display_name, email, password_hash, is_admin, is_deletion_officer, is_platform_admin, is_active)
VALUES ($1, $2, $3, $4, $5, TRUE, FALSE, FALSE, TRUE)`,
		tenantID, reg.AdminUsername, reg.AdminDisplayName, reg.AdminEmail, reg.PasswordHash,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrDuplicate
		}
		return nil, fmt.Errorf("create admin user: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM tenant_registrations WHERE id = $1`, reg.ID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return s.GetTenantByID(ctx, tenantID)
}
