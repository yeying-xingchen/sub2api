package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type pelicanTestRepository struct {
	db *sql.DB
}

func NewPelicanTestRepository(db *sql.DB) service.PelicanTestRepository {
	return &pelicanTestRepository{db: db}
}

func (r *pelicanTestRepository) Create(ctx context.Context, record *service.PelicanTestRecord) error {
	if r == nil || r.db == nil {
		return errors.New("pelican test repository unavailable")
	}
	if record == nil {
		return errors.New("invalid pelican test record")
	}
	return r.db.QueryRowContext(ctx, `
		INSERT INTO pelican_tests (account_id, account_name, platform, model, prompt, output, status, error_message, latency_ms, plan_id, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		RETURNING id
	`, record.AccountID, record.AccountName, record.Platform, record.Model, record.Prompt,
		record.Output, record.Status, record.ErrorMessage, record.LatencyMs, record.PlanID, record.CreatedAt).Scan(&record.ID)
}

func (r *pelicanTestRepository) List(ctx context.Context, page, pageSize int) ([]service.PelicanTestListItem, int64, error) {
	if r == nil || r.db == nil {
		return nil, 0, errors.New("pelican test repository unavailable")
	}
	var total int64
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pelican_tests`).Scan(&total); err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * pageSize
	if offset < 0 {
		offset = 0
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, account_id, account_name, platform, model, status, error_message, latency_ms, created_at
		FROM pelican_tests
		ORDER BY created_at DESC, id DESC
		LIMIT $1 OFFSET $2
	`, pageSize, offset)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]service.PelicanTestListItem, 0, pageSize)
	for rows.Next() {
		var item service.PelicanTestListItem
		if err := rows.Scan(&item.ID, &item.AccountID, &item.AccountName, &item.Platform, &item.Model,
			&item.Status, &item.ErrorMessage, &item.LatencyMs, &item.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, item)
	}
	return out, total, rows.Err()
}

func (r *pelicanTestRepository) Get(ctx context.Context, id int64) (*service.PelicanTestRecord, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("pelican test repository unavailable")
	}
	var record service.PelicanTestRecord
	var createdAt time.Time
	err := r.db.QueryRowContext(ctx, `
		SELECT id, account_id, account_name, platform, model, prompt, output, status, error_message, latency_ms, plan_id, created_at
		FROM pelican_tests WHERE id = $1
	`, id).Scan(&record.ID, &record.AccountID, &record.AccountName, &record.Platform, &record.Model,
		&record.Prompt, &record.Output, &record.Status, &record.ErrorMessage, &record.LatencyMs, &record.PlanID, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	record.CreatedAt = createdAt
	return &record, nil
}
