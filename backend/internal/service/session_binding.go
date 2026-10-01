package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// ErrSessionBindingMismatch 会话绑定的 User-Agent 发生变化，会话已失效。
var ErrSessionBindingMismatch = infraerrors.Unauthorized("SESSION_BINDING_MISMATCH", "session user agent changed, please login again")

// SessionBinding 保存请求的客户端 IP 与 User-Agent。
// 会话绑定只校验 User-Agent；IP 仅用于审计等请求上下文，不影响会话有效性。
type SessionBinding struct {
	IP        string
	UserAgent string
}

const sessionBindingHashPrefix = "ua-v1:"

// Hash 计算仅包含 User-Agent 的绑定指纹，IP 变化不影响哈希。
// 前缀用于区分旧版 IP+UA 指纹，避免升级后使已有会话失效。
func (b *SessionBinding) Hash() string {
	if b == nil {
		return ""
	}
	ua := strings.TrimSpace(b.UserAgent)
	if ua == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(ua))
	return sessionBindingHashPrefix + hex.EncodeToString(sum[:16])
}

// MatchesHash 校验会话绑定，兼容未绑定及旧版 IP+UA 指纹。
// 旧版 32 位十六进制指纹无法独立校验 UA，按未绑定会话放行，
// 下一次刷新时自动换发 UA 指纹；token 的签名、有效期等校验仍由调用方执行。
func (b *SessionBinding) MatchesHash(storedHash string) bool {
	if storedHash == "" {
		return true
	}
	if len(storedHash) == 32 {
		if _, err := hex.DecodeString(storedHash); err == nil {
			return true
		}
	}
	if b == nil {
		return true // 非 HTTP 调用未注入请求上下文，保持现有兼容行为。
	}
	return b.Hash() == storedHash
}

type sessionBindingCtxKey struct{}

// WithSessionBinding 将会话指纹注入 context（由 HTTP 入口中间件调用）。
func WithSessionBinding(ctx context.Context, binding *SessionBinding) context.Context {
	if binding == nil {
		return ctx
	}
	return context.WithValue(ctx, sessionBindingCtxKey{}, binding)
}

// SessionBindingFromContext 从 context 提取会话指纹；不存在时返回 nil。
func SessionBindingFromContext(ctx context.Context) *SessionBinding {
	if ctx == nil {
		return nil
	}
	binding, _ := ctx.Value(sessionBindingCtxKey{}).(*SessionBinding)
	return binding
}

// sessionBindingHashFromContext 提取指纹哈希，缺失时返回空串。
func sessionBindingHashFromContext(ctx context.Context) string {
	return SessionBindingFromContext(ctx).Hash()
}
