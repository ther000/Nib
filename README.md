# Nib

面向个人长篇连载作者的本地桌面 AI 网文写作工作台，围绕男频多题材（玄幻、仙侠、都市及通用男频）打造，核心解决长篇创作中的三大硬问题：百万字防吃书记忆、男频爽点与断章钩子节奏编排、去 AI 味与文风贴合。

> 当前仓库状态：Phase 1–4 全部核心引擎、SQLite 迁移（`001`–`009`）、MyGo v0.10.0 桌面绑定与 React 工作台均已实现，并通过全量单元测试与 Windows 桌面构建验证。工作台现支持书籍级写作规范与码字统计、专注写作与历史版本 Diff、全局命令面板、灵感便签、软删除回收站、自动滚动备份与恢复、章节拖拽/跨卷/批量操作，以及旧稿导入后的 AI 设定反推。

## 核心设计支柱

1. **微批处理 + 双人类闸门工作流**
   - **闸门 1（编排骨架）**：作者锁定卷纲、章纲情绪极性与节拍卡。
   - **串行流水线（1~5 章接力）**：上下文编译 → 节拍出稿 → 本地 AI 味检测 → 独立审校 → 手术式局部润色 → 状态变更抽取。
   - **闸门 2（高速审改）**：内联标红审校、局部 Diff 采纳、确认「记忆变更单」入库。
2. **百万字防吃书引擎（4 层混合记忆）**
   - 世界铁律（硬约束）+ 事件溯源状态账本（按章时间轴回看/回滚）+ 伏笔生命周期追踪 + 3 级摘要金字塔与 SQLite FTS5 精确检索。
   - 抽取结果不静默写入正式账本，必须生成变更单由作者一键确认。
3. **男频多题材节奏与去 AI 味**
   - 内置玄幻 / 仙侠 / 都市 / 通用男频 4 套题材包，显式编排 `立靶 → 蓄压 → 转折 → 爆发 → 落袋/留钩`。
   - 文风指纹 + 分场景样章锚定 + 0 成本本地确定性中文网文 AI 味探测器 + 病灶段落手术式局部 Diff 润色。

## 技术栈与运行环境

| 层次 | 选型 | 说明 |
| --- | --- | --- |
| 桌面壳 | [MyGo v0.10.0](https://mygo.run/) (`Web Frontend`) | 纯 Go、零 CGO，复用系统 WebView2，单文件约 7MB，内存约 62MB |
| 核心后端 | Go 1.27+ | 零 MyGo 依赖的纯 Go `internal/` 引擎，支持并发流式转发与单元测试 |
| 本地存储 | `modernc.org/sqlite` | 纯 Go 零 CGO SQLite（WAL + Foreign Keys + FTS5）+ 单向 Markdown 镜像导出 |
| 前端界面 | React 19 + TypeScript + Vite + Tailwind CSS v4 | Bun 驱动构建，`mygo generate` 自动生成强类型 `src/mygo.ts` |
| 模型接入 | BYOK（自带 API） | 统一流式适配 OpenAI 兼容、Anthropic、Gemini，支持 5 环节独立路由模型 |

### 本地环境要求

- Windows 10/11 x64（已内置 WebView2 Runtime）
- Go `>= 1.27`
- Bun `>= 1.4`

## 设计文档导航

| 文档 | 内容范围 |
| --- | --- |
| [AGENTS.md](./AGENTS.md) | 架构红线、MyGo 避坑指南、数据安全规范与验证入口 |
| [01-product-and-domain.md](./docs/01-product-and-domain.md) | 产品定位、双闸门主循环、4 套男频题材包规格、领域术语表 |
| [02-architecture-and-schema.md](./docs/02-architecture-and-schema.md) | 分层架构、目录树、Service 契约、完整 4 阶段 SQLite DDL、Provider 适配 |
| [03-core-engines.md](./docs/03-core-engines.md) | 状态账本时间旅行、上下文编译器配额、节奏流水线状态机、去 AI 味规则与手术润色 |
| [04-ui-ux-design.md](./docs/04-ui-ux-design.md) | 静谧深色/暖纸 Design Tokens、三大工作区线框图、快捷键与异常态交互 |
| [05-roadmap-and-verification.md](./docs/05-roadmap-and-verification.md) | Phase 1–4 交付矩阵、Phase 1 施工清单、自证测试矩阵与风险回滚 |

## 开发与验证命令速查

```powershell
# 生成强类型前后端绑定（不可在项目外使用 bunx mygo）
bunx mygo generate

# 本地热更新调试（后端改 Go 自动重编译，前端 Vite 热更）
bun run dev

# 一键自证验证（后端单测 + 前端类型检查 + 前端构建）
go test ./... ; bun run typecheck && bun run build:web

# 构建 Windows 发布产物
bun run build
```
