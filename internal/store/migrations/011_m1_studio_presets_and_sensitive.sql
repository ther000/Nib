-- 011: M1 写作台划词常用指令库 + deai_rules 扩展敏感词分类与一键替换词

CREATE TABLE IF NOT EXISTS rewrite_presets (
    id          INTEGER PRIMARY KEY,
    book_id     INTEGER NOT NULL DEFAULT 0,
    label       TEXT NOT NULL,
    instruction TEXT NOT NULL,
    sort_order  INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_rewrite_presets_book ON rewrite_presets(book_id, sort_order ASC, id ASC);

CREATE TABLE deai_rules_new (
    id              INTEGER PRIMARY KEY,
    book_id         INTEGER NOT NULL DEFAULT 0,
    category        TEXT NOT NULL CHECK (category IN ('cliche', 'structure', 'formatting', 'sensitive')),
    pattern         TEXT NOT NULL,
    is_regex        INTEGER NOT NULL DEFAULT 0,
    severity        TEXT NOT NULL DEFAULT 'warn' CHECK (severity IN ('info', 'warn', 'error')),
    max_per_chapter INTEGER NOT NULL DEFAULT 0,
    hint            TEXT NOT NULL DEFAULT '',
    replacement     TEXT NOT NULL DEFAULT '',
    enabled         INTEGER NOT NULL DEFAULT 1
);

INSERT INTO deai_rules_new (
    id, book_id, category, pattern, is_regex, severity, max_per_chapter, hint, replacement, enabled
)
SELECT
    id,
    book_id,
    CASE
        WHEN category = 'cliche' AND (hint LIKE '%敏感%' OR hint LIKE '%违禁%' OR hint LIKE '%禁词%') THEN 'sensitive'
        ELSE category
    END,
    pattern,
    is_regex,
    severity,
    max_per_chapter,
    hint,
    '',
    enabled
FROM deai_rules;

DROP TABLE deai_rules;
ALTER TABLE deai_rules_new RENAME TO deai_rules;
CREATE INDEX IF NOT EXISTS idx_deai_rules_book ON deai_rules(book_id, enabled);
