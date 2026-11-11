package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	codexTicketMaxProbesPerRound = 50
	codexTicketHarvestTick       = 30 * time.Second
	codexTicketProbeBodyMax      = 1 << 20
	codexAstraMinVersion         = "0.153.4"
)

// CodexTicketHarvestResult 是单次打票的结果，可直接返回管理端。
type CodexTicketHarvestResult struct {
	AccountID    int64  `json:"account_id"`
	AccountName  string `json:"account_name,omitempty"`
	Model        string `json:"model"`
	OK           bool   `json:"ok"`
	AlreadyReady bool   `json:"already_ready,omitempty"`
	Status       int    `json:"status,omitempty"`
	Length       int    `json:"length,omitempty"`
	Blocks       int    `json:"blocks,omitempty"`
	Error        string `json:"error,omitempty"`
}

// CodexTicketHarvestSummary 是一轮打票的汇总。
type CodexTicketHarvestSummary struct {
	StartedAt  time.Time                  `json:"started_at"`
	FinishedAt time.Time                  `json:"finished_at"`
	Probed     int                        `json:"probed"`
	Succeeded  int                        `json:"succeeded"`
	Skipped    int                        `json:"skipped"`
	Failed     int                        `json:"failed"`
	Results    []CodexTicketHarvestResult `json:"results"`
}

func (s *CodexTicketService) harvestLoop(ctx context.Context) {
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-timer.C:
	}
	ticker := time.NewTicker(codexTicketHarvestTick)
	defer ticker.Stop()
	for {
		s.maybeHarvestRound(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *CodexTicketService) maybeHarvestRound(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	settings := s.Settings(ctx)
	if !settings.Enabled {
		return
	}
	interval := time.Duration(settings.HarvestIntervalSeconds) * time.Second
	last := s.lastRoundUnix.Load()
	if last > 0 && time.Since(time.Unix(last, 0)) < interval {
		return
	}
	if _, err := s.HarvestRound(ctx); err != nil {
		logger.L().Debug("codex ticket harvest round skipped", zap.Error(err))
	}
}

// HarvestRound 执行一轮后台打票：只处理可调度、缺票或临近过期的账号/模型。
func (s *CodexTicketService) HarvestRound(ctx context.Context) (*CodexTicketHarvestSummary, error) {
	if s == nil || s.repo == nil || s.accountRepo == nil || s.tokenProvider == nil || s.upstream == nil {
		return nil, errors.New("codex ticket service unavailable")
	}
	if !s.roundMu.TryLock() {
		return nil, errors.New("codex ticket harvest already running")
	}
	defer s.roundMu.Unlock()

	settings := s.Settings(ctx)
	summary := &CodexTicketHarvestSummary{StartedAt: time.Now(), Results: make([]CodexTicketHarvestResult, 0, 8)}
	s.lastRoundUnix.Store(time.Now().Unix())

	accounts, err := s.accountRepo.ListByPlatform(ctx, PlatformOpenAI)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	type job struct {
		account *Account
		model   string
	}
	jobs := make([]job, 0, 16)
	for i := range accounts {
		account := &accounts[i]
		if !codexTicketHarvestEligible(account) || len(jobs) >= codexTicketMaxProbesPerRound {
			continue
		}
		targetLen := codexTicketExpectedLength(account)
		for _, model := range settings.Models {
			if len(jobs) >= codexTicketMaxProbesPerRound {
				break
			}
			if own := s.ownTicket(ctx, account, model, targetLen, now); own != nil && own.remaining(now) > codexTicketRefreshBefore {
				summary.Skipped++
				continue
			}
			jobs = append(jobs, job{account: account, model: model})
		}
	}
	if len(jobs) == 0 {
		summary.FinishedAt = time.Now()
		return summary, nil
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	jobCh := make(chan job)
	resultCh := make(chan CodexTicketHarvestResult, len(jobs))
	var wg sync.WaitGroup
	for i := 0; i < codexTicketHarvestWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range jobCh {
				resultCh <- s.probeOne(ctx, item.account, item.model, settings)
			}
		}()
	}
	go func() {
		defer close(jobCh)
		for _, item := range jobs {
			select {
			case <-ctx.Done():
				return
			case jobCh <- item:
			}
		}
	}()
	go func() {
		wg.Wait()
		close(resultCh)
	}()
	for result := range resultCh {
		summary.Probed++
		if result.OK {
			summary.Succeeded++
		} else if !result.AlreadyReady {
			summary.Failed++
		}
		summary.Results = append(summary.Results, result)
	}
	summary.FinishedAt = time.Now()
	if _, err := s.repo.DeleteExpired(ctx, time.Now()); err != nil {
		logger.L().Warn("codex ticket delete expired failed", zap.Error(err))
	}
	return summary, nil
}

// HarvestAccount 为单个账号立即打票（管理端手动触发）。
func (s *CodexTicketService) HarvestAccount(ctx context.Context, accountID int64, models []string) (*CodexTicketHarvestSummary, error) {
	return s.harvestAccount(ctx, accountID, models, false)
}

func (s *CodexTicketService) harvestAccount(ctx context.Context, accountID int64, models []string, force bool) (*CodexTicketHarvestSummary, error) {
	if s == nil || s.accountRepo == nil || s.repo == nil {
		return nil, errors.New("codex ticket service unavailable")
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, ErrAccountNotFound
	}
	if !codexTicketHarvestEligible(account) {
		return nil, errors.New("account is not eligible for codex ticket harvest")
	}
	settings := s.Settings(ctx)
	if len(models) == 0 {
		models = settings.Models
	}
	if len(models) > 16 {
		models = models[:16]
	}
	if !s.roundMu.TryLock() {
		return nil, errors.New("codex ticket harvest already running")
	}
	defer s.roundMu.Unlock()

	summary := &CodexTicketHarvestSummary{StartedAt: time.Now(), Results: make([]CodexTicketHarvestResult, 0, len(models))}
	targetLen := codexTicketExpectedLength(account)
	now := time.Now()
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		if own := s.ownTicket(ctx, account, model, targetLen, now); !force && own != nil && own.remaining(now) > codexTicketRefreshBefore {
			summary.Skipped++
			summary.Results = append(summary.Results, CodexTicketHarvestResult{
				AccountID: account.ID, AccountName: account.Name, Model: model, OK: true, AlreadyReady: true,
				Length: own.Length, Blocks: own.Blocks,
			})
			continue
		}
		result := s.probeOne(ctx, account, model, settings)
		summary.Probed++
		if result.OK {
			summary.Succeeded++
		} else {
			summary.Failed++
		}
		summary.Results = append(summary.Results, result)
	}
	summary.FinishedAt = time.Now()
	return summary, nil
}

func (s *CodexTicketService) probeOne(ctx context.Context, account *Account, model string, settings CodexTicketSettings) CodexTicketHarvestResult {
	model = normalizeCodexTicketModel(model)
	result := CodexTicketHarvestResult{AccountID: account.ID, AccountName: account.Name, Model: model}
	if account == nil || account.ID <= 0 {
		result.Error = "invalid account"
		return result
	}
	key := codexTicketCacheKey(account.ID, model)
	if raw, ok := s.probeCooldown.Load(key); ok {
		if until, _ := raw.(time.Time); time.Now().Before(until) {
			result.Error = "probe cooling down"
			return result
		}
	}
	defer func() {
		if !result.OK {
			s.probeCooldown.Store(key, time.Now().Add(codexTicketProbeCooldown))
		}
	}()

	token, err := s.tokenProvider.GetAccessToken(ctx, account)
	if err != nil || strings.TrimSpace(token) == "" {
		result.Error = "access token unavailable"
		return result
	}
	timeout := time.Duration(settings.ProbeTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = codexTicketDefaultProbeTimeout * time.Second
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	body := []byte(`{"model":` + codexTicketJSONString(model) + `,"store":false,"stream":true,"instructions":"Reply with exactly: pong. Do not call tools.","parallel_tool_calls":false,"include":["reasoning.encrypted_content"],"reasoning":{"context":"all_turns"},"input":[{"type":"additional_tools","role":"developer","tools":[{"type":"namespace","name":"codex","description":"local tools","tools":[{"type":"function","name":"noop","description":"Do nothing.","strict":false,"parameters":{"type":"object","properties":{},"additionalProperties":false}}]}]},{"role":"user","content":[{"type":"input_text","text":"ping"}]}]}`)
	req, err := http.NewRequestWithContext(probeCtx, http.MethodPost, chatgptCodexURL, bytes.NewReader(body))
	if err != nil {
		result.Error = err.Error()
		return result
	}
	req.Host = "chatgpt.com"
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("OpenAI-Beta", "responses=experimental")
	sessionID := uuid.NewString()
	req.Header.Set("session_id", sessionID)
	req.Header.Set(responsesLiteHeaderKey, "true")
	if err := resolveAndSetOpenAIChatGPTAccountHeaders(probeCtx, s.accountRepo, req.Header, account); err != nil {
		result.Error = err.Error()
		return result
	}
	applyOpenAICodexProbeHeaders(req.Header)
	enforceCodexIdentityHeaders(req.Header)
	if codexTicketAstraModel(model) {
		if CompareVersions(req.Header.Get("version"), codexAstraMinVersion) < 0 {
			req.Header.Set("version", codexAstraMinVersion)
			req.Header.Set("user-agent", buildCodexCLIUserAgent(codexAstraMinVersion))
		}
	}
	proxyURL := codexTicketProxyURL(account)
	resp, err := s.upstream.Do(req, proxyURL, account.ID, account.Concurrency)
	if err != nil {
		result.Error = "upstream request failed: " + err.Error()
		return result
	}
	if resp == nil {
		result.Error = "nil upstream response"
		return result
	}
	defer func() { _ = resp.Body.Close() }()
	result.Status = resp.StatusCode
	state := strings.TrimSpace(resp.Header.Get(openAICodexTurnStateHeader))
	var responseBody []byte
	if resp.Body != nil {
		responseBody, _ = io.ReadAll(io.LimitReader(resp.Body, codexTicketProbeBodyMax+1))
	}
	if resp.StatusCode != http.StatusOK {
		result.Error = "upstream status " + http.StatusText(resp.StatusCode)
		return result
	}
	if state == "" {
		result.Error = "upstream returned no turn-state"
		return result
	}
	if len(responseBody) > codexTicketProbeBodyMax {
		result.Error = "probe response too large"
		return result
	}
	if err := validateCodexTicketProbeResponse(responseBody, model); err != nil {
		result.Error = err.Error()
		return result
	}
	shape, err := parseCodexTicketShape(state)
	if err != nil {
		result.Error = "invalid turn-state: " + err.Error()
		return result
	}
	expectedLen := codexTicketExpectedLength(account)
	if len(state) != expectedLen || shape.Blocks != codexTicketExpectedBlocks(account) {
		result.Error = "turn-state length does not match account plan"
		return result
	}
	now := time.Now()
	expires := now.Add(time.Duration(settings.TTLSeconds) * time.Second)
	if issuedExpiry := shape.IssuedAt.Add(codexTicketStateLifetime); issuedExpiry.Before(expires) {
		expires = issuedExpiry
	}
	record := &CodexTicketRecord{
		Model:       model,
		State:       state,
		Length:      len(state),
		Transport:   "sse",
		Blocks:      shape.Blocks,
		IssuedAt:    shape.IssuedAt,
		CapturedAt:  now,
		ExpiresAt:   expires,
		Acquisition: "harvest",
		SessionID:   sessionID,
	}
	if err := s.storeTicket(ctx, account, record); err != nil {
		result.Error = "persist ticket failed: " + err.Error()
		return result
	}
	result.OK = true
	result.Length = record.Length
	result.Blocks = record.Blocks
	s.probeCooldown.Delete(key)
	logger.L().Info("codex ticket harvested",
		zap.Int64("account_id", account.ID),
		zap.String("model", model),
		zap.Int("length", record.Length),
		zap.Int("blocks", record.Blocks),
		zap.Duration("ttl", time.Until(record.ExpiresAt)))
	return result
}

func codexTicketJSONString(value string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return `""`
	}
	return string(encoded)
}

func codexTicketAstraModel(model string) bool {
	model = strings.ToLower(model)
	return strings.Contains(model, "gpt-6") || strings.Contains(model, "astra")
}

func codexTicketProxyURL(account *Account) string {
	if account == nil || account.ProxyID == nil || account.Proxy == nil {
		return ""
	}
	return account.Proxy.URL()
}

// validateCodexTicketProbeResponse 复刻上游 SSE 语义校验：只有
// response.completed 才算打票成功，避免把 200 + 错误事件当成有效票。
func validateCodexTicketProbeResponse(body []byte, expectedModel string) error {
	completed := false
	inspect := func(data []byte) error {
		if bytes.Equal(bytes.TrimSpace(data), []byte("[DONE]")) {
			return nil
		}
		var event struct {
			Model    string          `json:"model"`
			Type     string          `json:"type"`
			Status   string          `json:"status"`
			Error    json.RawMessage `json:"error"`
			Response *struct {
				Model  string `json:"model"`
				Status string `json:"status"`
			} `json:"response"`
		}
		if err := json.Unmarshal(data, &event); err != nil {
			return errors.New("invalid probe event")
		}
		if expectedModel != "" {
			models := []string{event.Model}
			if event.Response != nil {
				models = append(models, event.Response.Model)
			}
			for _, served := range models {
				if served != "" && normalizeCodexTicketModel(served) != normalizeCodexTicketModel(expectedModel) {
					return errors.New("probe response model mismatch")
				}
			}
		}
		if event.Type == "error" || event.Type == "response.failed" || event.Type == "response.incomplete" ||
			event.Status == "failed" || event.Status == "incomplete" || (len(event.Error) > 0 && string(event.Error) != "null") {
			return errors.New("probe response failed")
		}
		if event.Response != nil && (event.Response.Status == "failed" || event.Response.Status == "incomplete") {
			return errors.New("probe response failed")
		}
		if event.Type == "response.completed" {
			if event.Response == nil || event.Response.Status != "completed" {
				return errors.New("invalid probe completion")
			}
			completed = true
		}
		if event.Type == "" && event.Status == "completed" {
			completed = true
		}
		return nil
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) > 0 && trimmed[0] == '{' {
		if err := inspect(trimmed); err != nil {
			return err
		}
	} else {
		scanner := bufio.NewScanner(bytes.NewReader(body))
		scanner.Buffer(make([]byte, 4096), 1<<20)
		var data []string
		flush := func() error {
			if len(data) == 0 {
				return nil
			}
			err := inspect([]byte(strings.Join(data, "\n")))
			data = nil
			return err
		}
		for scanner.Scan() {
			line := strings.TrimSuffix(scanner.Text(), "\r")
			if line == "" {
				if err := flush(); err != nil {
					return err
				}
				continue
			}
			if strings.HasPrefix(line, "data:") {
				data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
			}
		}
		if scanner.Err() != nil {
			return errors.New("invalid probe stream")
		}
		if len(data) > 0 {
			return errors.New("unterminated probe event")
		}
	}
	if !completed {
		return errors.New("probe did not complete successfully")
	}
	return nil
}
