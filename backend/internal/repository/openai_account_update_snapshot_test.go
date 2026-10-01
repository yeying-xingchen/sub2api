package repository

import (
	"context"
	"regexp"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestOpenAIAccountUpdateRejectsStaleCredentials(t *testing.T) {
	for _, unchanged := range []bool{false, true} {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })
		client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
		account := &service.Account{ID: 3, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Credentials: map[string]any{"access_token": "old"}}
		ctx := service.WithOpenAIAccountUpdateSnapshot(context.Background(), account)
		// The service is now preparing an update; the snapshot must remain old.
		account.Credentials["access_token"] = "edited"
		mock.ExpectQuery(regexp.QuoteMeta("SELECT credentials = $2::jsonb, extra -> 'openai_auto_reauth' FROM accounts WHERE id = $1 AND deleted_at IS NULL")).WithArgs(int64(3), `{"access_token":"old"}`).WillReturnRows(sqlmock.NewRows([]string{"same", "state"}).AddRow(unchanged, `{"status":"running","retry_after":"2026-10-01T00:00:00Z"}`))
		extra := map[string]any{service.OpenAIAutoReauthStateKey: map[string]any{"status": "stale"}, "other": true}
		err = guardOpenAIAccountUpdate(ctx, client, 3, extra)
		if unchanged {
			require.NoError(t, err)
			require.Equal(t, "running", extra[service.OpenAIAutoReauthStateKey].(map[string]any)["status"])
			require.Equal(t, true, extra["other"])
		} else {
			require.True(t, infraerrors.IsConflict(err))
		}
		require.NoError(t, mock.ExpectationsWereMet())
	}
}
