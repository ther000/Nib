# V2 百万字长篇连载增强规划（M1–M4）

> 本文档沉淀 `/grill-me` 需求对齐后的功能升级共识，作为后续 **M1 → M2 → M3 → M4** 四个可独立验收阶段的施工蓝图。前置基础阶段见 [05-roadmap-and-verification.md](./05-roadmap-and-verification.md)。

---

## 1. 定位与边界

### 核心定位
- 聚焦**个人主力男频 50万~200万字长篇连载**，围绕**写作台高密度精修**与**跨章/全书防吃书与长程推演**补齐短板。

### 明确不做（Out of Scope）
- **不改单章出稿粒度**：维持「整章流式出稿 + 局部划词微调」，不做逐拍分段生成或 A/B 多稿盲选。
- **不做发布端排版**：不做一键复制平台发文格式或「作者有话说」拼接。
- **不做重型微批管理台与重型历法引擎**：不单设复杂批处理控制台，不做自定义纪元/年月日换算公式。
- **不引入重型前端图谱库**：关系图谱采用零外部重型依赖的纯 SVG 交互方案，不引入 React Flow / Cytoscape 等库。

### 架构红线延续
1. 严格遵循 [AGENTS.md](../AGENTS.md)：`internal/` 零 `mygo` 耦合、纯 Go 零 CGO SQLite（`modernc.org/sqlite`）、单向无环依赖 `app -> pipeline -> {compiler, llm, linter, store, exporter} -> {genres, domain}`。
2. 数据库版本化迁移从 `011` 起步；`src/mygo.ts` 严禁手改，统一经 `bunx mygo generate` 生成。
3. 任何正文覆写或大范围替换前，必须先归档 `chapter_versions` 快照。

---

## 2. 分批交付总览（`M1 → M2 → M3 → M4`）

| 阶段 | 核心目标 | 核心功能清单 | SQLite 迁移 | 主要触达范围 | 独立验收标准 |
| --- | --- | --- | --- | --- | --- |
| **M1** | 写作台高密度精修升级 | 左侧可折叠「三合一参照栏」、可复用划词指令库 + 词级 Diff、网文敏感词 0 成本预检 | `011_m1_studio_presets_and_sensitive.sql` | `internal/linter`, `internal/store`, `app/style_service.go`, `src/views/StudioView.tsx`, `src/components/studio/*`, `src/components/atlas/StyleRulesBoard.tsx` | 三合一参照不打断中文 IME；自定义改写指令持久化可复用且有词级 Diff；敏感词扫描毫秒级命中并支持一键替换 |
| **M2** | 长程数据与安全操作底座 | 故事内时间线与倒计时、实体关系章节演变字段、统一轻量安全撤销点（AI 导演一键撤销 + 实体合并与安全改名） | `012_m2_timeline_relations_and_safety.sql` | `internal/domain`, `internal/store`, `internal/memory`, `internal/compiler`, `internal/pipeline/director.go`, `app/*`, `src/components/DirectorDrawer.tsx`, `src/views/AtlasView.tsx` | 作战简报注入当前时间坐标与倒计时；关系查询按章切片不泄未来；导演写库自动挂撤销点且可一键回退；合并/改名支持预览勾选与双保险快照 |
| **M3** | 可视化人物/势力关系图谱与出场雷达 | 列表 / 纯 SVG 图谱双视图、时间轴滑块联动、出场频次与掉线预警雷达 | —（复用 M2 字段） | `internal/memory`, `app/atlas_service.go`, `src/components/atlas/RelationsBoard.tsx`, `src/views/AtlasView.tsx` | 拖动时间轴实时切换对应章关系拓扑；点击节点高亮 1~2 跳关系网；准确统计出场频次与掉线章数 |
| **M4** | 全书双层一致性体检与脏标修复闭环 | 第一层 0 成本本地全书秒扫、第二层按需单卷/区间 AI 深度语义审计、`upstream_dirty` 串联修复向导 | `013_m4_consistency_audit.sql` | `internal/memory`, `internal/pipeline`, `app/memory_service.go`, `src/views/ArchitectView.tsx`, `src/views/AtlasView.tsx` | 本地秒扫检出已死复现/掉线/超期伏笔/时间倒流/积压变更单与脏标；单卷 AI 审计可持久化回看；脏标章支持向导串联重抽/重审/消标 |

---

## 3. M1 · 写作台高密度精修升级

### 3.1 左侧可折叠「三合一参照栏」
- **布局原则**：挂在 [StudioView.tsx](../src/views/StudioView.tsx) 编辑器左侧（可折叠与调节宽度），右侧抽屉严格保留既有 4 页签（`briefing` / `audit` / `delta` / `versions`），避免与 AI 导演抽屉互相挤压。
- **历史章对照**：固定展示上一章或下拉选择的任意历史章正文（只读渲染），支持章节跳转与折叠，不夺走主 `<textarea>` 焦点或干扰中文输入法组合状态。
- **本章节拍对照**：
  - 默认走 **0 成本本地算法**：根据每张节拍卡绑定的出场实体、关键词权重及节拍序号顺序，将当前正文段落估算映射到对应节拍区间，标出覆盖率与疑似漏写节拍。
  - 当本章已执行 Critic 审校时，优先复用审校返回的节拍落点定位作为精准锚点。
- **本章实体与伏笔速查**：
  - 本地实时匹配当前正文出现的实体主名与 `aliases`。
  - 调用 `MemoryService.SnapshotAt(bookId, chapterSeq, includePending)`，直接展示截至本章时间点的实体属性（境界、伤势、归属、法宝等）与关联待收伏笔。
  - 在审校阅读态（[AuditProseParagraph.tsx](../src/components/AuditProseParagraph.tsx)）支持点击高亮实体名弹出档案详情卡片。

### 3.2 可复用划词改写指令库 + 词级 Diff 对比
- **持久化指令库**：扩展 [SelectionRewriteBar.tsx](../src/components/studio/SelectionRewriteBar.tsx)，允许作者将常用自定义精修口令（如「加强压迫感」「补充招式碰撞细节」「改冷峻短句口吻」）保存为可排序/可删改的快捷按钮，落库于 `rewrite_presets` 表。
- **词级 Diff 预览**：划词流式改写完成后，复用 [textDiff.ts](../src/lib/textDiff.ts) 对原文选区与候选改写做词级高亮 Diff 展示，作者确认后再一键替换正文并保留撤销能力。

### 3.3 网文敏感词 / 审核雷区 0 成本预检
- **复用规则引擎**：复用 `internal/linter` 确定性扫描器与 [StyleRulesBoard.tsx](../src/components/atlas/StyleRulesBoard.tsx) 管理界面。
- **迁移改造注意点**：[003_phase3_style.sql](../internal/store/migrations/003_phase3_style.sql) 中 `deai_rules.category` 带有 `CHECK (category IN ('cliche', 'structure', 'formatting'))`，SQLite 不支持直接 `ALTER` 修改 `CHECK` 约束；因此 `011` 迁移需通过**无损建新表迁数据再替换**的方式，将 `category` 扩展为 `('cliche', 'structure', 'formatting', 'sensitive')`，并新增 `replacement TEXT NOT NULL DEFAULT ''` 列。
- **交互闭环**：作者可在规则看板批量导入或单条维护高危敏感词/正则及替换词；进入写作台后 0 成本毫秒级标红提醒，审校态与问题清单支持按 `replacement` 一键替换为安全词或自定义改写。

---

## 4. M2 · 长程数据与安全操作底座

### 4.1 故事内时间线与期限倒计时
- **轻量双字段**：在 `chapters` 表新增 `story_time_label TEXT NOT NULL DEFAULT ''`（自由中文标签，如「天元三年秋·入宗第 7 日」）与 `story_day INTEGER`（可空相对天数，如 `7`，专门用于算间隔和检倒流）。
- **期限倒计时表**：新增 `story_deadlines` 表，记录书内重大约定或危机倒计时（标题、起始章、目标天数 `target_day`、目标时间描述、关联实体/伏笔 ID、状态 `active / reached / expired / canceled`）。
- **变更单自动推算**：扩展 [extractor.go](../internal/memory/extractor.go)，完章抽取记忆变更单时结合前章时间坐标与正文线索，自动给出本章时间标签、相对天数及期限状态变更建议，随变更单交由作者审定入库。
- **上下文注入**：[internal/compiler](../internal/compiler) 编译作战简报时，注入当前时间坐标与处于 `active` 状态的临近期限（附「距截止还剩 N 天」或「已超期」提示）。

### 4.2 实体关系章节演变维度
- **扩展关系表**：在 `entity_relations`（原定义于 [007_director_and_relations.sql](../internal/store/migrations/007_director_and_relations.sql)）新增 `effective_chapter_seq INTEGER NOT NULL DEFAULT 0`、`ended_chapter_seq INTEGER`（`NULL` 表示至今仍生效）、`status TEXT NOT NULL DEFAULT 'committed'`。
- **演变抽取与审定**：完章变更单支持提出关系变更建议（例如第 80 章起将「师兄弟」设为 `ended_chapter_seq = 80`，同时新增一条 `effective_chapter_seq = 80` 的「死敌」关系），由作者确认后生效。
- **时间切片查询**：新增 `ListEntityRelationsAt(bookID, chapterSeq, includePending)`，按 `effective_chapter_seq <= chapterSeq AND (ended_chapter_seq IS NULL OR ended_chapter_seq > chapterSeq)` 过滤，确保不会把未来章节的关系反转透漏给早期写作上下文。

### 4.3 统一轻量安全撤销点机制
- **公共热备元数据**：复用 `internal/store/db.go` 的 SQLite 在线备份与安全恢复内核，新增 `safety_snapshots` 表记录快照 ID、文件路径、触发来源（`director_turn` / `entity_merge` / `book_rename`）、关联书籍与摘要，并按上限滚动清理旧快照防止磁盘膨胀。
- **AI 导演无打断一键撤销**：
  - 在 [director.go](../internal/pipeline/director.go) 中，当某轮 `DirectorService.SendInstruction` 首次命中写库型工具前，静默生成一个轻量撤销点快照，并将快照 ID 回填至 `director_messages.undo_snapshot_id`。
  - [DirectorDrawer.tsx](../src/components/DirectorDrawer.tsx) 在对应助手消息卡片下方渲染「撤销本轮改动」按钮（不阻塞正常对话流，发现改歪可秒级回退到执行前库状态）。
- **实体去重合并 & 全书级安全改名**：
  - 采用**「预览确认 → 事务执行」两阶段 RPC**（`PreviewEntityMerge` / `ExecuteEntityMerge`、`PreviewBookRename` / `ExecuteBookRename`）。
  - **预览与防误伤勾选**：按「章节正文 / 节拍卡 / 章纲卷纲与摘要 / 实体档案与别名 / 关系 / 伏笔 / 状态事件」分类列出全部命中片段；正文替换支持逐章甚至逐条匹配项取消勾选，规避子串误伤（如将「林风」改为「林寒」时手动剔除「林风云」）。
  - **双保险落库**：执行阶段在单个事务内迁移外键与 JSON 引用；凡涉及正文的章节强制先写入 `chapter_versions`（标注 `reason = 'batch_rename'` 或 `'entity_merge'`），同时在操作前静默创建一次 `safety_snapshots` 热备；实体合并默认将源实体名称及别名合并入目标实体的 `aliases`，是否同步替换历史正文由作者勾选开关控制。

---

## 5. M3 · 可视化人物/势力关系图谱与出场雷达

### 5.1 列表 / 纯 SVG 交互拓扑图谱双视图
- **视图融合**：在 [RelationsBoard.tsx](../src/components/atlas/RelationsBoard.tsx) 顶部提供「关系列表 / 势力图谱」双视图切换，保留列表态快速录入能力。
- **纯 SVG 轻量化渲染**：零第三方图库依赖，在前端实现确定性力导向/同心环分组布局，支持画布拖拽平移、滚轮缩放、按实体分类/势力/关系类型过滤。
- **交互钻取**：点击任意节点高亮其 1~2 跳关联子图与关系标签，右侧/浮层卡片展示该实体的核心属性、出场章列表，并支持一键跳入对应实体档案。

### 5.2 时间轴实时联动
- 复用 [AtlasView.tsx](../src/views/AtlasView.tsx) 顶部现有的 `timelineSeq` 滑块与 `includePending` 开关。
- 拖动章节滑块时，图谱同步调用 `ListEntityRelationsAt` 与 `SnapshotAt`，实时重绘截至该章的势力格局与人物阵营归属（已终止的历史关系以虚线隐藏或移出当前拓扑）。

### 5.3 出场频次与掉线雷达
- 统计指定书籍所有核心实体的**总出场次数**、**最后出场章号（`lastSeenSeq`）**以及**已掉线章数（`latestChapterSeq - lastSeenSeq`）**（综合正文提及与节拍出场绑定）。
- 在图谱节点徽标与实体档案列表同步显示掉线状态（如超过设定阈值标注「已掉线 25 章」），帮助作者及时召回边缘化配角。

---

## 6. M4 · 全书双层一致性体检与脏标修复闭环

### 6.1 第一层：0 成本本地全书秒扫
- 纯 Go 实现于 `internal/memory` / `internal/pipeline`，零模型 Token 消耗，毫秒~秒级返回结构化诊断列表，每条均附精确章号与一键跳转入口（直达 `StudioView` / `ArchitectView` / `AtlasView`）：
  1. **已死/已毁实体复现**：状态事件已标记死亡、损毁或彻底移出的角色/法宝，在后续章正文或节拍中再次以活跃状态出场。
  2. **核心配角长期掉线**：主要实体连续超过阈值 `N` 章未出场且未标记退场/死亡。
  3. **超期未收伏笔**：状态仍为 `planted` / `hinted` 且当前章号已超过 `planned_resolve_end`。
  4. **故事内时间线倒流或异常跨度**：相邻章节 `story_day` 出现倒退，或超出合理范围的跳跃，以及期限倒计时已过期但未结算。
  5. **积压待审与脏标章**：全书所有仍处于 `pending` 状态的 `chapter_deltas` 与标记了 `upstream_dirty = 1` 的章节。

### 6.2 第二层：按需单卷 / 指定区间 AI 深度语义审计
- 针对作者指定的一卷或连续章节区间，组装该区间内的分层摘要（`arc_10` / `volume`）、关键状态事件流水、时间线节点与待收伏笔，送交 `critic` 路由执行跨章语义深度审查。
- 重点排查本地规则无法识别的深层逻辑问题：**战力体系跨阶崩坏**、**人物行为动机前后矛盾**、**金手指规则自相矛盾或设定暗冲突**。
- 审计结果持久化至 `book_consistency_reports` 表，支持历史回看与逐条标记已处理/忽略。

### 6.3 下游脏标（`upstream_dirty`）串联修复向导
- 当作者修改前章正文或确认前章记忆变更单导致下游 `N+1..M` 章被自动标脏时，在 [ArchitectView.tsx](../src/views/ArchitectView.tsx) 与体检面板提供集中「脏标修复队列」。
- 清晰展示每章被标脏的原因与上游改动摘要，支持作者按章号顺序串联执行以下动作：
  - 跳转该章正文对比修改；
  - 一键重抽本章记忆变更单（`ExtractChapterDelta`）；
  - 重跑 Critic 盲审复核受影响剧情；
  - 人工核查无误后批量或单章清除 `upstream_dirty` 标记。

---

## 7. SQLite 迁移规划与向后兼容（`011`–`013`）

所有迁移文件存放于 `internal/store/migrations/`，升级前自动执行 `nib.db.bak.<version>` 热备；新增字段均设向后兼容默认值，不破坏存量数据。

| 迁移文件 | 所属阶段 | 表结构变更摘要 | 兼容性说明 |
| --- | --- | --- | --- |
| `011_m1_studio_presets_and_sensitive.sql` | M1 | 1. 新建 `rewrite_presets(id, book_id, label, instruction, sort_order, created_at)`<br>2. 无损重建 `deai_rules` 表，扩展 `category CHECK` 增加 `'sensitive'` 并追加 `replacement TEXT NOT NULL DEFAULT ''` | 通过 `CREATE TABLE ... -> INSERT INTO ... SELECT ... -> DROP/RENAME` 保留用户既有去 AI 味规则与自定义开关状态 |
| `012_m2_timeline_relations_and_safety.sql` | M2 | 1. `chapters` 新增 `story_time_label TEXT NOT NULL DEFAULT ''`、`story_day INTEGER`<br>2. 新建 `story_deadlines(id, book_id, title, start_chapter_seq, target_day, target_label, entity_id, foreshadowing_id, status, note, created_at, updated_at)`<br>3. `entity_relations` 新增 `effective_chapter_seq INTEGER NOT NULL DEFAULT 0`、`ended_chapter_seq INTEGER`、`status TEXT NOT NULL DEFAULT 'committed'`<br>4. `director_messages` 新增 `undo_snapshot_id INTEGER NOT NULL DEFAULT 0`<br>5. 新建 `safety_snapshots(id, book_id, source, source_ref_id, summary, file_path, created_at)` | `story_day` 与 `ended_chapter_seq` 允许 `NULL`；既有关系默认 `effective_chapter_seq = 0`（自开篇即有效）与 `'committed'` |
| `013_m4_consistency_audit.sql` | M4 | 新建 `book_consistency_reports(id, book_id, scope_type, scope_ref_id, start_seq, end_seq, model_name, findings_json, summary, created_at)` | 纯增量新表，无旧表改造风险 |

---

## 8. 自证验证与关键回归用例

### 统一验证入口（每阶段交付前必跑）

```powershell
# 1. 核心引擎与仓储全量单元测试（纯 Go 零 CGO，零 skip）
$env:CGO_ENABLED="0"; go vet ./... && go test -v ./...

# 2. 前后端强类型绑定生成校验（Windows 环境关闭 buildvcs 避免属主提示）
$env:GOFLAGS="-buildvcs=false"; bunx mygo generate

# 3. 前端单测、类型检查与生产构建
bun test && bun run typecheck && bun run build:web
```

### 各阶段必补 Golden 回归用例
1. **M1**：敏感词词条/正则匹配与 `replacement` 一键替换测试；划词指令持久化 CRUD 与词级 Diff 边界用例（空串、中文标点、多段选区）。
2. **M2**：
   - 时间线倒流识别与 `story_deadlines` 剩余天数边界校验（`target_day == story_day`、过期判定）；
   - `ListEntityRelationsAt` 切片防泄漏 Golden 用例（第 80 章绝裂后，查第 50 章仍返回同门且不可见未来关系）；
   - 实体去重合并事务一致性测试（外键/伏笔/节拍全量迁移、零孤儿引用、旧名入别名、强制落 `chapter_versions`）；
   - AI 导演触发写库前自动落点并在一键撤销后完整回退库状态。
3. **M3**：多时间点图谱边集过滤单测 + 实体出场频次与 `lastSeenSeq` 掉线差值统计测试。
4. **M4**：构造 50 章长篇连载 Golden Fixture（内含已死复现、配角掉线 30 章、超期伏笔、时间倒流、`pending` 积压与 `upstream_dirty` 链），验证 0 成本本地秒扫 100% 检出率且零误报。
