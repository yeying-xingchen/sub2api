package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Codex 智商测试（门票状态探针）。
//
// 判据来自上游的 turn-state 行为：先裸发一发极小请求打一张票 S1，再带上
// S1（以及上游下发的负载均衡 Cookie）续接一发。上游如果回了新的票 S2，
// 说明账号可能已被降到低智力档位；没有回新票（或回原票）则视为满血。
// 两发都必须 200 且回复流完整结束，否则只给「无法判断」，不误伤账号。

const (
	CodexIQVerdictHealthy      = "healthy"
	CodexIQVerdictDegraded     = "degraded"
	CodexIQVerdictInconclusive = "inconclusive"

	CodexIQFailureUnsupported      = "unsupported"
	CodexIQFailureAccountError     = "account_error"
	CodexIQFailureRateLimited      = "rate_limited"
	CodexIQFailureModelUnsupported = "model_unsupported"
	CodexIQFailureUpstreamError    = "upstream_error"
	CodexIQFailureNetworkError     = "network_error"
	CodexIQFailureStreamError      = "stream_error"
	CodexIQFailureNoTicket         = "no_ticket"
	CodexIQFailureCancelled        = "cancelled"

	codexIQDefaultModel  = "gpt-6-astra"
	codexIQShotTimeout   = 45 * time.Second
	codexIQMaxBody       = 1 << 20
	codexIQMaxDetailByte = 300
)

// ErrCodexIQProbeBusy 表示该账号已有一轮智商测试在进行。
var ErrCodexIQProbeBusy = errors.New("codex IQ probe already running for this account")

// CodexIQTestResult 是一次智商测试的完整结果，可直接返回管理端。
type CodexIQTestResult struct {
	AccountID int64  `json:"account_id"`
	Model     string `json:"model"`
	Verdict   string `json:"verdict"`
	Reason    string `json:"reason"`
	Failure   string `json:"failure,omitempty"`
	Detail    string `json:"detail,omitempty"`

	MintStatus     int  `json:"mint_status"`
	ContinueStatus int  `json:"continue_status"`
	Minted         bool `json:"minted"`
	NewTicket      bool `json:"new_ticket"`
	TicketLength   int  `json:"ticket_length"`
	ContinueLength int  `json:"continue_ticket_length"`
	ShortTicket    bool `json:"short_ticket,omitempty"`

	LatencyMs int64     `json:"latency_ms"`
	TestedAt  time.Time `json:"tested_at"`
}

type codexIQShot struct {
	status    int
	state     string
	cookies   []string
	streamErr error
	detail    string
}

// ProbeIQ 对单个账号执行一次智商测试。只读：不写票库、不改账号、不影响调度。
func (s *CodexTicketService) ProbeIQ(ctx context.Context, accountID int64, model string) (*CodexIQTestResult, error) {
	if s == nil || s.accountRepo == nil || s.tokenProvider == nil || s.upstream == nil {
		return nil, errors.New("codex IQ probe unavailable")
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, ErrAccountNotFound
	}
	if reason := codexIQUnsupportedReason(account, model); reason != "" {
		return nil, errors.New(reason)
	}
	if model = strings.TrimSpace(model); model == "" {
		model = codexIQDefaultModel
		if models := s.Settings(ctx).Models; len(models) > 0 {
			model = models[0]
		}
	}
	unlock, ok := s.lockIQProbe(accountID)
	if !ok {
		return nil, ErrCodexIQProbeBusy
	}
	defer unlock()

	result := &CodexIQTestResult{AccountID: accountID, Verdict: CodexIQVerdictInconclusive, TestedAt: time.Now()}
	started := time.Now()
	defer func() {
		result.LatencyMs = time.Since(started).Milliseconds()
		if result.Reason == "" {
			result.Reason = codexIQVerdictReason(result.Verdict)
		}
		s.iqResults.Store(accountID, *result)
	}()

	upstreamModel := normalizeOpenAIModelForUpstream(account, account.GetMappedModel(model))
	result.Model = upstreamModel

	token, err := s.tokenProvider.GetAccessToken(ctx, account)
	if err != nil || strings.TrimSpace(token) == "" {
		result.fail(CodexIQFailureAccountError, "账号认证失败，无法取得访问令牌", safeCodexIQDetail(err))
		return result, nil
	}
	proxy := codexTicketProxyURL(account)

	fire := func(turnState, cookie string) (codexIQShot, error) {
		return s.fireCodexIQShot(ctx, account, token, upstreamModel, proxy, turnState, cookie)
	}
	s.runCodexIQProbe(ctx, result, fire)
	return result, nil
}

// LastIQResult 返回账号最近一次智商测试结果（进程内缓存）。
func (s *CodexTicketService) LastIQResult(accountID int64) *CodexIQTestResult {
	if s == nil || accountID <= 0 {
		return nil
	}
	raw, ok := s.iqResults.Load(accountID)
	if !ok {
		return nil
	}
	result, _ := raw.(CodexIQTestResult)
	return &result
}

func (s *CodexTicketService) lockIQProbe(accountID int64) (func(), bool) {
	if _, loaded := s.iqRunning.LoadOrStore(accountID, struct{}{}); loaded {
		return func() {}, false
	}
	return func() { s.iqRunning.Delete(accountID) }, true
}

func (s *CodexTicketService) runCodexIQProbe(ctx context.Context, result *CodexIQTestResult, fire func(string, string) (codexIQShot, error)) {
	mint, err := fire("", "")
	result.MintStatus = mint.status
	result.TicketLength = len(mint.state)
	if !result.shotUsable(ctx, "打票", mint, err) {
		return
	}
	if mint.state == "" {
		result.fail(CodexIQFailureNoTicket, "打票成功但上游没有返回 x-codex-turn-state，无法续接判断", "")
		return
	}
	result.Minted = true
	result.ShortTicket = len(mint.state) < 780

	cookie := strings.Join(mint.cookies, "; ")
	cont, err := fire(mint.state, cookie)
	result.ContinueStatus = cont.status
	result.ContinueLength = len(cont.state)
	if !result.shotUsable(ctx, "门票续接", cont, err) {
		return
	}
	result.NewTicket = cont.state != "" && cont.state != mint.state
	if result.NewTicket {
		result.Verdict = CodexIQVerdictDegraded
		result.Reason = codexIQVerdictReason(CodexIQVerdictDegraded)
	} else {
		result.Verdict = CodexIQVerdictHealthy
		result.Reason = codexIQVerdictReason(CodexIQVerdictHealthy)
	}
	if result.ShortTicket {
		result.Reason += "（本次为短票形态，结论仅供参考）"
	}
}

func (r *CodexIQTestResult) fail(kind, reason, detail string) {
	r.Verdict = CodexIQVerdictInconclusive
	r.Failure = kind
	r.Reason = reason
	r.Detail = detail
}

func (r *CodexIQTestResult) shotUsable(ctx context.Context, step string, shot codexIQShot, err error) bool {
	if err != nil {
		if ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded) {
			r.fail(CodexIQFailureCancelled, step+"请求被取消或超时", "")
			return false
		}
		r.fail(CodexIQFailureNetworkError, step+"请求失败（网络或代理异常）", safeCodexIQDetail(err))
		return false
	}
	switch {
	case shot.status == http.StatusOK:
		if shot.streamErr != nil {
			r.fail(CodexIQFailureStreamError, step+"返回 200 但回复流没有正常结束（上游过载或额度用尽等），本次无法判断", shot.detail)
			return false
		}
		return true
	case shot.status == http.StatusUnauthorized || shot.status == http.StatusForbidden:
		r.fail(CodexIQFailureAccountError, fmt.Sprintf("%s被上游拒绝（HTTP %d），账号令牌可能失效或被封", step, shot.status), shot.detail)
	case shot.status == http.StatusTooManyRequests:
		r.fail(CodexIQFailureRateLimited, step+"被上游限流（HTTP 429），请稍后再试", shot.detail)
	case shot.status == http.StatusBadRequest && strings.Contains(strings.ToLower(shot.detail), "not supported"):
		r.fail(CodexIQFailureModelUnsupported, "该账号的套餐不支持这个模型（HTTP 400），请换一个模型再测", shot.detail)
	default:
		r.fail(CodexIQFailureUpstreamError, fmt.Sprintf("%s返回异常状态（HTTP %d）", step, shot.status), shot.detail)
	}
	return false
}

func codexIQVerdictReason(verdict string) string {
	switch verdict {
	case CodexIQVerdictDegraded:
		return "带原门票续接时上游回了新的门票（x-codex-turn-state），符合降智特征：账号很可能已被降到低智力档位，建议尽快换号或降低权重。"
	case CodexIQVerdictHealthy:
		return "带原门票续接时上游没有回新门票，门票延续正常：账号仍处于满血状态。"
	default:
		return "未取得有效的门票续接探测结果，无法判断账号是否降智。"
	}
}

func codexIQUnsupportedReason(account *Account, model string) string {
	switch {
	case account == nil:
		return "账号不存在"
	case !account.IsOpenAIOAuthLike():
		return "智商测试只适用于 OpenAI ChatGPT 订阅（OAuth）账号"
	case account.IsSyntheticUITest():
		return "测试数据账号不向上游发真实请求，智商测试不适用"
	case account.IsOpenAIAgentIdentity():
		return "Agent Identity 账号不使用 turn-state，智商测试不适用"
	}
	return ""
}

func (s *CodexTicketService) fireCodexIQShot(ctx context.Context, account *Account, token, model, proxy, turnState, cookie string) (codexIQShot, error) {
	var out codexIQShot
	headers := make(http.Header)
	headers.Set("Authorization", "Bearer "+token)
	if err := resolveAndSetOpenAIChatGPTAccountHeaders(ctx, s.accountRepo, headers, account); err != nil {
		return out, err
	}
	applyCodexIQIdentity(headers, model)

	shotCtx, cancel := context.WithTimeout(ctx, codexIQShotTimeout)
	defer cancel()
	body := []byte(`{"model":` + codexTicketJSONString(model) + `,"instructions":"Reply with OK.","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"Reply with OK."}]}],"stream":true,"store":false,"parallel_tool_calls":true,"include":["reasoning.encrypted_content"]}`)
	req, err := http.NewRequestWithContext(shotCtx, http.MethodPost, chatgptCodexURL, bytes.NewReader(body))
	if err != nil {
		return out, err
	}
	req = req.WithContext(WithHTTPUpstreamProfile(req.Context(), HTTPUpstreamProfileOpenAI))
	req.Close = true
	req.Host = "chatgpt.com"
	req.Header = headers.Clone()
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("OpenAI-Beta", "responses=experimental")
	req.Header.Set("session_id", uuid.NewString())
	// 探针不携带业务会话、lite 形态或旧票，避免把业务身份带进判据。
	for _, name := range []string{
		responsesLiteHeaderKey, openAICodexTurnStateHeader, "Cookie",
		"Content-Length", "Content-Encoding", "conversation_id", "x-codex-turn-metadata",
	} {
		req.Header.Del(name)
	}
	if turnState = strings.TrimSpace(turnState); turnState != "" {
		req.Header.Set(openAICodexTurnStateHeader, turnState)
	}
	if cookie = strings.TrimSpace(cookie); cookie != "" {
		req.Header.Set("Cookie", cookie)
	}

	resp, err := s.upstream.Do(req, proxy, account.ID, account.Concurrency)
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		return out, err
	}
	if resp == nil {
		return out, errors.New("nil upstream response")
	}
	defer func() { _ = resp.Body.Close() }()
	out.status = resp.StatusCode
	out.state = strings.TrimSpace(resp.Header.Get(openAICodexTurnStateHeader))
	out.cookies = codexIQRouteCookies(resp)
	if resp.Body == nil {
		out.streamErr = errors.New("probe response body missing")
		return out, nil
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, codexIQMaxBody+1))
	if err != nil {
		if shotCtx.Err() != nil {
			return out, shotCtx.Err()
		}
		out.streamErr = errors.New("probe response incomplete")
		return out, nil
	}
	if len(data) > codexIQMaxBody {
		out.streamErr = errors.New("probe response too large")
		return out, nil
	}
	if out.status != http.StatusOK {
		out.detail = truncateString(string(data), codexIQMaxDetailByte)
		return out, nil
	}
	// 降智时 response.model 仍可能回报 astra/luna 之外的名字，模型名不作判据。
	if err := validateCodexTicketProbeResponse(data, ""); err != nil {
		out.streamErr = err
		out.detail = truncateString(string(data), codexIQMaxDetailByte)
	}
	return out, nil
}

// codexIQRouteCookies 只保留上游负载均衡路由 Cookie；续接门票需要同一路由。
func codexIQRouteCookies(resp *http.Response) []string {
	if resp == nil {
		return nil
	}
	out := make([]string, 0, 2)
	seen := map[string]bool{}
	for _, cookie := range resp.Cookies() {
		if cookie.Name != "__cflb" && cookie.Name != "__oailb" {
			continue
		}
		if seen[cookie.Name] || cookie.Value == "" || cookie.MaxAge < 0 {
			continue
		}
		seen[cookie.Name] = true
		out = append(out, cookie.Name+"="+cookie.Value)
	}
	return out
}

func applyCodexIQIdentity(h http.Header, model string) {
	ensureCodexIdentityHeaders(h)
	enforceCodexIdentityHeaders(h)
	if codexTicketAstraModel(model) && CompareVersions(h.Get("version"), codexAstraMinVersion) < 0 {
		h.Set("version", codexAstraMinVersion)
		h.Set("user-agent", buildCodexCLIUserAgent(codexAstraMinVersion))
	}
}

func safeCodexIQDetail(err error) string {
	if err == nil {
		return ""
	}
	return truncateString(err.Error(), codexIQMaxDetailByte)
}
