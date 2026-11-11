package middleware

import (
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"

	"github.com/gin-gonic/gin"
)

// ClientIPContext 全局中间件：为请求快照客户端 IP 解析模式，并归一化
// User-Agent，作为审计日志、限流等安全敏感记录的统一起点。
// IP 取值与 API Key IP 限制共用转发 IP 开关：开启时旧版原始转发头逻辑
// 接管解析，关闭时使用 Gin 的 server.trusted_proxies 可信代理链。
func ClientIPContext(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		forwardedIPSettings := cfg.ForwardedClientIPSettings()
		ip.SetForwardedIPSettings(c, forwardedIPSettings.TrustForwardedIP, forwardedIPSettings.Headers)
		userAgent := normalizePersistentText(c.Request.UserAgent(), maxPersistentUserAgentBytes)
		c.Request.Header.Set("User-Agent", userAgent)
		c.Next()
	}
}

// SecurityClientIP 返回当前请求用于安全敏感记录（审计日志等）的客户端 IP。
// 与 API Key IP 限制、面板限流共用同一套客户端 IP 来源。
func SecurityClientIP(c *gin.Context) string {
	return ip.GetSecurityClientIP(c, false)
}
