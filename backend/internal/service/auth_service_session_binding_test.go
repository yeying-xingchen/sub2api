//go:build unit

package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type sessionBindingRefreshCache struct {
	refreshTokenCacheStub
	tokens          map[string]*RefreshTokenData
	revokedFamilies []string
}

func (c *sessionBindingRefreshCache) StoreRefreshToken(_ context.Context, hash string, data *RefreshTokenData, _ time.Duration) error {
	cloned := *data
	c.tokens[hash] = &cloned
	return nil
}

func (c *sessionBindingRefreshCache) GetRefreshToken(_ context.Context, hash string) (*RefreshTokenData, error) {
	data, ok := c.tokens[hash]
	if !ok {
		return nil, ErrRefreshTokenNotFound
	}
	cloned := *data
	return &cloned, nil
}

func (c *sessionBindingRefreshCache) DeleteRefreshToken(_ context.Context, hash string) error {
	delete(c.tokens, hash)
	return nil
}

func (c *sessionBindingRefreshCache) DeleteTokenFamily(_ context.Context, familyID string) error {
	c.revokedFamilies = append(c.revokedFamilies, familyID)
	for hash, data := range c.tokens {
		if data.FamilyID == familyID {
			delete(c.tokens, hash)
		}
	}
	return nil
}

func TestAuthService_SessionBindingRefresh(t *testing.T) {
	issuedBinding := &SessionBinding{IP: "192.0.2.1", UserAgent: "Mozilla/5.0"}
	legacySum := sha256.Sum256([]byte(issuedBinding.IP + "\n" + issuedBinding.UserAgent))
	legacyHash := hex.EncodeToString(legacySum[:16])

	for _, mode := range []string{"access token refresh", "refresh token rotation"} {
		t.Run(mode, func(t *testing.T) {
			for _, tc := range []struct {
				name         string
				ip           string
				ua           string
				storedHash   string
				disabled     bool
				wantMismatch bool
			}{
				{name: "IPv4 changes", ip: "198.51.100.2", ua: issuedBinding.UserAgent, storedHash: issuedBinding.Hash()},
				{name: "IPv4 becomes IPv6", ip: "2001:db8::2", ua: issuedBinding.UserAgent, storedHash: issuedBinding.Hash()},
				{name: "legacy IP binding migrates", ip: "198.51.100.2", ua: issuedBinding.UserAgent, storedHash: legacyHash},
				{name: "unbound session migrates", ip: "198.51.100.2", ua: issuedBinding.UserAgent},
				{name: "UA changes revoke session", ip: "198.51.100.2", ua: "curl/8.0", storedHash: issuedBinding.Hash(), wantMismatch: true},
				{name: "missing UA revokes session", ip: "198.51.100.2", storedHash: issuedBinding.Hash(), wantMismatch: true},
				{name: "disabled binding accepts UA changes", ip: "198.51.100.2", ua: "curl/8.0", storedHash: issuedBinding.Hash(), disabled: true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					user := &User{ID: 1, Email: "session@example.com", PasswordHash: "original-password", Role: RoleUser, Status: StatusActive}
					enabled := "true"
					if tc.disabled {
						enabled = "false"
					}
					svc := newAuthService(&userRepoStub{user: user}, map[string]string{SettingKeySessionBindingEnabled: enabled}, nil, nil)
					svc.cfg.JWT.RefreshTokenExpireDays = 7
					cache := &sessionBindingRefreshCache{tokens: make(map[string]*RefreshTokenData)}
					svc.refreshTokenCache = cache
					issuedCtx := WithSessionBinding(context.Background(), issuedBinding)
					pair, err := svc.GenerateTokenPair(issuedCtx, user, "session-family")
					require.NoError(t, err)
					claims, err := svc.ValidateToken(pair.AccessToken)
					require.NoError(t, err)
					require.Equal(t, issuedBinding.Hash(), claims.BindingHash)
					data := cache.tokens[hashToken(pair.RefreshToken)]
					require.Equal(t, claims.BindingHash, data.BindingHash)

					// Recreate pre-upgrade fingerprints in signed access tokens and cached refresh tokens.
					data.BindingHash = tc.storedHash
					oldAccess, err := svc.generateAccessToken(user, "session-family", tc.storedHash)
					require.NoError(t, err)
					current := &SessionBinding{IP: tc.ip, UserAgent: tc.ua}
					ctx := WithSessionBinding(context.Background(), current)
					var access string
					if mode == "access token refresh" {
						access, err = svc.RefreshToken(ctx, oldAccess)
					} else {
						var refreshed *TokenPairWithUser
						refreshed, err = svc.RefreshTokenPair(ctx, pair.RefreshToken)
						if err == nil {
							access = refreshed.AccessToken
							require.NotEqual(t, pair.RefreshToken, refreshed.RefreshToken)
							rotated := cache.tokens[hashToken(refreshed.RefreshToken)]
							require.Equal(t, current.Hash(), rotated.BindingHash)
							require.Equal(t, "session-family", rotated.FamilyID)
							require.NotContains(t, cache.tokens, hashToken(pair.RefreshToken))
						}
					}
					if tc.wantMismatch {
						require.ErrorIs(t, err, ErrSessionBindingMismatch)
						require.Equal(t, []string{"session-family"}, cache.revokedFamilies)
						require.Empty(t, cache.tokens)
						return
					}
					require.NoError(t, err)
					require.Empty(t, cache.revokedFamilies)
					claims, err = svc.ValidateToken(access)
					require.NoError(t, err)
					require.Equal(t, current.Hash(), claims.BindingHash)
				})
			}
		})
	}
}

func TestAuthService_SessionBindingMigrationPreservesRefreshValidation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(*User, *RefreshTokenData)
		wantErr error
	}{
		{name: "expired refresh token", mutate: func(_ *User, data *RefreshTokenData) { data.ExpiresAt = time.Now().Add(-time.Hour) }, wantErr: ErrRefreshTokenExpired},
		{name: "password changed", mutate: func(user *User, _ *RefreshTokenData) { user.PasswordHash = "new-password" }, wantErr: ErrTokenRevoked},
		{name: "user disabled", mutate: func(user *User, _ *RefreshTokenData) { user.Status = "disabled" }, wantErr: ErrUserNotActive},
	} {
		t.Run(tc.name, func(t *testing.T) {
			user := &User{ID: 1, Email: "session@example.com", PasswordHash: "original-password", Role: RoleUser, Status: StatusActive}
			svc := newAuthService(&userRepoStub{user: user}, map[string]string{SettingKeySessionBindingEnabled: "true"}, nil, nil)
			svc.cfg.JWT.RefreshTokenExpireDays = 7
			cache := &sessionBindingRefreshCache{tokens: make(map[string]*RefreshTokenData)}
			svc.refreshTokenCache = cache
			pair, err := svc.GenerateTokenPair(context.Background(), user, "session-family")
			require.NoError(t, err)
			data := cache.tokens[hashToken(pair.RefreshToken)]
			data.BindingHash = "0123456789abcdef0123456789abcdef"
			tc.mutate(user, data)
			ctx := WithSessionBinding(context.Background(), &SessionBinding{IP: "198.51.100.2", UserAgent: "Mozilla/5.0"})
			_, err = svc.RefreshTokenPair(ctx, pair.RefreshToken)
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}
