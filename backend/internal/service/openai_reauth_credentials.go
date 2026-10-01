package service

import (
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

const (
	OpenAIAutoReauthEnabledKey          = "openai_auto_reauth_enabled"
	OpenAILoginCredentialsKey           = "openai_login_credentials"
	OpenAILoginCredentialsEncryptedKey  = "openai_login_credentials_encrypted"
	OpenAILoginCredentialsConfiguredKey = "openai_login_credentials_configured"
	OpenAIAutoReauthStateKey            = "openai_auto_reauth"
)

// prepareOpenAIReauthCredentials accepts a write-only login line. Only ciphertext
// is persisted; omitted fields preserve configuration across token replacement.
func prepareOpenAIReauthCredentials(platform, accountType string, existing, incoming map[string]any, encryptor SecretEncryptor) (map[string]any, error) {
	out := shallowCopyMap(incoming)
	if out == nil {
		out = make(map[string]any)
	}
	delete(out, OpenAILoginCredentialsConfiguredKey)
	// Ciphertext is server-owned. Import/export must never transport this secret.
	delete(out, OpenAILoginCredentialsEncryptedKey)
	if stored, ok := existing[OpenAILoginCredentialsEncryptedKey]; ok {
		out[OpenAILoginCredentialsEncryptedKey] = stored
	}
	if _, provided := incoming[OpenAIAutoReauthEnabledKey]; !provided {
		if enabled, ok := existing[OpenAIAutoReauthEnabledKey]; ok {
			out[OpenAIAutoReauthEnabledKey] = enabled
		}
	}
	if raw, provided := incoming[OpenAILoginCredentialsKey]; provided {
		delete(out, OpenAILoginCredentialsKey)
		line, ok := raw.(string)
		if !ok {
			return nil, infraerrors.BadRequest("INVALID_OPENAI_LOGIN_CREDENTIALS", "login credentials must be an email----password----2FA string")
		}
		if strings.TrimSpace(line) == "" {
			out[OpenAILoginCredentialsEncryptedKey] = ""
		} else {
			if platform != PlatformOpenAI || accountType != AccountTypeOAuth {
				return nil, infraerrors.BadRequest("OPENAI_REAUTH_UNSUPPORTED", "automatic reauthorization requires an OpenAI OAuth account")
			}
			login, err := openai.ParseLoginCredentials(line)
			if err != nil {
				return nil, infraerrors.BadRequest("INVALID_OPENAI_LOGIN_CREDENTIALS", "invalid email----password----2FA credentials; provide a TOTP secret or otpauth:// URI, not a one-time code")
			}
			email, _ := incoming["email"].(string)
			if email == "" {
				email, _ = existing["email"].(string)
			}
			if email != "" && !strings.EqualFold(strings.TrimSpace(email), login.Email) {
				return nil, infraerrors.BadRequest("OPENAI_REAUTH_EMAIL_MISMATCH", "login email must match the OAuth account email")
			}
			if encryptor == nil {
				return nil, infraerrors.InternalServer("OPENAI_REAUTH_ENCRYPTION_UNAVAILABLE", "account credential encryption is unavailable")
			}
			ciphertext, err := encryptor.Encrypt(line)
			if err != nil {
				return nil, infraerrors.InternalServer("OPENAI_REAUTH_ENCRYPTION_FAILED", "could not encrypt account login credentials")
			}
			out[OpenAILoginCredentialsEncryptedKey] = ciphertext
		}
	}
	if value, present := out[OpenAIAutoReauthEnabledKey]; present {
		enabled, ok := value.(bool)
		if !ok {
			return nil, infraerrors.BadRequest("INVALID_OPENAI_AUTO_REAUTH", "openai_auto_reauth_enabled must be a boolean")
		}
		if enabled {
			if platform != PlatformOpenAI || accountType != AccountTypeOAuth {
				return nil, infraerrors.BadRequest("OPENAI_REAUTH_UNSUPPORTED", "automatic reauthorization requires an OpenAI OAuth account")
			}
			if secret, _ := out[OpenAILoginCredentialsEncryptedKey].(string); secret == "" {
				return nil, infraerrors.BadRequest("OPENAI_LOGIN_CREDENTIALS_REQUIRED", "save account login credentials before enabling automatic reauthorization")
			}
		}
	}
	return out, nil
}

// ExportAccountCredentials excludes login secrets and disables automatic login
// in exported files, which can be imported under a different encryption key.
func ExportAccountCredentials(credentials map[string]any) map[string]any {
	out := shallowCopyMap(credentials)
	delete(out, OpenAILoginCredentialsKey)
	delete(out, OpenAILoginCredentialsEncryptedKey)
	delete(out, OpenAILoginCredentialsConfiguredKey)
	delete(out, OpenAIAutoReauthEnabledKey)
	return out
}
