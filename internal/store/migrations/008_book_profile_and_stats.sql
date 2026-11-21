-- 008: 书籍级写作规范与日更目标 + 每日码字统计表

ALTER TABLE books ADD COLUMN pov_mode TEXT NOT NULL DEFAULT 'third_limited';
ALTER TABLE books ADD COLUMN pacing_style TEXT NOT NULL DEFAULT '';
ALTER TABLE books ADD COLUMN writing_prompt TEXT NOT NULL DEFAULT '';
ALTER TABLE books ADD COLUMN daily_word_goal INTEGER NOT NULL DEFAULT 4000;

CREATE TABLE IF NOT EXISTS writing_stats_daily (
    stat_date        TEXT NOT NULL,
    book_id          INTEGER NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    words_delta      INTEGER NOT NULL DEFAULT 0,
    chapters_touched INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (stat_date, book_id)
);
CREATE INDEX IF NOT EXISTS idx_writing_stats_book_date ON writing_stats_daily(book_id, stat_date DESC);
