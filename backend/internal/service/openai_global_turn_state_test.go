package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type globalTurnStateSettingRepoStub struct {
	SettingRepository
	values map[string]string
}

func (r *globalTurnStateSettingRepoStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		out[key] = r.values[key]
	}
	return out, nil
}

func (r *globalTurnStateSettingRepoStub) SetMultiple(_ context.Context, values map[string]string) error {
	for key, value := range values {
		r.values[key] = value
	}
	return nil
}

func TestApplyGlobalCodexTurnStateScopesToOAuthLikeAccounts(t *testing.T) {
	repo := &globalTurnStateSettingRepoStub{values: map[string]string{
		SettingKeyOpenAIGlobalTurnStateEnabled:        "true",
		SettingKeyOpenAIGlobalTurnStateAccountID:      "42",
		SettingKeyOpenAIGlobalTurnState:               "global-turn-state",
		SettingKeyOpenAIGlobalTurnStateUpdatedAt:      time.Now().UTC().Format(time.RFC3339Nano),
	}}
	svc := &OpenAIGatewayService{settingService: &SettingService{settingRepo: repo}}

	oauthHeaders := make(http.Header)
	svc.applyGlobalCodexTurnState(context.Background(), &Account{Type: AccountTypeOAuth}, oauthHeaders)
	require.Equal(t, "global-turn-state", oauthHeaders.Get(openAICodexTurnStateHeader))

	apiKeyHeaders := make(http.Header)
	svc.applyGlobalCodexTurnState(context.Background(), &Account{Type: AccountTypeAPIKey}, apiKeyHeaders)
	require.Empty(t, apiKeyHeaders.Get(openAICodexTurnStateHeader))
}

func TestApplyGlobalCodexTurnStateDisabledLeavesClientValue(t *testing.T) {
	repo := &globalTurnStateSettingRepoStub{values: map[string]string{
		SettingKeyOpenAIGlobalTurnStateEnabled:   "false",
		SettingKeyOpenAIGlobalTurnStateAccountID: "42",
		SettingKeyOpenAIGlobalTurnState:          "global-turn-state",
	}}
	svc := &OpenAIGatewayService{settingService: &SettingService{settingRepo: repo}}
	headers := http.Header{"X-Codex-Turn-State": []string{"client-state"}}
	svc.applyGlobalCodexTurnState(context.Background(), &Account{Type: AccountTypeOAuth}, headers)
	require.Equal(t, "client-state", headers.Get(openAICodexTurnStateHeader))
}
