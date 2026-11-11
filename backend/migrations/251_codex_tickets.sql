-- Codex 打票（turn-state ticket）存储。
-- 一张票由 (账号, 模型) 唯一确定：后台打票成功后就地 upsert，
-- 业务请求按账号优先、全站共用池兜底的顺序取票。
CREATE TABLE IF NOT EXISTS codex_tickets (
    id BIGSERIAL PRIMARY KEY,
    account_id BIGINT NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    model TEXT NOT NULL,
    state TEXT NOT NULL,
    length INTEGER NOT NULL DEFAULT 0,
    identity TEXT NOT NULL DEFAULT '',
    transport TEXT NOT NULL DEFAULT '',
    blocks INTEGER NOT NULL DEFAULT 0,
    issued_at TIMESTAMPTZ,
    captured_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    acquisition TEXT NOT NULL DEFAULT '',
    session_id TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (account_id, model)
);

ALTER TABLE codex_tickets ADD COLUMN IF NOT EXISTS session_id TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_codex_tickets_model_expires
    ON codex_tickets (model, expires_at);

CREATE INDEX IF NOT EXISTS idx_codex_tickets_expires
    ON codex_tickets (expires_at);
