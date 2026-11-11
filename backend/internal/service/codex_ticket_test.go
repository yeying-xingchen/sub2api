package service

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type codexTicketRepoFake struct {
	mu      sync.Mutex
	records map[string]CodexTicketRecord
}

func newCodexTicketRepoFake() *codexTicketRepoFake {
	return &codexTicketRepoFake{records: make(map[string]CodexTicketRecord)}
}

func (r *codexTicketRepoFake) Upsert(_ context.Context, ticket *CodexTicketRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records[codexTicketCacheKey(ticket.AccountID, ticket.Model)] = *ticket
	return nil
}

func (r *codexTicketRepoFake) Get(_ context.Context, accountID int64, model string) (*CodexTicketRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.records[codexTicketCacheKey(accountID, model)]
	if !ok {
		return nil, nil
	}
	return &record, nil
}

func (r *codexTicketRepoFake) ListValid(_ context.Context, model string, now time.Time) ([]CodexTicketRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]CodexTicketRecord, 0, len(r.records))
	for _, record := range r.records {
		if now.Before(record.ExpiresAt) && (model == "" || normalizeCodexTicketModel(record.Model) == normalizeCodexTicketModel(model)) {
			out = append(out, record)
		}
	}
	return out, nil
}

func (r *codexTicketRepoFake) ListByAccount(_ context.Context, accountID int64) ([]CodexTicketRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]CodexTicketRecord, 0, 2)
	for _, record := range r.records {
		if record.AccountID == accountID {
			out = append(out, record)
		}
	}
	return out, nil
}

func (r *codexTicketRepoFake) List(_ context.Context, filter CodexTicketListFilter) ([]CodexTicketRecord, int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	matched := make([]CodexTicketRecord, 0, len(r.records))
	for _, record := range r.records {
		if filter.AccountID > 0 && record.AccountID != filter.AccountID {
			continue
		}
		if filter.Model != "" && normalizeCodexTicketModel(record.Model) != normalizeCodexTicketModel(filter.Model) {
			continue
		}
		expired := !now.Before(record.ExpiresAt)
		switch filter.Status {
		case "valid":
			if expired {
				continue
			}
		case "expired":
			if !expired {
				continue
			}
		}
		matched = append(matched, record)
	}
	total := int64(len(matched))
	page := filter.Page
	if page <= 0 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	start := (page - 1) * pageSize
	if start >= len(matched) {
		return []CodexTicketRecord{}, total, nil
	}
	end := min(start+pageSize, len(matched))
	return matched[start:end], total, nil
}

func (r *codexTicketRepoFake) GetByID(_ context.Context, id int64) (*CodexTicketRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, record := range r.records {
		if record.ID == id {
			copy := record
			return &copy, nil
		}
	}
	return nil, nil
}

func (r *codexTicketRepoFake) DeleteByID(_ context.Context, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, record := range r.records {
		if record.ID == id {
			delete(r.records, key)
			return nil
		}
	}
	return nil
}

func (r *codexTicketRepoFake) DeleteExpired(_ context.Context, now time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var deleted int64
	for key, record := range r.records {
		if !now.Before(record.ExpiresAt) {
			delete(r.records, key)
			deleted++
		}
	}
	return deleted, nil
}

var codexTicketTestStateSeq byte

func makeCodexTicketState(t *testing.T, blocks int, issued time.Time) string {
	t.Helper()
	raw := make([]byte, 57+16*blocks)
	raw[0] = 0x80
	binary.BigEndian.PutUint64(raw[1:9], uint64(issued.Unix()))
	codexTicketTestStateSeq++
	raw[len(raw)-1] = codexTicketTestStateSeq
	return base64.URLEncoding.EncodeToString(raw)
}

func codexTicketTestAccount(id int64, chatGPTID string) *Account {
	return &Account{
		ID:       id,
		Name:     "acc",
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Status:   StatusActive,
		Credentials: map[string]any{
			"chatgpt_account_id": chatGPTID,
			"email":              chatGPTID + "@example.com",
			"access_token":       "token",
		},
	}
}

func TestCodexTicketExpectedLengths(t *testing.T) {
	personal := codexTicketTestAccount(1, "personal")
	require.Equal(t, 292, codexTicketExpectedLength(personal))

	team := codexTicketTestAccount(2, "team")
	team.Credentials["plan_type"] = "self_serve_business"
	require.Equal(t, 332, codexTicketExpectedLength(team))
}

func TestParseCodexTicketShape(t *testing.T) {
	issued := time.Unix(1_800_000_000, 0)
	state := makeCodexTicketState(t, 10, issued)
	shape, err := parseCodexTicketShape(state)
	require.NoError(t, err)
	require.Equal(t, 10, shape.Blocks)
	require.Equal(t, issued.Unix(), shape.IssuedAt.Unix())
	require.Equal(t, 292, len(state))
}

func TestCodexTicketSettingsNormalize(t *testing.T) {
	defaults := normalizeCodexTicketSettings(DefaultCodexTicketSettings())
	require.False(t, defaults.Enabled)
	require.True(t, defaults.Shared)
	require.Equal(t, []string{"gpt-6-astra", "gpt-5.6-sol"}, defaults.Models)
	require.Equal(t, codexTicketDefaultHarvestInterval, defaults.HarvestIntervalSeconds)

	custom := normalizeCodexTicketSettings(CodexTicketSettings{
		Models:                 []string{" gpt-6-astra ", "GPT-6-ASTRA", ""},
		HarvestIntervalSeconds: 1,
		TTLSeconds:             999999,
		ProbeTimeoutSeconds:    0,
	})
	require.Equal(t, []string{"gpt-6-astra"}, custom.Models)
	require.Equal(t, codexTicketDefaultHarvestInterval, custom.HarvestIntervalSeconds)
	require.Equal(t, codexTicketDefaultTTLSeconds, custom.TTLSeconds)
	require.Equal(t, codexTicketDefaultProbeTimeout, custom.ProbeTimeoutSeconds)
}

func newCodexTicketTestService(repo CodexTicketRepository, settings CodexTicketSettings) *CodexTicketService {
	svc := NewCodexTicketService(nil, nil, nil, repo, nil)
	svc.settings = normalizeCodexTicketSettings(settings)
	svc.settingsAt = time.Now()
	return svc
}

func TestCodexTicketInjectPrefersOwnThenShared(t *testing.T) {
	now := time.Now()
	owner := codexTicketTestAccount(1, "owner")
	borrower := codexTicketTestAccount(2, "borrower")
	ownerState := makeCodexTicketState(t, 10, now)
	sharedState := makeCodexTicketState(t, 10, now)
	require.NotEqual(t, ownerState, sharedState)

	repo := newCodexTicketRepoFake()
	require.NoError(t, repo.Upsert(context.Background(), &CodexTicketRecord{
		AccountID: owner.ID, Model: "gpt-6-astra", State: ownerState, Length: len(ownerState),
		Identity: codexTicketIdentity(owner), CapturedAt: now, ExpiresAt: now.Add(time.Hour),
	}))
	require.NoError(t, repo.Upsert(context.Background(), &CodexTicketRecord{
		AccountID: borrower.ID, Model: "gpt-6-astra", State: sharedState, Length: len(sharedState),
		Identity: codexTicketIdentity(borrower), CapturedAt: now, ExpiresAt: now.Add(time.Hour),
	}))

	settings := DefaultCodexTicketSettings()
	settings.Enabled = true
	settings.Shared = true
	svc := newCodexTicketTestService(repo, settings)

	reqHeader := http.Header{}
	require.NoError(t, svc.Inject(context.Background(), owner, "gpt-6-astra", reqHeader))
	require.Equal(t, ownerState, reqHeader.Get(openAICodexTurnStateHeader), "own ticket must win")

	// Remove the owner's ticket: the borrower should fall back to the shared pool.
	repo.mu.Lock()
	delete(repo.records, codexTicketCacheKey(owner.ID, "gpt-6-astra"))
	repo.mu.Unlock()
	svc.tickets.Range(func(key, _ any) bool {
		svc.tickets.Delete(key)
		return true
	})

	reqHeader = http.Header{}
	require.NoError(t, svc.Inject(context.Background(), owner, "gpt-6-astra", reqHeader))
	require.Equal(t, sharedState, reqHeader.Get(openAICodexTurnStateHeader), "shared pool must be used as fallback")
}

func TestCodexTicketApplyPinsSessionAndStripsBodyIdentity(t *testing.T) {
	now := time.Now()
	owner := codexTicketTestAccount(1, "owner")
	state := makeCodexTicketState(t, 10, now)
	repo := newCodexTicketRepoFake()
	require.NoError(t, repo.Upsert(context.Background(), &CodexTicketRecord{
		AccountID: owner.ID, Model: "gpt-6-astra", State: state, Length: len(state),
		Identity: codexTicketIdentity(owner), CapturedAt: now, ExpiresAt: now.Add(time.Hour),
		SessionID: "harvest-session",
	}))
	settings := DefaultCodexTicketSettings()
	settings.Enabled = true
	svc := newCodexTicketTestService(repo, settings)

	header := http.Header{}
	header.Set("conversation_id", "client-conversation")
	header.Set("x-codex-installation-id", "client-install")
	body := []byte(`{"model":"gpt-6-astra","prompt_cache_key":"cache-key","client_metadata":{"x":1},"input":[]}`)
	next, err := svc.Apply(context.Background(), owner, "gpt-6-astra", header, body)
	require.NoError(t, err)
	require.Equal(t, "harvest-session", header.Get("session_id"))
	require.Empty(t, header.Get("conversation_id"))
	require.Empty(t, header.Get("x-codex-installation-id"))
	require.Equal(t, "true", header.Get(responsesLiteHeaderKey))
	require.NotContains(t, string(next), "prompt_cache_key")
	require.NotContains(t, string(next), "client_metadata")
	require.Contains(t, string(next), `"input":[]`)
}

func TestCodexTicketInjectSharedDisabledAndFailClosed(t *testing.T) {
	now := time.Now()
	donor := codexTicketTestAccount(1, "donor")
	borrower := codexTicketTestAccount(2, "borrower")
	state := makeCodexTicketState(t, 10, now)
	repo := newCodexTicketRepoFake()
	require.NoError(t, repo.Upsert(context.Background(), &CodexTicketRecord{
		AccountID: donor.ID, Model: "gpt-6-astra", State: state, Length: len(state),
		Identity: codexTicketIdentity(donor), CapturedAt: now, ExpiresAt: now.Add(time.Hour),
	}))

	settings := DefaultCodexTicketSettings()
	settings.Enabled = true
	settings.Shared = false
	svc := newCodexTicketTestService(repo, settings)
	header := http.Header{}
	require.NoError(t, svc.Inject(context.Background(), borrower, "gpt-6-astra", header))
	require.Empty(t, header.Get(openAICodexTurnStateHeader))

	settings.Shared = true
	settings.FailClosed = true
	svc = newCodexTicketTestService(newCodexTicketRepoFake(), settings)
	err := svc.Inject(context.Background(), borrower, "gpt-4.1-codex", header)
	require.NoError(t, err, "models outside the gated list are never fail-closed")
	err = svc.Inject(context.Background(), borrower, "gpt-6-astra", header)
	require.ErrorIs(t, err, ErrCodexTicketUnavailable)
}

func TestCodexTicketRejectsForeignIdentity(t *testing.T) {
	now := time.Now()
	owner := codexTicketTestAccount(1, "owner")
	state := makeCodexTicketState(t, 10, now)
	repo := newCodexTicketRepoFake()
	require.NoError(t, repo.Upsert(context.Background(), &CodexTicketRecord{
		AccountID: owner.ID, Model: "gpt-6-astra", State: state, Length: len(state),
		Identity: codexTicketIdentity(owner), CapturedAt: now, ExpiresAt: now.Add(time.Hour),
	}))
	settings := DefaultCodexTicketSettings()
	settings.Enabled = true
	settings.Shared = false
	svc := newCodexTicketTestService(repo, settings)

	impostor := codexTicketTestAccount(owner.ID, "other")
	header := http.Header{}
	require.NoError(t, svc.Inject(context.Background(), impostor, "gpt-6-astra", header))
	require.Empty(t, header.Get(openAICodexTurnStateHeader))
}

func TestValidateCodexTicketProbeResponse(t *testing.T) {
	completed := []byte("event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"model\":\"gpt-6-astra\"}}\n\n")
	require.NoError(t, validateCodexTicketProbeResponse(completed, "gpt-6-astra"))

	failed := []byte("event: response.failed\ndata: {\"type\":\"response.failed\",\"response\":{\"status\":\"failed\"}}\n\n")
	require.Error(t, validateCodexTicketProbeResponse(failed, "gpt-6-astra"))

	mismatch := []byte("event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"model\":\"gpt-5.6-sol\"}}\n\n")
	require.Error(t, validateCodexTicketProbeResponse(mismatch, "gpt-6-astra"))
}

type codexTicketAccountRepoFake struct {
	AccountRepository
	accounts []Account
}

func (r *codexTicketAccountRepoFake) ListByPlatform(_ context.Context, _ string) ([]Account, error) {
	return r.accounts, nil
}

func (r *codexTicketAccountRepoFake) GetByID(_ context.Context, id int64) (*Account, error) {
	for i := range r.accounts {
		if r.accounts[i].ID == id {
			return &r.accounts[i], nil
		}
	}
	return nil, ErrAccountNotFound
}

func TestCodexTicketOverviewHidesState(t *testing.T) {
	now := time.Now()
	first := codexTicketTestAccount(1, "first")
	second := codexTicketTestAccount(2, "second")
	state := makeCodexTicketState(t, 10, now)
	repo := newCodexTicketRepoFake()
	require.NoError(t, repo.Upsert(context.Background(), &CodexTicketRecord{
		AccountID: first.ID, Model: "gpt-6-astra", State: state, Length: len(state),
		Identity: codexTicketIdentity(first), CapturedAt: now, ExpiresAt: now.Add(time.Hour),
		SessionID: "session-1",
	}))
	settings := DefaultCodexTicketSettings()
	settings.Enabled = true
	settings.Shared = true
	svc := newCodexTicketTestService(repo, settings)
	svc.accountRepo = &codexTicketAccountRepoFake{accounts: []Account{*first, *second}}

	overview, err := svc.Overview(context.Background())
	require.NoError(t, err)
	require.Len(t, overview.Accounts, 2)
	require.Len(t, overview.Pool, 1)
	require.Equal(t, first.ID, overview.Pool[0].SourceAccountID)
	require.Equal(t, 292, overview.Pool[0].Length)
	require.NotContains(t, fmt.Sprintf("%+v", overview), state, "state must never be exposed to admin DTOs")
}

func TestCodexTicketListTicketsStatusAndPagination(t *testing.T) {
	now := time.Now()
	first := codexTicketTestAccount(1, "first")
	second := codexTicketTestAccount(2, "second")
	repo := newCodexTicketRepoFake()
	require.NoError(t, repo.Upsert(context.Background(), &CodexTicketRecord{
		ID: 1, AccountID: first.ID, Model: "gpt-6-astra", State: makeCodexTicketState(t, 10, now),
		Length: 292, CapturedAt: now, ExpiresAt: now.Add(time.Hour),
	}))
	require.NoError(t, repo.Upsert(context.Background(), &CodexTicketRecord{
		ID: 2, AccountID: second.ID, Model: "gpt-6-astra", State: makeCodexTicketState(t, 10, now.Add(-2*time.Hour)),
		Length: 292, CapturedAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(-time.Hour),
	}))
	settings := DefaultCodexTicketSettings()
	svc := newCodexTicketTestService(repo, settings)
	svc.accountRepo = &codexTicketAccountRepoFake{accounts: []Account{*first, *second}}

	result, err := svc.ListTickets(context.Background(), CodexTicketListFilter{Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.EqualValues(t, 2, result.Total)
	require.Len(t, result.Items, 2)
	statuses := map[int64]string{}
	for _, item := range result.Items {
		statuses[item.AccountID] = item.Status
	}
	require.Equal(t, "valid", statuses[first.ID])
	require.Equal(t, "expired", statuses[second.ID])

	filtered, err := svc.ListTickets(context.Background(), CodexTicketListFilter{Status: "valid", Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.EqualValues(t, 1, filtered.Total)
	require.Equal(t, first.ID, filtered.Items[0].AccountID)
}

func TestCodexTicketDeleteInvalidatesList(t *testing.T) {
	now := time.Now()
	account := codexTicketTestAccount(1, "first")
	repo := newCodexTicketRepoFake()
	require.NoError(t, repo.Upsert(context.Background(), &CodexTicketRecord{
		ID: 7, AccountID: account.ID, Model: "gpt-6-astra", State: makeCodexTicketState(t, 10, now),
		Length: 292, CapturedAt: now, ExpiresAt: now.Add(time.Hour),
	}))
	svc := newCodexTicketTestService(repo, DefaultCodexTicketSettings())
	svc.accountRepo = &codexTicketAccountRepoFake{accounts: []Account{*account}}

	require.NoError(t, svc.DeleteTicket(context.Background(), 7))
	result, err := svc.ListTickets(context.Background(), CodexTicketListFilter{Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.EqualValues(t, 0, result.Total)
	require.Empty(t, result.Items)
}

func TestCodexTicketExpiredRecordIsIgnored(t *testing.T) {
	now := time.Now()
	record := &CodexTicketRecord{
		AccountID: 1, Model: "gpt-6-astra", State: makeCodexTicketState(t, 10, now.Add(-2*time.Hour)),
		Length: 292, CapturedAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(-time.Minute),
	}
	require.False(t, record.valid(now, 292))
}
