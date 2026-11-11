package service

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRunCodexIQProbeVerdicts(t *testing.T) {
	ctx := context.Background()
	mintState := "gAAAAA-" + "mint"
	otherState := "gAAAAA-" + "other"

	tests := []struct {
		name         string
		shots        []codexIQShot
		errs         []error
		wantVerdict  string
		wantFailure  string
		wantNew      bool
		wantMinted   bool
		wantContinue int
	}{
		{
			name:         "healthy when no new ticket",
			shots:        []codexIQShot{{status: 200, state: mintState}, {status: 200}},
			wantVerdict:  CodexIQVerdictHealthy,
			wantMinted:   true,
			wantContinue: 200,
		},
		{
			name:         "healthy when continuation echoes same ticket",
			shots:        []codexIQShot{{status: 200, state: mintState}, {status: 200, state: mintState}},
			wantVerdict:  CodexIQVerdictHealthy,
			wantMinted:   true,
			wantContinue: 200,
		},
		{
			name:         "degraded when continuation mints a new ticket",
			shots:        []codexIQShot{{status: 200, state: mintState}, {status: 200, state: otherState}},
			wantVerdict:  CodexIQVerdictDegraded,
			wantNew:      true,
			wantMinted:   true,
			wantContinue: 200,
		},
		{
			name:        "inconclusive when mint returns no ticket",
			shots:       []codexIQShot{{status: 200}},
			wantVerdict: CodexIQVerdictInconclusive,
			wantFailure: CodexIQFailureNoTicket,
		},
		{
			name:         "inconclusive when continuation is rate limited",
			shots:        []codexIQShot{{status: 200, state: mintState}, {status: 429}},
			wantVerdict:  CodexIQVerdictInconclusive,
			wantFailure:  CodexIQFailureRateLimited,
			wantMinted:   true,
			wantContinue: 429,
		},
		{
			name:        "inconclusive when stream fails",
			shots:       []codexIQShot{{status: 200, state: mintState, streamErr: errors.New("overloaded")}},
			wantVerdict: CodexIQVerdictInconclusive,
			wantFailure: CodexIQFailureStreamError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &CodexIQTestResult{Verdict: CodexIQVerdictInconclusive, TestedAt: time.Now()}
			index := 0
			fire := func(string, string) (codexIQShot, error) {
				if index >= len(tt.shots) {
					return codexIQShot{}, errors.New("unexpected extra shot")
				}
				shot := tt.shots[index]
				var err error
				if index < len(tt.errs) {
					err = tt.errs[index]
				}
				index++
				return shot, err
			}
			(&CodexTicketService{}).runCodexIQProbe(ctx, result, fire)
			require.Equal(t, tt.wantVerdict, result.Verdict, result.Reason)
			require.Equal(t, tt.wantFailure, result.Failure)
			require.Equal(t, tt.wantNew, result.NewTicket)
			require.Equal(t, tt.wantMinted, result.Minted)
			require.Equal(t, tt.wantContinue, result.ContinueStatus)
		})
	}
}

func TestCodexIQRouteCookies(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Add("Set-Cookie", "__cflb=one; Path=/; Max-Age=3600")
	resp.Header.Add("Set-Cookie", "__oailb=two; Path=/; Max-Age=3600")
	resp.Header.Add("Set-Cookie", "other=three; Path=/")
	require.Equal(t, []string{"__cflb=one", "__oailb=two"}, codexIQRouteCookies(resp))
}

func TestCodexIQUnsupportedReason(t *testing.T) {
	apiKey := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	require.Contains(t, codexIQUnsupportedReason(apiKey, ""), "OAuth")

	oauth := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive}
	require.Empty(t, codexIQUnsupportedReason(oauth, ""))
}

func TestCodexIQResultShortTicketNote(t *testing.T) {
	result := &CodexIQTestResult{Verdict: CodexIQVerdictInconclusive}
	fire := func(string, string) (codexIQShot, error) {
		if result.Minted {
			return codexIQShot{status: 200}, nil
		}
		return codexIQShot{status: 200, state: "gAAAAA-short"}, nil
	}
	(&CodexTicketService{}).runCodexIQProbe(context.Background(), result, fire)
	require.True(t, result.ShortTicket)
	require.Contains(t, result.Reason, "短票形态")
}
