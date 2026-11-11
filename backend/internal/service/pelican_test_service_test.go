package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParsePelicanSSEOutput(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"content","text":"<html><body>"}`,
		`data: {"type":"content","text":"<svg></svg></body></html>"}`,
		`data: {"type":"test_complete","success":true}`,
	}, "\n")
	output, message := parsePelicanSSEOutput(body)
	require.Equal(t, "<html><body><svg></svg></body></html>", output)
	require.Empty(t, message)
}

func TestParsePelicanSSEOutputFailure(t *testing.T) {
	body := `data: {"type":"error","error":"upstream overloaded"}`
	output, message := parsePelicanSSEOutput(body)
	require.Empty(t, output)
	require.Contains(t, message, "upstream overloaded")
}

func TestCleanPelicanOutputStripsCodeFence(t *testing.T) {
	require.Equal(t, "<html><svg></svg></html>", cleanPelicanOutput("```html\n<html><svg></svg></html>\n```"))
	require.Equal(t, "<svg></svg>", cleanPelicanOutput("  <svg></svg>  "))
}

func TestValidatePelicanOutput(t *testing.T) {
	require.Empty(t, validatePelicanOutput("<html><body>ok</body></html>"))
	require.Empty(t, validatePelicanOutput("<svg><circle/></svg>"))
	require.NotEmpty(t, validatePelicanOutput(""))
	require.NotEmpty(t, validatePelicanOutput("here is a pelican"))
}

type pelicanPlanRepoFake struct {
	nextID int64
	plans  map[int64]PelicanTestPlan
}

func newPelicanPlanRepoFake() *pelicanPlanRepoFake {
	return &pelicanPlanRepoFake{plans: map[int64]PelicanTestPlan{}}
}

func (r *pelicanPlanRepoFake) Create(_ context.Context, plan *PelicanTestPlan) error {
	r.nextID++
	plan.ID = r.nextID
	r.plans[plan.ID] = *plan
	return nil
}

func (r *pelicanPlanRepoFake) Update(_ context.Context, plan *PelicanTestPlan) error {
	r.plans[plan.ID] = *plan
	return nil
}

func (r *pelicanPlanRepoFake) Delete(_ context.Context, id int64) error {
	delete(r.plans, id)
	return nil
}

func (r *pelicanPlanRepoFake) GetByID(_ context.Context, id int64) (*PelicanTestPlan, error) {
	plan, ok := r.plans[id]
	if !ok {
		return nil, nil
	}
	return &plan, nil
}

func (r *pelicanPlanRepoFake) List(context.Context) ([]PelicanTestPlan, error) {
	out := make([]PelicanTestPlan, 0, len(r.plans))
	for _, plan := range r.plans {
		out = append(out, plan)
	}
	return out, nil
}

func (r *pelicanPlanRepoFake) ListDue(_ context.Context, now time.Time) ([]PelicanTestPlan, error) {
	out := make([]PelicanTestPlan, 0, len(r.plans))
	for _, plan := range r.plans {
		if plan.Enabled && plan.NextRunAt != nil && !plan.NextRunAt.After(now) {
			out = append(out, plan)
		}
	}
	return out, nil
}

func (r *pelicanPlanRepoFake) Claim(_ context.Context, id int64, now, next time.Time) (bool, error) {
	plan, ok := r.plans[id]
	if !ok || !plan.Enabled || plan.NextRunAt == nil || plan.NextRunAt.After(now) {
		return false, nil
	}
	plan.NextRunAt = &next
	r.plans[id] = plan
	return true, nil
}

func (r *pelicanPlanRepoFake) Finish(_ context.Context, id int64, runAt time.Time, status, errMsg string) error {
	plan, ok := r.plans[id]
	if !ok {
		return nil
	}
	plan.LastRunAt = &runAt
	plan.LastStatus = status
	plan.LastError = errMsg
	r.plans[id] = plan
	return nil
}

func TestPelicanPlanDefaultsAndCronValidation(t *testing.T) {
	account := codexTicketTestAccount(1, "pelican")
	repo := newPelicanPlanRepoFake()
	svc := NewPelicanTestService(&codexTicketAccountRepoFake{accounts: []Account{*account}}, nil, nil, repo)

	plan, err := svc.CreatePlan(context.Background(), PelicanTestPlanInput{
		AccountID: account.ID, CronExpression: "*/5 * * * *", Enabled: true,
	})
	require.NoError(t, err)
	require.Equal(t, PelicanDefaultPrompt, plan.Prompt)
	require.NotEmpty(t, plan.Model)
	require.NotNil(t, plan.NextRunAt)
	require.True(t, plan.NextRunAt.After(time.Now()))

	plans, err := svc.ListPlans(context.Background())
	require.NoError(t, err)
	require.Len(t, plans, 1)

	_, err = svc.CreatePlan(context.Background(), PelicanTestPlanInput{
		AccountID: account.ID, CronExpression: "not-a-cron", Enabled: true,
	})
	require.Error(t, err)
}
