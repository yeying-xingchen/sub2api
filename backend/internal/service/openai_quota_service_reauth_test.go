//go:build unit

package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/stretchr/testify/require"
)

type quotaAutoReauthRepo struct {
	*stubQuotaAccountRepo
	attempts *reauthRepoStub
}

func (r *quotaAutoReauthRepo) ClaimOpenAIReauth(ctx context.Context, account *Account, state map[string]any, until time.Time) (bool, error) {
	return r.attempts.ClaimOpenAIReauth(ctx, account, state, until)
}

func (r *quotaAutoReauthRepo) CompleteOpenAIReauth(ctx context.Context, account *Account, credentials, state map[string]any, succeeded bool) (bool, error) {
	return r.attempts.CompleteOpenAIReauth(ctx, account, credentials, state, succeeded)
}

func TestOpenAIQuotaService401TriggersAutomaticReauthorization(t *testing.T) {
	for _, name := range []string{"usage", "details", "reset", "targeted reset", "shadow", "stale token", "forbidden"} {
		t.Run(name, func(t *testing.T) {
			owner := reauthTestAccount()
			accountID := owner.ID
			accounts := map[int64]*Account{owner.ID: owner}
			if name == "shadow" {
				accountID = 2
				accounts[accountID] = &Account{ID: accountID, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
					ParentAccountID: &owner.ID, QuotaDimension: QuotaDimensionSpark}
			}
			if name == "stale token" {
				owner.Credentials["access_token"] = "newer-authorized-token"
			}
			attempts := &reauthRepoStub{account: owner, claimAllowed: true, completeAllowed: true, completed: make(chan bool, 1)}
			repo := &quotaAutoReauthRepo{stubQuotaAccountRepo: &stubQuotaAccountRepo{accounts: accounts}, attempts: attempts}
			auto := NewOpenAIAutoReauthService(repo, nil, reauthTestCipher{}, nil)
			auto.login = func(_ context.Context, login openai.LoginCredentials, _, workspace string) (*openai.TokenResponse, error) {
				return reauthTestToken(login.Email, workspace), nil
			}
			rate := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
			rate.openAIAutoReauth = auto
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer old" {
					t.Errorf("unexpected request token: %q", r.Header.Get("Authorization"))
				}
				w.Header().Set("Content-Type", "application/json")
				if name == "details" && r.URL.Path == "/backend-api/wham/usage" {
					_, _ = w.Write([]byte(`{"account_id":"workspace","plan_type":"plus"}`))
					return
				}
				status := http.StatusUnauthorized
				if name == "forbidden" {
					status = http.StatusForbidden
				}
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":{"code":"token_revoked"}}`))
			}))
			defer srv.Close()
			cache := &stubQuotaTokenCache{tokens: map[string]string{OpenAITokenCacheKey(owner): "old"}}
			svc := NewOpenAIQuotaService(repo, nil, NewOpenAITokenProvider(repo, cache, nil), newQuotaRedirectingFactory(srv), nil)
			svc.rateLimitService = rate
			var err error
			switch name {
			case "reset":
				_, err = svc.ResetCredit(context.Background(), accountID)
			case "targeted reset":
				_, err = svc.ResetCreditTargeted(context.Background(), accountID, "credit", "request")
			default:
				_, err = svc.QueryUsage(context.Background(), accountID)
			}
			if name == "details" {
				require.NoError(t, err, "optional detail failure must preserve the successful usage result")
			} else {
				require.Error(t, err, "the original request must still report its upstream failure")
			}
			if name == "stale token" || name == "forbidden" {
				require.Zero(t, attempts.claims)
				return
			}
			require.True(t, awaitReauthCompletion(t, attempts))
			attempts.mu.Lock()
			defer attempts.mu.Unlock()
			require.Equal(t, 1, attempts.claims)
			require.Equal(t, "new-access", attempts.credentials["access_token"])
		})
	}
}
