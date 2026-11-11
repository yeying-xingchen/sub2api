package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/gin-gonic/gin"
)

// 鹈鹕测智：让模型直接产出一个可展示的 HTML/SVG 作品，用作品的完整度
// 直观判断账号当前是否“降智”。这是账号级手动测试的核心版；分组定时、
// 用户展示页与成本统计不在此范围内。

const (
	PelicanDefaultPrompt = "用单个 HTML 文件 + 内联 SVG 画一只骑自行车的鹈鹕，要求构图完整、比例协调、细节丰富。只输出完整 HTML，不要解释，不要 Markdown 代码块，不要使用外部图片或脚本。"

	PelicanStatusSuccess = "success"
	PelicanStatusFailed  = "failed"

	pelicanCaptureLimit = 4 << 20
	pelicanOutputLimit  = 2 << 20
	pelicanRunTimeout   = 10 * time.Minute
)

var pelicanHTMLPattern = regexp.MustCompile(`(?i)<(?:!doctype\s+html|html|svg)[\s>]`)

// PelicanTestRecord 是一次鹈鹕测智的持久化记录。
type PelicanTestRecord struct {
	ID           int64     `json:"id"`
	AccountID    int64     `json:"account_id"`
	AccountName  string    `json:"account_name"`
	Platform     string    `json:"platform"`
	Model        string    `json:"model"`
	Prompt       string    `json:"prompt"`
	PlanID       int64     `json:"plan_id,omitempty"`
	Output       string    `json:"output,omitempty"`
	Status       string    `json:"status"`
	ErrorMessage string    `json:"error_message,omitempty"`
	LatencyMs    int64     `json:"latency_ms"`
	CreatedAt    time.Time `json:"created_at"`
}

// PelicanTestListItem 是历史列表项（不带作品原文）。
type PelicanTestListItem struct {
	ID           int64     `json:"id"`
	AccountID    int64     `json:"account_id"`
	AccountName  string    `json:"account_name"`
	Platform     string    `json:"platform"`
	Model        string    `json:"model"`
	Status       string    `json:"status"`
	ErrorMessage string    `json:"error_message,omitempty"`
	LatencyMs    int64     `json:"latency_ms"`
	CreatedAt    time.Time `json:"created_at"`
}

type PelicanTestListResult struct {
	Items    []PelicanTestListItem `json:"items"`
	Total    int64                 `json:"total"`
	Page     int                   `json:"page"`
	PageSize int                   `json:"page_size"`
}

type PelicanTestRepository interface {
	Create(ctx context.Context, record *PelicanTestRecord) error
	List(ctx context.Context, page, pageSize int) ([]PelicanTestListItem, int64, error)
	Get(ctx context.Context, id int64) (*PelicanTestRecord, error)
}

type PelicanTestInput struct {
	AccountID int64
	Model     string
	Prompt    string
	PlanID    int64
}

// PelicanTestService 负责运行测试、解析作品与保存历史。
type PelicanTestService struct {
	accountRepo AccountRepository
	accountTest *AccountTestService
	repo        PelicanTestRepository
	planRepo    PelicanTestPlanRepository
	running     sync.Map // accountID -> struct{}
	scheduler   pelicanSchedulerState
}

func NewPelicanTestService(accountRepo AccountRepository, accountTest *AccountTestService, repo PelicanTestRepository, planRepo PelicanTestPlanRepository) *PelicanTestService {
	return &PelicanTestService{accountRepo: accountRepo, accountTest: accountTest, repo: repo, planRepo: planRepo}
}

func (s *PelicanTestService) Run(ctx context.Context, input PelicanTestInput) (*PelicanTestRecord, error) {
	if s == nil || s.accountRepo == nil || s.accountTest == nil || s.repo == nil {
		return nil, errors.New("pelican test service unavailable")
	}
	account, err := s.accountRepo.GetByID(ctx, input.AccountID)
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, ErrAccountNotFound
	}
	if account.IsSyntheticUITest() {
		return nil, errors.New("测试数据账号不向真实上游发请求")
	}
	if account.Platform != PlatformOpenAI {
		return nil, errors.New("鹈鹕测智核心版目前仅支持 OpenAI 账号")
	}
	if _, loaded := s.running.LoadOrStore(account.ID, struct{}{}); loaded {
		return nil, errors.New("该账号已有一轮鹈鹕测智在运行")
	}
	defer s.running.Delete(account.ID)

	prompt := strings.TrimSpace(input.Prompt)
	if prompt == "" {
		prompt = PelicanDefaultPrompt
	} else {
		prompt = prompt + "\n\n" + "直接返回独立 HTML，不使用 Markdown 代码块或外部依赖。只输出 HTML，不要解释。"
	}
	model := strings.TrimSpace(input.Model)
	if model == "" {
		model = openai.DefaultTestModel
	}

	runCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	recorder := &pelicanRecorder{ResponseRecorder: httptest.NewRecorder(), cancel: cancel}
	c, _ := gin.CreateTestContext(recorder)
	c.Request = (&http.Request{Header: make(http.Header)}).WithContext(runCtx)
	c.Set(accountTestBackgroundKey, true)

	started := time.Now()
	runErr := s.accountTest.TestAccountConnection(c, account.ID, model, prompt, AccountTestModeDefault)
	output, message := parsePelicanSSEOutput(recorder.Body.String())
	if recorder.overflow {
		output = ""
		message = "作品超过 4 MiB 抓取上限"
	}
	if message == "" && runErr != nil {
		message = runErr.Error()
	}
	if message == "" {
		message = validatePelicanOutput(output)
	}
	if len(output) > pelicanOutputLimit {
		output = ""
		message = "作品超过 2 MiB 历史保存上限"
	}
	status := PelicanStatusSuccess
	if message != "" {
		status = PelicanStatusFailed
	}
	record := &PelicanTestRecord{
		AccountID:    account.ID,
		AccountName:  account.Name,
		Platform:     account.Platform,
		Model:        model,
		Prompt:       prompt,
		PlanID:       input.PlanID,
		Output:       output,
		Status:       status,
		ErrorMessage: message,
		LatencyMs:    time.Since(started).Milliseconds(),
		CreatedAt:    time.Now(),
	}
	if record.Model == "" {
		record.Model = model
	}
	if err := s.repo.Create(runCtx, record); err != nil {
		return nil, err
	}
	return record, nil
}

func (s *PelicanTestService) List(ctx context.Context, page, pageSize int) (*PelicanTestListResult, error) {
	if s == nil || s.repo == nil {
		return nil, errors.New("pelican test service unavailable")
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	items, total, err := s.repo.List(ctx, page, pageSize)
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []PelicanTestListItem{}
	}
	return &PelicanTestListResult{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

func (s *PelicanTestService) Get(ctx context.Context, id int64) (*PelicanTestRecord, error) {
	if s == nil || s.repo == nil {
		return nil, errors.New("pelican test service unavailable")
	}
	record, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, errors.New("pelican test not found")
	}
	return record, nil
}

type pelicanRecorder struct {
	*httptest.ResponseRecorder
	cancel   context.CancelFunc
	overflow bool
}

func (w *pelicanRecorder) Write(data []byte) (int, error) {
	if w.overflow || w.Body.Len()+len(data) > pelicanCaptureLimit {
		w.overflow = true
		if w.cancel != nil {
			w.cancel()
		}
		return 0, io.ErrShortWrite
	}
	return w.ResponseRecorder.Write(data)
}

func (w *pelicanRecorder) WriteString(data string) (int, error) { return w.Write([]byte(data)) }

func parsePelicanSSEOutput(body string) (string, string) {
	output, message := parseTestSSEOutput(body)
	complete := false
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var event TestEvent
		if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event) != nil {
			continue
		}
		if event.Type == "test_complete" {
			complete = event.Success
			if !event.Success && message == "" {
				message = firstNonEmpty(event.Error, "生成未正常完成")
			}
		}
	}
	if !complete && message == "" {
		message = "生成流在完成前结束"
	}
	return cleanPelicanOutput(output), message
}

func cleanPelicanOutput(output string) string {
	output = strings.TrimSpace(output)
	if strings.HasPrefix(output, "```") {
		if index := strings.IndexByte(output, '\n'); index >= 0 {
			output = output[index+1:]
		}
		output = strings.TrimSpace(output)
		output = strings.TrimSuffix(output, "```")
		output = strings.TrimSpace(output)
	}
	return output
}

func validatePelicanOutput(output string) string {
	if strings.TrimSpace(output) == "" {
		return "模型返回空输出"
	}
	if !pelicanHTMLPattern.MatchString(output) {
		return "模型没有返回可展示的 HTML/SVG"
	}
	return ""
}
