package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// codexTicketRepository persists Codex turn-state tickets in PostgreSQL.
// Tickets are keyed by (account_id, model); the shared pool is a read-time
// selection over every account's current ticket.
type codexTicketRepository struct {
	db *sql.DB
}

// NewCodexTicketRepository creates the codex ticket repository.
func NewCodexTicketRepository(db *sql.DB) service.CodexTicketRepository {
	return &codexTicketRepository{db: db}
}

const codexTicketColumns = `id, account_id, model, state, length, identity, transport, blocks,
	issued_at, captured_at, expires_at, acquisition, session_id`

func (r *codexTicketRepository) Upsert(ctx context.Context, ticket *service.CodexTicketRecord) error {
	if r == nil || r.db == nil {
		return errors.New("codex ticket repository unavailable")
	}
	if ticket == nil || ticket.AccountID <= 0 || strings.TrimSpace(ticket.Model) == "" || strings.TrimSpace(ticket.State) == "" {
		return errors.New("invalid codex ticket")
	}
	var issuedAt any
	if !ticket.IssuedAt.IsZero() {
		issuedAt = ticket.IssuedAt
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO codex_tickets (
			account_id, model, state, length, identity, transport, blocks,
			issued_at, captured_at, expires_at, acquisition, session_id
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT (account_id, model) DO UPDATE SET
			state = EXCLUDED.state,
			length = EXCLUDED.length,
			identity = EXCLUDED.identity,
			transport = EXCLUDED.transport,
			blocks = EXCLUDED.blocks,
			issued_at = EXCLUDED.issued_at,
			captured_at = EXCLUDED.captured_at,
			expires_at = EXCLUDED.expires_at,
			acquisition = EXCLUDED.acquisition,
			session_id = EXCLUDED.session_id,
			updated_at = now()
	`, ticket.AccountID, ticket.Model, ticket.State, ticket.Length, ticket.Identity, ticket.Transport,
		ticket.Blocks, issuedAt, ticket.CapturedAt, ticket.ExpiresAt, ticket.Acquisition, ticket.SessionID)
	return err
}

func (r *codexTicketRepository) Get(ctx context.Context, accountID int64, model string) (*service.CodexTicketRecord, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("codex ticket repository unavailable")
	}
	row := r.db.QueryRowContext(ctx, `SELECT `+codexTicketColumns+` FROM codex_tickets WHERE account_id = $1 AND model = $2`, accountID, model)
	record, err := scanCodexTicket(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return record, err
}

// ListValid returns tickets that are still usable at now. An empty model means
// every model.
func (r *codexTicketRepository) ListValid(ctx context.Context, model string, now time.Time) ([]service.CodexTicketRecord, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("codex ticket repository unavailable")
	}
	query := `SELECT ` + codexTicketColumns + ` FROM codex_tickets WHERE expires_at > $1`
	args := []any{now}
	if strings.TrimSpace(model) != "" {
		query += ` AND model = $2`
		args = append(args, model)
	}
	query += ` ORDER BY expires_at DESC`
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]service.CodexTicketRecord, 0, 16)
	for rows.Next() {
		record, scanErr := scanCodexTicket(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, *record)
	}
	return out, rows.Err()
}

func (r *codexTicketRepository) ListByAccount(ctx context.Context, accountID int64) ([]service.CodexTicketRecord, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("codex ticket repository unavailable")
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+codexTicketColumns+` FROM codex_tickets WHERE account_id = $1 ORDER BY model`, accountID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]service.CodexTicketRecord, 0, 4)
	for rows.Next() {
		record, scanErr := scanCodexTicket(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, *record)
	}
	return out, rows.Err()
}

// List returns a page of tickets (valid and expired) with a total count.
func (r *codexTicketRepository) List(ctx context.Context, filter service.CodexTicketListFilter) ([]service.CodexTicketRecord, int64, error) {
	if r == nil || r.db == nil {
		return nil, 0, errors.New("codex ticket repository unavailable")
	}
	now := time.Now()
	where := ` WHERE ($1 = 0 OR account_id = $1)
		AND ($2 = '' OR model = $2)
		AND ($3 = '' OR (($3 = 'valid' AND expires_at > $4) OR ($3 = 'expired' AND expires_at <= $4)))`
	args := []any{filter.AccountID, strings.TrimSpace(filter.Model), strings.TrimSpace(filter.Status), now}

	var total int64
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM codex_tickets`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	page := filter.Page
	if page <= 0 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	query := `SELECT ` + codexTicketColumns + ` FROM codex_tickets` + where + ` ORDER BY expires_at DESC, id DESC LIMIT $5 OFFSET $6`
	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]service.CodexTicketRecord, 0, pageSize)
	for rows.Next() {
		record, scanErr := scanCodexTicket(rows)
		if scanErr != nil {
			return nil, 0, scanErr
		}
		out = append(out, *record)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

func (r *codexTicketRepository) GetByID(ctx context.Context, id int64) (*service.CodexTicketRecord, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("codex ticket repository unavailable")
	}
	row := r.db.QueryRowContext(ctx, `SELECT `+codexTicketColumns+` FROM codex_tickets WHERE id = $1`, id)
	record, err := scanCodexTicket(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return record, err
}

func (r *codexTicketRepository) DeleteByID(ctx context.Context, id int64) error {
	if r == nil || r.db == nil {
		return errors.New("codex ticket repository unavailable")
	}
	_, err := r.db.ExecContext(ctx, `DELETE FROM codex_tickets WHERE id = $1`, id)
	return err
}

func (r *codexTicketRepository) DeleteExpired(ctx context.Context, now time.Time) (int64, error) {
	if r == nil || r.db == nil {
		return 0, errors.New("codex ticket repository unavailable")
	}
	result, err := r.db.ExecContext(ctx, `DELETE FROM codex_tickets WHERE expires_at <= $1`, now)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

type codexTicketScanner interface {
	Scan(dest ...any) error
}

func scanCodexTicket(scanner codexTicketScanner) (*service.CodexTicketRecord, error) {
	var (
		record   service.CodexTicketRecord
		issuedAt sql.NullTime
	)
	if err := scanner.Scan(
		&record.ID,
		&record.AccountID,
		&record.Model,
		&record.State,
		&record.Length,
		&record.Identity,
		&record.Transport,
		&record.Blocks,
		&issuedAt,
		&record.CapturedAt,
		&record.ExpiresAt,
		&record.Acquisition,
		&record.SessionID,
	); err != nil {
		return nil, err
	}
	if issuedAt.Valid {
		record.IssuedAt = issuedAt.Time
	}
	return &record, nil
}
