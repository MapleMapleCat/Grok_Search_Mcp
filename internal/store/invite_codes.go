package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/MapleMapleCat/Grok_Search_Mcp/internal/keyhash"
)

const inviteCodeColumns = `id, code_hash, code_prefix, registration_limit, registration_count, enabled, created_by_user_id, created_at, updated_at`

func scanInviteCode(row interface {
	Scan(dest ...any) error
}) (*InviteCode, error) {
	var inviteCode InviteCode
	var enabled int
	var createdByUserID sql.NullString
	var createdAt string
	var updatedAt string

	err := row.Scan(
		&inviteCode.ID,
		&inviteCode.CodeHash,
		&inviteCode.CodePrefix,
		&inviteCode.RegistrationLimit,
		&inviteCode.RegistrationCount,
		&enabled,
		&createdByUserID,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return nil, err
	}

	inviteCode.Enabled = enabled != 0
	if createdByUserID.Valid {
		inviteCode.CreatedByUserID = createdByUserID.String
	}
	var parseErr error
	inviteCode.CreatedAt, parseErr = parseTime(createdAt)
	if parseErr != nil {
		return nil, parseErr
	}
	inviteCode.UpdatedAt, parseErr = parseTime(updatedAt)
	if parseErr != nil {
		return nil, parseErr
	}
	return &inviteCode, nil
}

const inviteCodeRedemptionColumns = `id, invite_code_id, invite_code_prefix, user_id, username, redeemed_at`

func scanInviteCodeRedemption(row interface {
	Scan(dest ...any) error
}) (*InviteCodeRedemption, error) {
	var redemption InviteCodeRedemption
	var redeemedAt string
	if err := row.Scan(
		&redemption.ID,
		&redemption.InviteCodeID,
		&redemption.InviteCodePrefix,
		&redemption.UserID,
		&redemption.Username,
		&redeemedAt,
	); err != nil {
		return nil, err
	}

	parsedRedeemedAt, err := parseTime(redeemedAt)
	if err != nil {
		return nil, err
	}
	redemption.RedeemedAt = parsedRedeemedAt
	return &redemption, nil
}

func (s *SQLiteStore) ListInviteCodeRedemptionsPage(
	ctx context.Context,
	inviteCodeID string,
	cursor *TimeIDCursor,
	limit int,
) (*InviteCodeRedemptionPage, error) {
	pageLimit := normalizePanelPageLimit(limit)
	query := `SELECT ` + inviteCodeRedemptionColumns + `
		 FROM invite_code_redemptions
		 WHERE invite_code_id = ?`
	queryArguments := []any{strings.TrimSpace(inviteCodeID)}
	if cursor != nil {
		query += ` AND ` + timeIDCursorPredicateForColumn("redeemed_at", timeIDDescending)
		queryArguments = appendTimeIDCursorArguments(queryArguments, cursor)
	}
	query += ` ORDER BY redeemed_at DESC, id DESC LIMIT ?`
	queryArguments = append(queryArguments, keysetFetchLimit(pageLimit))

	rows, err := s.readDB.QueryContext(ctx, query, queryArguments...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	redemptions := make([]*InviteCodeRedemption, 0, keysetFetchLimit(pageLimit))
	for rows.Next() {
		redemption, scanErr := scanInviteCodeRedemption(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		redemptions = append(redemptions, redemption)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	redemptions, hasMore, nextCursor := finalizeTimeIDPage(redemptions, pageLimit, func(redemption *InviteCodeRedemption) TimeIDCursor {
		return TimeIDCursor{Timestamp: redemption.RedeemedAt, ID: redemption.ID}
	})
	return &InviteCodeRedemptionPage{
		Redemptions: redemptions,
		HasMore:     hasMore,
		NextCursor:  nextCursor,
	}, nil
}

func recordInviteCodeRedemptionInTransaction(
	ctx context.Context,
	transaction *sql.Tx,
	inviteCode *InviteCode,
	userID string,
	username string,
	redeemedAt string,
) error {
	redemptionID, err := randomID()
	if err != nil {
		return err
	}
	_, err = transaction.ExecContext(ctx,
		`INSERT INTO invite_code_redemptions (
			id, invite_code_id, invite_code_prefix, user_id, username, redeemed_at
		 ) VALUES (?, ?, ?, ?, ?, ?)`,
		redemptionID,
		inviteCode.ID,
		inviteCode.CodePrefix,
		userID,
		username,
		redeemedAt,
	)
	if err != nil {
		return fmt.Errorf("insert invite code redemption: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ListInviteCodesPage(ctx context.Context, cursor *TimeIDCursor, limit int) (*InviteCodePage, error) {
	pageLimit := normalizePanelPageLimit(limit)
	query := `SELECT ` + inviteCodeColumns + ` FROM invite_codes`
	queryArgs := make([]any, 0, 4)
	if cursor != nil {
		query += ` WHERE ` + timeIDCursorPredicate(timeIDDescending)
		queryArgs = appendTimeIDCursorArguments(queryArgs, cursor)
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	queryArgs = append(queryArgs, keysetFetchLimit(pageLimit))

	rows, err := s.readDB.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	inviteCodes := make([]*InviteCode, 0, keysetFetchLimit(pageLimit))
	for rows.Next() {
		inviteCode, scanErr := scanInviteCode(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		inviteCodes = append(inviteCodes, inviteCode)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	inviteCodes, hasMore, nextCursor := finalizeTimeIDPage(inviteCodes, pageLimit, func(inviteCode *InviteCode) TimeIDCursor {
		return TimeIDCursor{Timestamp: inviteCode.CreatedAt, ID: inviteCode.ID}
	})
	page := &InviteCodePage{
		InviteCodes: inviteCodes,
		HasMore:     hasMore,
		NextCursor:  nextCursor,
	}
	if err := s.readDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM invite_codes`).Scan(&page.TotalCount); err != nil {
		return nil, err
	}
	return page, nil
}

func (s *SQLiteStore) CreateInviteCode(ctx context.Context, createdByUserID string, registrationLimit int) (*InviteCode, string, error) {
	createdByUserID = strings.TrimSpace(createdByUserID)
	if registrationLimit <= 0 {
		return nil, "", fmt.Errorf("registration_limit must be positive")
	}

	rawInviteCode, err := generateRawKey()
	if err != nil {
		return nil, "", err
	}
	inviteCodeID, err := randomID()
	if err != nil {
		return nil, "", err
	}

	codePrefix := rawInviteCode
	if len(codePrefix) > 12 {
		codePrefix = codePrefix[:12]
	}
	now := formatTime(time.Now().UTC())

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO invite_codes (id, code, code_hash, code_prefix, registration_limit, registration_count, enabled, created_by_user_id, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, 0, 1, ?, ?, ?)`,
		inviteCodeID,
		"",
		keyhash.HashAPIKey(rawInviteCode),
		codePrefix,
		registrationLimit,
		createdByUserID,
		now,
		now,
	)
	if err != nil {
		return nil, "", fmt.Errorf("insert invite code: %w", err)
	}

	inviteCode, err := s.getInviteCodeByID(ctx, inviteCodeID)
	if err != nil {
		return nil, "", err
	}
	return inviteCode, rawInviteCode, nil
}

func (s *SQLiteStore) UpdateInviteCode(ctx context.Context, id string, updates InviteCodeUpdates) (*InviteCode, error) {
	existingInviteCode, err := s.getInviteCodeByID(ctx, id)
	if err != nil {
		return nil, err
	}

	sets := make([]string, 0, 3)
	args := make([]any, 0, 4)
	if updates.RegistrationLimit != nil {
		registrationLimit := *updates.RegistrationLimit
		if registrationLimit <= 0 {
			return nil, fmt.Errorf("registration_limit must be positive")
		}
		if registrationLimit < existingInviteCode.RegistrationCount {
			return nil, ErrInviteCodeLimitTooLow
		}
		sets = append(sets, "registration_limit = ?")
		args = append(args, registrationLimit)
	}
	if updates.Enabled != nil {
		sets = append(sets, "enabled = ?")
		args = append(args, boolAsInteger(*updates.Enabled))
	}

	if len(sets) == 0 {
		return existingInviteCode, nil
	}

	sets = append(sets, "updated_at = ?")
	args = append(args, formatTime(time.Now().UTC()))
	args = append(args, strings.TrimSpace(id))

	query := `UPDATE invite_codes SET ` + strings.Join(sets, ", ") + ` WHERE id = ?`
	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return nil, ErrInviteCodeNotFound
	}
	return s.getInviteCodeByID(ctx, id)
}

func (s *SQLiteStore) DeleteInviteCode(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM invite_codes WHERE id = ?`, strings.TrimSpace(id))
	if err != nil {
		return err
	}
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return ErrInviteCodeNotFound
	}
	return nil
}

// InviteCodeExists performs a cheap, non-authoritative lookup so callers can
// reject unknown codes before expensive password hashing. Registration must
// still validate and consume the code inside mode-aware transactional registration.
func (s *SQLiteStore) InviteCodeExists(ctx context.Context, rawInviteCode string) (bool, error) {
	rawInviteCode = strings.TrimSpace(rawInviteCode)
	if rawInviteCode == "" {
		return false, nil
	}

	var exists int
	err := s.readDB.QueryRowContext(ctx,
		`SELECT 1 FROM invite_codes WHERE code_hash = ? LIMIT 1`, keyhash.HashAPIKey(rawInviteCode),
	).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *SQLiteStore) getInviteCodeByID(ctx context.Context, id string) (*InviteCode, error) {
	inviteCode, err := scanInviteCode(s.readDB.QueryRowContext(ctx,
		`SELECT `+inviteCodeColumns+` FROM invite_codes WHERE id = ?`, strings.TrimSpace(id),
	))
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrInviteCodeNotFound
		}
		return nil, err
	}
	return inviteCode, nil
}
