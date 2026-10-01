//go:build unit

package service

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/stretchr/testify/require"
)

const reauthTestLine = "owner@example.com----test-password----JBSWY3DPEHPK3PXP"

type reauthTestCipher struct{}

func (reauthTestCipher) Encrypt(s string) (string, error) {
	return base64.StdEncoding.EncodeToString([]byte(s)), nil
}
func (reauthTestCipher) Decrypt(s string) (string, error) {
	b, err := base64.StdEncoding.DecodeString(s)
	return string(b), err
}

func TestPrepareOpenAIReauthCredentials(t *testing.T) {
	incoming := map[string]any{OpenAILoginCredentialsKey: reauthTestLine, OpenAIAutoReauthEnabledKey: true}
	out, err := prepareOpenAIReauthCredentials(PlatformOpenAI, AccountTypeOAuth, nil, incoming, reauthTestCipher{})
	require.NoError(t, err)
	require.NotContains(t, out, OpenAILoginCredentialsKey)
	require.NotEqual(t, reauthTestLine, out[OpenAILoginCredentialsEncryptedKey])
	require.Equal(t, reauthTestLine, incoming[OpenAILoginCredentialsKey])
	secret := out[OpenAILoginCredentialsEncryptedKey]
	preserved, err := prepareOpenAIReauthCredentials(PlatformOpenAI, AccountTypeOAuth, out, map[string]any{"access_token": "new"}, nil)
	require.NoError(t, err)
	require.Equal(t, secret, preserved[OpenAILoginCredentialsEncryptedKey])
	require.Equal(t, true, preserved[OpenAIAutoReauthEnabledKey])
	cleared, err := prepareOpenAIReauthCredentials(PlatformOpenAI, AccountTypeOAuth, out, map[string]any{OpenAILoginCredentialsKey: "", OpenAIAutoReauthEnabledKey: false}, nil)
	require.NoError(t, err)
	merged := MergePreservingSensitiveCreds(out, cleared)
	require.Empty(t, merged[OpenAILoginCredentialsEncryptedKey])
	for name, input := range map[string]map[string]any{
		"missing":           {OpenAIAutoReauthEnabledKey: true},
		"wrong type":        {OpenAIAutoReauthEnabledKey: "true"},
		"short code":        {OpenAILoginCredentialsKey: "owner@example.com----password----123456"},
		"wrong email":       {OpenAILoginCredentialsKey: reauthTestLine, "email": "other@example.com"},
		"forged ciphertext": {OpenAILoginCredentialsEncryptedKey: "forged", OpenAIAutoReauthEnabledKey: true},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := prepareOpenAIReauthCredentials(PlatformOpenAI, AccountTypeOAuth, nil, input, reauthTestCipher{})
			require.Error(t, err)
		})
	}
	_, err = prepareOpenAIReauthCredentials(PlatformAnthropic, AccountTypeOAuth, nil, incoming, reauthTestCipher{})
	require.Error(t, err)
	_, err = prepareOpenAIReauthCredentials(PlatformOpenAI, AccountTypeOAuth, nil, incoming, nil)
	require.Error(t, err)
	exported := ExportAccountCredentials(out)
	require.NotContains(t, exported, OpenAILoginCredentialsEncryptedKey)
	require.NotContains(t, exported, OpenAIAutoReauthEnabledKey)
	require.Contains(t, out, OpenAILoginCredentialsEncryptedKey)
}

type reauthRepoStub struct {
	AccountRepository
	mu              sync.Mutex
	account         *Account
	claims          int
	claimAllowed    bool
	completeAllowed bool
	completed       chan bool
	credentials     map[string]any
	state           map[string]any
}

func (r *reauthRepoStub) GetByID(context.Context, int64) (*Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a := *r.account
	a.Credentials = shallowCopyMap(r.account.Credentials)
	return &a, nil
}
func (r *reauthRepoStub) ClaimOpenAIReauth(_ context.Context, _ *Account, _ map[string]any, _ time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.claims++
	return r.claimAllowed, nil
}
func (r *reauthRepoStub) CompleteOpenAIReauth(_ context.Context, _ *Account, credentials, state map[string]any, success bool) (bool, error) {
	r.mu.Lock()
	r.credentials, r.state = credentials, state
	allowed := r.completeAllowed
	r.mu.Unlock()
	r.completed <- success
	return allowed, nil
}
func reauthTestAccount() *Account {
	secret, _ := (reauthTestCipher{}).Encrypt(reauthTestLine)
	return &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{OpenAIAutoReauthEnabledKey: true, OpenAILoginCredentialsEncryptedKey: secret, "access_token": "old", "email": "owner@example.com", "chatgpt_account_id": "workspace", "model_mapping": map[string]any{"a": "b"}}}
}
func reauthTestToken(email, workspace string) *openai.TokenResponse {
	payload := `{"email":"` + email + `","https://api.openai.com/auth":{"chatgpt_account_id":"` + workspace + `"}}`
	return &openai.TokenResponse{AccessToken: "new-access", RefreshToken: "new-refresh", ExpiresIn: 3600, IDToken: "e30." + base64.RawURLEncoding.EncodeToString([]byte(payload)) + ".signature"}
}
func awaitReauthCompletion(t *testing.T, repo *reauthRepoStub) bool {
	t.Helper()
	select {
	case ok := <-repo.completed:
		return ok
	case <-time.After(3 * time.Second):
		t.Fatal("reauth did not finish")
		return false
	}
}

func TestOpenAIAutoReauth401DeduplicatesAndPersists(t *testing.T) {
	account := reauthTestAccount()
	repo := &reauthRepoStub{account: account, claimAllowed: true, completeAllowed: true, completed: make(chan bool, 1)}
	svc := NewOpenAIAutoReauthService(repo, nil, reauthTestCipher{}, nil)
	entered, finish := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	svc.login = func(ctx context.Context, input openai.LoginCredentials, proxy, workspace string) (*openai.TokenResponse, error) {
		calls.Add(1)
		close(entered)
		<-finish
		require.NoError(t, ctx.Err())
		require.Equal(t, "workspace", workspace)
		return reauthTestToken(input.Email, workspace), nil
	}
	rate := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	rate.openAIAutoReauth = svc
	ctx, cancel := context.WithCancel(context.Background())
	require.True(t, rate.HandleUpstreamError(ctx, account, http.StatusUnauthorized, nil, []byte(`{"error":{"code":"token_revoked"}}`)))
	cancel() // The request may fail over while the detached login continues.
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("login not started")
	}
	for i := 0; i < 10; i++ {
		require.Equal(t, ErrorPolicyTempUnscheduled, rate.CheckErrorPolicy(context.Background(), account, 401, nil))
	}
	close(finish)
	require.True(t, awaitReauthCompletion(t, repo))
	require.Equal(t, int32(1), calls.Load())
	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Equal(t, 1, repo.claims)
	require.Equal(t, "new-access", repo.credentials["access_token"])
	require.Equal(t, "new-refresh", repo.credentials["refresh_token"])
	require.Equal(t, account.Credentials["model_mapping"], repo.credentials["model_mapping"])
	require.Equal(t, "succeeded", repo.state["status"])
}

func TestOpenAIAutoReauthRejectsInvalidIdentityAndFailures(t *testing.T) {
	for _, test := range []struct {
		name, email, workspace string
		err                    error
	}{
		{"password error", "owner@example.com", "workspace", errors.New("secret should never be logged")},
		{"different user", "other@example.com", "workspace", nil},
		{"different workspace", "owner@example.com", "other", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			account := reauthTestAccount()
			repo := &reauthRepoStub{account: account, claimAllowed: true, completeAllowed: true, completed: make(chan bool, 1)}
			svc := NewOpenAIAutoReauthService(repo, nil, reauthTestCipher{}, nil)
			svc.login = func(context.Context, openai.LoginCredentials, string, string) (*openai.TokenResponse, error) {
				return reauthTestToken(test.email, test.workspace), test.err
			}
			require.True(t, svc.Handle401(context.Background(), account, nil))
			require.False(t, awaitReauthCompletion(t, repo))
			repo.mu.Lock()
			defer repo.mu.Unlock()
			require.Nil(t, repo.credentials)
			require.Equal(t, "failed", repo.state["status"])
			require.NotContains(t, repo.state["error"], "secret should never")
		})
	}
}

func TestOpenAIAutoReauthSkipsCooldownStaleTokenAndDisabled(t *testing.T) {
	for _, name := range []string{"cooldown", "stale token", "disabled", "non oauth"} {
		t.Run(name, func(t *testing.T) {
			account := reauthTestAccount()
			repo := &reauthRepoStub{account: account}
			svc := NewOpenAIAutoReauthService(repo, nil, reauthTestCipher{}, nil)
			svc.login = func(context.Context, openai.LoginCredentials, string, string) (*openai.TokenResponse, error) {
				t.Error("unexpected login")
				return nil, errors.New("unexpected")
			}
			request := *account
			request.Credentials = shallowCopyMap(account.Credentials)
			expected := true
			switch name {
			case "stale token":
				request.Credentials["access_token"] = "stale"
			case "disabled":
				account.Credentials[OpenAIAutoReauthEnabledKey] = false
				expected = false
			case "non oauth":
				request.Type = AccountTypeAPIKey
				expected = false
			}
			require.Equal(t, expected, svc.Handle401(context.Background(), &request, nil))
			if name != "cooldown" {
				require.Zero(t, repo.claims)
			}
		})
	}
}

func TestAdminUpdateOpenAIAutoReauthCredentialLifecycle(t *testing.T) {
	repo := &updateAccountCredsRepoStub{account: &Account{ID: 44, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Credentials: map[string]any{"access_token": "existing", "email": "owner@example.com"}}}
	svc := &adminServiceImpl{accountRepo: repo, accountEncryptor: reauthTestCipher{}}
	updated, err := svc.UpdateAccount(context.Background(), 44, &UpdateAccountInput{Credentials: map[string]any{OpenAILoginCredentialsKey: reauthTestLine, OpenAIAutoReauthEnabledKey: true}})
	require.NoError(t, err)
	require.Equal(t, "existing", updated.Credentials["access_token"])
	require.NotContains(t, updated.Credentials, OpenAILoginCredentialsKey)
	ciphertext := updated.Credentials[OpenAILoginCredentialsEncryptedKey]
	updated, err = svc.UpdateAccount(context.Background(), 44, &UpdateAccountInput{Credentials: map[string]any{"access_token": "manual-new-token"}})
	require.NoError(t, err)
	require.Equal(t, true, updated.Credentials[OpenAIAutoReauthEnabledKey])
	require.Equal(t, ciphertext, updated.Credentials[OpenAILoginCredentialsEncryptedKey])
	updated, err = svc.UpdateAccount(context.Background(), 44, &UpdateAccountInput{Credentials: map[string]any{OpenAIAutoReauthEnabledKey: false}})
	require.NoError(t, err)
	require.Equal(t, false, updated.Credentials[OpenAIAutoReauthEnabledKey])
	require.Equal(t, ciphertext, updated.Credentials[OpenAILoginCredentialsEncryptedKey])
	updated, err = svc.UpdateAccount(context.Background(), 44, &UpdateAccountInput{Credentials: map[string]any{OpenAILoginCredentialsKey: "", OpenAIAutoReauthEnabledKey: false}})
	require.NoError(t, err)
	require.Empty(t, updated.Credentials[OpenAILoginCredentialsEncryptedKey])
	require.Equal(t, "manual-new-token", updated.Credentials["access_token"])
}
