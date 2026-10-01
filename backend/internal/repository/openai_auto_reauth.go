package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// ClaimOpenAIReauth uses the existing JSONB credential version as a CAS guard.
// Publishing the scheduler event in the same statement prevents a server crash
// between the claim and the outbox insert from leaving a stale scheduler cache.
func (r *accountRepository) ClaimOpenAIReauth(ctx context.Context, account *service.Account, state map[string]any, until time.Time) (bool, error) {
	expected, err := json.Marshal(normalizeJSONMap(account.Credentials))
	if err != nil {
		return false, err
	}
	stateJSON, err := json.Marshal(state)
	if err != nil {
		return false, err
	}
	result, err := r.sql.ExecContext(ctx, `
		WITH updated AS (
			UPDATE accounts SET
				extra = COALESCE(extra, '{}'::jsonb) || jsonb_build_object('openai_auto_reauth', $4::jsonb),
				temp_unschedulable_until = $5, temp_unschedulable_reason = $6, updated_at = NOW()
			WHERE id = $1 AND credentials = $2::jsonb AND proxy_id IS NOT DISTINCT FROM $3
				AND deleted_at IS NULL AND platform = 'openai' AND type = 'oauth'
				AND parent_account_id IS NULL AND status = 'active' AND schedulable IS TRUE
				AND (auto_pause_on_expired IS NOT TRUE OR expires_at IS NULL OR expires_at > NOW())
				AND COALESCE(extra->'openai_auto_reauth'->>'retry_after', '') <= $7
				AND (temp_unschedulable_until IS NULL OR temp_unschedulable_until <= NOW()
					OR temp_unschedulable_reason IN ($6, $9))
			RETURNING id
		)
		INSERT INTO scheduler_outbox (event_type, account_id, group_id, payload)
		SELECT $8, updated.id, NULL, NULL FROM updated
	`, account.ID, string(expected), account.ProxyID, string(stateJSON), until,
		service.OpenAIReauthPendingReason, time.Now().UTC().Format(time.RFC3339), service.SchedulerOutboxEventAccountChanged, service.OpenAIReauthFailedReason)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	if err == nil && n > 0 {
		r.syncSchedulerAccountSnapshotDetached(ctx, account.ID)
	}
	return n > 0, err
}

func (r *accountRepository) CompleteOpenAIReauth(ctx context.Context, account *service.Account, credentials, state map[string]any, succeeded bool) (bool, error) {
	expected, err := json.Marshal(normalizeJSONMap(account.Credentials))
	if err != nil {
		return false, err
	}
	if !succeeded {
		credentials = account.Credentials
	}
	next, err := json.Marshal(normalizeJSONMap(credentials))
	if err != nil {
		return false, err
	}
	stateJSON, err := json.Marshal(state)
	if err != nil {
		return false, err
	}
	result, err := r.sql.ExecContext(ctx, `
		WITH updated AS (
			UPDATE accounts SET credentials = $4::jsonb,
				extra = COALESCE(extra, '{}'::jsonb) || jsonb_build_object('openai_auto_reauth', $5::jsonb),
				temp_unschedulable_until = CASE WHEN $6 THEN NULL ELSE temp_unschedulable_until END,
				temp_unschedulable_reason = CASE WHEN $6 THEN '' ELSE $7 END,
				updated_at = NOW()
			WHERE id = $1 AND credentials = $2::jsonb AND proxy_id IS NOT DISTINCT FROM $3
				AND deleted_at IS NULL AND platform = 'openai' AND type = 'oauth'
				AND parent_account_id IS NULL AND status = 'active' AND schedulable IS TRUE
				AND temp_unschedulable_reason = $8
				AND (auto_pause_on_expired IS NOT TRUE OR expires_at IS NULL OR expires_at > NOW())
			RETURNING id
		)
		INSERT INTO scheduler_outbox (event_type, account_id, group_id, payload)
		SELECT $9, updated.id, NULL, NULL FROM updated
	`, account.ID, string(expected), account.ProxyID, string(next), string(stateJSON), succeeded,
		service.OpenAIReauthFailedReason, service.OpenAIReauthPendingReason, service.SchedulerOutboxEventAccountChanged)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	if err == nil && n > 0 {
		r.syncSchedulerAccountSnapshotDetached(ctx, account.ID)
	}
	return n > 0, err
}
