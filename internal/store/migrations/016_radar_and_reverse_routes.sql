-- pragma:disable_foreign_keys
-- 016: 扩充 model_routes 与 token_usage_logs 的 stage 枚举，支持「扫榜拆解(radar)」与「拆书反推(reverse_extract)」独立模型分配

CREATE TABLE model_routes_new (
    stage                TEXT PRIMARY KEY CHECK (stage IN ('outline', 'draft', 'critic', 'polish', 'extract', 'radar', 'reverse_extract')),
    provider_id          INTEGER REFERENCES providers(id) ON DELETE SET NULL,
    model_name           TEXT NOT NULL DEFAULT '',
    temperature          REAL NOT NULL DEFAULT 0.7,
    max_tokens           INTEGER NOT NULL DEFAULT 4096,
    updated_at           TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    fallback_provider_id INTEGER REFERENCES providers(id) ON DELETE SET NULL,
    fallback_model_name  TEXT NOT NULL DEFAULT ''
);

INSERT INTO model_routes_new (
    stage, provider_id, model_name, temperature, max_tokens, updated_at, fallback_provider_id, fallback_model_name
)
SELECT
    stage, provider_id, model_name, temperature, max_tokens, updated_at, fallback_provider_id, fallback_model_name
FROM model_routes;

DROP TABLE model_routes;
ALTER TABLE model_routes_new RENAME TO model_routes;

CREATE TABLE token_usage_logs_new (
    id                INTEGER PRIMARY KEY,
    book_id           INTEGER NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    chapter_id        INTEGER REFERENCES chapters(id) ON DELETE SET NULL,
    stage             TEXT NOT NULL CHECK (stage IN ('outline', 'draft', 'critic', 'polish', 'extract', 'style', 'radar', 'reverse_extract')),
    provider_name     TEXT NOT NULL,
    model_name        TEXT NOT NULL,
    prompt_tokens     INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    latency_ms        INTEGER NOT NULL DEFAULT 0,
    created_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

INSERT INTO token_usage_logs_new (
    id, book_id, chapter_id, stage, provider_name, model_name, prompt_tokens, completion_tokens, latency_ms, created_at
)
SELECT
    id, book_id, chapter_id, stage, provider_name, model_name, prompt_tokens, completion_tokens, latency_ms, created_at
FROM token_usage_logs;

DROP TABLE token_usage_logs;
ALTER TABLE token_usage_logs_new RENAME TO token_usage_logs;

CREATE INDEX idx_token_usage_book ON token_usage_logs(book_id, created_at);
CREATE INDEX idx_token_usage_chapter ON token_usage_logs(chapter_id);
