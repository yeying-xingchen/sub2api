-- 鹈鹕测智：保存每次账号级测试的题目、生成作品与耗时。
-- 作品是模型输出原文，管理端只在沙箱 iframe 中展示。
CREATE TABLE IF NOT EXISTS pelican_tests (
    id BIGSERIAL PRIMARY KEY,
    account_id BIGINT NOT NULL,
    account_name TEXT NOT NULL DEFAULT '',
    platform TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    prompt TEXT NOT NULL DEFAULT '',
    output TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    error_message TEXT NOT NULL DEFAULT '',
    latency_ms BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_pelican_tests_created_at
    ON pelican_tests (created_at DESC);

CREATE INDEX IF NOT EXISTS idx_pelican_tests_account
    ON pelican_tests (account_id, created_at DESC);
