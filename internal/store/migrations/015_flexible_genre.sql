-- pragma:disable_foreign_keys
-- 015: 放开 books.genre 的 4 枚举硬约束，支持 8 大题材基底与自定义题材名

CREATE TABLE books_new (
    id                   INTEGER PRIMARY KEY,
    title                TEXT NOT NULL,
    genre                TEXT NOT NULL CHECK (length(trim(genre)) > 0),
    premise              TEXT NOT NULL DEFAULT '',
    golden_finger        TEXT NOT NULL DEFAULT '',
    target_chapter_words INTEGER NOT NULL DEFAULT 2500,
    export_dir           TEXT NOT NULL DEFAULT '',
    created_at           TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at           TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    pov_mode             TEXT NOT NULL DEFAULT 'third_limited',
    pacing_style         TEXT NOT NULL DEFAULT '',
    writing_prompt       TEXT NOT NULL DEFAULT '',
    daily_word_goal      INTEGER NOT NULL DEFAULT 4000
);

INSERT INTO books_new (
    id, title, genre, premise, golden_finger, target_chapter_words, export_dir,
    created_at, updated_at, pov_mode, pacing_style, writing_prompt, daily_word_goal
)
SELECT
    id, title, genre, premise, golden_finger, target_chapter_words, export_dir,
    created_at, updated_at, pov_mode, pacing_style, writing_prompt, daily_word_goal
FROM books;

DROP TABLE books;
ALTER TABLE books_new RENAME TO books;
