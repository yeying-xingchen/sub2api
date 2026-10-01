package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

const openAIReauthCooldown = 10 * time.Minute
const openAIReauthTimeout = 2 * time.Minute
const OpenAIReauthPendingReason = "OpenAI automatic reauthorization in progress (401)"
const OpenAIReauthFailedReason = "OpenAI automatic reauthorization failed; check login credentials or reauthorize manually (401)"

// OpenAIReauthRepository claims and completes an attempt against the exact
// credential version. The DB claim deduplicates attempts across server instances.
type OpenAIReauthRepository interface {
	ClaimOpenAIReauth(ctx context.Context, account *Account, state map[string]any, until time.Time) (bool, error)
	CompleteOpenAIReauth(ctx context.Context, account *Account, credentials, state map[string]any, succeeded bool) (bool, error)
}

type OpenAIAutoReauthService struct {
	accountRepo AccountRepository
	proxyRepo   ProxyRepository
	encryptor   SecretEncryptor
	refreshAPI  *OAuthRefreshAPI
	invalidator TokenCacheInvalidator
	login       func(context.Context, openai.LoginCredentials, string, string) (*openai.TokenResponse, error)
	workers     chan struct{}
	mu          sync.Mutex
	pending     map[int64]bool
}

func NewOpenAIAutoReauthService(repo AccountRepository, proxies ProxyRepository, encryptor SecretEncryptor, invalidator TokenCacheInvalidator) *OpenAIAutoReauthService {
	return &OpenAIAutoReauthService{accountRepo: repo, proxyRepo: proxies, encryptor: encryptor, invalidator: invalidator,
		login: openai.LoginWithCredentialsForAccount, workers: make(chan struct{}, 2), pending: make(map[int64]bool)}
}

// Handle401 schedules a detached, bounded attempt. true means this service owns
// the 401 recovery, so legacy permanent-disable logic must not overwrite it.
func (s *OpenAIAutoReauthService) Handle401(ctx context.Context, account *Account, recovered func(int64)) bool {
	if s == nil || account == nil || account.Platform != PlatformOpenAI || account.Type != AccountTypeOAuth || s.encryptor == nil {
		return false
	}
	repo, ok := s.accountRepo.(OpenAIReauthRepository)
	if !ok {
		return false
	}
	owner, err := resolveCredentialAccount(ctx, s.accountRepo, account)
	if err != nil || owner == nil {
		return false
	}
	fresh, err := s.accountRepo.GetByID(ctx, owner.ID)
	if err != nil || fresh == nil {
		return false
	}
	enabled, _ := fresh.Credentials[OpenAIAutoReauthEnabledKey].(bool)
	if !enabled || fresh.GetCredential(OpenAILoginCredentialsEncryptedKey) == "" || fresh.Platform != PlatformOpenAI || fresh.Type != AccountTypeOAuth || fresh.IsCredentialShadow() {
		return false
	}
	// A delayed 401 must never quarantine a newly authorized credential version.
	if owner.GetCredential("access_token") != "" && owner.GetCredential("access_token") != fresh.GetCredential("access_token") {
		return true
	}
	if fresh.Status != StatusActive || !fresh.Schedulable {
		return true
	}
	if fresh.AutoPauseOnExpired && fresh.ExpiresAt != nil && !fresh.ExpiresAt.After(time.Now()) {
		return true
	}

	s.mu.Lock()
	if s.pending[fresh.ID] {
		s.mu.Unlock()
		return true
	}
	s.pending[fresh.ID] = true
	s.mu.Unlock()
	release := func() { s.mu.Lock(); delete(s.pending, fresh.ID); s.mu.Unlock() }
	// No unbounded goroutine queue during a widespread credential outage.
	select {
	case s.workers <- struct{}{}:
	default:
		release()
		return true
	}
	now := time.Now().UTC()
	state := map[string]any{"status": "running", "last_attempt_at": now.Format(time.RFC3339), "retry_after": now.Add(openAIReauthCooldown).Format(time.RFC3339)}
	claimed, err := repo.ClaimOpenAIReauth(ctx, fresh, state, now.Add(openAIReauthCooldown))
	if err != nil || !claimed {
		<-s.workers
		release()
		if err != nil {
			slog.Warn("openai_auto_reauth_claim_failed", "account_id", fresh.ID)
		}
		return true
	}
	go func() {
		defer func() { <-s.workers; release() }()
		attemptCtx, cancel := context.WithTimeout(context.Background(), openAIReauthTimeout)
		defer cancel()
		unlock, lockErr := s.lockRefresh(attemptCtx, fresh)
		if unlock != nil {
			defer unlock()
		}
		var credentials map[string]any
		loginErr := lockErr
		if loginErr == nil {
			credentials, loginErr = s.reauthorize(attemptCtx, fresh)
		}
		state["status"] = "failed"
		state["error"] = "Automatic login failed; verify the password and TOTP secret, or complete authorization manually"
		if loginErr == nil {
			state["status"] = "succeeded"
			state["error"] = ""
			state["last_success_at"] = time.Now().UTC().Format(time.RFC3339)
		}
		// Persistence must still run when the protocol request timed out.
		persistCtx, persistCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer persistCancel()
		applied, persistErr := repo.CompleteOpenAIReauth(persistCtx, fresh, credentials, state, loginErr == nil)
		if persistErr != nil {
			slog.Warn("openai_auto_reauth_persist_failed", "account_id", fresh.ID)
			return
		}
		if !applied {
			return
		} // Admin edit, refresh, disable, or deletion won the race.
		if loginErr != nil {
			slog.Warn("openai_auto_reauth_failed", "account_id", fresh.ID)
			return
		}
		if s.invalidator != nil {
			if err := s.invalidator.InvalidateToken(persistCtx, fresh); err != nil {
				slog.Warn("openai_auto_reauth_invalidate_failed", "account_id", fresh.ID)
			}
		}
		if recovered != nil {
			recovered(fresh.ID)
		}
		slog.Info("openai_auto_reauth_succeeded", "account_id", fresh.ID)
	}()
	return true
}

func (s *OpenAIAutoReauthService) reauthorize(ctx context.Context, account *Account) (map[string]any, error) {
	plaintext, err := s.encryptor.Decrypt(account.GetCredential(OpenAILoginCredentialsEncryptedKey))
	if err != nil {
		return nil, errors.New("login credentials could not be decrypted")
	}
	login, err := openai.ParseLoginCredentials(plaintext)
	if err != nil {
		return nil, errors.New("login credentials are invalid")
	}
	if email := account.GetCredential("email"); email != "" && !strings.EqualFold(email, login.Email) {
		return nil, errors.New("login email mismatch")
	}
	proxyURL := ""
	if account.ProxyID != nil {
		if s.proxyRepo == nil {
			return nil, errors.New("proxy repository unavailable")
		}
		proxy, proxyErr := s.proxyRepo.GetByID(ctx, *account.ProxyID)
		if proxyErr != nil || proxy == nil {
			return nil, errors.New("configured proxy unavailable")
		}
		proxyURL = proxy.URL()
	}
	token, err := s.login(ctx, login, proxyURL, account.GetCredential("chatgpt_account_id"))
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if token == nil || token.AccessToken == "" || token.RefreshToken == "" || token.ExpiresIn <= 0 {
		return nil, errors.New("incomplete login token response")
	}
	info := &OpenAITokenInfo{AccessToken: token.AccessToken, RefreshToken: token.RefreshToken, IDToken: token.IDToken,
		ExpiresAt: time.Now().Unix() + int64(token.ExpiresIn), ClientID: openai.ClientID, Email: login.Email}
	claims, err := openai.ParseIDToken(token.IDToken)
	if err != nil {
		return nil, errors.New("login identity missing")
	}
	user := claims.GetUserInfo()
	if user == nil || !strings.EqualFold(user.Email, login.Email) {
		return nil, errors.New("login identity mismatch")
	}
	if id := account.GetCredential("chatgpt_account_id"); id != "" && id != user.ChatGPTAccountID {
		return nil, errors.New("login workspace mismatch")
	}
	if id := account.GetCredential("chatgpt_user_id"); id != "" && id != user.ChatGPTUserID {
		return nil, errors.New("login user mismatch")
	}
	info.ChatGPTAccountID, info.ChatGPTUserID = user.ChatGPTAccountID, user.ChatGPTUserID
	info.OrganizationID, info.PlanType = user.OrganizationID, user.PlanType
	credentials := MergeCredentials(account.Credentials, (&OpenAIOAuthService{}).BuildAccountCredentials(info))
	// Password login produces a standard OAuth session, replacing any old PAT or agent identity.
	for _, key := range []string{"auth_mode", "openai_auth_mode", "agent_private_key", "agent_runtime_id", "task_id"} {
		delete(credentials, key)
	}
	credentials["_token_version"] = time.Now().UnixMilli()
	return credentials, nil
}

// Use the same local and distributed locks as OAuthRefreshAPI. The longer lease
// covers the bounded password flow and its final credential commit.
func (s *OpenAIAutoReauthService) lockRefresh(ctx context.Context, account *Account) (func(), error) {
	if s.refreshAPI == nil {
		return nil, nil
	}
	key := OpenAITokenCacheKey(account)
	mu := s.refreshAPI.getLocalLock(key)
	if err := mu.Lock(ctx); err != nil {
		return nil, err
	}
	if s.refreshAPI.tokenCache == nil {
		return mu.Unlock, nil
	}
	acquired, err := s.refreshAPI.tokenCache.AcquireRefreshLock(ctx, key, openAIReauthTimeout+30*time.Second)
	if err != nil || !acquired {
		mu.Unlock()
		return nil, errors.New("another credential refresh is in progress")
	}
	return func() { s.refreshAPI.releaseRefreshLock(context.Background(), key); mu.Unlock() }, nil
}
