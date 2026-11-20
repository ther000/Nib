-- 迁移记录表
CREATE TABLE IF NOT EXISTS schema_migrations (
    version     INTEGER PRIMARY KEY,
    name        TEXT NOT NULL,
    applied_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- API 供应商配置（API Key 仅存本地 DB，返回前端时脱敏）
CREATE TABLE providers (
    id          INTEGER PRIMARY KEY,
    name        TEXT NOT NULL,
    protocol    TEXT NOT NULL CHECK (protocol IN ('openai_compat', 'anthropic', 'gemini')),
    base_url    TEXT NOT NULL,
    api_key     TEXT NOT NULL DEFAULT '',
    models_json TEXT NOT NULL DEFAULT '[]',
    enabled     INTEGER NOT NULL DEFAULT 1,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- 5 环节独立模型路由（outline / draft / critic / polish / extract）
CREATE TABLE model_routes (
    stage       TEXT PRIMARY KEY CHECK (stage IN ('outline', 'draft', 'critic', 'polish', 'extract')),
    provider_id INTEGER REFERENCES providers(id) ON DELETE SET NULL,
    model_name  TEXT NOT NULL DEFAULT '',
    temperature REAL NOT NULL DEFAULT 0.7,
    max_tokens  INTEGER NOT NULL DEFAULT 4096,
    updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- 书籍主表
CREATE TABLE books (
    id                  INTEGER PRIMARY KEY,
    title               TEXT NOT NULL,
    genre               TEXT NOT NULL CHECK (genre IN ('xuanhuan', 'xianxia', 'urban', 'general')),
    premise             TEXT NOT NULL DEFAULT '',
    golden_finger       TEXT NOT NULL DEFAULT '',
    target_chapter_words INTEGER NOT NULL DEFAULT 2500,
    export_dir          TEXT NOT NULL DEFAULT '',
    created_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- 卷表
CREATE TABLE volumes (
    id          INTEGER PRIMARY KEY,
    book_id     INTEGER NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    seq         INTEGER NOT NULL,
    title       TEXT NOT NULL,
    goal        TEXT NOT NULL DEFAULT '',
    climax_plan TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (book_id, seq)
);

-- 章节主表
CREATE TABLE chapters (
    id                 INTEGER PRIMARY KEY,
    book_id            INTEGER NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    volume_id          INTEGER NOT NULL REFERENCES volumes(id) ON DELETE CASCADE,
    global_seq         INTEGER NOT NULL,
    vol_seq            INTEGER NOT NULL,
    title              TEXT NOT NULL,
    status             TEXT NOT NULL DEFAULT 'planned'
                       CHECK (status IN ('planned', 'outlined', 'drafting', 'partial', 'reviewing', 'finalized', 'upstream_dirty')),
    emotion_tag        TEXT NOT NULL DEFAULT 'setup'
                       CHECK (emotion_tag IN ('setup', 'suppress', 'turn', 'burst', 'harvest')),
    hook_type          TEXT NOT NULL DEFAULT 'mystery'
                       CHECK (hook_type IN ('mystery', 'crisis', 'counterattack_eve', 'reward_pending', 'new_arc')),
    hook_note          TEXT NOT NULL DEFAULT '',
    outline_summary    TEXT NOT NULL DEFAULT '',
    causal_summary     TEXT NOT NULL DEFAULT '',
    content            TEXT NOT NULL DEFAULT '',
    word_count         INTEGER NOT NULL DEFAULT 0,
    last_briefing_json TEXT NOT NULL DEFAULT '{}',
    created_at         TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at         TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (book_id, global_seq)
);
CREATE INDEX idx_chapters_volume ON chapters(volume_id, vol_seq);

-- 章级节拍卡
CREATE TABLE chapter_beats (
    id              INTEGER PRIMARY KEY,
    chapter_id      INTEGER NOT NULL REFERENCES chapters(id) ON DELETE CASCADE,
    seq             INTEGER NOT NULL,
    stage           TEXT NOT NULL CHECK (stage IN ('setup', 'target', 'suppress', 'turn', 'burst', 'harvest', 'hook')),
    summary         TEXT NOT NULL,
    conflict_note   TEXT NOT NULL DEFAULT '',
    pov_entity_id   INTEGER,
    entity_ids_json TEXT NOT NULL DEFAULT '[]',
    target_words    INTEGER NOT NULL DEFAULT 500,
    UNIQUE (chapter_id, seq)
);

-- 章节历史版本快照（任何 AI 覆写或回滚前强制先落库）
CREATE TABLE chapter_versions (
    id          INTEGER PRIMARY KEY,
    chapter_id  INTEGER NOT NULL REFERENCES chapters(id) ON DELETE CASCADE,
    source      TEXT NOT NULL CHECK (source IN ('manual_backup', 'ai_draft', 'surgical_polish', 'restore_backup')),
    model_name  TEXT NOT NULL DEFAULT '',
    content     TEXT NOT NULL,
    word_count  INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX idx_chapter_versions_chap ON chapter_versions(chapter_id, id DESC);

-- 设定实体与世界铁律
CREATE TABLE entities (
    id              INTEGER PRIMARY KEY,
    book_id         INTEGER NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    kind            TEXT NOT NULL CHECK (kind IN ('character', 'faction', 'location', 'item', 'system', 'rule')),
    name            TEXT NOT NULL,
    aliases_json    TEXT NOT NULL DEFAULT '[]',
    is_invariant    INTEGER NOT NULL DEFAULT 0,
    summary         TEXT NOT NULL DEFAULT '',
    attributes_json TEXT NOT NULL DEFAULT '{}',
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX idx_entities_book_kind ON entities(book_id, kind, is_invariant);
