-- 010: 补充高频查询与外键级联索引，消除长篇小说规模下的全表扫描

CREATE INDEX IF NOT EXISTS idx_chapters_book_status ON chapters(book_id, status);
CREATE INDEX IF NOT EXISTS idx_state_events_book_status_seq ON entity_state_events(book_id, status, chapter_seq);
CREATE INDEX IF NOT EXISTS idx_batch_runs_book_id ON batch_runs(book_id, id DESC);
CREATE INDEX IF NOT EXISTS idx_batch_runs_status ON batch_runs(status);
CREATE INDEX IF NOT EXISTS idx_batch_items_chapter ON batch_run_items(chapter_id);
CREATE INDEX IF NOT EXISTS idx_token_usage_chapter ON token_usage_logs(chapter_id);
CREATE INDEX IF NOT EXISTS idx_autopilot_runs_status ON autopilot_runs(status);
CREATE INDEX IF NOT EXISTS idx_entity_relations_src ON entity_relations(source_entity_id);
CREATE INDEX IF NOT EXISTS idx_entity_relations_dst ON entity_relations(target_entity_id);
