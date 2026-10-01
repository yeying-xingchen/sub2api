package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// step-up 开关转换的门控测试。
// 测试环境不注入认证上下文/userService，因此一旦触发校验会以 401/403/500 中止；
// 借此区分「触发了转换校验」与「直接放行到常规保存（200）」。

func newStepUpSwitchTestHandler(t *testing.T, stored map[string]string) (*SettingHandler, *settingHandlerRepoStub) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	repo := &settingHandlerRepoStub{values: stored}
	svc := service.NewSettingService(repo, &config.Config{Default: config.DefaultConfig{UserConcurrency: 5}})
	return NewSettingHandler(svc, nil, nil, nil, nil, nil, nil), repo
}

func doUpdateSettings(t *testing.T, h *SettingHandler, body map[string]any, prepare func(c *gin.Context)) *httptest.ResponseRecorder {
	t.Helper()
	rawBody, err := json.Marshal(body)
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings", bytes.NewReader(rawBody))
	c.Request.Header.Set("Content-Type", "application/json")
	if prepare != nil {
		prepare(c)
	}

	h.UpdateSettings(c)
	return rec
}

// 开启兼容性开关不需要认证或 TOTP，且设置仍可正常保存。
func TestUpdateSettingsEnableStepUpDoesNotRequire2FA(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})

	rec := doUpdateSettings(t, h, map[string]any{"step_up_enabled": true}, nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "true", repo.values[service.SettingKeyStepUpEnabled])
}

// 兼容性开关允许通过 admin API key 修改，不再触发 2FA 门控。
func TestUpdateSettingsEnableStepUpAllowsAdminAPIKey(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})

	rec := doUpdateSettings(t, h, map[string]any{"step_up_enabled": true}, func(c *gin.Context) {
		c.Set("auth_method", service.AuditAuthMethodAdminAPIKey)
	})

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "true", repo.values[service.SettingKeyStepUpEnabled])
}

// 开启兼容性开关不依赖用户服务或 TOTP 校验。
func TestUpdateSettingsEnableStepUpAllowsWithoutUserService(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})

	rec := doUpdateSettings(t, h, map[string]any{"step_up_enabled": true}, nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "true", repo.values[service.SettingKeyStepUpEnabled])
}

// 关闭兼容性开关不需要认证或 step-up 2FA。
func TestUpdateSettingsDisableStepUpDoesNotRequire2FA(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyStepUpEnabled: "true",
	})

	rec := doUpdateSettings(t, h, map[string]any{"step_up_enabled": false}, nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "false", repo.values[service.SettingKeyStepUpEnabled])
}

// 关闭兼容性开关允许通过 admin API key，不再触发 2FA 门控。
func TestUpdateSettingsDisableStepUpAllowsAdminAPIKey(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyStepUpEnabled: "true",
	})

	rec := doUpdateSettings(t, h, map[string]any{"step_up_enabled": false}, func(c *gin.Context) {
		c.Set("auth_method", service.AuditAuthMethodAdminAPIKey)
	})

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "false", repo.values[service.SettingKeyStepUpEnabled])
}

// 无状态转换（false→false）：不触发任何转换校验，常规保存成功且默认持久化为 false。
func TestUpdateSettingsStepUpNoTransitionSkipsGate(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})

	rec := doUpdateSettings(t, h, map[string]any{"step_up_enabled": false}, nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "false", repo.values[service.SettingKeyStepUpEnabled])
	// 会话 User-Agent 绑定默认关闭：未显式提交时持久化 false。
	require.Equal(t, "false", repo.values[service.SettingKeySessionBindingEnabled])
}

// 保持开启（true→true）：不触发转换校验，常规保存不被打断。
func TestUpdateSettingsStepUpKeepEnabledSkipsGate(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyStepUpEnabled: "true",
	})

	rec := doUpdateSettings(t, h, map[string]any{"step_up_enabled": true}, nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "true", repo.values[service.SettingKeyStepUpEnabled])
}

// 省略字段=保持现值：不含 step_up_enabled/session_binding_enabled 的旧客户端全量保存
// 不得把已开启的安全开关静默重置，也不触发任何转换门控。
func TestUpdateSettingsOmittedSecuritySwitchesKeepStoredValues(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyStepUpEnabled:         "true",
		service.SettingKeySessionBindingEnabled: "true",
	})

	rec := doUpdateSettings(t, h, map[string]any{"registration_enabled": true}, nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "true", repo.values[service.SettingKeyStepUpEnabled])
	require.Equal(t, "true", repo.values[service.SettingKeySessionBindingEnabled])
}

// 省略字段在开关本就关闭时同样保持关闭（默认值路径）。
func TestUpdateSettingsOmittedSecuritySwitchesKeepDisabled(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})

	rec := doUpdateSettings(t, h, map[string]any{"registration_enabled": true}, nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "false", repo.values[service.SettingKeyStepUpEnabled])
	require.Equal(t, "false", repo.values[service.SettingKeySessionBindingEnabled])
}

func TestUpdateSettingsForwardedClientIPHeadersOmittedPreservesAndEmptyClears(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyForwardedClientIPHeaders: `["X-Cdn-Ip","True-Client-Ip"]`,
	})

	preserved := doUpdateSettings(t, h, map[string]any{"registration_enabled": true}, nil)
	require.Equal(t, http.StatusOK, preserved.Code)
	require.JSONEq(t, `["X-Cdn-Ip","True-Client-Ip"]`, repo.values[service.SettingKeyForwardedClientIPHeaders])
	require.Contains(t, preserved.Body.String(), `"forwarded_client_ip_headers":["X-Cdn-Ip","True-Client-Ip"]`)

	cleared := doUpdateSettings(t, h, map[string]any{"forwarded_client_ip_headers": []string{}}, nil)
	require.Equal(t, http.StatusOK, cleared.Code)
	require.JSONEq(t, `[]`, repo.values[service.SettingKeyForwardedClientIPHeaders])
	require.Contains(t, cleared.Body.String(), `"forwarded_client_ip_headers":[]`)
}

func TestUpdateSettingsMalformedForwardedClientIPHeadersRemainFailClosedWhenOmitted(t *testing.T) {
	cfg := &config.Config{Default: config.DefaultConfig{UserConcurrency: 5}}
	repo := &settingHandlerRepoStub{values: map[string]string{
		service.SettingKeyAPIKeyACLTrustForwardedIP: "true",
		service.SettingKeyForwardedClientIPHeaders:  `{"not":"an array"}`,
	}}
	svc := service.NewSettingService(repo, cfg)
	require.ErrorContains(t, svc.LoadForwardedClientIPSettings(context.Background()), "load forwarded client ip headers")
	require.False(t, cfg.ForwardedClientIPSettings().TrustForwardedIP)
	h := NewSettingHandler(svc, nil, nil, nil, nil, nil, nil)

	rec := doUpdateSettings(t, h, map[string]any{"registration_enabled": true}, nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "false", repo.values[service.SettingKeyAPIKeyACLTrustForwardedIP])
	require.JSONEq(t, `[]`, repo.values[service.SettingKeyForwardedClientIPHeaders])
	runtimeSettings := cfg.ForwardedClientIPSettings()
	require.False(t, runtimeSettings.TrustForwardedIP)
	require.Empty(t, runtimeSettings.Headers)
	require.Contains(t, rec.Body.String(), `"api_key_acl_trust_forwarded_ip":false`)
	require.Contains(t, rec.Body.String(), `"forwarded_client_ip_headers":[]`)
}

func TestUpdateSettingsRejectsInvalidForwardedClientIPHeader(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyForwardedClientIPHeaders: `["X-Existing-IP"]`,
	})

	rec := doUpdateSettings(t, h, map[string]any{
		"forwarded_client_ip_headers": []string{"X Invalid"},
	}, nil)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.JSONEq(t, `["X-Existing-IP"]`, repo.values[service.SettingKeyForwardedClientIPHeaders])
}
