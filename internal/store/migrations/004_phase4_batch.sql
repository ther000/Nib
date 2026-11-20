-- 串行微批调度任务容器
CREATE TABLE batch_runs (
    id          INTEGER PRIMARY KEY,
    book_id     INTEGER NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    mode        TEXT NOT NULL CHECK (mode IN ('draft_only', 'draft_and_polish', 'full_pipeline')),
    status      TEXT NOT NULL CHECK (status IN ('running', 'paused', 'completed', 'canceled', 'failed')),
    total_items INTEGER NOT NULL DEFAULT 0,
    done_items  INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    finished_at TEXT NOT NULL DEFAULT ''
);

-- 微批单章排队明细
CREATE TABLE batch_run_items (
    id           INTEGER PRIMARY KEY,
    batch_run_id INTEGER NOT NULL REFERENCES batch_runs(id) ON DELETE CASCADE,
    chapter_id   INTEGER NOT NULL REFERENCES chapters(id) ON DELETE CASCADE,
    seq          INTEGER NOT NULL,
    state        TEXT NOT NULL CHECK (state IN (
                    'queued', 'compiling', 'drafting', 'linting',
                    'critiquing', 'polishing', 'extracting',
                    'awaiting_review', 'failed', 'canceled'
                 )),
    input_hash   TEXT NOT NULL DEFAULT '',
    error_msg    TEXT NOT NULL DEFAULT '',
    started_at   TEXT NOT NULL DEFAULT '',
    finished_at  TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_batch_items_run ON batch_run_items(batch_run_id, seq);

-- 模型 Token 与费用流水（用于单章与整卷成本统计）
CREATE TABLE token_usage_logs (
    id                INTEGER PRIMARY KEY,
    book_id           INTEGER NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    chapter_id        INTEGER REFERENCES chapters(id) ON DELETE SET NULL,
    stage             TEXT NOT NULL CHECK (stage IN ('outline', 'draft', 'critic', 'polish', 'extract', 'style')),
    provider_name     TEXT NOT NULL,
    model_name        TEXT NOT NULL,
    prompt_tokens     INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    latency_ms        INTEGER NOT NULL DEFAULT 0,
    created_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX idx_token_usage_book ON token_usage_logs(book_id, created_at);
