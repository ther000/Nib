-- 扩展全书 FTS5 trigram 检索覆盖面至伏笔（foreshadowings）与弧线/卷摘要（arc_summaries）
CREATE VIRTUAL TABLE foreshadowings_fts USING fts5(
    title,
    description,
    content='foreshadowings',
    content_rowid='id',
    tokenize='trigram'
);

CREATE TRIGGER foreshadowings_ai AFTER INSERT ON foreshadowings BEGIN
    INSERT INTO foreshadowings_fts(rowid, title, description) VALUES (new.id, new.title, new.description);
END;
CREATE TRIGGER foreshadowings_ad AFTER DELETE ON foreshadowings BEGIN
    INSERT INTO foreshadowings_fts(foreshadowings_fts, rowid, title, description) VALUES ('delete', old.id, old.title, old.description);
END;
CREATE TRIGGER foreshadowings_au AFTER UPDATE OF title, description ON foreshadowings BEGIN
    INSERT INTO foreshadowings_fts(foreshadowings_fts, rowid, title, description) VALUES ('delete', old.id, old.title, old.description);
    INSERT INTO foreshadowings_fts(rowid, title, description) VALUES (new.id, new.title, new.description);
END;

CREATE VIRTUAL TABLE arc_summaries_fts USING fts5(
    summary,
    content='arc_summaries',
    content_rowid='id',
    tokenize='trigram'
);

CREATE TRIGGER arc_summaries_ai AFTER INSERT ON arc_summaries BEGIN
    INSERT INTO arc_summaries_fts(rowid, summary) VALUES (new.id, new.summary);
END;
CREATE TRIGGER arc_summaries_ad AFTER DELETE ON arc_summaries BEGIN
    INSERT INTO arc_summaries_fts(arc_summaries_fts, rowid, summary) VALUES ('delete', old.id, old.summary);
END;
CREATE TRIGGER arc_summaries_au AFTER UPDATE OF summary ON arc_summaries BEGIN
    INSERT INTO arc_summaries_fts(arc_summaries_fts, rowid, summary) VALUES ('delete', old.id, old.summary);
    INSERT INTO arc_summaries_fts(rowid, summary) VALUES (new.id, new.summary);
END;

-- 升级旧库时立即回填既有伏笔与弧线/卷摘要到 FTS5 索引
INSERT INTO foreshadowings_fts(foreshadowings_fts) VALUES ('rebuild');
INSERT INTO arc_summaries_fts(arc_summaries_fts) VALUES ('rebuild');
