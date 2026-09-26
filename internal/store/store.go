package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type Group struct {
	ID          int64
	TenantID    int64
	ParentID    sql.NullInt64
	Name        string
	Description string
	CreatedBy   sql.NullInt64
	CreatedAt   time.Time
	ChildCount  int
	SkillCount  int
}

type SkillRatingRow struct {
	UserID      int64
	DisplayName string
	Level       int
	HasLevel    bool
	IsMe        bool
}

func (r SkillRatingRow) LevelValue() int {
	if !r.HasLevel {
		return -1
	}
	return r.Level
}

func (r SkillRatingRow) LevelIs(n int) bool {
	return r.HasLevel && r.Level == n
}

type Skill struct {
	ID               int64
	TenantID         int64
	Name             string
	Description      string
	CreatedBy        sql.NullInt64
	CreatedAt        time.Time
	MyLevel          sql.NullInt16 // unset = kein Wissen
	Ratings          []SkillRatingRow
	RatingCount      int
	OtherRatingCount int
	PendingDeletion  bool
	CanHardDelete    bool
	CanRequestDelete bool
	Active           bool
}

func (s Skill) LevelIs(n int) bool {
	return s.MyLevel.Valid && int(s.MyLevel.Int16) == n
}

// LevelValue returns the level for templates/CSS, or -1 if unset.
func (s Skill) LevelValue() int {
	if !s.MyLevel.Valid {
		return -1
	}
	return int(s.MyLevel.Int16)
}

// LevelMarks renders ●/○ for a level (0–5). Used in select labels.
func LevelMarks(n int) string {
	if n < 0 {
		n = 0
	}
	if n > 5 {
		n = 5
	}
	b := make([]rune, 0, 5)
	for i := 0; i < 5; i++ {
		if i < n {
			b = append(b, '●')
		} else {
			b = append(b, '○')
		}
	}
	return string(b)
}

func (s Skill) CreatedByID() int64 {
	if s.CreatedBy.Valid {
		return s.CreatedBy.Int64
	}
	return 0
}


type TreeNode struct {
	Group
	Active   bool
	Open     bool
	Children []TreeNode
}

type Store struct {
	db *sql.DB
}

func New(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) ListAllGroups(ctx context.Context, tenantID int64) ([]Group, error) {
	const q = `
SELECT g.id, g.tenant_id, g.parent_id, g.name, g.description, g.created_by, g.created_at,
       (SELECT COUNT(*) FROM groups c WHERE c.parent_id = g.id) AS child_count,
       (SELECT COUNT(*) FROM group_skills gs WHERE gs.group_id = g.id) AS skill_count
FROM groups g
WHERE g.tenant_id = $1
ORDER BY g.name`
	return s.scanGroups(ctx, q, tenantID)
}

func (s *Store) GetGroup(ctx context.Context, tenantID, id int64) (*Group, error) {
	const q = `
SELECT g.id, g.tenant_id, g.parent_id, g.name, g.description, g.created_by, g.created_at,
       (SELECT COUNT(*) FROM groups c WHERE c.parent_id = g.id) AS child_count,
       (SELECT COUNT(*) FROM group_skills gs WHERE gs.group_id = g.id) AS skill_count
FROM groups g
WHERE g.id = $1 AND g.tenant_id = $2`
	rows, err := s.scanGroups(ctx, q, id, tenantID)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, sql.ErrNoRows
	}
	return &rows[0], nil
}

func (s *Store) CreateGroup(ctx context.Context, tenantID int64, parentID *int64, name, description string, createdBy int64) (int64, error) {
	if parentID != nil {
		var parentTenant int64
		err := s.db.QueryRowContext(ctx, `SELECT tenant_id FROM groups WHERE id = $1`, *parentID).Scan(&parentTenant)
		if err != nil {
			return 0, err
		}
		if parentTenant != tenantID {
			return 0, sql.ErrNoRows
		}
	}
	const q = `
INSERT INTO groups (tenant_id, parent_id, name, description, created_by)
VALUES ($1, $2, $3, $4, $5)
RETURNING id`
	var id int64
	err := s.db.QueryRowContext(ctx, q, tenantID, parentID, name, description, createdBy).Scan(&id)
	return id, err
}

func (s *Store) UpdateGroup(ctx context.Context, tenantID, id int64, name, description string) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE groups SET name = $3, description = $4
WHERE id = $1 AND tenant_id = $2`, id, tenantID, name, description)
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

func (s *Store) UpdateSkill(ctx context.Context, tenantID, id int64, name, description string) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE skills SET name = $3, description = $4
WHERE id = $1 AND tenant_id = $2`, id, tenantID, name, description)
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

// ListSkillsInGroup returns skills in a group without loading all ratings
// (efficient for the sidebar list).
func (s *Store) ListSkillsInGroup(ctx context.Context, tenantID, groupID, userID int64) ([]Skill, error) {
	const q = `
SELECT s.id, s.tenant_id, s.name, s.description, s.created_by, s.created_at, r.level,
       (SELECT COUNT(*) FROM skill_ratings sr WHERE sr.skill_id = s.id) AS rating_count,
       EXISTS (
         SELECT 1 FROM deletion_requests dr
         WHERE dr.entity_type = 'skill' AND dr.entity_id = s.id AND dr.status = 'pending'
       ) AS pending_deletion
FROM skills s
JOIN group_skills gs ON gs.skill_id = s.id
JOIN groups g ON g.id = gs.group_id AND g.tenant_id = $1
LEFT JOIN skill_ratings r ON r.skill_id = s.id AND r.user_id = $3
WHERE gs.group_id = $2 AND s.tenant_id = $1
ORDER BY s.name`
	rows, err := s.db.QueryContext(ctx, q, tenantID, groupID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Skill
	for rows.Next() {
		var sk Skill
		if err := rows.Scan(&sk.ID, &sk.TenantID, &sk.Name, &sk.Description, &sk.CreatedBy, &sk.CreatedAt, &sk.MyLevel, &sk.RatingCount, &sk.PendingDeletion); err != nil {
			return nil, err
		}
		out = append(out, sk)
	}
	return out, rows.Err()
}

// GetSkillWithRatings loads one skill and its user ratings for the detail pane.
func (s *Store) GetSkillWithRatings(ctx context.Context, tenantID, skillID, userID int64) (*Skill, error) {
	user, err := s.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user.TenantID != tenantID {
		return nil, sql.ErrNoRows
	}
	const q = `
SELECT s.id, s.tenant_id, s.name, s.description, s.created_by, s.created_at, r.level,
       (SELECT COUNT(*) FROM skill_ratings sr WHERE sr.skill_id = s.id) AS rating_count,
       EXISTS (
         SELECT 1 FROM deletion_requests dr
         WHERE dr.entity_type = 'skill' AND dr.entity_id = s.id AND dr.status = 'pending'
       ) AS pending_deletion
FROM skills s
LEFT JOIN skill_ratings r ON r.skill_id = s.id AND r.user_id = $3
WHERE s.id = $1 AND s.tenant_id = $2`
	var sk Skill
	err = s.db.QueryRowContext(ctx, q, skillID, tenantID, userID).Scan(
		&sk.ID, &sk.TenantID, &sk.Name, &sk.Description, &sk.CreatedBy, &sk.CreatedAt, &sk.MyLevel, &sk.RatingCount, &sk.PendingDeletion,
	)
	if err != nil {
		return nil, err
	}

	bySkill, err := s.listAllRatings(ctx, tenantID, []int64{skillID})
	if err != nil {
		return nil, err
	}
	ratings := bySkill[skillID]
	hasMe := false
	var meRow *SkillRatingRow
	var rest []SkillRatingRow
	for j := range ratings {
		if ratings[j].UserID == userID {
			ratings[j].IsMe = true
			hasMe = true
			cp := ratings[j]
			meRow = &cp
		} else {
			rest = append(rest, ratings[j])
			if ratings[j].HasLevel {
				sk.OtherRatingCount++
			}
		}
	}
	ordered := make([]SkillRatingRow, 0, len(ratings)+1)
	if hasMe {
		ordered = append(ordered, *meRow)
	} else {
		ordered = append(ordered, SkillRatingRow{
			UserID:      user.ID,
			DisplayName: user.Label(),
			IsMe:        true,
		})
	}
	ordered = append(ordered, rest...)
	sk.Ratings = ordered
	sk.Active = true
	return &sk, nil
}

func (s *Store) listAllRatings(ctx context.Context, tenantID int64, skillIDs []int64) (map[int64][]SkillRatingRow, error) {
	if len(skillIDs) == 0 {
		return map[int64][]SkillRatingRow{}, nil
	}
	args := make([]any, 0, len(skillIDs)+1)
	args = append(args, tenantID)
	in := make([]string, len(skillIDs))
	for i, id := range skillIDs {
		args = append(args, id)
		in[i] = fmt.Sprintf("$%d", i+2)
	}
	q := fmt.Sprintf(`
SELECT sr.skill_id, sr.user_id, COALESCE(NULLIF(u.display_name, ''), u.username), sr.level
FROM skill_ratings sr
JOIN users u ON u.id = sr.user_id AND u.tenant_id = $1
JOIN skills s ON s.id = sr.skill_id AND s.tenant_id = $1
WHERE sr.skill_id IN (%s)
ORDER BY 3`, strings.Join(in, ","))

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[int64][]SkillRatingRow{}
	for rows.Next() {
		var skillID int64
		var row SkillRatingRow
		var level int16
		if err := rows.Scan(&skillID, &row.UserID, &row.DisplayName, &level); err != nil {
			return nil, err
		}
		row.Level = int(level)
		row.HasLevel = true
		out[skillID] = append(out[skillID], row)
	}
	return out, rows.Err()
}

func (s *Store) GetSkill(ctx context.Context, tenantID, id int64) (*Skill, error) {
	const q = `
SELECT s.id, s.tenant_id, s.name, s.description, s.created_by, s.created_at
FROM skills s WHERE s.id = $1 AND s.tenant_id = $2`
	var sk Skill
	err := s.db.QueryRowContext(ctx, q, id, tenantID).Scan(&sk.ID, &sk.TenantID, &sk.Name, &sk.Description, &sk.CreatedBy, &sk.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &sk, nil
}

// SetSkillRating speichert 0–5. clear=true löscht die Bewertung (= kein Wissen).
func (s *Store) SetSkillRating(ctx context.Context, tenantID, userID, skillID int64, level int, clear bool) error {
	var skillTenant int64
	err := s.db.QueryRowContext(ctx, `SELECT tenant_id FROM skills WHERE id = $1`, skillID).Scan(&skillTenant)
	if err != nil {
		return err
	}
	if skillTenant != tenantID {
		return sql.ErrNoRows
	}
	if clear {
		_, err := s.db.ExecContext(ctx, `
DELETE FROM skill_ratings WHERE user_id = $1 AND skill_id = $2`, userID, skillID)
		return err
	}
	if level < 0 || level > 5 {
		return fmt.Errorf("level must be 0–5")
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO skill_ratings (user_id, skill_id, level, updated_at)
VALUES ($1, $2, $3, now())
ON CONFLICT (user_id, skill_id)
DO UPDATE SET level = EXCLUDED.level, updated_at = now()`, userID, skillID, level)
	return err
}

func (s *Store) CreateSkillInGroup(ctx context.Context, tenantID, groupID int64, name, description string, createdBy int64) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	var groupTenant int64
	err = tx.QueryRowContext(ctx, `SELECT tenant_id FROM groups WHERE id = $1`, groupID).Scan(&groupTenant)
	if err != nil {
		return 0, err
	}
	if groupTenant != tenantID {
		return 0, sql.ErrNoRows
	}

	var skillID int64
	err = tx.QueryRowContext(ctx, `
INSERT INTO skills (tenant_id, name, description, created_by)
VALUES ($1, $2, $3, $4)
RETURNING id`, tenantID, name, description, createdBy).Scan(&skillID)
	if err != nil {
		return 0, err
	}

	_, err = tx.ExecContext(ctx, `
INSERT INTO group_skills (group_id, skill_id)
VALUES ($1, $2)`, groupID, skillID)
	if err != nil {
		return 0, err
	}

	_, err = tx.ExecContext(ctx, `
INSERT INTO notifications (user_id, group_id, skill_id, message)
SELECT gw.user_id, $1, $2, 'Neuer Skill „' || $3 || '“ in einer beobachteten Gruppe.'
FROM group_watches gw
WHERE gw.group_id = $1`, groupID, skillID, name)
	if err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return skillID, nil
}

func (s *Store) DemoUserID(ctx context.Context) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `
SELECT u.id FROM users u
JOIN tenants t ON t.id = u.tenant_id
WHERE t.slug = 'demo' AND u.username = 'demo'`).Scan(&id)
	return id, err
}

// BuildTree turns a flat group list into a nested tree. selectedID marks the
// active node and opens all ancestors.
func BuildTree(groups []Group, selectedID int64) []TreeNode {
	byParent := map[int64][]Group{}
	var roots []Group
	for _, g := range groups {
		if g.ParentID.Valid {
			byParent[g.ParentID.Int64] = append(byParent[g.ParentID.Int64], g)
		} else {
			roots = append(roots, g)
		}
	}

	var walk func(g Group) TreeNode
	walk = func(g Group) TreeNode {
		n := TreeNode{Group: g, Active: g.ID == selectedID}
		for _, child := range byParent[g.ID] {
			cn := walk(child)
			if cn.Active || cn.Open {
				n.Open = true
			}
			n.Children = append(n.Children, cn)
		}
		if n.Active {
			n.Open = true
		}
		return n
	}

	out := make([]TreeNode, 0, len(roots))
	for _, r := range roots {
		out = append(out, walk(r))
	}
	return out
}

func (s *Store) scanGroups(ctx context.Context, q string, args ...any) ([]Group, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Group
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ID, &g.TenantID, &g.ParentID, &g.Name, &g.Description, &g.CreatedBy, &g.CreatedAt, &g.ChildCount, &g.SkillCount); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
