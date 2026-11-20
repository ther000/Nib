# 分阶段交付路线、施工清单与验收方案

## 1. 阶段切分原则

四个阶段严格保证**可独立合入、每阶段交付后即是一套真实可用的写书软件**，不写跨阶段半成品占位代码。

| 阶段 | 核心交付 | 做完立刻能干什么 | 涉及数据库迁移 |
| --- | --- | --- | --- |
| **Phase 1** | 工程骨架 + BYOK 多模型路由 + 题材包 + 静态实体/铁律 + 节拍出稿 + 审改与导出 | 建一本男频书、配 API 测通、写 4 张节拍、预览简报、流式出章并镜像导出 `.md` | `001_phase1_core.sql` |
| **Phase 2** | 事件溯源状态账本 + 伏笔生命周期 + 3 级摘要 + FTS5 检索 + 完章增量变更单 | 连写 20~100 章时角色伤势/道具/境界/伏笔自动跟踪且可按章滑块回溯 | `002_phase2_memory.sql` |
| **Phase 3** | Style DNA 样章锚定 + 0 成本本地 AI 味探测器 + Critic 盲审 + 局部 Diff 润色 | 草稿出来毫秒级标红 AI 味与吃书冲突，逐段按 Diff 局部洗稿不伤好段落 | `003_phase3_style.sql` |
| **Phase 4** | 卷情绪折线诊断 + 批量生成 3~10 章纲节拍 + 串行微批队列 + Token 统计 | 一次编排 5 章细纲并放着串行自动出稿与抽状态，回来集中审改入库 | `004_phase4_batch.sql` |

---

## 2. Phase 1 详细施工清单（直接照此执行）

### Step 1：初始化 MyGo v0.10.0 骨架与锁定依赖
- 运行 `bunx mygo-cli init -name Nib -module nib .`（或等效脚手架后调整目录），`go.mod` 固定 `github.com/egoist/mygo v0.10.0` 并引入 `modernc.org/sqlite`。
- 配置 `mygo.config.ts`：`identifier: "com.nib.studio"`、`devUrl: "http://localhost:5173"`、`bindings: "src/mygo.ts"`；安装 React 19 + TypeScript + Tailwind CSS v4。
- 编写 `main.go`：单实例锁、`WhenReady` 内初始化 DB、`Hidden: true` + `BackgroundColor: "light-dark(#F6F3EC, #141416)"` + `OnReadyToShow` 防闪白、`OnBeforeQuit` 安全停库。

### Step 2：领域模型与男频题材包（`internal/domain` + `internal/genres`）
- 定义 `Book / Volume / Chapter / Beat / Entity / ChapterVersion / Provider / ModelRoute / Briefing / GenChunk` 结构体及带 JSON tag 强约束枚举。
- 实现 `xuanhuan / xianxia / urban / general` 4 套题材包，内置默认实体字段定义、节拍模板与基础题材指令约束。

### Step 3：SQLite 仓储与 Markdown 导出（`internal/store` + `internal/exporter`）
- 嵌入执行 `001_phase1_core.sql`，每次连接启用 `WAL + foreign_keys=ON + busy_timeout=5000`。
- 实现全部 Phase 1 CRUD 与快照事务：确保更新章节正文前必然先写 `chapter_versions`。
- 实现 `exporter`：清洗 Windows 非法路径字符，按 `<export_dir>/<书名>/第01卷_<卷名>/第001章_<章名>.md` 单向同步输出。

### Step 4：多模型流式适配、编译与生成管线（`internal/llm` + `internal/compiler` + `internal/pipeline`）
- 实现 OpenAI 兼容、Anthropic、Gemini 统一流式解析器及 `TestConnection`。
- 实现 Phase 1 版 `compiler`：装配铁律 + 本章节拍出场实体 + 前 2 章结尾 + 题材规范，输出透明 `Briefing`。
- 实现 `pipeline.GenerateChapter`：流式推块、支持 `context.Cancel` 中途安全停笔保留 `partial` 内容并触发导出。

### Step 5：MyGo 绑定层与强类型客户端生成（`app/` + `src/mygo.ts`）
- 实现 `LibraryService`、`AtlasService`、`SettingsService`（Key 脱敏返回）、`StudioService`（`GenerateChapter` 绑定 `*mygo.Channel[GenChunk]`）。
- 执行 `bunx mygo generate` 生成并校验 `src/mygo.ts`。

### Step 6：UI 四大核心页面（`src/`）
- 配置 `styles/tokens.css`（深色护眼默认 + 暖纸浅色、720px 中文正文排印）。
- 实现顶栏与 `SettingsModal`、`AtlasView`（铁律置顶 + 实体档案）、`ArchitectView`（卷章树 + 节拍卡编排）、`StudioView`（沉浸正文 + 流式输出/中止 + 右侧作战简报预览 & 版本快照对比回滚）。

### Step 7：Phase 1 端到端自证验收
- 运行全部 Go 单测与前端构建命令，启动 `bun run dev` 在真机 Windows 11 窗口完成一条真实写书测试链路。

---

## 3. Phase 2–4 后续交付矩阵（增量演进约束）

- **Phase 2 增量交付**：
  - 数据库：`002_phase2_memory.sql`
  - 后端：`internal/memory`（`SnapshotAt`、伏笔查询、FTS5+双字回退、变更单 JSON 抽取）、`compiler` 接入 `SnapshotAt` 与摘要金字塔、`app/memory_service.go`
  - 前端：`AtlasView` 增加时间轴滑块与伏笔看板；`StudioView` 右侧抽屉新增「记忆变更单」确认界面与高危标红样式
- **Phase 3 增量交付**：
  - 数据库：`003_phase3_style.sql`
  - 后端：`internal/linter`（内置中文 AI 味套话/结构病/排版病词库）、`pipeline` 新增 Critic 盲审与 `[prev, target, next]` 手术润色、`compiler` 接入 Style DNA + 场景样章锚点、`app/style_service.go`
  - 前端：`StudioView` 正文内联波浪/下划线告警与局部 Diff 采纳浮条；`AtlasView` 新增「文风与去 AI 味规则」页签
- **Phase 4 增量交付**：
  - 数据库：`004_phase4_batch.sql`
  - 后端：`pipeline` 串行微批调度器（含崩溃恢复与上游修改置 `upstream_dirty`）、`LibraryService.BatchGenerateOutlines`、`app/batch_service.go`
  - 前端：`ArchitectView` 接入卷级情绪折线与诊断气泡 + 右侧串行微批队列面板；`SettingsModal` 新增 Token 与费用统计面板

---

## 4. 测试、回归与自证基准

| 验证层级 | 验证内容 | 判定标准 |
| --- | --- | --- |
| **仓储与迁移单测** | `:memory:` SQLite 跑全套迁移；外键级联删除；并发读写 WAL；`chapter_versions` 先存后写验证 | `go test ./internal/store/...` 全通过 |
| **流式 LLM Mock 测试** | `httptest.Server` 模拟 OpenAI / Anthropic / Gemini 分片、心跳、429 重试与客户端 Cancel 断开 | 无 Goroutine 泄漏，Cancel 后 100ms 内退出 |
| **记忆防吃书 Golden 测试** | 构造 30 章连续剧情 Fixture（含道具易主、伤势与旧章重写回滚），校验 `SnapshotAt` 绝不读取未来章事件且 `high_risk` 必标红 | 断言 100% 命中，作为每次改动必跑回归用例 |
| **AI 味探测器基准测试** | 准备典型 AI 腔样本 20 段 + 优秀男频网文样章 20 段，验证套话/三段排比/鸡汤结尾/超长段落检出率与低误报 | 样本检出覆盖预置规则 100% |
| **桌面端真实运行验收** | `go test ./... ; bun run typecheck && bun run build:web ; bun run build` 全绿，Windows 实机打开无闪白、流式停止、快捷键与 Markdown 导出实测通过 | 零错误退出码 |

---

## 5. 风险登记与回滚方案

| 风险项 | 触发信号 | 缓解与无损回滚方案 |
| --- | --- | --- |
| MyGo `v0.10.0` 出现 Windows 特定输入法或窗口兼容缺陷 | 升级或实测时发现 WebView2 IME 异常或打包异常 | 因核心全在 `internal/`，可零改动保留全部核心引擎，仅将 `main.go` + `app/` 壳层平替为 Wails v3 |
| 低价模型抽取 `chapter_deltas` 输出非法 JSON | `extract` 环节连续解析失败 | 内置 JSON 围栏剥离与 1 次自动纠错重问；若仍失败则保留正文并将变更单标为待手动补充，绝不丢稿或阻断出稿 |
| 数据库迁移升级失败或误删数据 | 升级新版启动迁移报错 | 每次运行高版本迁移前自动将 `nib.db` 拷贝备份为 `nib.db.bak.<version>`，出错即中止启动并可秒级还原库文件 |
| 整章润色洗坏作者满意片段 | 作者感觉部分改写过猛 | 强约束采用逐段手术 Diff + 任何覆盖先落 `chapter_versions`，点击版本历史可毫秒级回滚任意版本 |
