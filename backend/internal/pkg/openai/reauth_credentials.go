package openai

import (
	"encoding/base32"
	"errors"
	"net/mail"
	"net/url"
	"strings"
	"unicode/utf8"
)

// LoginCredentials is transient plaintext; callers must encrypt it before storage.
type LoginCredentials struct {
	Email      string
	Password   string
	TOTPSecret string
}

const maxLoginCredentialsBytes = 8192

var errInvalidLoginCredentials = errors.New("invalid email----password----2FA credentials; provide a TOTP secret, not a one-time code")

// ParseLoginCredentials accepts one login line. The explicit ---- delimiter has
// priority, so passwords containing | remain intact. Errors never include input.
func ParseLoginCredentials(raw string) (LoginCredentials, error) {
	if len(raw) > maxLoginCredentialsBytes || !utf8.ValidString(raw) || strings.ContainsAny(raw, "\r\n\x00") {
		return LoginCredentials{}, errInvalidLoginCredentials
	}
	delimiter := "|"
	if strings.Contains(raw, "----") {
		delimiter = "----"
	}
	parts := strings.Split(raw, delimiter)
	if len(parts) != 3 {
		return LoginCredentials{}, errInvalidLoginCredentials
	}
	return validateLoginCredentials(LoginCredentials{
		Email: strings.TrimSpace(parts[0]), Password: parts[1], TOTPSecret: strings.TrimSpace(parts[2]),
	})
}

func validateLoginCredentials(c LoginCredentials) (LoginCredentials, error) {
	combined := c.Email + c.Password + c.TOTPSecret
	if len(combined) > maxLoginCredentialsBytes || !utf8.ValidString(combined) || strings.ContainsAny(combined, "\r\n\x00") {
		return LoginCredentials{}, errInvalidLoginCredentials
	}
	address, err := mail.ParseAddress(c.Email)
	if err != nil || address.Address != c.Email || c.Password == "" {
		return LoginCredentials{}, errInvalidLoginCredentials
	}
	c.TOTPSecret, err = normalizeTOTPSecret(c.TOTPSecret)
	if err != nil {
		return LoginCredentials{}, err
	}
	return c, nil
}

func normalizeTOTPSecret(secret string) (string, error) {
	secret = strings.TrimSpace(secret)
	if strings.HasPrefix(strings.ToLower(secret), "otpauth://") {
		u, err := url.Parse(secret)
		if err != nil || u.Scheme != "otpauth" || u.Host != "totp" || u.User != nil || u.Fragment != "" {
			return "", errInvalidLoginCredentials
		}
		q, err := url.ParseQuery(u.RawQuery)
		if err != nil || len(q["secret"]) != 1 {
			return "", errInvalidLoginCredentials
		}
		for key, expected := range map[string]string{"algorithm": "SHA1", "digits": "6", "period": "30"} {
			if len(q[key]) > 1 || (q.Get(key) != "" && !strings.EqualFold(q.Get(key), expected)) {
				return "", errInvalidLoginCredentials
			}
		}
		secret = q.Get("secret")
	}
	secret = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(secret), " ", ""))
	secret = strings.TrimRight(secret, "=")
	if len(secret) == 6 && isDigits(secret) {
		return "", errInvalidLoginCredentials
	}
	if len(secret) < 16 {
		return "", errInvalidLoginCredentials
	}
	decoded, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil || len(decoded) < 10 {
		return "", errInvalidLoginCredentials
	}
	return secret, nil
}

func isDigits(value string) bool {
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return value != ""
}
