package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"go.uber.org/zap"
)

// 鹈鹕测智定时计划：按 Cron 到点执行账号级测试。
// 计划只保存题目与账号；每次运行的作品仍写入 pelican_tests（带 plan_id）。

type PelicanTestPlan struct {
	ID             int64      `json:"id"`
	AccountID      int64      `json:"account_id"`
	AccountName    string     `json:"account_name"`
	Model          string     `json:"model"`
	Prompt         string     `json:"prompt"`
	CronExpression string     `json:"cron_expression"`
	Enabled        bool       `json:"enabled"`
	MaxResults     int        `json:"max_results"`
	LastRunAt      *time.Time `json:"last_run_at,omitempty"`
	NextRunAt      *time.Time `json:"next_run_at,omitempty"`
	LastStatus     string     `json:"last_status,omitempty"`
	LastError      string     `json:"last_error,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type PelicanTestPlanInput struct {
	AccountID      int64
	Model          string
	Prompt         string
	CronExpression string
	Enabled        bool
	MaxResults     int
}

type PelicanTestPlanRepository interface {
	Create(ctx context.Context, plan *PelicanTestPlan) error
	Update(ctx context.Context, plan *PelicanTestPlan) error
	Delete(ctx context.Context, id int64) error
	GetByID(ctx context.Context, id int64) (*PelicanTestPlan, error)
	List(ctx context.Context) ([]PelicanTestPlan, error)
	ListDue(ctx context.Context, now time.Time) ([]PelicanTestPlan, error)
	Claim(ctx context.Context, id int64, now, next time.Time) (bool, error)
	Finish(ctx context.Context, id int64, runAt time.Time, status, errMsg string) error
}

type pelicanSchedulerState struct {
	mu     sync.Mutex
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func (s *PelicanTestService) StartScheduler() {
	if s == nil || s.planRepo == nil {
		return
	}
	s.scheduler.mu.Lock()
	defer s.scheduler.mu.Unlock()
	if s.scheduler.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.scheduler.cancel = cancel
	s.scheduler.wg.Add(1)
	go func() {
		defer s.scheduler.wg.Done()
		s.pelicanScheduleLoop(ctx)
	}()
	logger.L().Info("pelican test scheduler started")
}

func (s *PelicanTestService) StopScheduler() {
	if s == nil {
		return
	}
	s.scheduler.mu.Lock()
	cancel := s.scheduler.cancel
	s.scheduler.cancel = nil
	s.scheduler.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	done := make(chan struct{})
	go func() {
		s.scheduler.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
}

func (s *PelicanTestService) pelicanScheduleLoop(ctx context.Context) {
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-timer.C:
	}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		s.runDuePelicanPlans(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *PelicanTestService) runDuePelicanPlans(ctx context.Context) {
	if ctx.Err() != nil || s.planRepo == nil {
		return
	}
	now := time.Now()
	plans, err := s.planRepo.ListDue(ctx, now)
	if err != nil {
		logger.L().Warn("pelican plans list due failed", zap.Error(err))
		return
	}
	if len(plans) == 0 {
		return
	}
	sem := make(chan struct{}, 2)
	var wg sync.WaitGroup
	for i := range plans {
		plan := plans[i]
		next, err := computeNextRun(plan.CronExpression, now)
		if err != nil {
			logger.L().Warn("pelican plan invalid cron", zap.Int64("plan_id", plan.ID), zap.Error(err))
			continue
		}
		claimed, err := s.planRepo.Claim(ctx, plan.ID, now, next)
		if err != nil || !claimed {
			continue
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(p PelicanTestPlan) {
			defer wg.Done()
			defer func() { <-sem }()
			s.runPelicanPlan(ctx, p)
		}(plan)
	}
	wg.Wait()
}

func (s *PelicanTestService) runPelicanPlan(ctx context.Context, plan PelicanTestPlan) {
	runCtx, cancel := context.WithTimeout(ctx, 12*time.Minute)
	defer cancel()
	record, err := s.Run(runCtx, PelicanTestInput{
		AccountID: plan.AccountID,
		Model:     plan.Model,
		Prompt:    plan.Prompt,
		PlanID:    plan.ID,
	})
	status, message := "", ""
	runAt := time.Now()
	if err != nil {
		status = PelicanStatusFailed
		message = err.Error()
	} else {
		status = record.Status
		message = record.ErrorMessage
		runAt = record.CreatedAt
	}
	if finishErr := s.planRepo.Finish(ctx, plan.ID, runAt, status, message); finishErr != nil {
		logger.L().Warn("pelican plan finish failed", zap.Int64("plan_id", plan.ID), zap.Error(finishErr))
	}
}

func (s *PelicanTestService) CreatePlan(ctx context.Context, input PelicanTestPlanInput) (*PelicanTestPlan, error) {
	if s == nil || s.planRepo == nil || s.accountRepo == nil {
		return nil, errors.New("pelican plan service unavailable")
	}
	plan, err := s.buildPlan(ctx, input)
	if err != nil {
		return nil, err
	}
	if err := s.planRepo.Create(ctx, plan); err != nil {
		return nil, err
	}
	return plan, nil
}

func (s *PelicanTestService) UpdatePlan(ctx context.Context, id int64, input PelicanTestPlanInput) (*PelicanTestPlan, error) {
	if s == nil || s.planRepo == nil {
		return nil, errors.New("pelican plan service unavailable")
	}
	current, err := s.planRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, errors.New("pelican plan not found")
	}
	plan, err := s.buildPlan(ctx, input)
	if err != nil {
		return nil, err
	}
	plan.ID = current.ID
	plan.CreatedAt = current.CreatedAt
	plan.LastRunAt = current.LastRunAt
	plan.LastStatus = current.LastStatus
	plan.LastError = current.LastError
	if current.CronExpression == plan.CronExpression && plan.NextRunAt == nil {
		plan.NextRunAt = current.NextRunAt
	}
	if err := s.planRepo.Update(ctx, plan); err != nil {
		return nil, err
	}
	return plan, nil
}

func (s *PelicanTestService) DeletePlan(ctx context.Context, id int64) error {
	if s == nil || s.planRepo == nil {
		return errors.New("pelican plan service unavailable")
	}
	return s.planRepo.Delete(ctx, id)
}

func (s *PelicanTestService) ListPlans(ctx context.Context) ([]PelicanTestPlan, error) {
	if s == nil || s.planRepo == nil {
		return nil, errors.New("pelican plan service unavailable")
	}
	plans, err := s.planRepo.List(ctx)
	if err != nil {
		return nil, err
	}
	if plans == nil {
		plans = []PelicanTestPlan{}
	}
	return plans, nil
}

// RunPlanNow 立即执行一次计划，并更新最近状态（不改变已计算的 next_run_at）。
func (s *PelicanTestService) RunPlanNow(ctx context.Context, id int64) (*PelicanTestRecord, error) {
	if s == nil || s.planRepo == nil {
		return nil, errors.New("pelican plan service unavailable")
	}
	plan, err := s.planRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if plan == nil {
		return nil, errors.New("pelican plan not found")
	}
	record, runErr := s.Run(ctx, PelicanTestInput{
		AccountID: plan.AccountID,
		Model:     plan.Model,
		Prompt:    plan.Prompt,
		PlanID:    plan.ID,
	})
	status, message := PelicanStatusFailed, ""
	if runErr != nil {
		message = runErr.Error()
	} else {
		status = record.Status
		message = record.ErrorMessage
	}
	if finishErr := s.planRepo.Finish(ctx, plan.ID, time.Now(), status, message); finishErr != nil {
		return record, finishErr
	}
	return record, runErr
}

func (s *PelicanTestService) buildPlan(ctx context.Context, input PelicanTestPlanInput) (*PelicanTestPlan, error) {
	if input.AccountID <= 0 {
		return nil, errors.New("account_id is required")
	}
	account, err := s.accountRepo.GetByID(ctx, input.AccountID)
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, ErrAccountNotFound
	}
	if account.Platform != PlatformOpenAI {
		return nil, errors.New("鹈鹕测智定时计划目前仅支持 OpenAI 账号")
	}
	cronExpr := strings.TrimSpace(input.CronExpression)
	if cronExpr == "" {
		cronExpr = "0 9 * * *"
	}
	nextRun, err := computeNextRun(cronExpr, time.Now())
	if err != nil {
		return nil, errors.New("invalid cron expression: " + err.Error())
	}
	model := strings.TrimSpace(input.Model)
	if model == "" {
		model = openai.DefaultTestModel
	}
	prompt := strings.TrimSpace(input.Prompt)
	if prompt == "" {
		prompt = PelicanDefaultPrompt
	}
	next := nextRun
	return &PelicanTestPlan{
		AccountID:      account.ID,
		AccountName:    account.Name,
		Model:          model,
		Prompt:         prompt,
		CronExpression: cronExpr,
		Enabled:        input.Enabled,
		MaxResults:     20,
		NextRunAt:      &next,
	}, nil
}
