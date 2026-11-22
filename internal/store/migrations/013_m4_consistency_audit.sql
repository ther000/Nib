-- 013: M4 全书双层一致性体检报告表

CREATE TABLE IF NOT EXISTS book_consistency_reports (
    id            INTEGER PRIMARY KEY,
    book_id       INTEGER NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    scope_type    TEXT NOT NULL CHECK (scope_type IN ('volume', 'range', 'book')),
    scope_ref_id  INTEGER NOT NULL DEFAULT 0,
    start_seq     INTEGER NOT NULL DEFAULT 0,
    end_seq       INTEGER NOT NULL DEFAULT 0,
    model_name    TEXT NOT NULL DEFAULT '',
    findings_json TEXT NOT NULL DEFAULT '[]',
    summary       TEXT NOT NULL DEFAULT '',
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_book_consistency_reports_book
    ON book_consistency_reports(book_id, id DESC);
