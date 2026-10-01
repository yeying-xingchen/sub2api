package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestOpenAIReauthClaimGuardsCredentialsProxyCooldownAndScheduling(t *testing.T) {
	for _, affected := range []int64{0, 1} {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })
		repo := newAccountRepositoryWithSQL(nil, db, nil)
		query := "(?s)" + regexp.QuoteMeta("UPDATE accounts SET") + ".*" + regexp.QuoteMeta("credentials = $2::jsonb AND proxy_id IS NOT DISTINCT FROM $3") + ".*" + regexp.QuoteMeta("status = 'active' AND schedulable IS TRUE") + ".*" + regexp.QuoteMeta("extra->'openai_auto_reauth'->>'retry_after'") + ".*" + regexp.QuoteMeta("INSERT INTO scheduler_outbox")
		mock.ExpectExec(query).
			WithArgs(int64(10), `{"access_token":"old"}`, nil, `{"status":"running"}`, sqlmock.AnyArg(), service.OpenAIReauthPendingReason, sqlmock.AnyArg(), service.SchedulerOutboxEventAccountChanged, service.OpenAIReauthFailedReason).
			WillReturnResult(sqlmock.NewResult(0, affected))
		claimed, err := repo.ClaimOpenAIReauth(context.Background(), &service.Account{ID: 10, Credentials: map[string]any{"access_token": "old"}}, map[string]any{"status": "running"}, time.Now().Add(time.Minute))
		require.NoError(t, err)
		require.Equal(t, affected > 0, claimed)
		require.NoError(t, mock.ExpectationsWereMet())
	}
}

func TestOpenAIReauthCompletionPreservesCredentialsOnFailureAndUsesCAS(t *testing.T) {
	for _, success := range []bool{false, true} {
		for _, affected := range []int64{0, 1} {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			repo := newAccountRepositoryWithSQL(nil, db, nil)
			next := `{"access_token":"old"}`
			if success {
				next = `{"access_token":"new"}`
			}
			query := "(?s)" + regexp.QuoteMeta("UPDATE accounts SET credentials = $4::jsonb") + ".*" + regexp.QuoteMeta("credentials = $2::jsonb AND proxy_id IS NOT DISTINCT FROM $3") + ".*" + regexp.QuoteMeta("status = 'active' AND schedulable IS TRUE") + ".*" + regexp.QuoteMeta("temp_unschedulable_reason = $8") + ".*" + regexp.QuoteMeta("INSERT INTO scheduler_outbox")
			mock.ExpectExec(query).WithArgs(int64(10), `{"access_token":"old"}`, nil, next, `{"status":"done"}`, success, service.OpenAIReauthFailedReason, service.OpenAIReauthPendingReason, service.SchedulerOutboxEventAccountChanged).WillReturnResult(sqlmock.NewResult(0, affected))
			applied, err := repo.CompleteOpenAIReauth(context.Background(), &service.Account{ID: 10, Credentials: map[string]any{"access_token": "old"}}, map[string]any{"access_token": "new"}, map[string]any{"status": "done"}, success)
			require.NoError(t, err)
			require.Equal(t, affected > 0, applied)
			require.NoError(t, mock.ExpectationsWereMet())
		}
	}
}
