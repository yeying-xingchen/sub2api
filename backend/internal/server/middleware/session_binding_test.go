//go:build unit

package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSessionBindingContextFollowsForwardedIPSwitch(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, tc := range []struct {
		name           string
		trustForwarded bool
		trustedProxies []string
		wantIP         string
	}{
		{name: "enabled switch takes over raw headers", trustForwarded: true, wantIP: "1.2.3.4"},
		{name: "disabled switch ignores untrusted headers", trustForwarded: false, wantIP: "127.0.0.1"},
		{name: "disabled switch uses configured Gin proxy", trustForwarded: false, trustedProxies: []string{"127.0.0.1"}, wantIP: "1.2.3.4"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.SetTrustForwardedIPForAPIKeyACL(tc.trustForwarded)

			r := gin.New()
			require.NoError(t, r.SetTrustedProxies(tc.trustedProxies))
			r.Use(SessionBindingContext(cfg))
			r.GET("/t", func(c *gin.Context) {
				binding := service.SessionBindingFromContext(c.Request.Context())
				require.NotNil(t, binding)
				require.Equal(t, tc.wantIP, binding.IP)
				require.Equal(t, "test-agent", binding.UserAgent)
				require.Equal(t, tc.wantIP, SecurityClientIP(c))
				c.Status(200)
			})

			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/t", nil)
			req.RemoteAddr = "127.0.0.1:54321"
			req.Header.Set("X-Real-IP", "1.2.3.4")
			req.Header.Set("User-Agent", "test-agent")
			r.ServeHTTP(w, req)

			require.Equal(t, 200, w.Code)
		})
	}
}

func TestSessionBindingContextSnapshotsForwardedModeAndHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{}
	cfg.SetForwardedClientIPSettings(true, []string{"X-Initial-IP"})

	r := gin.New()
	require.NoError(t, r.SetTrustedProxies(nil))
	r.Use(SessionBindingContext(cfg))
	r.GET("/t", func(c *gin.Context) {
		binding := service.SessionBindingFromContext(c.Request.Context())
		require.NotNil(t, binding)
		require.Equal(t, "1.2.3.4", binding.IP)

		cfg.SetForwardedClientIPSettings(false, []string{"X-Changed-IP"})
		require.Equal(t, "1.2.3.4", ip.GetSecurityClientIP(c, false))
		c.Status(200)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/t", nil)
	req.RemoteAddr = "9.9.9.9:12345"
	req.Header.Set("X-Initial-IP", "1.2.3.4")
	req.Header.Set("X-Changed-IP", "4.4.4.4")
	req.Header.Set("X-Real-IP", "8.8.8.8")
	r.ServeHTTP(w, req)

	require.Equal(t, 200, w.Code)
	runtimeSettings := cfg.ForwardedClientIPSettings()
	require.False(t, runtimeSettings.TrustForwardedIP)
	require.Equal(t, []string{"X-Changed-IP"}, runtimeSettings.Headers)
}

func TestSessionBindingContextBoundsPersistedUserAgent(t *testing.T) {
	cfg := &config.Config{}
	r := gin.New()
	r.Use(SessionBindingContext(cfg))
	r.GET("/t", func(c *gin.Context) {
		binding := service.SessionBindingFromContext(c.Request.Context())
		require.Len(t, binding.UserAgent, maxPersistentUserAgentBytes)
		require.Equal(t, binding.UserAgent, c.Request.UserAgent())
		c.Status(200)
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/t", nil)
	req.Header.Set("User-Agent", strings.Repeat("u", 2048))
	r.ServeHTTP(w, req)
	require.Equal(t, 200, w.Code)
}

// 未经过 SessionBindingContext 注入时（异常挂载顺序/单测直调），回退 trusted_proxies 链，
// 等价于开关关闭时的历史行为。
func TestSecurityClientIPFallsBackWithoutInjectedBinding(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	require.NoError(t, r.SetTrustedProxies(nil))
	r.GET("/t", func(c *gin.Context) {
		c.String(200, SecurityClientIP(c))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/t", nil)
	req.RemoteAddr = "9.9.9.9:12345"
	req.Header.Set("X-Real-IP", "1.2.3.4")
	r.ServeHTTP(w, req)

	require.Equal(t, 200, w.Code)
	require.Equal(t, "9.9.9.9", w.Body.String())
}

func TestRequestSessionBindingPrefersInjectedBinding(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{}
	cfg.SetTrustForwardedIPForAPIKeyACL(true)

	r := gin.New()
	require.NoError(t, r.SetTrustedProxies([]string{"127.0.0.1"}))
	r.Use(SessionBindingContext(cfg))
	r.GET("/t", func(c *gin.Context) {
		issued := &service.SessionBinding{IP: "1.2.3.4", UserAgent: "test-agent"}
		require.Equal(t, issued.Hash(), requestSessionBinding(c).Hash())
		c.Status(200)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/t", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	req.Header.Set("X-Real-IP", "1.2.3.4")
	req.Header.Set("User-Agent", "test-agent")
	r.ServeHTTP(w, req)

	require.Equal(t, 200, w.Code)
}

func TestEnforceSessionBindingCompatibilityAndUserAgent(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Construct both persisted formats independently of Hash so legacy migration
	// cannot accidentally weaken the User-Agent checks for newly issued tokens.
	uaSum := sha256.Sum256([]byte("test-agent"))
	newHash := "ua-v1:" + hex.EncodeToString(uaSum[:16])
	legacySum := sha256.Sum256([]byte("192.0.2.1\ntest-agent"))
	legacyHash := hex.EncodeToString(legacySum[:16])

	for _, tc := range []struct {
		name       string
		storedHash string
		requestIP  string
		userAgent  string
		disabled   bool
		wantAllow  bool
	}{
		{
			name:       "new binding allows same user agent",
			storedHash: newHash,
			requestIP:  "198.51.100.2",
			userAgent:  "test-agent",
			wantAllow:  true,
		},
		{
			name:       "legacy binding allows IPv4 change",
			storedHash: legacyHash,
			requestIP:  "198.51.100.2",
			userAgent:  "test-agent",
			wantAllow:  true,
		},
		{
			name:       "legacy binding allows change to IPv6",
			storedHash: legacyHash,
			requestIP:  "2001:db8::2",
			userAgent:  "test-agent",
			wantAllow:  true,
		},
		{
			name:       "new binding rejects user agent change with same IP",
			storedHash: newHash,
			requestIP:  "192.0.2.1",
			userAgent:  "changed-agent",
		},
		{
			name:       "new binding rejects user agent and IP change",
			storedHash: newHash,
			requestIP:  "198.51.100.2",
			userAgent:  "changed-agent",
		},
		{
			name:       "new binding rejects missing user agent",
			storedHash: newHash,
			requestIP:  "198.51.100.2",
		},
		{
			name:       "new binding rejects whitespace user agent",
			storedHash: newHash,
			requestIP:  "198.51.100.2",
			userAgent:  " \t ",
		},
		{
			name:       "disabled binding allows user agent and IP change",
			storedHash: newHash,
			requestIP:  "198.51.100.2",
			userAgent:  "changed-agent",
			disabled:   true,
			wantAllow:  true,
		},
		{
			name:      "missing stored hash allows user agent and IP change",
			requestIP: "198.51.100.2",
			userAgent: "changed-agent",
			wantAllow: true,
		},
		{
			name:       "non hex hash cannot use legacy migration",
			storedHash: strings.Repeat("z", 32),
			requestIP:  "198.51.100.2",
			userAgent:  "test-agent",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{}
			enabled := "true"
			if tc.disabled {
				enabled = "false"
			}
			settingService := service.NewSettingService(fakeSettingRepo{
				values: map[string]string{service.SettingKeySessionBindingEnabled: enabled},
			}, cfg)
			claims := &service.JWTClaims{BindingHash: tc.storedHash}

			r := gin.New()
			require.NoError(t, r.SetTrustedProxies(nil))
			r.Use(SessionBindingContext(cfg))
			r.Use(func(c *gin.Context) {
				require.Equal(t, tc.requestIP, requestSessionBinding(c).IP)
				allowed := enforceSessionBinding(c, nil, settingService, nil, claims)
				require.Equal(t, tc.wantAllow, allowed)
				require.Equal(t, !tc.wantAllow, c.IsAborted())
				if !allowed {
					return
				}
				c.Next()
			})
			var reachedHandler bool
			r.GET("/t", func(c *gin.Context) {
				reachedHandler = true
				c.Status(http.StatusOK)
			})

			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/t", nil)
			req.RemoteAddr = net.JoinHostPort(tc.requestIP, "54321")
			if tc.userAgent != "" {
				req.Header.Set("User-Agent", tc.userAgent)
			}
			r.ServeHTTP(w, req)

			require.Equal(t, tc.wantAllow, reachedHandler)
			if tc.wantAllow {
				require.Equal(t, http.StatusOK, w.Code)
				return
			}
			require.Equal(t, http.StatusUnauthorized, w.Code)
			var response ErrorResponse
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
			require.Equal(t, "SESSION_BINDING_MISMATCH", response.Code)
		})
	}
}

func TestEnforceSessionBindingAllowsIPChanges(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, tc := range []struct {
		name             string
		issuedRemoteIP   string
		requestRemoteIP  string
		issuedForwarded  bool
		requestForwarded bool
	}{
		{
			name:            "IPv4 changes",
			issuedRemoteIP:  "192.0.2.1",
			requestRemoteIP: "198.51.100.2",
		},
		{
			name:            "IPv6 changes",
			issuedRemoteIP:  "2001:db8::1",
			requestRemoteIP: "2001:db8::2",
		},
		{
			name:            "forwarded IP switch disabled after issuance",
			issuedRemoteIP:  "198.51.100.2",
			requestRemoteIP: "198.51.100.2",
			issuedForwarded: true,
		},
		{
			name:             "forwarded IP switch enabled after issuance",
			issuedRemoteIP:   "198.51.100.2",
			requestRemoteIP:  "198.51.100.2",
			requestForwarded: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.SetTrustForwardedIPForAPIKeyACL(tc.issuedForwarded)
			settingService := service.NewSettingService(fakeSettingRepo{
				values: map[string]string{service.SettingKeySessionBindingEnabled: "true"},
			}, cfg)
			claims := &service.JWTClaims{}
			var issuedIP string

			r := gin.New()
			require.NoError(t, r.SetTrustedProxies(nil))
			r.Use(SessionBindingContext(cfg))
			r.GET("/issue", func(c *gin.Context) {
				binding := requestSessionBinding(c)
				issuedIP = binding.IP
				claims.BindingHash = binding.Hash()
				c.Status(http.StatusOK)
			})
			r.GET("/t", func(c *gin.Context) {
				require.NotEqual(t, issuedIP, requestSessionBinding(c).IP)
				require.True(t, enforceSessionBinding(c, nil, settingService, nil, claims))
				require.False(t, c.IsAborted())
				c.Status(http.StatusOK)
			})

			issueResponse := httptest.NewRecorder()
			issueRequest := httptest.NewRequest(http.MethodGet, "/issue", nil)
			issueRequest.RemoteAddr = net.JoinHostPort(tc.issuedRemoteIP, "54321")
			issueRequest.Header.Set("X-Real-IP", "203.0.113.3")
			issueRequest.Header.Set("User-Agent", "test-agent")
			r.ServeHTTP(issueResponse, issueRequest)
			require.Equal(t, http.StatusOK, issueResponse.Code)
			require.True(t, strings.HasPrefix(claims.BindingHash, "ua-v1:"))

			cfg.SetTrustForwardedIPForAPIKeyACL(tc.requestForwarded)
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/t", nil)
			req.RemoteAddr = net.JoinHostPort(tc.requestRemoteIP, "54321")
			req.Header.Set("X-Real-IP", "203.0.113.3")
			req.Header.Set("User-Agent", "test-agent")
			r.ServeHTTP(w, req)

			require.Equal(t, http.StatusOK, w.Code)
		})
	}
}
