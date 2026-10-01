package repository

import (
	"context"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// The caller already holds the account row lock in its update transaction.
// Preserve server-owned reauth state and reject a stale credential replacement.
func guardOpenAIAccountUpdate(ctx context.Context, client *dbent.Client, accountID int64, extra map[string]any) error {
	snapshot, guarded := service.OpenAIAccountUpdateSnapshot(ctx)
	if !guarded {
		return nil
	}
	rows, err := client.QueryContext(ctx, `SELECT credentials = $2::jsonb, extra -> 'openai_auto_reauth' FROM accounts WHERE id = $1 AND deleted_at IS NULL`, accountID, snapshot)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return service.ErrAccountNotFound
	}
	var unchanged bool
	var state []byte
	if err := rows.Scan(&unchanged, &state); err != nil {
		return err
	}
	if !unchanged {
		return infraerrors.Conflict("ACCOUNT_CREDENTIALS_CHANGED", "account credentials changed during this update; reload the account and retry")
	}
	delete(extra, service.OpenAIAutoReauthStateKey)
	value, present, err := decodeAccountExtraJSON(state)
	if err != nil {
		return err
	}
	if present {
		extra[service.OpenAIAutoReauthStateKey] = value
	}
	return rows.Err()
}
