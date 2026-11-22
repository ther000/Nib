# Nib 开发与工程规约

> 本文件只记录代码之外的硬约束、框架边界、数据安全红线与固定踩坑对策。面向所有人工开发者与 AI 编码 Agent。

## 1. 分层与依赖红线

```text
src/ (React + TS)  ──►  app/ (MyGo Service 胶水)  ──►  internal/ (纯 Go 核心引擎)
```

1. **`internal/` 零框架耦合**：`internal/` 下任何包严禁 `import "github.com/egoist/mygo"`。所有核心逻辑（数据库、记忆账本、编译器、LLM 客户端、流水线、导出器、去 AI 味检测）必须能在无桌面窗口环境下通过 `go test ./...` 独立跑通。
2. **`app/` 只做薄绑定**：`app/` 负责参数校验、将纯 Go channel/callback 桥接为 `*mygo.Channel[T]` 或 `mygo.NewEvent[T]`、统一返回错误。不在 `app/` 中直接写 SQL 或拼接 Prompt。
3. **严禁手改生成产物**：`src/mygo.ts` 由 `mygo generate` 自动覆写，修改 Go 暴露方法或结构体后通过 `bunx mygo generate` 刷新。
4. **单向依赖无环**：包引用顺序锁定为 `app -> pipeline -> {compiler, llm, linter, store, exporter} -> {genres, domain}`，禁止逆向依赖。

## 2. MyGo v0.10.0 框架硬约束

依据 MyGo v0.10.0 官方行为，违反以下任意一条均会导致构建或运行时故障：

1. **数据库与迁移禁止放 `main()` 顶层**：`mygo generate` 会注入 `MYGO_GENERATE` 环境变量并执行 `main()` 直至 `mygo.App.Run()` 后立即退出。因此 `main()` 中只能做 `mygo.Bind(...)`、`mygo.App.RequestSingleInstanceLock()` 与生命周期回调注册；SQLite 连接池打开、建表迁移与流水线恢复必须全部写在 `mygo.App.WhenReady(...)` 内。
2. **CLI 包名区分**：项目尚未初始化 `mygo-cli` 依赖时，严禁运行 `bunx mygo`（npm 上存在同名无关包），必须使用 `bunx mygo-cli`；项目依赖装好后在根目录使用 `bun run dev` / `bunx mygo generate` / `bun run build`。
3. **流式传输禁走自定义协议且禁与 `.with({ signal })` 混用**：Windows WebView2 会缓冲自定义 URL Scheme 响应（约 1MB 门槛且阻塞同源读取），无法承载流式长连接。所有 AI Token 流与流水线进度必须走 `*mygo.Channel[T]` + `context.Context` 或 `mygo.NewEvent[T]`；**注意**：MyGo v0.10.0 中带 `*mygo.Channel[T]` 参数的方法严禁在前端使用 `.with({ signal })`（`ipc.go` 包装 `pageCtx` 会导致 `channel.go` 中 `w.pageCtx == page` 恒为 `false`，通道在进入方法前即被关闭），取消流式调用必须直接调用 `ch.close()`。
4. **防闪白、防双开、防强退损坏库**：
   - 窗口创建必须设置 `Hidden: true`、`BackgroundColor: "light-dark(#F6F3EC, #141416)"`、`StateKey: "main"`，并在 `win.OnReadyToShow(win.Show)` 后再显示。
   - 必须启用 `mygo.App.RequestSingleInstanceLock()`，防止双开进程争抢 SQLite 写锁，二次拉起时聚焦已存在窗口。
   - 在 `mygo.App.OnBeforeQuit` 中必须通知运行中流水线取消、等待正在落库的事务结束、再关闭 SQLite 连接。
5. **开发期与正式期数据隔离**：`mygo dev` 使用 `<identifier>.dev` 数据目录，开发测试数据不会污染作者正式使用的 `%APPDATA%\Nib\nib.db`。

## 3. SQLite 与编译规范

1. **纯 Go 零 CGO**：固定使用 `modernc.org/sqlite`，构建与测试保持 `CGO_ENABLED=0`，不得引入依赖 MinGW/GCC 的 CGO SQLite 驱动。
2. **连接初始化必备 PRAGMA**：每次打开数据库实例时顺序执行：
   ```sql
   PRAGMA journal_mode = WAL;
   PRAGMA foreign_keys = ON;
   PRAGMA busy_timeout = 5000;
   PRAGMA synchronous = NORMAL;
   ```
3. **版本化迁移**：所有表结构改动必须按序号新增内嵌 SQL 脚本（`001_*.sql`、`002_*.sql`...），由迁移器在事务内比对 `schema_migrations` 顺序执行，禁止在业务代码中散落 `CREATE TABLE IF NOT EXISTS` 或无版本 `ALTER`。
4. **FTS5 中文检索约定**：中文人名、招式、道具检索虚表统一使用 `tokenize='trigram'`；当用户检索词长度小于 3 个字符时，查询层自动回退 `LIKE '%keyword%'` 并集，避免 trigram 对双字人名漏召回。

## 4. 数据安全与防污染红线

1. **正文快照必先于覆写**：任何 AI 成稿写入、局部润色整段替换、或历史版本回滚前，必须先将当前 `chapters.content` 归档至 `chapter_versions`。
2. **记忆变更永不静默入库**：完章抽取的实体属性变化与伏笔变更必须先以 `pending` 状态进入 `chapter_deltas` / `entity_state_events`。串行微批时下游章节仅允许显式按 `includePending=true` 读取同批次临时状态；只有作者在审改台点击确认后才转为 `committed`。
3. **API Key 隔离与脱敏**：
   - 供应商 API Key 仅持久化在用户数据目录 SQLite 中，严禁写入工程目录、`mygo.config.ts`、导出的 Markdown 或测试夹具。
   - 返回给前端的 `Provider` 对象必须抹除完整 Key（仅透出 `hasApiKey` 与尾号掩码如 `sk-...a8f2`）。
   - 日志、报错信息与剪贴板调试输出严禁打印完整 `Authorization` / `x-api-key` 请求头。
4. **单向导出原则**：本地磁盘 Markdown 目录仅为人类可读备份与外发副本（SQLite 为唯一真相源），不监听反向修改覆盖数据库，避免章节元数据、节拍与账本状态撕裂。

## 5. 自证验证入口

每次提交代码或交付阶段前，必须按顺序通过以下检查：

```powershell
# 1. 核心引擎与仓储全量单元测试（需 100% 通过，无 skip）
$env:CGO_ENABLED="0"; go test -v ./...

# 2. 绑定生成检查（确认 src/mygo.ts 与 Go 绑定同步）
bunx mygo generate

# 3. 前端单元测试、类型检查与生产构建
bun test && bun run typecheck && bun run build:web
```

## 6. 每次改动交付闭环

每次完成代码或文档改动并通过第 5 节验证后，必须按顺序完成以下三步交付动作，不留未提交或未同步状态：

1. **Git 提交**：只暂存本次改动的文件，按「做了什么 / 怎么验证的 / 回滚方式」写清提交信息并提交到本地 `main` 分支（严禁把 `build/`、`dist/`、`*.db`、`*.exe` 或任何密钥文件纳入提交）。
2. **打包最新 `.exe`**：执行纯 Go 生产打包，产出最新 `build/windows-amd64/Nib.exe` 并确认产物时间戳与体积：
   ```powershell
   $env:CGO_ENABLED="0"; $env:GOFLAGS="-buildvcs=false"; bunx mygo build -skip-installer
   ```
   （若需要额外生成 NSIS 安装包，再运行 `bun run build`。）
3. **同步到 GitHub 仓库**：将最新提交推送到远端 `origin main`（`git push origin main`），并在汇报中给出 commit hash、`.exe` 路径与同步结果。

> 详细架构与 DDL 见 [02-architecture-and-schema.md](./docs/02-architecture-and-schema.md)，阶段施工清单见 [05-roadmap-and-verification.md](./docs/05-roadmap-and-verification.md)。
