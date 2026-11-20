-- 事件溯源状态账本（支撑任意章节快照回看与安全回滚）
CREATE TABLE entity_state_events (
    id               INTEGER PRIMARY KEY,
    book_id          INTEGER NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    entity_id        INTEGER NOT NULL REFERENCES entities(id) ON DELETE CASCADE,
    chapter_id       INTEGER NOT NULL REFERENCES chapters(id) ON DELETE CASCADE,
    chapter_seq      INTEGER NOT NULL,
    attr_key         TEXT NOT NULL,
    prev_value       TEXT NOT NULL DEFAULT '',
    new_value        TEXT NOT NULL,
    change_severity  TEXT NOT NULL DEFAULT 'normal' CHECK (change_severity IN ('normal', 'high_risk')),
    reason           TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'committed', 'reverted')),
    created_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX idx_state_events_timeline ON entity_state_events(book_id, entity_id, chapter_seq, status);
CREATE INDEX idx_state_events_chapter ON entity_state_events(chapter_id, status);

-- 伏笔生命周期追踪表
CREATE TABLE foreshadowings (
    id                       INTEGER PRIMARY KEY,
    book_id                  INTEGER NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    title                    TEXT NOT NULL,
    description              TEXT NOT NULL DEFAULT '',
    planted_chapter_seq      INTEGER NOT NULL,
    last_hint_seq            INTEGER NOT NULL DEFAULT 0,
    target_resolve_seq       INTEGER NOT NULL DEFAULT 0,
    resolved_chapter_seq     INTEGER NOT NULL DEFAULT 0,
    status                   TEXT NOT NULL DEFAULT 'planted'
                             CHECK (status IN ('pending_plant', 'planted', 'hinted', 'resolved', 'abandoned')),
    involved_entity_ids_json TEXT NOT NULL DEFAULT '[]',
    created_at               TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at               TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX idx_foreshadowings_book ON foreshadowings(book_id, status, target_resolve_seq);

-- 分层摘要金字塔（10 章弧线摘要 & 卷摘要）
CREATE TABLE arc_summaries (
    id          INTEGER PRIMARY KEY,
    book_id     INTEGER NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    level       TEXT NOT NULL CHECK (level IN ('arc_10', 'volume')),
    volume_id   INTEGER REFERENCES volumes(id) ON DELETE CASCADE,
    start_seq   INTEGER NOT NULL,
    end_seq     INTEGER NOT NULL,
    summary     TEXT NOT NULL,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX idx_arc_summaries_range ON arc_summaries(book_id, level, start_seq, end_seq);

-- 完章记忆变更单（暂存 AI 抽取结果与作者审核结论）
CREATE TABLE chapter_deltas (
    id                INTEGER PRIMARY KEY,
    chapter_id        INTEGER NOT NULL UNIQUE REFERENCES chapters(id) ON DELETE CASCADE,
    causal_summary    TEXT NOT NULL DEFAULT '',
    new_entities_json TEXT NOT NULL DEFAULT '[]',
    state_patch_json  TEXT NOT NULL DEFAULT '[]',
    foreshadow_json   TEXT NOT NULL DEFAULT '[]',
    high_risk_flags   INTEGER NOT NULL DEFAULT 0,
    review_status     TEXT NOT NULL DEFAULT 'pending' CHECK (review_status IN ('pending', 'applied', 'dismissed')),
    created_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    reviewed_at       TEXT NOT NULL DEFAULT ''
);

-- FTS5 中文精确检索虚表（trigram 分词支持中文字串，双字短词由查询层自动补 LIKE）
CREATE VIRTUAL TABLE chapters_fts USING fts5(
    title,
    content,
    content='chapters',
    content_rowid='id',
    tokenize='trigram'
);

CREATE TRIGGER chapters_ai AFTER INSERT ON chapters BEGIN
    INSERT INTO chapters_fts(rowid, title, content) VALUES (new.id, new.title, new.content);
END;
CREATE TRIGGER chapters_ad AFTER DELETE ON chapters BEGIN
    INSERT INTO chapters_fts(chapters_fts, rowid, title, content) VALUES ('delete', old.id, old.title, old.content);
END;
CREATE TRIGGER chapters_au AFTER UPDATE OF title, content ON chapters BEGIN
    INSERT INTO chapters_fts(chapters_fts, rowid, title, content) VALUES ('delete', old.id, old.title, old.content);
    INSERT INTO chapters_fts(rowid, title, content) VALUES (new.id, new.title, new.content);
END;

CREATE VIRTUAL TABLE entities_fts USING fts5(
    name,
    aliases_json,
    summary,
    content='entities',
    content_rowid='id',
    tokenize='trigram'
);

CREATE TRIGGER entities_ai AFTER INSERT ON entities BEGIN
    INSERT INTO entities_fts(rowid, name, aliases_json, summary) VALUES (new.id, new.name, new.aliases_json, new.summary);
END;
CREATE TRIGGER entities_ad AFTER DELETE ON entities BEGIN
    INSERT INTO entities_fts(entities_fts, rowid, name, aliases_json, summary) VALUES ('delete', old.id, old.name, old.aliases_json, old.summary);
END;
CREATE TRIGGER entities_au AFTER UPDATE OF name, aliases_json, summary ON entities BEGIN
    INSERT INTO entities_fts(entities_fts, rowid, name, aliases_json, summary) VALUES ('delete', old.id, old.name, old.aliases_json, old.summary);
    INSERT INTO entities_fts(rowid, name, aliases_json, summary) VALUES (new.id, new.name, new.aliases_json, new.summary);
END;

-- 升级旧库时立即回填既有章节与实体到 FTS5 索引
INSERT INTO chapters_fts(chapters_fts) VALUES ('rebuild');
INSERT INTO entities_fts(entities_fts) VALUES ('rebuild');
