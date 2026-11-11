package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type pelicanTestPlanRepository struct {
	db *sql.DB
}

func NewPelicanTestPlanRepository(db *sql.DB) service.PelicanTestPlanRepository {
	return &pelicanTestPlanRepository{db: db}
}

const pelicanPlanColumns = `id, account_id, account_name, model, prompt, cron_expression, enabled,
	max_results, last_run_at, next_run_at, last_status, last_error, created_at, updated_at`

func (r *pelicanTestPlanRepository) Create(ctx context.Context, plan *service.PelicanTestPlan) error {
	if r == nil || r.db == nil {
		return errors.New("pelican plan repository unavailable")
	}
	if plan == nil {
		return errors.New("invalid pelican plan")
	}
	var next any
	if plan.NextRunAt != nil {
		next = *plan.NextRunAt
	}
	now := time.Now()
	plan.CreatedAt = now
	plan.UpdatedAt = now
	return r.db.QueryRowContext(ctx, `
		INSERT INTO pelican_test_plans
			(account_id, account_name, model, prompt, cron_expression, enabled, max_results, next_run_at, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		RETURNING id
	`, plan.AccountID, plan.AccountName, plan.Model, plan.Prompt, plan.CronExpression, plan.Enabled,
		plan.MaxResults, next, plan.CreatedAt, plan.UpdatedAt).Scan(&plan.ID)
}

func (r *pelicanTestPlanRepository) Update(ctx context.Context, plan *service.PelicanTestPlan) error {
	if r == nil || r.db == nil {
		return errors.New("pelican plan repository unavailable")
	}
	if plan == nil || plan.ID <= 0 {
		return errors.New("invalid pelican plan")
	}
	var next any
	if plan.NextRunAt != nil {
		next = *plan.NextRunAt
	}
	plan.UpdatedAt = time.Now()
	_, err := r.db.ExecContext(ctx, `
		UPDATE pelican_test_plans SET
			account_id = $2, account_name = $3, model = $4, prompt = $5, cron_expression = $6,
			enabled = $7, max_results = $8, next_run_at = $9, last_run_at = $10,
			last_status = $11, last_error = $12, updated_at = $13
		WHERE id = $1
	`, plan.ID, plan.AccountID, plan.AccountName, plan.Model, plan.Prompt, plan.CronExpression,
		plan.Enabled, plan.MaxResults, next, plan.LastRunAt, plan.LastStatus, plan.LastError, plan.UpdatedAt)
	return err
}

func (r *pelicanTestPlanRepository) Delete(ctx context.Context, id int64) error {
	if r == nil || r.db == nil {
		return errors.New("pelican plan repository unavailable")
	}
	_, err := r.db.ExecContext(ctx, `DELETE FROM pelican_test_plans WHERE id = $1`, id)
	return err
}

func (r *pelicanTestPlanRepository) GetByID(ctx context.Context, id int64) (*service.PelicanTestPlan, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("pelican plan repository unavailable")
	}
	row := r.db.QueryRowContext(ctx, `SELECT `+pelicanPlanColumns+` FROM pelican_test_plans WHERE id = $1`, id)
	plan, err := scanPelicanPlan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return plan, err
}

func (r *pelicanTestPlanRepository) List(ctx context.Context) ([]service.PelicanTestPlan, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("pelican plan repository unavailable")
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+pelicanPlanColumns+` FROM pelican_test_plans ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]service.PelicanTestPlan, 0, 16)
	for rows.Next() {
		plan, scanErr := scanPelicanPlan(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, *plan)
	}
	return out, rows.Err()
}

func (r *pelicanTestPlanRepository) ListDue(ctx context.Context, now time.Time) ([]service.PelicanTestPlan, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("pelican plan repository unavailable")
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+pelicanPlanColumns+`
		FROM pelican_test_plans
		WHERE enabled AND next_run_at IS NOT NULL AND next_run_at <= $1
		ORDER BY next_run_at
		LIMIT 50`, now)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]service.PelicanTestPlan, 0, 8)
	for rows.Next() {
		plan, scanErr := scanPelicanPlan(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, *plan)
	}
	return out, rows.Err()
}

func (r *pelicanTestPlanRepository) Claim(ctx context.Context, id int64, now, next time.Time) (bool, error) {
	if r == nil || r.db == nil {
		return false, errors.New("pelican plan repository unavailable")
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE pelican_test_plans
		SET next_run_at = $3, updated_at = now()
		WHERE id = $1 AND enabled AND next_run_at IS NOT NULL AND next_run_at <= $2
	`, id, now, next)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

func (r *pelicanTestPlanRepository) Finish(ctx context.Context, id int64, runAt time.Time, status, errMsg string) error {
	if r == nil || r.db == nil {
		return errors.New("pelican plan repository unavailable")
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE pelican_test_plans
		SET last_run_at = $2, last_status = $3, last_error = $4, updated_at = now()
		WHERE id = $1
	`, id, runAt, status, errMsg)
	return err
}

type pelicanPlanScanner interface {
	Scan(dest ...any) error
}

func scanPelicanPlan(scanner pelicanPlanScanner) (*service.PelicanTestPlan, error) {
	var (
		plan    service.PelicanTestPlan
		lastRun sql.NullTime
		nextRun sql.NullTime
	)
	if err := scanner.Scan(
		&plan.ID,
		&plan.AccountID,
		&plan.AccountName,
		&plan.Model,
		&plan.Prompt,
		&plan.CronExpression,
		&plan.Enabled,
		&plan.MaxResults,
		&lastRun,
		&nextRun,
		&plan.LastStatus,
		&plan.LastError,
		&plan.CreatedAt,
		&plan.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if lastRun.Valid {
		plan.LastRunAt = &lastRun.Time
	}
	if nextRun.Valid {
		plan.NextRunAt = &nextRun.Time
	}
	return &plan, nil
}
