-- 鹈鹕测智定时计划：按 Cron 到点执行账号级鹈鹕测试。
CREATE TABLE IF NOT EXISTS pelican_test_plans (
    id BIGSERIAL PRIMARY KEY,
    account_id BIGINT NOT NULL,
    account_name TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    prompt TEXT NOT NULL DEFAULT '',
    cron_expression TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT true,
    max_results INT NOT NULL DEFAULT 20,
    last_run_at TIMESTAMPTZ,
    next_run_at TIMESTAMPTZ,
    last_status TEXT NOT NULL DEFAULT '',
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_pelican_test_plans_due
    ON pelican_test_plans (next_run_at)
    WHERE enabled;

ALTER TABLE pelican_tests ADD COLUMN IF NOT EXISTS plan_id BIGINT NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_pelican_tests_plan
    ON pelican_tests (plan_id, created_at DESC);
