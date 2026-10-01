package openai

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestParseLoginCredentialsSupportsExplicitDelimiterAndPipePassword(t *testing.T) {
	got, err := ParseLoginCredentials("owner@example.com----pa|ssword----JBSWY3DPEHPK3PXP")
	if err != nil {
		t.Fatalf("ParseLoginCredentials() error = %v", err)
	}
	if got.Email != "owner@example.com" || got.Password != "pa|ssword" || got.TOTPSecret != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("unexpected credentials: %#v", got)
	}

	got, err = ParseLoginCredentials("owner@example.com|password|jbswy3dpehpk3pxp")
	if err != nil || got.TOTPSecret != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("pipe credentials = %#v, error = %v", got, err)
	}
}

func TestParseLoginCredentialsRejectsCodesAndMalformedInput(t *testing.T) {
	cases := []string{
		"owner@example.com----password----123456",
		"owner@example.com|password|123456",
		"owner@example.com|pa|ssword|JBSWY3DPEHPK3PXP",
		"owner@example.com----password----short",
		"owner@example.com----password----otpauth://hotp/x?secret=JBSWY3DPEHPK3PXP",
		"owner@example.com----password----otpauth://totp/x?secret=JBSWY3DPEHPK3PXP&algorithm=SHA256",
	}
	for _, input := range cases {
		if _, err := ParseLoginCredentials(input); err == nil {
			t.Errorf("expected malformed credentials to fail: %q", input)
		}
	}
	tooLong := "owner@example.com----" + strings.Repeat("x", maxLoginCredentialsBytes) + "----JBSWY3DPEHPK3PXP"
	if _, err := ParseLoginCredentials(tooLong); err == nil {
		t.Error("expected oversized credentials to fail")
	}
}

func TestParseLoginCredentialsAcceptsOTPAuthURI(t *testing.T) {
	got, err := ParseLoginCredentials("owner@example.com----password----otpauth://totp/OpenAI:owner?secret=JBSWY3DPEHPK3PXP&issuer=OpenAI")
	if err != nil {
		t.Fatalf("ParseLoginCredentials() error = %v", err)
	}
	if got.TOTPSecret != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("unexpected normalized secret: %q", got.TOTPSecret)
	}
}

func TestSentinelDifficultyAndMFAFactorExtraction(t *testing.T) {
	if !sentinelDifficultySatisfied("00abcdef", "00") {
		t.Error("expected matching difficulty")
	}
	if sentinelDifficultySatisfied("01abcdef", "00") {
		t.Error("unexpected difficulty match")
	}
	data := map[string]any{
		"page": map[string]any{
			"payload": map[string]any{
				"mfa_factors": []any{
					map[string]any{"id": "sms", "factor_type": "sms"},
					map[string]any{"id": "totp-id", "factor_type": "totp"},
				},
			},
		},
	}
	factor := findTOTPFactor(data)
	if factor == nil || factor["id"] != "totp-id" {
		t.Fatalf("unexpected factor: %#v", factor)
	}
}

func TestDecodeCookieCandidate(t *testing.T) {
	payload := map[string]any{"workspaces": []any{map[string]any{"id": "workspace-2"}}}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	value := base64.RawURLEncoding.EncodeToString(encoded)
	got, ok := decodeCookieCandidate(value)
	if !ok || got["workspaces"] == nil {
		t.Fatalf("decoded cookie = %#v, ok=%v", got, ok)
	}
}
