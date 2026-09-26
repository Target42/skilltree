package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const (
	EntityGroup = "group"
	EntitySkill = "skill"

	DeletionPending  = "pending"
	DeletionApproved = "approved"
	DeletionRejected = "rejected"
)

type DeletionRequest struct {
	ID           int64
	TenantID     int64
	EntityType   string
	EntityID     int64
	EntityLabel  string
	RequestedBy  int64
	Requester    string
	Reason       string
	Status       string
	ReviewedBy   sql.NullInt64
	ReviewNote   string
	CreatedAt    time.Time
	ReviewedAt   sql.NullTime
}

func (s *Store) GroupIsEmpty(ctx context.Context, groupID int64) (bool, error) {
	var children, skills int
	err := s.db.QueryRowContext(ctx, `
SELECT
  (SELECT COUNT(*) FROM groups WHERE parent_id = $1),
  (SELECT COUNT(*) FROM group_skills WHERE group_id = $1)`, groupID).Scan(&children, &skills)
	if err != nil {
		return false, err
	}
	return children == 0 && skills == 0, nil
}

func (s *Store) SkillOtherRatingCount(ctx context.Context, skillID, creatorID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM skill_ratings
WHERE skill_id = $1 AND user_id IS DISTINCT FROM $2`, skillID, creatorID).Scan(&n)
	return n, err
}

func (s *Store) DeleteGroup(ctx context.Context, tenantID, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM groups WHERE id = $1 AND tenant_id = $2`, id, tenantID)
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

func (s *Store) DeleteSkill(ctx context.Context, tenantID, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM skills WHERE id = $1 AND tenant_id = $2`, id, tenantID)
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

func (s *Store) CreateDeletionRequest(ctx context.Context, tenantID int64, entityType string, entityID int64, label string, userID int64, reason string) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `
INSERT INTO deletion_requests (tenant_id, entity_type, entity_id, entity_label, requested_by, reason)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id`, tenantID, entityType, entityID, label, userID, reason).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("create deletion request: %w", err)
	}
	return id, nil
}

func (s *Store) HasPendingDeletion(ctx context.Context, entityType string, entityID int64) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM deletion_requests
WHERE entity_type = $1 AND entity_id = $2 AND status = 'pending'`, entityType, entityID).Scan(&n)
	return n > 0, err
}

func (s *Store) ListPendingDeletions(ctx context.Context, tenantID int64) ([]DeletionRequest, error) {
	const q = `
SELECT dr.id, dr.tenant_id, dr.entity_type, dr.entity_id, dr.entity_label, dr.requested_by,
       COALESCE(NULLIF(u.display_name, ''), u.username) AS requester,
       dr.reason, dr.status, dr.reviewed_by, dr.review_note, dr.created_at, dr.reviewed_at
FROM deletion_requests dr
JOIN users u ON u.id = dr.requested_by
WHERE dr.status = 'pending' AND dr.tenant_id = $1
ORDER BY dr.created_at`
	rows, err := s.db.QueryContext(ctx, q, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []DeletionRequest
	for rows.Next() {
		var d DeletionRequest
		if err := rows.Scan(
			&d.ID, &d.TenantID, &d.EntityType, &d.EntityID, &d.EntityLabel, &d.RequestedBy,
			&d.Requester, &d.Reason, &d.Status, &d.ReviewedBy, &d.ReviewNote,
			&d.CreatedAt, &d.ReviewedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) GetDeletionRequest(ctx context.Context, tenantID, id int64) (*DeletionRequest, error) {
	const q = `
SELECT dr.id, dr.tenant_id, dr.entity_type, dr.entity_id, dr.entity_label, dr.requested_by,
       COALESCE(NULLIF(u.display_name, ''), u.username) AS requester,
       dr.reason, dr.status, dr.reviewed_by, dr.review_note, dr.created_at, dr.reviewed_at
FROM deletion_requests dr
JOIN users u ON u.id = dr.requested_by
WHERE dr.id = $1 AND dr.tenant_id = $2`
	var d DeletionRequest
	err := s.db.QueryRowContext(ctx, q, id, tenantID).Scan(
		&d.ID, &d.TenantID, &d.EntityType, &d.EntityID, &d.EntityLabel, &d.RequestedBy,
		&d.Requester, &d.Reason, &d.Status, &d.ReviewedBy, &d.ReviewNote,
		&d.CreatedAt, &d.ReviewedAt,
	)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *Store) DecideDeletion(ctx context.Context, tenantID, reqID, reviewerID int64, approve bool, note string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var d DeletionRequest
	err = tx.QueryRowContext(ctx, `
SELECT id, tenant_id, entity_type, entity_id, status
FROM deletion_requests WHERE id = $1 AND tenant_id = $2 FOR UPDATE`, reqID, tenantID).
		Scan(&d.ID, &d.TenantID, &d.EntityType, &d.EntityID, &d.Status)
	if err != nil {
		return err
	}
	if d.Status != DeletionPending {
		return fmt.Errorf("request already decided")
	}

	status := DeletionRejected
	if approve {
		status = DeletionApproved
		switch d.EntityType {
		case EntityGroup:
			if _, err := tx.ExecContext(ctx, `DELETE FROM groups WHERE id = $1 AND tenant_id = $2`, d.EntityID, tenantID); err != nil {
				return err
			}
		case EntitySkill:
			if _, err := tx.ExecContext(ctx, `DELETE FROM skills WHERE id = $1 AND tenant_id = $2`, d.EntityID, tenantID); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown entity type %q", d.EntityType)
		}
	}

	_, err = tx.ExecContext(ctx, `
UPDATE deletion_requests
SET status = $2, reviewed_by = $3, review_note = $4, reviewed_at = now()
WHERE id = $1 AND tenant_id = $5`, reqID, status, reviewerID, note, tenantID)
	if err != nil {
		return err
	}
	return tx.Commit()
}
