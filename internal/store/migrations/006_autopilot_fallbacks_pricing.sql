-- 006: 模型路由备用降级、自定义模型单价、全自动托管持久化表

ALTER TABLE model_routes
    ADD COLUMN fallback_provider_id INTEGER REFERENCES providers(id) ON DELETE SET NULL;

ALTER TABLE model_routes
    ADD COLUMN fallback_model_name TEXT NOT NULL DEFAULT '';

-- 自定义模型单价表（每百万 Token 价格，支持 CNY / USD）
CREATE TABLE IF NOT EXISTS model_pricing (
    provider_name          TEXT NOT NULL,
    model_name             TEXT NOT NULL,
    input_price_per_m      REAL NOT NULL DEFAULT 0,
    output_price_per_m     REAL NOT NULL DEFAULT 0,
    currency               TEXT NOT NULL DEFAULT 'CNY' CHECK (currency IN ('CNY', 'USD')),
    updated_at             TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (provider_name, model_name)
);

-- 全自动托管持久化记录表（支持重启保留进度与一键断点续跑）
CREATE TABLE IF NOT EXISTS autopilot_runs (
    id                    INTEGER PRIMARY KEY,
    book_id               INTEGER NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    volume_id             INTEGER NOT NULL DEFAULT 0,
    status                TEXT NOT NULL DEFAULT 'running'
                          CHECK (status IN ('idle', 'running', 'waiting_decision', 'completed', 'stopped', 'failed')),
    step                  TEXT NOT NULL DEFAULT 'preparing',
    target_chapters       INTEGER NOT NULL DEFAULT 3,
    completed_chapters    INTEGER NOT NULL DEFAULT 0,
    auto_outline          INTEGER NOT NULL DEFAULT 1,
    auto_polish           INTEGER NOT NULL DEFAULT 1,
    auto_commit_memory    INTEGER NOT NULL DEFAULT 1,
    max_tokens_budget     INTEGER NOT NULL DEFAULT 0,
    tokens_used           INTEGER NOT NULL DEFAULT 0,
    arc_goal              TEXT NOT NULL DEFAULT '',
    current_chapter_id    INTEGER NOT NULL DEFAULT 0,
    current_chapter_seq   INTEGER NOT NULL DEFAULT 0,
    current_chapter_title TEXT NOT NULL DEFAULT '',
    message               TEXT NOT NULL DEFAULT '',
    error_msg             TEXT NOT NULL DEFAULT '',
    logs_json             TEXT NOT NULL DEFAULT '[]',
    started_at            TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at            TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_autopilot_runs_book ON autopilot_runs(book_id, id DESC);
