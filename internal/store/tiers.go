package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

const tierColumns = `id, name, rpm, success_limit, is_default, created_at, updated_at`

func scanTier(row interface {
	Scan(dest ...any) error
}) (*Tier, error) {
	var t Tier
	var isDefault int
	var createdAt, updatedAt string
	err := row.Scan(
		&t.ID, &t.Name, &t.RPM, &t.SuccessLimit,
		&isDefault, &createdAt, &updatedAt,
	)
	if err != nil {
		return nil, err
	}
	if t.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if t.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, err
	}
	t.IsDefault = isDefault != 0
	return &t, nil
}

func (s *SQLiteStore) GetTierByID(ctx context.Context, id string) (*Tier, error) {
	row := s.readDB.QueryRowContext(ctx,
		`SELECT `+tierColumns+` FROM tiers WHERE id = ?`, id)
	t, err := scanTier(row)
	if err == sql.ErrNoRows {
		return nil, ErrTierNotFound
	}
	return t, err
}

// GetDefaultTier returns the explicitly selected tier for newly created users.
func (s *SQLiteStore) GetDefaultTier(ctx context.Context) (*Tier, error) {
	row := s.readDB.QueryRowContext(ctx,
		`SELECT `+tierColumns+` FROM tiers WHERE is_default = 1 LIMIT 1`)
	tier, err := scanTier(row)
	if err == sql.ErrNoRows {
		return nil, ErrTierNotFound
	}
	return tier, err
}

// GetTiersByIDs loads all matching tiers in one query. Missing IDs are omitted
// from the returned map so callers can distinguish broken user references.
func (s *SQLiteStore) GetTiersByIDs(ctx context.Context, ids []string) (map[string]*Tier, error) {
	tierByID := make(map[string]*Tier)
	uniqueTierIDs := make([]string, 0, len(ids))
	seenTierIDs := make(map[string]struct{}, len(ids))
	for _, rawTierID := range ids {
		tierID := strings.TrimSpace(rawTierID)
		if tierID == "" {
			continue
		}
		if _, alreadyAdded := seenTierIDs[tierID]; alreadyAdded {
			continue
		}
		seenTierIDs[tierID] = struct{}{}
		uniqueTierIDs = append(uniqueTierIDs, tierID)
	}
	if len(uniqueTierIDs) == 0 {
		return tierByID, nil
	}

	placeholders := make([]string, len(uniqueTierIDs))
	queryArguments := make([]any, len(uniqueTierIDs))
	for index, tierID := range uniqueTierIDs {
		placeholders[index] = "?"
		queryArguments[index] = tierID
	}
	rows, err := s.readDB.QueryContext(ctx,
		`SELECT `+tierColumns+` FROM tiers WHERE id IN (`+strings.Join(placeholders, ", ")+`)`,
		queryArguments...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		tier, scanErr := scanTier(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		tierByID[tier.ID] = tier
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return tierByID, nil
}

func (s *SQLiteStore) ListTiersPage(ctx context.Context, cursor *TierCursor, limit int) (*TierPage, error) {
	pageLimit := normalizePanelPageLimit(limit)
	query := `SELECT ` + tierColumns + ` FROM tiers`
	queryArgs := make([]any, 0, 3)
	if cursor != nil {
		query += ` WHERE created_at > ? OR (created_at = ? AND id > ?)`
		formattedCreatedAt := formatTime(cursor.CreatedAt)
		queryArgs = append(queryArgs, formattedCreatedAt, formattedCreatedAt, cursor.ID)
	}
	query += ` ORDER BY created_at ASC, id ASC LIMIT ?`
	queryArgs = append(queryArgs, pageLimit+1)

	rows, err := s.readDB.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tiers := make([]*Tier, 0, pageLimit+1)
	for rows.Next() {
		tier, scanErr := scanTier(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		tiers = append(tiers, tier)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	page := &TierPage{}
	if len(tiers) > pageLimit {
		page.HasMore = true
		tiers = tiers[:pageLimit]
	}
	page.Tiers = tiers
	if page.HasMore && len(tiers) > 0 {
		lastTier := tiers[len(tiers)-1]
		page.NextCursor = &TierCursor{CreatedAt: lastTier.CreatedAt, ID: lastTier.ID}
	}
	if err := s.readDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM tiers`).Scan(&page.TotalCount); err != nil {
		return nil, err
	}
	return page, nil
}

func (s *SQLiteStore) CreateTier(ctx context.Context, name string, rpm, successLimit int, isDefault bool) (*Tier, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("tier name is required")
	}
	if rpm < 0 {
		return nil, fmt.Errorf("rpm must be >= 0")
	}
	if successLimit < 0 {
		return nil, fmt.Errorf("success_limit must be >= 0")
	}
	id, err := randomID()
	if err != nil {
		return nil, err
	}
	now := formatTime(nowUTC())
	transaction, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = transaction.Rollback() }()

	var defaultTierCount int
	if err := transaction.QueryRowContext(ctx, `SELECT COUNT(*) FROM tiers WHERE is_default = 1`).Scan(&defaultTierCount); err != nil {
		return nil, err
	}
	shouldBecomeDefault := isDefault || defaultTierCount == 0
	if shouldBecomeDefault && defaultTierCount > 0 {
		if _, err := transaction.ExecContext(ctx,
			`UPDATE tiers SET is_default = 0, updated_at = ? WHERE is_default = 1`, now,
		); err != nil {
			return nil, err
		}
	}

	_, err = transaction.ExecContext(ctx,
		`INSERT INTO tiers (id, name, rpm, success_limit, is_default, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, name, rpm, successLimit, boolAsInteger(shouldBecomeDefault), now, now,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, ErrTierNameTaken
		}
		return nil, fmt.Errorf("insert tier: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return nil, err
	}
	return s.GetTierByID(ctx, id)
}

func (s *SQLiteStore) UpdateTier(ctx context.Context, id string, updates TierUpdates) (*Tier, error) {
	transaction, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = transaction.Rollback() }()

	existingTier, err := scanTier(transaction.QueryRowContext(ctx, `SELECT `+tierColumns+` FROM tiers WHERE id = ?`, id))
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrTierNotFound
		}
		return nil, err
	}
	var sets []string
	var args []any
	if updates.Name != nil {
		name := strings.TrimSpace(*updates.Name)
		if name == "" {
			return nil, fmt.Errorf("tier name must not be empty")
		}
		sets = append(sets, "name = ?")
		args = append(args, name)
	}
	if updates.RPM != nil {
		if *updates.RPM < 0 {
			return nil, fmt.Errorf("rpm must be >= 0")
		}
		sets = append(sets, "rpm = ?")
		args = append(args, *updates.RPM)
	}
	if updates.SuccessLimit != nil {
		if *updates.SuccessLimit < 0 {
			return nil, fmt.Errorf("success_limit must be >= 0")
		}
		sets = append(sets, "success_limit = ?")
		args = append(args, *updates.SuccessLimit)
	}
	if updates.IsDefault != nil {
		if !*updates.IsDefault && existingTier.IsDefault {
			return nil, ErrDefaultTierProtected
		}
		if *updates.IsDefault && !existingTier.IsDefault {
			now := formatTime(nowUTC())
			if _, err := transaction.ExecContext(ctx,
				`UPDATE tiers SET is_default = 0, updated_at = ? WHERE is_default = 1 AND id <> ?`,
				now, id,
			); err != nil {
				return nil, err
			}
			sets = append(sets, "is_default = 1")
		}
	}
	if len(sets) == 0 {
		if err := transaction.Commit(); err != nil {
			return nil, err
		}
		return existingTier, nil
	}
	sets = append(sets, "updated_at = ?")
	args = append(args, formatTime(nowUTC()))
	args = append(args, id)
	q := `UPDATE tiers SET ` + strings.Join(sets, ", ") + ` WHERE id = ?`
	if _, err := transaction.ExecContext(ctx, q, args...); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, ErrTierNameTaken
		}
		return nil, err
	}
	updatedTier, err := scanTier(transaction.QueryRowContext(ctx, `SELECT `+tierColumns+` FROM tiers WHERE id = ?`, id))
	if err != nil {
		return nil, err
	}
	if err := transaction.Commit(); err != nil {
		return nil, err
	}
	return updatedTier, nil
}

func (s *SQLiteStore) DeleteTier(ctx context.Context, id string) error {
	transaction, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = transaction.Rollback() }()

	tier, err := scanTier(transaction.QueryRowContext(ctx, `SELECT `+tierColumns+` FROM tiers WHERE id = ?`, id))
	if err != nil {
		if err == sql.ErrNoRows {
			return ErrTierNotFound
		}
		return err
	}
	if tier.IsDefault {
		return ErrDefaultTierProtected
	}

	var defaultTierID string
	if err := transaction.QueryRowContext(ctx,
		`SELECT id FROM tiers WHERE is_default = 1 LIMIT 1`,
	).Scan(&defaultTierID); err != nil {
		if err == sql.ErrNoRows {
			return ErrDefaultTierMissing
		}
		return err
	}

	updatedAt := formatTime(nowUTC())
	if _, err := transaction.ExecContext(ctx,
		`UPDATE users SET tier_id = ?, updated_at = ? WHERE tier_id = ?`,
		defaultTierID, updatedAt, id,
	); err != nil {
		return err
	}

	res, err := transaction.ExecContext(ctx, `DELETE FROM tiers WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrTierNotFound
	}
	return transaction.Commit()
}

const countUsersByTierQuery = `SELECT COUNT(*) FROM users WHERE tier_id = ?`

func (s *SQLiteStore) CountUsersByTier(ctx context.Context, tierID string) (int64, error) {
	var n int64
	err := s.readDB.QueryRowContext(ctx, countUsersByTierQuery, tierID).Scan(&n)
	return n, err
}
