-- 012: M2 故事内时间线与倒计时 + 实体关系章节演变 + 统一安全撤销点

-- 1. 章节表扩展轻量故事内时间双字段
ALTER TABLE chapters ADD COLUMN story_time_label TEXT NOT NULL DEFAULT '';
ALTER TABLE chapters ADD COLUMN story_day INTEGER;

-- 2. 故事内关键期限与倒计时表
CREATE TABLE IF NOT EXISTS story_deadlines (
    id                INTEGER PRIMARY KEY,
    book_id           INTEGER NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    title             TEXT NOT NULL,
    start_chapter_seq INTEGER NOT NULL DEFAULT 0,
    target_day        INTEGER NOT NULL DEFAULT 0,
    target_label      TEXT NOT NULL DEFAULT '',
    entity_id         INTEGER REFERENCES entities(id) ON DELETE SET NULL,
    foreshadowing_id  INTEGER REFERENCES foreshadowings(id) ON DELETE SET NULL,
    status            TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'reached', 'expired', 'canceled')),
    note              TEXT NOT NULL DEFAULT '',
    created_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_story_deadlines_book_status ON story_deadlines(book_id, status, target_day ASC);

-- 3. 实体关系扩展章节生效区间与审批状态
ALTER TABLE entity_relations ADD COLUMN effective_chapter_seq INTEGER NOT NULL DEFAULT 0;
ALTER TABLE entity_relations ADD COLUMN ended_chapter_seq INTEGER;
ALTER TABLE entity_relations ADD COLUMN status TEXT NOT NULL DEFAULT 'committed';
CREATE INDEX IF NOT EXISTS idx_entity_relations_book_seq ON entity_relations(book_id, effective_chapter_seq, ended_chapter_seq);

-- 4. 完章变更单扩展时间线、关系演变与期限提案字段
ALTER TABLE chapter_deltas ADD COLUMN story_time_label TEXT NOT NULL DEFAULT '';
ALTER TABLE chapter_deltas ADD COLUMN story_day INTEGER;
ALTER TABLE chapter_deltas ADD COLUMN relation_changes_json TEXT NOT NULL DEFAULT '[]';
ALTER TABLE chapter_deltas ADD COLUMN deadline_changes_json TEXT NOT NULL DEFAULT '[]';

-- 5. AI 导演对话撤销点绑定 + 统一轻量安全快照表
ALTER TABLE director_messages ADD COLUMN undo_snapshot_id INTEGER NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS safety_snapshots (
    id            INTEGER PRIMARY KEY,
    book_id       INTEGER NOT NULL DEFAULT 0,
    source        TEXT NOT NULL,
    source_ref_id INTEGER NOT NULL DEFAULT 0,
    summary       TEXT NOT NULL DEFAULT '',
    file_path     TEXT NOT NULL DEFAULT '',
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_safety_snapshots_book ON safety_snapshots(book_id, id DESC);
