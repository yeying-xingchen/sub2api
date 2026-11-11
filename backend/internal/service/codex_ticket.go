package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"go.uber.org/zap"
)

// Codex 打票（turn-state ticket）核心服务。
//
// 上游在 /backend-api/codex/responses 的响应头里铸造 x-codex-turn-state，
// 客户端随后在同一回合的请求里原样回带。本服务做两件事：
//  1. 后台为 Codex OAuth 账号主动打票，按 (账号, 模型) 持久化；
//  2. 业务请求优先注入账号自己的票；没有可用票时，按「全站共用票」设置
//     从共享池借用其他账号的有效票。
//
// 票值是上游凭证材料，任何管理端 DTO 都不返回 state 明文。

const (
	// CodexTicketSettingsKey 是打票设置的 system_settings 键。
	CodexTicketSettingsKey = "codex_ticket_settings"

	codexTicketStatePrefix     = "gAAAAA"
	codexTicketDefaultModel    = "gpt-6-astra"
	codexTicketDefaultSolModel = "gpt-5.6-sol"
	codexTicketPersonalBlocks  = 10
	codexTicketTeamBlocks      = 12
	codexTicketStateLifetime   = 6 * time.Hour

	codexTicketDefaultHarvestInterval = 180
	codexTicketDefaultTTLSeconds      = 3600
	codexTicketDefaultProbeTimeout    = 25
	codexTicketMinProbeTimeout        = 5
	codexTicketMaxProbeTimeout        = 120
	codexTicketMinHarvestInterval     = 30
	codexTicketMaxHarvestInterval     = 3600
	codexTicketMinTTLSeconds          = 60
	codexTicketMaxTTLSeconds          = 86400
	codexTicketProbeCooldown          = 3 * time.Minute
	codexTicketTicketCacheTTL         = 20 * time.Second
	codexTicketPoolCacheTTL           = 10 * time.Second
	codexTicketRefreshBefore          = 10 * time.Minute
	codexTicketHarvestWorkers         = 2
)

// ErrCodexTicketUnavailable 表示启用了 fail-closed 但当前账号既没有自己的
// 票，共享池里也没有可用票。
var ErrCodexTicketUnavailable = errors.New("codex turn-state ticket unavailable")

// CodexTicketSettings 是管理端可配置的打票参数。
type CodexTicketSettings struct {
	Enabled bool `json:"enabled"`
	// Shared 开启「全站共用票」：账号缺票时可借用其他账号的有效票。
	Shared bool `json:"shared"`
	// FailClosed 开启后，无票请求不再放行而是直接失败。
	FailClosed             bool     `json:"fail_closed"`
	Models                 []string `json:"models"`
	HarvestIntervalSeconds int      `json:"harvest_interval_seconds"`
	TTLSeconds             int      `json:"ttl_seconds"`
	ProbeTimeoutSeconds    int      `json:"probe_timeout_seconds"`
}

// DefaultCodexTicketSettings 返回默认打票配置（默认关闭，避免无意中产生上游请求）。
func DefaultCodexTicketSettings() CodexTicketSettings {
	return CodexTicketSettings{
		Enabled:                false,
		Shared:                 true,
		FailClosed:             false,
		Models:                 []string{codexTicketDefaultModel, codexTicketDefaultSolModel},
		HarvestIntervalSeconds: codexTicketDefaultHarvestInterval,
		TTLSeconds:             codexTicketDefaultTTLSeconds,
		ProbeTimeoutSeconds:    codexTicketDefaultProbeTimeout,
	}
}

func normalizeCodexTicketModels(models []string) []string {
	seen := make(map[string]struct{}, len(models))
	out := make([]string, 0, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		key := normalizeCodexTicketModel(model)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, model)
	}
	return out
}

func normalizeCodexTicketModel(model string) string {
	return strings.ToLower(strings.TrimSpace(model))
}

func normalizeCodexTicketSettings(in CodexTicketSettings) CodexTicketSettings {
	out := in
	if out.Models == nil {
		out.Models = DefaultCodexTicketSettings().Models
	}
	out.Models = normalizeCodexTicketModels(out.Models)
	if out.HarvestIntervalSeconds < codexTicketMinHarvestInterval || out.HarvestIntervalSeconds > codexTicketMaxHarvestInterval {
		out.HarvestIntervalSeconds = codexTicketDefaultHarvestInterval
	}
	if out.TTLSeconds < codexTicketMinTTLSeconds || out.TTLSeconds > codexTicketMaxTTLSeconds {
		out.TTLSeconds = codexTicketDefaultTTLSeconds
	}
	if out.ProbeTimeoutSeconds < codexTicketMinProbeTimeout || out.ProbeTimeoutSeconds > codexTicketMaxProbeTimeout {
		out.ProbeTimeoutSeconds = codexTicketDefaultProbeTimeout
	}
	return out
}

func (s CodexTicketSettings) gates(model string) bool {
	model = normalizeCodexTicketModel(model)
	if model == "" {
		return false
	}
	for _, item := range s.Models {
		if normalizeCodexTicketModel(item) == model {
			return true
		}
	}
	return false
}

// CodexTicketRecord 是一张已入库的票。State 是上游凭证材料，禁止透出管理端。
type CodexTicketRecord struct {
	ID          int64     `json:"id,omitempty"`
	AccountID   int64     `json:"account_id"`
	Model       string    `json:"model"`
	State       string    `json:"-"`
	Length      int       `json:"length"`
	Identity    string    `json:"identity,omitempty"`
	Transport   string    `json:"transport,omitempty"`
	Blocks      int       `json:"blocks,omitempty"`
	IssuedAt    time.Time `json:"issued_at,omitempty"`
	CapturedAt  time.Time `json:"captured_at"`
	ExpiresAt   time.Time `json:"expires_at"`
	Acquisition string    `json:"acquisition,omitempty"`
	// SessionID 是打票探测使用的上游 session_id；业务请求注入票据时需要
	// 对齐同一会话身份，否则上游会看到一张来自其他会话的 turn-state。
	SessionID string `json:"session_id,omitempty"`
}

// CodexTicketListFilter 是门票管理页的查询条件。
type CodexTicketListFilter struct {
	AccountID int64
	Model     string
	Status    string // valid / expired / 空
	Page      int
	PageSize  int
}

// CodexTicketListItem 是门票管理页的一行，不含 state 明文。
type CodexTicketListItem struct {
	ID               int64     `json:"id"`
	AccountID        int64     `json:"account_id"`
	AccountName      string    `json:"account_name,omitempty"`
	Model            string    `json:"model"`
	Status           string    `json:"status"`
	Length           int       `json:"length"`
	Blocks           int       `json:"blocks,omitempty"`
	Acquisition      string    `json:"acquisition,omitempty"`
	CapturedAt       time.Time `json:"captured_at"`
	ExpiresAt        time.Time `json:"expires_at"`
	RemainingSeconds int64     `json:"remaining_seconds"`
}

// CodexTicketListResult 是门票管理页的分页结果。
type CodexTicketListResult struct {
	Items    []CodexTicketListItem `json:"items"`
	Total    int64                 `json:"total"`
	Page     int                   `json:"page"`
	PageSize int                   `json:"page_size"`
}

// CodexTicketRepository 是票的持久化端口。
type CodexTicketRepository interface {
	Upsert(ctx context.Context, ticket *CodexTicketRecord) error
	Get(ctx context.Context, accountID int64, model string) (*CodexTicketRecord, error)
	ListValid(ctx context.Context, model string, now time.Time) ([]CodexTicketRecord, error)
	ListByAccount(ctx context.Context, accountID int64) ([]CodexTicketRecord, error)
	List(ctx context.Context, filter CodexTicketListFilter) ([]CodexTicketRecord, int64, error)
	GetByID(ctx context.Context, id int64) (*CodexTicketRecord, error)
	DeleteByID(ctx context.Context, id int64) error
	DeleteExpired(ctx context.Context, now time.Time) (int64, error)
}

func (t *CodexTicketRecord) valid(now time.Time, targetLen int) bool {
	if t == nil {
		return false
	}
	state := strings.TrimSpace(t.State)
	if state == "" || !strings.HasPrefix(state, codexTicketStatePrefix) {
		return false
	}
	length := t.Length
	if length == 0 {
		length = len(state)
	}
	if length != len(state) {
		return false
	}
	if targetLen > 0 && length != targetLen {
		return false
	}
	if t.ExpiresAt.IsZero() || !now.Before(t.ExpiresAt) {
		return false
	}
	if !t.IssuedAt.IsZero() && (t.IssuedAt.After(now.Add(30*time.Second)) || !now.Before(t.IssuedAt.Add(codexTicketStateLifetime))) {
		return false
	}
	return true
}

func (t *CodexTicketRecord) remaining(now time.Time) time.Duration {
	if t == nil || t.ExpiresAt.IsZero() {
		return 0
	}
	remaining := t.ExpiresAt.Sub(now)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// CodexTicketModelStatus 是单个账号单个模型的票据状态（不含 state）。
type CodexTicketModelStatus struct {
	Model             string     `json:"model"`
	Ready             bool       `json:"ready"`
	Own               bool       `json:"own"`
	Shared            bool       `json:"shared"`
	SourceAccountID   int64      `json:"source_account_id,omitempty"`
	SourceAccountName string     `json:"source_account_name,omitempty"`
	Length            int        `json:"length,omitempty"`
	Blocks            int        `json:"blocks,omitempty"`
	RemainingSeconds  int64      `json:"remaining_seconds"`
	CapturedAt        *time.Time `json:"captured_at,omitempty"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`
}

// CodexTicketAccountStatus 是一个账号的打票状态。
type CodexTicketAccountStatus struct {
	ID       int64                    `json:"id"`
	Name     string                   `json:"name"`
	Status   string                   `json:"status"`
	Platform string                   `json:"platform"`
	Tickets  []CodexTicketModelStatus `json:"tickets"`
	// IQ 是最近一次智商测试结果；进程重启后清空，不影响票据本身。
	IQ *CodexIQTestResult `json:"iq,omitempty"`
}

// CodexTicketPoolEntry 是共享票池里的一条可借用票据（不含 state）。
type CodexTicketPoolEntry struct {
	Model             string    `json:"model"`
	SourceAccountID   int64     `json:"source_account_id"`
	SourceAccountName string    `json:"source_account_name,omitempty"`
	Length            int       `json:"length"`
	Blocks            int       `json:"blocks,omitempty"`
	Acquisition       string    `json:"acquisition,omitempty"`
	CapturedAt        time.Time `json:"captured_at"`
	ExpiresAt         time.Time `json:"expires_at"`
	RemainingSeconds  int64     `json:"remaining_seconds"`
}

// CodexTicketOverview 是管理端打票页的数据。
type CodexTicketOverview struct {
	Settings    CodexTicketSettings        `json:"settings"`
	Accounts    []CodexTicketAccountStatus `json:"accounts"`
	Pool        []CodexTicketPoolEntry     `json:"pool"`
	LastRoundAt *time.Time                 `json:"last_round_at,omitempty"`
}

type codexTicketCacheEntry struct {
	record   CodexTicketRecord
	loadedAt time.Time
}

type codexTicketPoolCache struct {
	records  []CodexTicketRecord
	loadedAt time.Time
}

// CodexTicketService 负责打票、票据存储、全站共享与请求注入。
type CodexTicketService struct {
	accountRepo   AccountRepository
	tokenProvider *OpenAITokenProvider
	upstream      HTTPUpstream
	repo          CodexTicketRepository
	settingRepo   SettingRepository

	settingsMu  sync.RWMutex
	settings    CodexTicketSettings
	settingsAt  time.Time
	settingsTTL time.Duration

	tickets sync.Map // accountID\x00model -> codexTicketCacheEntry
	pool    sync.Map // model -> codexTicketPoolCache

	sharedLastUsed sync.Map // model\x00sourceAccountID -> time.Time
	probeCooldown  sync.Map // accountID\x00model -> time.Time
	iqResults      sync.Map // accountID -> CodexIQTestResult（最近一次智商测试）
	iqRunning      sync.Map // accountID -> struct{}（避免同账号并发测试）

	roundMu       sync.Mutex
	lastRoundUnix atomic.Int64

	started atomic.Bool
	stopCh  chan struct{}
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// NewCodexTicketService 创建打票服务；repo 为空时服务只做无操作透传。
func NewCodexTicketService(
	accountRepo AccountRepository,
	tokenProvider *OpenAITokenProvider,
	upstream HTTPUpstream,
	repo CodexTicketRepository,
	settingRepo SettingRepository,
) *CodexTicketService {
	return &CodexTicketService{
		accountRepo:   accountRepo,
		tokenProvider: tokenProvider,
		upstream:      upstream,
		repo:          repo,
		settingRepo:   settingRepo,
		settings:      DefaultCodexTicketSettings(),
		settingsTTL:   15 * time.Second,
		stopCh:        make(chan struct{}),
	}
}

// Settings 返回当前打票配置，短 TTL 缓存以免请求热路径查库。
func (s *CodexTicketService) Settings(ctx context.Context) CodexTicketSettings {
	if s == nil {
		return DefaultCodexTicketSettings()
	}
	now := time.Now()
	s.settingsMu.RLock()
	if !s.settingsAt.IsZero() && now.Sub(s.settingsAt) < s.settingsTTL {
		settings := s.settings
		s.settingsMu.RUnlock()
		return settings
	}
	s.settingsMu.RUnlock()

	settings := DefaultCodexTicketSettings()
	if s.settingRepo != nil {
		if raw, err := s.settingRepo.GetValue(ctx, CodexTicketSettingsKey); err == nil && strings.TrimSpace(raw) != "" {
			var stored CodexTicketSettings
			if json.Unmarshal([]byte(raw), &stored) == nil {
				settings = stored
			}
		}
	}
	settings = normalizeCodexTicketSettings(settings)

	s.settingsMu.Lock()
	s.settings = settings
	s.settingsAt = now
	s.settingsMu.Unlock()
	return settings
}

// UpdateSettings 校验并保存打票配置。
func (s *CodexTicketService) UpdateSettings(ctx context.Context, in CodexTicketSettings) (CodexTicketSettings, error) {
	if s == nil || s.settingRepo == nil {
		return CodexTicketSettings{}, errors.New("codex ticket settings unavailable")
	}
	if len(in.Models) > 32 {
		return CodexTicketSettings{}, errors.New("too many codex ticket models (max 32)")
	}
	for _, model := range in.Models {
		if len(model) > 128 {
			return CodexTicketSettings{}, errors.New("codex ticket model name too long")
		}
	}
	out := normalizeCodexTicketSettings(in)
	payload, err := json.Marshal(out)
	if err != nil {
		return CodexTicketSettings{}, err
	}
	if err := s.settingRepo.Set(ctx, CodexTicketSettingsKey, string(payload)); err != nil {
		return CodexTicketSettings{}, err
	}
	s.settingsMu.Lock()
	s.settings = out
	s.settingsAt = time.Now()
	s.settingsMu.Unlock()
	// 设置变更后共享池的可用范围立即重算。
	s.pool.Range(func(key, _ any) bool {
		s.pool.Delete(key)
		return true
	})
	return out, nil
}

// Start 启动后台打票循环。
func (s *CodexTicketService) Start() {
	if s == nil || s.repo == nil || s.accountRepo == nil || s.tokenProvider == nil || s.upstream == nil {
		return
	}
	if !s.started.CompareAndSwap(false, true) {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.harvestLoop(ctx)
	}()
	logger.L().Info("codex ticket harvester started")
}

// Stop 停止后台打票循环。
func (s *CodexTicketService) Stop() {
	if s == nil || !s.started.CompareAndSwap(true, false) {
		return
	}
	if s.cancel != nil {
		s.cancel()
	}
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
	if s.stopCh != nil {
		select {
		case <-s.stopCh:
		default:
			close(s.stopCh)
		}
	}
}

// Inject 在出站请求头写入 turn-state（不修改 body）。优先级：自己的票 > 全站共享票。
func (s *CodexTicketService) Inject(ctx context.Context, account *Account, model string, header http.Header) error {
	_, err := s.Apply(ctx, account, model, header, nil)
	return err
}

// Apply 注入 turn-state，并返回清理过打票身份字段的请求体。
// 上游的 turn-state 与铸造时的出站身份绑定：除 session_id 外，业务请求
// 自带的 prompt_cache_key / client_metadata / device_id 也会引入第二身份，
// 需要在注入票据时一并清掉。
func (s *CodexTicketService) Apply(ctx context.Context, account *Account, model string, header http.Header, body []byte) ([]byte, error) {
	if s == nil || s.repo == nil || header == nil || account == nil {
		return body, nil
	}
	settings := s.Settings(ctx)
	if !settings.Enabled || !settings.gates(model) {
		return body, nil
	}
	if !account.UsesOpenAICodexProtocol() || account.IsShadow() {
		return body, nil
	}
	targetLen := codexTicketExpectedLength(account)
	now := time.Now()
	if own := s.ownTicket(ctx, account, model, targetLen, now); own != nil {
		applyCodexTicketToHeader(header, own)
		return stripCodexTicketBodyIdentity(body), nil
	}
	if settings.Shared {
		if shared := s.pickSharedTicket(ctx, model, targetLen, account.ID, now, true); shared != nil {
			applyCodexTicketToHeader(header, shared)
			logger.L().Debug("codex ticket borrowed from shared pool",
				zap.Int64("account_id", account.ID),
				zap.Int64("source_account_id", shared.AccountID),
				zap.String("model", model))
			return stripCodexTicketBodyIdentity(body), nil
		}
	}
	if settings.FailClosed {
		return body, ErrCodexTicketUnavailable
	}
	return body, nil
}

func stripCodexTicketBodyIdentity(body []byte) []byte {
	if len(body) == 0 {
		return body
	}
	for _, key := range []string{"prompt_cache_key", "client_metadata", "device_id"} {
		if !gjson.GetBytes(body, key).Exists() {
			continue
		}
		if next, err := sjson.DeleteBytes(body, key); err == nil {
			body = next
		}
	}
	return body
}

// applyCodexTicketToHeader 注入 turn-state，并把出站会话身份对齐到打票会话。
// 上游在“出站身份”下铸造 turn-state；同一账号回放时必须清掉业务请求自带的
// 安装/线程/会话标识，否则票面与会话身份互相矛盾。
func applyCodexTicketToHeader(header http.Header, ticket *CodexTicketRecord) {
	if header == nil || ticket == nil {
		return
	}
	header.Set(openAICodexTurnStateHeader, ticket.State)
	if session := strings.TrimSpace(ticket.SessionID); session != "" {
		header.Set("session_id", session)
	}
	// 292 票由 responses-lite 探测铸造，业务请求保持同一形态。
	header.Set(responsesLiteHeaderKey, "true")
	for _, key := range []string{
		"conversation_id", "conversation-id",
		"installation_id", "x-codex-installation-id",
		"thread_id", "thread-id",
		"turn_id", "turn-id",
		"window_id", "x-codex-window-id",
		"x-client-request-id", "x-codex-turn-metadata",
		"x-codex-beta-features", "accept-language",
	} {
		header.Del(key)
	}
}

func (s *CodexTicketService) ownTicket(ctx context.Context, account *Account, model string, targetLen int, now time.Time) *CodexTicketRecord {
	if account == nil || account.ID <= 0 {
		return nil
	}
	model = normalizeCodexTicketModel(model)
	key := codexTicketCacheKey(account.ID, model)
	if raw, ok := s.tickets.Load(key); ok {
		entry, _ := raw.(codexTicketCacheEntry)
		if now.Sub(entry.loadedAt) < codexTicketTicketCacheTTL {
			if entry.record.valid(now, targetLen) && codexTicketIdentityMatches(account, &entry.record) {
				record := entry.record
				return &record
			}
			return nil
		}
	}
	record, err := s.repo.Get(ctx, account.ID, model)
	if err != nil {
		logger.L().Warn("codex ticket load failed", zap.Int64("account_id", account.ID), zap.String("model", model), zap.Error(err))
		return nil
	}
	if record == nil {
		return nil
	}
	s.tickets.Store(key, codexTicketCacheEntry{record: *record, loadedAt: now})
	if !record.valid(now, targetLen) || !codexTicketIdentityMatches(account, record) {
		return nil
	}
	return record
}

func (s *CodexTicketService) pickSharedTicket(ctx context.Context, model string, targetLen int, borrowerID int64, now time.Time, markUsed bool) *CodexTicketRecord {
	records, err := s.poolRecords(ctx, model, now)
	if err != nil {
		logger.L().Warn("codex ticket shared pool load failed", zap.String("model", model), zap.Error(err))
		return nil
	}
	type candidate struct {
		record   CodexTicketRecord
		lastUsed time.Time
	}
	candidates := make([]candidate, 0, len(records))
	for _, record := range records {
		if record.AccountID == borrowerID {
			continue
		}
		if !record.valid(now, targetLen) {
			continue
		}
		lastUsed := time.Time{}
		if raw, ok := s.sharedLastUsed.Load(codexTicketPoolKey(record.Model, record.AccountID)); ok {
			lastUsed, _ = raw.(time.Time)
		}
		candidates = append(candidates, candidate{record: record, lastUsed: lastUsed})
	}
	if len(candidates) == 0 {
		return nil
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if !candidates[i].lastUsed.Equal(candidates[j].lastUsed) {
			return candidates[i].lastUsed.Before(candidates[j].lastUsed)
		}
		return candidates[i].record.ExpiresAt.After(candidates[j].record.ExpiresAt)
	})
	selected := candidates[0].record
	if markUsed {
		s.sharedLastUsed.Store(codexTicketPoolKey(selected.Model, selected.AccountID), now)
	}
	return &selected
}

func (s *CodexTicketService) poolRecords(ctx context.Context, model string, now time.Time) ([]CodexTicketRecord, error) {
	key := normalizeCodexTicketModel(model)
	if raw, ok := s.pool.Load(key); ok {
		cache, _ := raw.(codexTicketPoolCache)
		if now.Sub(cache.loadedAt) < codexTicketPoolCacheTTL {
			return cache.records, nil
		}
	}
	records, err := s.repo.ListValid(ctx, key, now)
	if err != nil {
		return nil, err
	}
	s.pool.Store(key, codexTicketPoolCache{records: records, loadedAt: now})
	return records, nil
}

func (s *CodexTicketService) storeTicket(ctx context.Context, account *Account, record *CodexTicketRecord) error {
	if s == nil || s.repo == nil || account == nil || record == nil {
		return errors.New("invalid codex ticket store input")
	}
	record.AccountID = account.ID
	record.Model = normalizeCodexTicketModel(record.Model)
	record.Identity = codexTicketIdentity(account)
	if record.Length == 0 {
		record.Length = len(record.State)
	}
	if record.CapturedAt.IsZero() {
		record.CapturedAt = time.Now()
	}
	if err := s.repo.Upsert(ctx, record); err != nil {
		return err
	}
	s.tickets.Store(codexTicketCacheKey(account.ID, record.Model), codexTicketCacheEntry{record: *record, loadedAt: time.Now()})
	s.pool.Delete(normalizeCodexTicketModel(record.Model))
	return nil
}

// ListTickets 返回门票管理页的分页列表。
func (s *CodexTicketService) ListTickets(ctx context.Context, filter CodexTicketListFilter) (*CodexTicketListResult, error) {
	if s == nil || s.repo == nil || s.accountRepo == nil {
		return nil, errors.New("codex ticket service unavailable")
	}
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PageSize <= 0 {
		filter.PageSize = 20
	}
	if filter.PageSize > 100 {
		filter.PageSize = 100
	}
	filter.Model = normalizeCodexTicketModel(filter.Model)
	switch filter.Status {
	case "", "valid", "expired":
	default:
		filter.Status = ""
	}
	records, total, err := s.repo.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	accounts, err := s.accountRepo.ListByPlatform(ctx, PlatformOpenAI)
	names := make(map[int64]string, len(accounts))
	if err == nil {
		for i := range accounts {
			names[accounts[i].ID] = accounts[i].Name
		}
	}
	now := time.Now()
	result := &CodexTicketListResult{Items: make([]CodexTicketListItem, 0, len(records)), Total: total, Page: filter.Page, PageSize: filter.PageSize}
	for i := range records {
		record := &records[i]
		status := "expired"
		remaining := int64(0)
		if now.Before(record.ExpiresAt) && record.valid(now, 0) {
			status = "valid"
			remaining = int64(record.remaining(now) / time.Second)
		}
		result.Items = append(result.Items, CodexTicketListItem{
			ID: record.ID, AccountID: record.AccountID, AccountName: names[record.AccountID],
			Model: record.Model, Status: status, Length: record.Length, Blocks: record.Blocks,
			Acquisition: record.Acquisition, CapturedAt: record.CapturedAt, ExpiresAt: record.ExpiresAt,
			RemainingSeconds: remaining,
		})
	}
	return result, nil
}

// DeleteTicket 删除一张票并从进程缓存中移除。
func (s *CodexTicketService) DeleteTicket(ctx context.Context, id int64) error {
	if s == nil || s.repo == nil {
		return errors.New("codex ticket service unavailable")
	}
	record, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if record == nil {
		return errors.New("codex ticket not found")
	}
	if err := s.repo.DeleteByID(ctx, id); err != nil {
		return err
	}
	s.tickets.Delete(codexTicketCacheKey(record.AccountID, record.Model))
	s.pool.Delete(normalizeCodexTicketModel(record.Model))
	return nil
}

// RefreshTicket 对指定门票重新打票（强制刷新，不受剩余时间阈值影响）。
func (s *CodexTicketService) RefreshTicket(ctx context.Context, id int64) (*CodexTicketHarvestSummary, error) {
	if s == nil || s.repo == nil {
		return nil, errors.New("codex ticket service unavailable")
	}
	record, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, errors.New("codex ticket not found")
	}
	return s.harvestAccount(ctx, record.AccountID, []string{record.Model}, true)
}

func (s *CodexTicketService) StatusForAccountID(ctx context.Context, accountID int64) (*CodexTicketAccountStatus, error) {
	if s == nil || s.accountRepo == nil {
		return nil, errors.New("codex ticket service unavailable")
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, ErrAccountNotFound
	}
	status := s.statusForAccount(ctx, account)
	return &status, nil
}

func (s *CodexTicketService) statusForAccount(ctx context.Context, account *Account) CodexTicketAccountStatus {
	status := CodexTicketAccountStatus{ID: account.ID, Name: account.Name, Status: account.Status, Platform: account.Platform, Tickets: make([]CodexTicketModelStatus, 0, 2), IQ: s.LastIQResult(account.ID)}
	settings := s.Settings(ctx)
	now := time.Now()
	targetLen := codexTicketExpectedLength(account)
	for _, model := range settings.Models {
		modelStatus := CodexTicketModelStatus{Model: model}
		if own := s.ownTicket(ctx, account, model, targetLen, now); own != nil {
			modelStatus.Ready = true
			modelStatus.Own = true
			modelStatus.Length = own.Length
			modelStatus.Blocks = own.Blocks
			modelStatus.RemainingSeconds = int64(own.remaining(now) / time.Second)
			expires, captured := own.ExpiresAt, own.CapturedAt
			modelStatus.ExpiresAt, modelStatus.CapturedAt = &expires, &captured
		} else if settings.Shared {
			if shared := s.pickSharedTicket(ctx, model, targetLen, account.ID, now, false); shared != nil {
				modelStatus.Ready = true
				modelStatus.Shared = true
				modelStatus.SourceAccountID = shared.AccountID
				modelStatus.SourceAccountName = s.accountName(ctx, shared.AccountID)
				modelStatus.Length = shared.Length
				modelStatus.Blocks = shared.Blocks
				modelStatus.RemainingSeconds = int64(shared.remaining(now) / time.Second)
				expires, captured := shared.ExpiresAt, shared.CapturedAt
				modelStatus.ExpiresAt, modelStatus.CapturedAt = &expires, &captured
			}
		}
		status.Tickets = append(status.Tickets, modelStatus)
	}
	return status
}

// Overview 返回管理端打票页所需的设置、账号状态与共享池列表。
func (s *CodexTicketService) Overview(ctx context.Context) (*CodexTicketOverview, error) {
	if s == nil || s.accountRepo == nil || s.repo == nil {
		return nil, errors.New("codex ticket service unavailable")
	}
	settings := s.Settings(ctx)
	now := time.Now()
	accounts, err := s.accountRepo.ListByPlatform(ctx, PlatformOpenAI)
	if err != nil {
		return nil, err
	}
	validTickets, err := s.repo.ListValid(ctx, "", now)
	if err != nil {
		return nil, err
	}
	accountNames := make(map[int64]string, len(accounts))
	for i := range accounts {
		accountNames[accounts[i].ID] = accounts[i].Name
	}
	byAccount := make(map[int64][]CodexTicketRecord)
	for _, record := range validTickets {
		byAccount[record.AccountID] = append(byAccount[record.AccountID], record)
	}

	overview := &CodexTicketOverview{Settings: settings, Accounts: make([]CodexTicketAccountStatus, 0, 64), Pool: make([]CodexTicketPoolEntry, 0, len(validTickets))}
	if unix := s.lastRoundUnix.Load(); unix > 0 {
		last := time.Unix(unix, 0)
		overview.LastRoundAt = &last
	}
	for i := range accounts {
		account := &accounts[i]
		if !codexTicketHarvestEligible(account) {
			continue
		}
		accountStatus := CodexTicketAccountStatus{ID: account.ID, Name: account.Name, Status: account.Status, Platform: account.Platform, Tickets: make([]CodexTicketModelStatus, 0, len(settings.Models)), IQ: s.LastIQResult(account.ID)}
		targetLen := codexTicketExpectedLength(account)
		own := byAccount[account.ID]
		for _, model := range settings.Models {
			modelStatus := CodexTicketModelStatus{Model: model}
			for j := range own {
				record := &own[j]
				if normalizeCodexTicketModel(record.Model) != normalizeCodexTicketModel(model) {
					continue
				}
				if record.valid(now, targetLen) && codexTicketIdentityMatches(account, record) {
					modelStatus.Ready, modelStatus.Own = true, true
					modelStatus.Length, modelStatus.Blocks = record.Length, record.Blocks
					modelStatus.RemainingSeconds = int64(record.remaining(now) / time.Second)
					expires, captured := record.ExpiresAt, record.CapturedAt
					modelStatus.ExpiresAt, modelStatus.CapturedAt = &expires, &captured
				}
				break
			}
			if !modelStatus.Ready && settings.Shared {
				if shared := s.pickSharedTicket(ctx, model, targetLen, account.ID, now, false); shared != nil {
					modelStatus.Ready, modelStatus.Shared = true, true
					modelStatus.SourceAccountID = shared.AccountID
					modelStatus.SourceAccountName = accountNames[shared.AccountID]
					modelStatus.Length, modelStatus.Blocks = shared.Length, shared.Blocks
					modelStatus.RemainingSeconds = int64(shared.remaining(now) / time.Second)
					expires, captured := shared.ExpiresAt, shared.CapturedAt
					modelStatus.ExpiresAt, modelStatus.CapturedAt = &expires, &captured
				}
			}
			accountStatus.Tickets = append(accountStatus.Tickets, modelStatus)
		}
		overview.Accounts = append(overview.Accounts, accountStatus)
	}
	for _, record := range validTickets {
		if !record.valid(now, 0) {
			continue
		}
		overview.Pool = append(overview.Pool, CodexTicketPoolEntry{
			Model:             record.Model,
			SourceAccountID:   record.AccountID,
			SourceAccountName: accountNames[record.AccountID],
			Length:            record.Length,
			Blocks:            record.Blocks,
			Acquisition:       record.Acquisition,
			CapturedAt:        record.CapturedAt,
			ExpiresAt:         record.ExpiresAt,
			RemainingSeconds:  int64(record.remaining(now) / time.Second),
		})
	}
	sort.SliceStable(overview.Pool, func(i, j int) bool {
		if overview.Pool[i].Model != overview.Pool[j].Model {
			return overview.Pool[i].Model < overview.Pool[j].Model
		}
		return overview.Pool[i].RemainingSeconds > overview.Pool[j].RemainingSeconds
	})
	return overview, nil
}

func (s *CodexTicketService) accountName(ctx context.Context, accountID int64) string {
	if s.accountRepo == nil || accountID <= 0 {
		return ""
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil || account == nil {
		return ""
	}
	return account.Name
}

func codexTicketCacheKey(accountID int64, model string) string {
	return fmt.Sprintf("%d\x00%s", accountID, normalizeCodexTicketModel(model))
}

func codexTicketPoolKey(model string, accountID int64) string {
	return fmt.Sprintf("%s\x00%d", normalizeCodexTicketModel(model), accountID)
}

func codexTicketIdentity(account *Account) string {
	if account == nil {
		return ""
	}
	identity := fmt.Sprintf("%d\x00%s\x00%s", account.ID, account.GetChatGPTAccountID(), account.GetCredential("email"))
	sum := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(sum[:])
}

func codexTicketIdentityMatches(account *Account, ticket *CodexTicketRecord) bool {
	if ticket == nil {
		return false
	}
	if strings.TrimSpace(ticket.Identity) == "" {
		return true
	}
	return ticket.Identity == codexTicketIdentity(account)
}

func codexTicketTeamPlan(account *Account) bool {
	if account == nil {
		return false
	}
	plan := strings.ToLower(strings.TrimSpace(account.GetCredential("plan_type")))
	for _, marker := range []string{"team", "business", "enterprise"} {
		if strings.Contains(plan, marker) {
			return true
		}
	}
	return false
}

func codexTicketExpectedBlocks(account *Account) int {
	if codexTicketTeamPlan(account) {
		return codexTicketTeamBlocks
	}
	return codexTicketPersonalBlocks
}

func codexTicketExpectedLength(account *Account) int {
	return base64.URLEncoding.EncodedLen(57 + 16*codexTicketExpectedBlocks(account))
}

type codexTicketShape struct {
	Blocks   int
	IssuedAt time.Time
}

func parseCodexTicketShape(value string) (codexTicketShape, error) {
	value = strings.TrimSpace(value)
	if len(value) > 2048 || strings.ContainsAny(value, "\r\n\t ") {
		return codexTicketShape{}, errors.New("invalid state encoding")
	}
	core := strings.TrimRight(value, "=")
	if len(value)-len(core) > 2 {
		return codexTicketShape{}, errors.New("invalid state padding")
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(core)
	if err != nil || len(raw) < 73 || raw[0] != 0x80 || (len(raw)-57)%16 != 0 {
		return codexTicketShape{}, errors.New("unrecognized state envelope")
	}
	issuedUnix := binary.BigEndian.Uint64(raw[1:9])
	if issuedUnix < 1577836800 || issuedUnix >= 4102444800 {
		return codexTicketShape{}, errors.New("state timestamp out of range")
	}
	return codexTicketShape{Blocks: (len(raw) - 57) / 16, IssuedAt: time.Unix(int64(issuedUnix), 0)}, nil
}

func codexTicketHarvestEligible(account *Account) bool {
	if account == nil || account.ID <= 0 || !account.IsActive() || account.IsShadow() {
		return false
	}
	if account.Platform != PlatformOpenAI || account.Type != AccountTypeOAuth {
		return false
	}
	if !account.UsesOpenAICodexProtocol() || account.IsOpenAIAgentIdentity() {
		return false
	}
	now := time.Now()
	if account.ExpiresAt != nil && !now.Before(*account.ExpiresAt) {
		return false
	}
	if account.IsRateLimited() || account.IsOverloaded() {
		return false
	}
	return true
}
