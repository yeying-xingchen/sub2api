package service

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	globalTurnStateRefreshInterval = 10 * time.Minute
	globalTurnStateRequestTimeout  = 45 * time.Second
	globalTurnStateMaxBodyBytes    = 16 << 10
	globalTurnStateConfigCacheTTL  = 2 * time.Second
	globalTurnStateRetryBackoff    = 1 * time.Minute
)

// startGlobalTurnStateRefresh runs the opt-in source-account probe. The probe is
// intentionally attached to the gateway lifecycle so all HTTP and WS paths share
// the same value without adding another wire dependency.
func (s *OpenAIGatewayService) startGlobalTurnStateRefresh() {
	if s == nil || s.settingService == nil || s.settingService.settingRepo == nil || s.accountRepo == nil || s.httpUpstream == nil {
		return
	}
	s.globalTurnStateStop = make(chan struct{})
	s.globalTurnStateWG.Add(1)
	go func() {
		defer s.globalTurnStateWG.Done()
		s.refreshGlobalTurnState(context.Background())
		ticker := time.NewTicker(globalTurnStateRefreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.refreshGlobalTurnState(context.Background())
			case <-s.globalTurnStateStop:
				return
			}
		}
	}()
}

func (s *OpenAIGatewayService) globalTurnStateConfig(ctx context.Context) (bool, int64, string, time.Time, int64) {
	if s == nil || s.settingService == nil || s.settingService.settingRepo == nil {
		return false, 0, "", time.Time{}, 0
	}
	s.globalTurnStateConfigMu.RLock()
	if !s.globalTurnStateConfigAt.IsZero() && time.Since(s.globalTurnStateConfigAt) < globalTurnStateConfigCacheTTL {
		enabled, accountID, state, updatedAt, sourceID := s.globalTurnStateEnabled, s.globalTurnStateAccountID, s.globalTurnStateValue, s.globalTurnStateUpdatedAt, s.globalTurnStateSourceID
		s.globalTurnStateConfigMu.RUnlock()
		return enabled, accountID, state, updatedAt, sourceID
	}
	s.globalTurnStateConfigMu.RUnlock()

	values, err := s.settingService.settingRepo.GetMultiple(ctx, []string{
		SettingKeyOpenAIGlobalTurnStateEnabled,
		SettingKeyOpenAIGlobalTurnStateAccountID,
		SettingKeyOpenAIGlobalTurnState,
		SettingKeyOpenAIGlobalTurnStateUpdatedAt,
		SettingKeyOpenAIGlobalTurnStateSourceAccountID,
	})
	if err != nil {
		return false, 0, "", time.Time{}, 0
	}
	enabled := strings.EqualFold(strings.TrimSpace(values[SettingKeyOpenAIGlobalTurnStateEnabled]), "true")
	accountID, _ := strconv.ParseInt(strings.TrimSpace(values[SettingKeyOpenAIGlobalTurnStateAccountID]), 10, 64)
	state := strings.TrimSpace(values[SettingKeyOpenAIGlobalTurnState])
	updatedAt, _ := time.Parse(time.RFC3339Nano, strings.TrimSpace(values[SettingKeyOpenAIGlobalTurnStateUpdatedAt]))
	sourceID, _ := strconv.ParseInt(strings.TrimSpace(values[SettingKeyOpenAIGlobalTurnStateSourceAccountID]), 10, 64)
	if sourceID <= 0 && state != "" {
		// Older installations did not persist provenance; treat the current
		// configured source as the provenance until the next refresh.
		sourceID = accountID
	}
	s.globalTurnStateConfigMu.Lock()
	if !s.globalTurnStateConfigAt.IsZero() && (s.globalTurnStateEnabled != enabled || s.globalTurnStateAccountID != accountID || s.globalTurnStateSourceID != sourceID) {
		s.globalTurnStateAttemptAt = time.Time{}
	}
	s.globalTurnStateConfigAt = time.Now()
	s.globalTurnStateEnabled = enabled
	s.globalTurnStateAccountID = accountID
	s.globalTurnStateValue = state
	s.globalTurnStateUpdatedAt = updatedAt
	s.globalTurnStateSourceID = sourceID
	s.globalTurnStateConfigMu.Unlock()
	return enabled, accountID, state, updatedAt, sourceID
}

// globalCodexTurnState returns the latest configured value, synchronously
// refreshing an empty or stale value so enabling the feature takes effect on the
// next request even before the periodic worker's first tick.
func (s *OpenAIGatewayService) globalCodexTurnState(ctx context.Context) string {
	enabled, accountID, state, updatedAt, sourceID := s.globalTurnStateConfig(ctx)
	if !enabled || accountID <= 0 || (state != "" && sourceID != accountID) {
		return ""
	}
	if state != "" && sourceID == accountID && !updatedAt.IsZero() && time.Since(updatedAt) < globalTurnStateRefreshInterval {
		return state
	}
	s.refreshGlobalTurnState(ctx)
	_, _, state, _, sourceID = s.globalTurnStateConfig(ctx)
	if sourceID != accountID {
		return ""
	}
	return state
}

func (s *OpenAIGatewayService) applyGlobalCodexTurnState(ctx context.Context, account *Account, headers http.Header) {
	if headers == nil || account == nil || !account.UsesOpenAICodexProtocol() {
		return
	}
	if state := s.globalCodexTurnState(ctx); state != "" {
		headers.Set(openAICodexTurnStateHeader, state)
	}
}

func (s *OpenAIGatewayService) refreshGlobalTurnState(parent context.Context) {
	if s == nil || s.accountRepo == nil || s.httpUpstream == nil || s.settingService == nil || s.settingService.settingRepo == nil {
		return
	}
	s.globalTurnStateRefreshMu.Lock()
	defer s.globalTurnStateRefreshMu.Unlock()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), globalTurnStateRequestTimeout)
	defer cancel()
	enabled, accountID, state, updatedAt, sourceID := s.globalTurnStateConfig(ctx)
	if !enabled || accountID <= 0 {
		return
	}
	if state != "" && sourceID == accountID && !updatedAt.IsZero() && time.Since(updatedAt) < globalTurnStateRefreshInterval {
		return
	}
	s.globalTurnStateConfigMu.RLock()
	lastAttemptAt := s.globalTurnStateAttemptAt
	s.globalTurnStateConfigMu.RUnlock()
	if !lastAttemptAt.IsZero() && time.Since(lastAttemptAt) < globalTurnStateRetryBackoff {
		return
	}
	s.globalTurnStateConfigMu.Lock()
	s.globalTurnStateAttemptAt = time.Now()
	s.globalTurnStateConfigMu.Unlock()
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		slog.Warn("global_codex_turn_state_source_account_load_failed", "account_id", accountID, "error", err)
		return
	}
	if account == nil || !account.IsActive() || !account.UsesOpenAICodexProtocol() {
		slog.Warn("global_codex_turn_state_source_account_invalid", "account_id", accountID)
		return
	}
	token, _, err := s.GetAccessToken(ctx, account)
	if err != nil {
		slog.Warn("global_codex_turn_state_source_token_failed", "account_id", accountID, "error", err)
		return
	}
	body, _ := json.Marshal(map[string]any{
		"model":             "gpt-5-codex",
		"instructions":      "Respond with a short acknowledgement.",
		"input":             []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_text", "text": "ping"}}}},
		"max_output_tokens": 1,
		"stream":            false,
		"store":             false,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, chatgptCodexURL, strings.NewReader(string(body)))
	if err != nil {
		return
	}
	req = req.WithContext(WithHTTPUpstreamProfile(req.Context(), HTTPUpstreamProfileOpenAI))
	authHeaders, err := s.buildOpenAIAuthenticationHeaders(ctx, account, token)
	if err != nil {
		slog.Warn("global_codex_turn_state_auth_headers_failed", "account_id", accountID, "error", err)
		return
	}
	for key, values := range authHeaders {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	req.Host = "chatgpt.com"
	if err := resolveAndSetOpenAIChatGPTAccountHeaders(ctx, s.accountRepo, req.Header, account); err != nil {
		slog.Warn("global_codex_turn_state_account_headers_failed", "account_id", accountID, "error", err)
		return
	}
	sessionID := uuid.New().String()
	req.Header.Set("session_id", sessionID)
	req.Header.Set("conversation_id", sessionID)
	ensureCodexIdentityHeaders(req.Header)
	enforceCodexIdentityHeaders(req.Header)
	req.Header.Set("accept", "application/json")
	req.Header.Set("content-type", "application/json")
	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	resp, err := s.httpUpstream.Do(req, proxyURL, account.ID, account.Concurrency)
	if err != nil {
		slog.Warn("global_codex_turn_state_probe_failed", "account_id", accountID, "error", err)
		return
	}
	if resp == nil {
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, globalTurnStateMaxBodyBytes))
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		slog.Warn("global_codex_turn_state_probe_http_error", "account_id", accountID, "status", resp.StatusCode)
		return
	}
	turnState := strings.TrimSpace(resp.Header.Get(openAICodexTurnStateHeader))
	if turnState == "" {
		slog.Warn("global_codex_turn_state_probe_empty", "account_id", accountID, "status", resp.StatusCode)
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := s.settingService.settingRepo.SetMultiple(ctx, map[string]string{
		SettingKeyOpenAIGlobalTurnState:                turnState,
		SettingKeyOpenAIGlobalTurnStateUpdatedAt:       now,
		SettingKeyOpenAIGlobalTurnStateSourceAccountID: strconv.FormatInt(accountID, 10),
	}); err != nil {
		slog.Warn("global_codex_turn_state_persist_failed", "account_id", accountID, "error", err)
		return
	}
	s.globalTurnStateConfigMu.Lock()
	s.globalTurnStateConfigAt = time.Time{}
	s.globalTurnStateConfigMu.Unlock()
	slog.Info("global_codex_turn_state_refreshed", "account_id", accountID, "status", resp.StatusCode)
}

func (s *OpenAIGatewayService) maybePersistGlobalTurnState(account *Account, upstream http.Header) {
	if s == nil || account == nil || upstream == nil || s.settingService == nil || s.settingService.settingRepo == nil {
		return
	}
	enabled, sourceID, _, _, _ := s.globalTurnStateConfig(context.Background())
	if !enabled || sourceID <= 0 || account.ID != sourceID {
		return
	}
	state := strings.TrimSpace(upstream.Get(openAICodexTurnStateHeader))
	if state == "" {
		return
	}
	if err := s.settingService.settingRepo.SetMultiple(context.Background(), map[string]string{
		SettingKeyOpenAIGlobalTurnState:                state,
		SettingKeyOpenAIGlobalTurnStateUpdatedAt:       time.Now().UTC().Format(time.RFC3339Nano),
		SettingKeyOpenAIGlobalTurnStateSourceAccountID: strconv.FormatInt(sourceID, 10),
	}); err != nil {
		return
	}
	s.globalTurnStateConfigMu.Lock()
	s.globalTurnStateConfigAt = time.Time{}
	s.globalTurnStateConfigMu.Unlock()
}
