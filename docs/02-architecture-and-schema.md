# 系统架构、接口契约与数据模型

## 1. 分层架构与解耦原则

```text
+-------------------------------------------------------------------------+
| src/  (React 19 + TypeScript + Vite + Tailwind CSS v4, WebView2)        |
|   ├── ArchitectView   ├── StudioView   ├── AtlasView   ├── Settings     |
|   └── 唯一通道: src/mygo.ts (由 mygo generate 自动生成)                  |
+------------------------------------+------------------------------------+
                                     │ Typed RPC / *mygo.Channel[T] / Events
                                     ▼
+------------------------------------+------------------------------------+
| app/  (薄桌面绑定层: 仅依赖 github.com/egoist/mygo v0.10.0)              |
|   ├── LibraryService  ├── AtlasService   ├── SettingsService            |
|   ├── StudioService   ├── MemoryService  ├── StyleService  ├── Batch... |
+------------------------------------+------------------------------------+
                                     │ 纯 Go 接口调用 (零 MyGo 依赖)
                                     ▼
+------------------------------------+------------------------------------+
| internal/  (纯 Go 核心引擎，可独立运行 go test ./...)                    |
|   ├── pipeline  ──► {compiler, llm, linter, store, exporter}            |
|   ├── memory    ──► {store, llm}                                        |
|   ├── compiler  ──► {store, genres, domain}                             |
|   ├── llm       ──► net/http + SSE 解析 (OpenAI / Anthropic / Gemini)   |
|   ├── linter    ──► domain (0 成本确定性正则与词库扫描)                   |
|   ├── exporter  ──► store + domain (单向 Markdown 镜像)                 |
|   └── store     ──► modernc.org/sqlite (WAL + FK + FTS5)                |
+-------------------------------------------------------------------------+
```

### 架构关键决策
- **桌面壳可平替**：MyGo 处于 `v0.10.0` 快速演进期。把所有核心状态机、编译管线、数据库仓储隔离在 `internal/` 后，即便未来更换桌面壳也只需重写 `main.go` 与 `app/` 薄层。
- **MyGo 运行期生命周期安全**：`mygo generate` 会启动 `main()` 至 `mygo.App.Run()` 后立即退出。`main.go` 的职责严格限定为：
  1. `mygo.App.RequestSingleInstanceLock()` 防双开争抢 SQLite 写锁；
  2. `mygo.Bind(...)` 注册全部 Service；
  3. `mygo.App.WhenReady(...)` 内打开并迁移 SQLite、恢复未完成微批状态、创建 `Hidden: true` + `BackgroundColor: "light-dark(#F6F3EC, #141416)"` + `StateKey: "main"` 的主窗口并在 `OnReadyToShow` 时显示；
  4. `mygo.App.OnBeforeQuit(...)` 中发出退出上下文取消、等待正在落库的写事务完成后关闭 DB。

### MyGo 启动与窗口配置基线（`main.go`）

```go
func main() {
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}

	var mainWin *mygo.Window
	mygo.App.OnSecondInstance(func(_ []string, _ string) {
		if mainWin != nil {
			mainWin.Restore()
			mainWin.Focus()
		}
	})

	// 仅注册 Service 绑定，确保 mygo generate 执行 main() 到 App.Run() 时不触碰数据库
	mygo.Bind(&app.LibraryService{}, &app.AtlasService{}, &app.SettingsService{}, &app.StudioService{})

	mygo.App.WhenReady(func() {
		dataDir, _ := mygo.App.Path(mygo.PathUserData)
		// 在此初始化 SQLite(filepath.Join(dataDir, "nib.db")) 并执行迁移
		mainWin = mygo.NewWindow(mygo.WindowOptions{
			Title:           "Nib",
			URL:             "/",
			Width:           1360,
			Height:          860,
			MinWidth:        960,
			MinHeight:       640,
			StateKey:        "main",
			Hidden:          true,
			BackgroundColor: "light-dark(#F6F3EC, #141416)",
		})
		mainWin.OnReadyToShow(mainWin.Show)
	})

	mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) {
		// 取消运行中任务并关闭 SQLite 连接
	})

	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
```

---

## 2. 仓库目录规划（覆盖 Phase 1–4）

```text
Nib/
├── go.mod / go.sum                      # [P1] 锁定 github.com/egoist/mygo v0.10.0 + modernc.org/sqlite
├── main.go                              # [P1] 生命周期、单实例锁、防闪白主窗口创建
├── mygo.config.ts                       # [P1] identifier: com.nib.studio, bindings: src/mygo.ts
├── package.json                         # [P1] React 19, Vite, Tailwind v4, mygo-runtime, mygo-cli
├── tsconfig.json / vite.config.ts       # [P1]
├── AGENTS.md / README.md                # [设计文档已落地]
├── docs/                                # [设计文档已落地]
├── internal/
│   ├── domain/                          # [P1-P4] 领域模型、错误类型与状态枚举
│   ├── genres/                          # [P1] 男频四套题材包（xuanhuan/xianxia/urban/general）
│   ├── store/
│   │   ├── db.go                        # [P1] SQLite WAL 初始化 + 事务包装 + 编号迁移执行器
│   │   └── migrations/
│   │       ├── 001_phase1_core.sql      # [P1]
│   │       ├── 002_phase2_memory.sql    # [P2]
│   │       ├── 003_phase3_style.sql     # [P3]
│   │       └── 004_phase4_batch.sql     # [P4]
│   ├── exporter/                        # [P1] Markdown 合法文件名清洗与卷章树镜像导出
│   ├── llm/                             # [P1] OpenAI-compat / Anthropic / Gemini 流式适配器与测速
│   ├── compiler/                        # [P1-P3] 作战简报编译与确定性 Token 预算分配
│   ├── pipeline/                        # [P1-P4] 单章生成、审校、局部润色与串行微批调度
│   ├── memory/                          # [P2] 状态账本时间旅行、FTS5 检索、完章增量抽取
│   └── linter/                          # [P3] 0 成本中文网文 AI 味探测器与样式规则引擎
├── app/                                 # [P1-P4] MyGo Service 绑定（供 mygo generate 生成 TS）
│   ├── library_service.go               # [P1,P4] 书/卷/章/节拍 CRUD、批量章纲生成、全书导出
│   ├── atlas_service.go                 # [P1] 实体与世界铁律 CRUD
│   ├── settings_service.go              # [P1,P4] Provider CRUD、测速、5 节点路由、费用统计
│   ├── studio_service.go                # [P1] 简报预览、流式出稿、版本快照与回滚
│   ├── memory_service.go                # [P2] 时间轴快照查询、伏笔板、记忆变更单确认/驳回
│   ├── style_service.go                 # [P3] 样章文风抽取、AI 味检测、Critic 审校、手术 Diff 润色
│   └── batch_service.go                 # [P4] 1~5 章串行微批创建、进度订阅、中止与单章重跑
└── src/
    ├── mygo.ts                          # [自动生成] 严禁手改
    ├── styles/tokens.css                # [P1] 深色/暖纸主题 Tokens 与 720px 中文阅读排版变量
    ├── components/                      # [P1-P4] 顶栏、Diff 视图、模态窗、快捷键监听
    └── views/
        ├── ArchitectView.tsx            # [P1,P4] 左卷章树 + 中曲线/节拍卡 + 右微批队列
        ├── StudioView.tsx               # [P1,P2,P3] 80% 正文审改画布 + 右抽屉（简报/质检/变更单/历史）
        ├── AtlasView.tsx                # [P1,P2,P3] 铁律/实体卡 + 时间轴滑块 + 伏笔板 + 文风规则库
        └── SettingsModal.tsx            # [P1,P4] 多 API、5 环节模型路由、导出目录、主题切换
```

---

## 3. MyGo Service 接口契约清单

前端通过 `src/mygo.ts` 调用以下 7 个绑定服务。凡涉及流式输出或长任务，统一采用 `*mygo.Channel[T]` 并把 `ctx context.Context` 置为 Go 方法首个参数（映射前端 `with({ signal: AbortController.signal })`）。

| Service | 方法签名的核心能力 | 所属阶段 | 流式 / 取消契约 |
| --- | --- | --- | --- |
| `LibraryService` | `ListGenrePresets()` / `ListBooks()` / `CreateBook(input)` / `UpdateBook(input)` | P1 | 同步 RPC |
| | `ListVolumes(bookID)` / `UpsertVolume(input)` / `ListChapters(volumeID)` / `CreateChapter(input)` / `SaveChapterDraft(input)` / `FinalizeChapter(chapterID)` | P1 | 同步 RPC（保存/定稿触发单向导出） |
| | `ReplaceBeats(chapterID, beats[])` / `ExportBookMarkdown(bookID)` | P1 | 同步 RPC |
| | `BatchGenerateOutlines(ctx, req, ch *mygo.Channel[OutlineChunk])` | P4 | Channel 流式生成 3~10 章结构化章纲与节拍 |
| `AtlasService` | `ListEntities(bookID, filter)` / `UpsertEntity(input)` / `DeleteEntity(id)` | P1 | 同步 RPC |
| `SettingsService` | `ListProviders()` / `UpsertProvider(input)` / `DeleteProvider(id)`（返回脱敏密钥） | P1 | 同步 RPC |
| | `TestProvider(ctx, id)` | P1 | `context.Context` 超时控制（10s） |
| | `ListModelRoutes()` / `SaveModelRoute(route)` | P1 | 同步 RPC |
| | `GetUsageSummary(bookID, range)` | P4 | 同步 RPC（Token 与费用聚合） |
| `StudioService` | `PreviewBriefing(chapterID, opts)` | P1 | 同步 RPC（返回结构化槽位与 Token 估算） |
| | `GenerateChapter(ctx, chapterID, opts, ch *mygo.Channel[GenChunk])` | P1 | Channel 逐 token 推流；中断自动保留 `partial` 文本并先落快照 |
| | `ListChapterVersions(chapterID)` / `RestoreVersion(versionID)` | P1 | 同步 RPC |
| `MemoryService` | `SnapshotAt(bookID, chapterSeq, includePending)` | P2 | 同步 RPC（时间轴回看全书实体实时状态） |
| | `ListForeshadowings(bookID, status)` / `UpsertForeshadowing(input)` | P2 | 同步 RPC |
| | `ExtractChapterDelta(ctx, chapterID)` / `GetPendingDelta(chapterID)` | P2 | 支持 AbortSignal 取消抽取出错重试 |
| | `ApplyDeltaDecisions(deltaID, decisions[])` | P2 | 事务提交选中的状态事件与伏笔，驳回则标 `reverted` |
| | `SearchMemory(bookID, query, maxChapterSeq)` | P2 | 同步 RPC（FTS5 + 双字 `LIKE` 混合检索） |
| `StyleService` | `ExtractStyleProfile(ctx, bookID, sampleTexts[])` / `UpsertSample(input)` | P3 | 支持 AbortSignal |
| | `ListDeaiRules(bookID)` / `UpsertDeaiRule(input)` | P3 | 同步 RPC |
| | `LintChapterLocal(chapterID)` | P3 | 同步 RPC（<20ms 返回全部局部定位告警） |
| | `RunCriticAudit(ctx, chapterID)` / `GenerateSurgicalDiffs(ctx, chapterID, targetParagraphs[])` | P3 | 支持 AbortSignal；Diff 返回行内前后对比数组 |
| `BatchService` | `StartBatchRun(bookID, chapterIDs[], mode)` | P4 | 立即返回 `batchID`，后台串行执行；进度通过 `mygo.NewEvent[BatchProgress]("batch:progress")` 推送 |
| | `CancelBatchRun(batchID)` / `RetryBatchItem(itemID)` / `GetBatchRun(batchID)` | P4 | 同步控制接口，取消当前章节安全点后中止队列 |

---

## 4. 完整 SQLite Schema（按阶段版本化迁移）

所有迁移文件存放于 `internal/store/migrations/` 并通过 `//go:embed` 打包进二进制。

### 4.1 `001_phase1_core.sql`

```sql
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
```

### 4.2 `002_phase2_memory.sql`

```sql
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
    id                 INTEGER PRIMARY KEY,
    book_id            INTEGER NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    title              TEXT NOT NULL,
    description        TEXT NOT NULL DEFAULT '',
    planted_chapter_seq INTEGER NOT NULL,
    last_hint_seq      INTEGER NOT NULL DEFAULT 0,
    target_resolve_seq INTEGER NOT NULL DEFAULT 0,
    resolved_chapter_seq INTEGER NOT NULL DEFAULT 0,
    status             TEXT NOT NULL DEFAULT 'planted'
                       CHECK (status IN ('pending_plant', 'planted', 'hinted', 'resolved', 'abandoned')),
    involved_entity_ids_json TEXT NOT NULL DEFAULT '[]',
    created_at         TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at         TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
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
    id               INTEGER PRIMARY KEY,
    chapter_id       INTEGER NOT NULL UNIQUE REFERENCES chapters(id) ON DELETE CASCADE,
    causal_summary   TEXT NOT NULL DEFAULT '',
    new_entities_json TEXT NOT NULL DEFAULT '[]',
    state_patch_json TEXT NOT NULL DEFAULT '[]',
    foreshadow_json  TEXT NOT NULL DEFAULT '[]',
    high_risk_flags  INTEGER NOT NULL DEFAULT 0,
    review_status    TEXT NOT NULL DEFAULT 'pending' CHECK (review_status IN ('pending', 'applied', 'dismissed')),
    created_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    reviewed_at      TEXT NOT NULL DEFAULT ''
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
```

### 4.3 `003_phase3_style.sql`

```sql
-- 文风指纹档案（每本书可维护一套量化风格约束）
CREATE TABLE style_profiles (
    id                   INTEGER PRIMARY KEY,
    book_id              INTEGER NOT NULL UNIQUE REFERENCES books(id) ON DELETE CASCADE,
    avg_sentence_chars   REAL NOT NULL DEFAULT 18.0,
    max_paragraph_chars  INTEGER NOT NULL DEFAULT 140,
    dialogue_ratio_min   REAL NOT NULL DEFAULT 0.25,
    dialogue_ratio_max   REAL NOT NULL DEFAULT 0.55,
    pov_rule             TEXT NOT NULL DEFAULT 'third_limited',
    tone_directives      TEXT NOT NULL DEFAULT '',
    forbidden_tropes_note TEXT NOT NULL DEFAULT '',
    updated_at           TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- 分场景样章锚点库（按节拍场景动态选 1~2 段喂给写作模型）
CREATE TABLE style_samples (
    id          INTEGER PRIMARY KEY,
    book_id     INTEGER NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    scene_type  TEXT NOT NULL CHECK (scene_type IN ('combat', 'dialogue', 'daily', 'suspense')),
    title       TEXT NOT NULL,
    excerpt     TEXT NOT NULL,
    char_count  INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX idx_style_samples_book_scene ON style_samples(book_id, scene_type);

-- 去 AI 味黑名单与结构规则库（book_id=0 表示全局默认内置规则）
CREATE TABLE deai_rules (
    id             INTEGER PRIMARY KEY,
    book_id        INTEGER NOT NULL DEFAULT 0,
    category       TEXT NOT NULL CHECK (category IN ('cliche', 'structure', 'formatting')),
    pattern        TEXT NOT NULL,
    is_regex       INTEGER NOT NULL DEFAULT 0,
    severity       TEXT NOT NULL DEFAULT 'warn' CHECK (severity IN ('info', 'warn', 'error')),
    max_per_chapter INTEGER NOT NULL DEFAULT 0,
    hint           TEXT NOT NULL DEFAULT '',
    enabled        INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX idx_deai_rules_book ON deai_rules(book_id, enabled);

-- 章节审校与手术润色报告
CREATE TABLE chapter_audits (
    id               INTEGER PRIMARY KEY,
    chapter_id       INTEGER NOT NULL UNIQUE REFERENCES chapters(id) ON DELETE CASCADE,
    lint_json        TEXT NOT NULL DEFAULT '[]',
    critic_json      TEXT NOT NULL DEFAULT '[]',
    diffs_json       TEXT NOT NULL DEFAULT '[]',
    hook_passed      INTEGER NOT NULL DEFAULT 0,
    deai_score       INTEGER NOT NULL DEFAULT 100,
    updated_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
```

### 4.4 `004_phase4_batch.sql`

```sql
-- 串行微批调度任务容器
CREATE TABLE batch_runs (
    id          INTEGER PRIMARY KEY,
    book_id     INTEGER NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    mode        TEXT NOT NULL CHECK (mode IN ('draft_only', 'draft_and_polish', 'full_pipeline')),
    status      TEXT NOT NULL CHECK (status IN ('running', 'paused', 'completed', 'canceled', 'failed')),
    total_items INTEGER NOT NULL DEFAULT 0,
    done_items  INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    finished_at TEXT NOT NULL DEFAULT ''
);

-- 微批单章排队明细
CREATE TABLE batch_run_items (
    id           INTEGER PRIMARY KEY,
    batch_run_id INTEGER NOT NULL REFERENCES batch_runs(id) ON DELETE CASCADE,
    chapter_id   INTEGER NOT NULL REFERENCES chapters(id) ON DELETE CASCADE,
    seq          INTEGER NOT NULL,
    state        TEXT NOT NULL CHECK (state IN (
                    'queued', 'compiling', 'drafting', 'linting',
                    'critiquing', 'polishing', 'extracting',
                    'awaiting_review', 'failed', 'canceled'
                 )),
    input_hash   TEXT NOT NULL DEFAULT '',
    error_msg    TEXT NOT NULL DEFAULT '',
    started_at   TEXT NOT NULL DEFAULT '',
    finished_at  TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_batch_items_run ON batch_run_items(batch_run_id, seq);

-- 模型 Token 与费用流水（用于单章与整卷成本统计）
CREATE TABLE token_usage_logs (
    id                INTEGER PRIMARY KEY,
    book_id           INTEGER NOT NULL REFERENCES books(id) ON DELETE CASCADE,
    chapter_id        INTEGER REFERENCES chapters(id) ON DELETE SET NULL,
    stage             TEXT NOT NULL CHECK (stage IN ('outline', 'draft', 'critic', 'polish', 'extract', 'style')),
    provider_name     TEXT NOT NULL,
    model_name        TEXT NOT NULL,
    prompt_tokens     INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    latency_ms        INTEGER NOT NULL DEFAULT 0,
    created_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX idx_token_usage_book ON token_usage_logs(book_id, created_at);
```

---

## 5. 本地存储与单向 Markdown 镜像导出

- **数据库路径**：调用 MyGo `mygo.App.Path(mygo.PathUserData)` 获取系统用户数据目录，生产版落盘 `%APPDATA%\Nib\nib.db`，开发版自动隔离至 `%APPDATA%\Nib Dev\nib.db`。
- **单向 Markdown 镜像规则**：
  - 触发时机：作者在审改台手动保存正文、定稿章节，或点击整书导出。
  - 目录布局：`<export_dir>/<合法化书名>/第01卷_<卷名>/第001章_<章名>.md`，外加 `00_设定与铁律汇总.md`。
  - 文件名清洗：剔除 Windows 非法字符 `\/:*?"<>|` 及尾随空格/句点，保留中文与常规标点，截断过长标题至 60 runes 以防路径超长。
  - **为什么禁止双向同步**：外部修改 `.md` 若强行回灌 SQLite，会破坏章节版本链、`entity_state_events` 章节偏移与节拍绑定。需要外发或归档时导出纯净正文 Markdown 即可。

---

## 6. 多 Provider 统一 LLM 适配层

`internal/llm` 直接基于 Go 标准库 `net/http` 构建轻量适配器，统一输出 `StreamEvent{ Delta, Usage, FinishReason, Err }`：

| 协议类型 (`protocol`) | 端点与认证头 | 流式解析与容错关键点 |
| --- | --- | --- |
| `openai_compat` | `POST {base_url}/chat/completions`，`Authorization: Bearer {key}` | 设 `stream: true` 与 `stream_options: {"include_usage": true}`；兼容中转站注释行 `: keepalive`、非标准空 `choices`、末尾 `[DONE]` |
| `anthropic` | `POST {base_url}/v1/messages`，`x-api-key: {key}` + `anthropic-version: 2023-06-01` | 解析 `content_block_delta` (`text_delta`) 与 `message_delta` 中的精确 token `usage` |
| `gemini` | `POST {base_url}/v1beta/models/{model}:streamGenerateContent?alt=sse`，`x-goog-api-key: {key}` | 兼容分段 SSE `candidates[0].content.parts[].text` 与 `usageMetadata` |

### 五环节默认路由策略推荐

| 环节 (`stage`) | 推荐温度 | 模型能力侧重 |
| --- | --- | --- |
| `outline`（大纲/节拍） | `0.65` | 结构化逻辑与反转设计强模型 |
| `draft`（正文撰写） | `0.80` | 中文网文语感强、少翻译腔的模型 |
| `critic`（吃书/钩子审校） | `0.20` | 强指令遵循与 JSON 严格输出模型 |
| `polish`（局部手术润色） | `0.55` | 贴合原风格、不擅自加戏的精细改写模型 |
| `extract`（记忆增量抽取） | `0.10` | 低成本快模型 + 稳定 JSON Schema 输出 |

### 错误分级与退避
- **不可重试（立即停并提示用户）**：`401/403`（Key 无效）、`404`（模型名或 Base URL 错误）、上下文窗口溢出 `400`、`context.Canceled`（用户主动点击停止）。
- **可自动重试（指数退避 1s → 3s → 8s，最多 3 次）**：`429` 限流、`502/503/504` 上游波动、流式响应中途 EOF 断流。

---

> 下一步参考：[03-core-engines.md](./03-core-engines.md)（记忆账本、上下文编译器、节拍微批流水线与去 AI 味规则）。
