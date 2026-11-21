-- 009: 灵感便签 / 废稿池 + 软删除回收站

CREATE TABLE IF NOT EXISTS scratch_notes (
    id         INTEGER PRIMARY KEY,
    book_id    INTEGER NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    title      TEXT NOT NULL DEFAULT '',
    content    TEXT NOT NULL DEFAULT '',
    tags_json  TEXT NOT NULL DEFAULT '[]',
    pinned     INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_scratch_notes_book ON scratch_notes(book_id, pinned DESC, updated_at DESC);

-- 回收站快照：payload_json 保存删除前的完整领域对象，30 天内可还原。
CREATE TABLE IF NOT EXISTS trash_items (
    id          INTEGER PRIMARY KEY,
    item_type   TEXT NOT NULL CHECK (item_type IN ('book', 'volume', 'chapter', 'entity')),
    book_id     INTEGER NOT NULL DEFAULT 0,
    title       TEXT NOT NULL DEFAULT '',
    summary     TEXT NOT NULL DEFAULT '',
    payload_json TEXT NOT NULL,
    deleted_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_trash_items_deleted ON trash_items(deleted_at DESC);
