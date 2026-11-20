# 三大核心引擎技术规格

## 1. 引擎一：百万字防吃书与动态记忆引擎

### 1.1 为什么放弃纯向量 RAG，采用 4 层混合记忆
纯向量检索只擅长“语义相近”，极易在长篇网文中漏掉强逻辑关联（例如第 12 章“主角把半块玉佩交给师妹”，第 180 章“在黑市遇到拍卖该玉佩”在向量空间毫无交集）。Nib 采用 **4 层混合记忆**：

1. **层 1 — 世界铁律（Hard Invariants）**：`entities.is_invariant = 1`，全书物理/修炼/金手指底线规则，每章强制注入，禁止裁剪。
2. **层 2 — 事件溯源状态账本（Event-Sourced Ledger）**：`entity_state_events` 记录每个实体的每一条属性变更与所属 `chapter_seq`，不覆盖历史，只追加事件。
3. **层 3 — 伏笔生命周期表（Foreshadowing Tracker）**：追踪所有未关闭伏笔（`planted` / `hinted`），临近或超出 `target_resolve_seq` 自动提权注入简报并在编排台标橙提醒。
4. **层 4 — 3 级摘要金字塔 + SQLite FTS5**：
   - 单章因果摘要（`chapters.causal_summary`，150~250 字，只记因果转折与收获，不写水词）
   - 10 章弧线摘要（`arc_summaries` `level='arc_10'`）与卷摘要（`level='volume'`）
   - FTS5（`chapters_fts` / `entities_fts`）按人名/功法/道具精确反查出处原文片段。

### 1.2 事件溯源时间旅行算法：`SnapshotAt`

当编排或重写第 `K` 章时，必须严防第 `K+5` 章的未来事件反向剧透第 `K` 章。状态合并算法严格按章序折算：

```go
// SnapshotAt 伪流程：计算截至 targetChapterSeq（含）时的世界状态
func SnapshotAt(bookID int64, targetSeq int, includePending bool) []EntitySnapshot {
    entities := queryEntities(bookID) // 取初始静态档案 attributes_json
    allowedStatuses := []string{"committed"}
    if includePending { // 仅串行微批中读取同批次上游暂存状态
        allowedStatuses = append(allowedStatuses, "pending")
    }
    events := queryEventsWhere(
        "book_id = ? AND chapter_seq <= ? AND status IN (?)",
        bookID, targetSeq, allowedStatuses,
        ORDER BY chapter_seq ASC, id ASC,
    )
    // 按实体把 attributes_json 底座逐事件覆写 attr_key -> new_value
    return foldAttributes(entities, events)
}
```

> **修改旧章连带保护**：如果作者手动修改了第 `N` 章正文并重新确认变更单，系统检索所有 `global_seq > N` 且状态为 `outlined / drafting / reviewing` 的后续章节（或同微批队列中的后续章），将其状态置为 `upstream_dirty` 并在 UI 高亮提示重跑或复核。

### 1.3 上下文编译器（Context Compiler）与确定性 Token 预算

写作模型输入上下文上限按默认安全窗口 **`12,000` tokens（约 18,000~20,000 汉字，支持在高级路由设置中调大）** 严格配额装配，绝不盲目塞爆模型注意力：

| 槽位优先级 | 内容来源 | 默认上限 | 裁剪策略 |
| --- | --- | --- | --- |
| **P0（不可裁）** | 系统指令 + 书籍核心设定（题材包规则 + 金手指）+ 本章世界铁律 (`is_invariant=1`) | ~1,200 tokens | 永远完整保留 |
| **P1（不可裁）** | 本章情绪标签、钩子要求、全部 `chapter_beats`（含节拍目标字数）与作者附加指令 | ~1,000 tokens | 永远完整保留 |
| **P2（高优）** | 本章节拍引用的全部出场实体 `SnapshotAt(seq-1)` 状态快照 | ~2,400 tokens | 超限优先保留 POV 与核心冲突角色详细属性，其余压缩为单行状态摘要 |
| **P3（高优）** | 关联出场人物的未回收伏笔 + 超期/临期预警伏笔 | ~800 tokens | 按临近 `target_resolve_seq` 排序截断 |
| **P4（衔接）** | 上一章最后 1,200 字原文 + 上两章最后 400 字原文（保证语气与场景无缝衔接） | ~1,600 tokens | 逐章收缩尾部保留字符数 |
| **P5（记忆）** | 近 15 章因果摘要 + 所属 10 章弧线摘要 + 历史各卷摘要 | ~2,800 tokens | 从最远章节开始向上聚合为弧线摘要 |
| **P6（检索）** | 按本章节拍关键词触发 FTS5 命中的前文关键段落（最多 3 条） | ~1,000 tokens | 按 BM25 相关度排序丢弃低分片 |
| **P7（文风）** | Style DNA 量化指标 + 匹配当前主节拍类型的 1~2 段样章锚点 | ~1,200 tokens | 缩减为 1 段最贴合锚点 |

每次编译后，整份装配明细会存入 `chapters.last_briefing_json`，作者在审改台右侧随时可查看每个槽位装了多少内容、删减或临时钉选额外实体。

### 1.4 完章增量抽取契约与高危标红

每章生成完毕后，由 `extract` 路由模型输出严格 JSON，写入 `chapter_deltas`：

```json
{
  "causal_summary": "主角以筑基初期修为借助残阵反杀黑煞宗内门执事，夺得破障丹与血玉令牌，左肩被阴雷震伤，决定连夜撤离坊市。",
  "state_patches": [
    {
      "entity_name": "林渊",
      "attr_key": "injury",
      "prev_value": "完好",
      "new_value": "左肩阴雷暗伤（三日内不可强动真元）",
      "severity": "normal",
      "reason": "第42章交战受创"
    },
    {
      "entity_name": "破障丹",
      "attr_key": "owner",
      "prev_value": "王执事",
      "new_value": "林渊",
      "severity": "high_risk",
      "reason": "击杀战利品易主"
    }
  ],
  "foreshadowing_updates": [
    {
      "action": "plant",
      "title": "血玉令牌背后的宗门密库",
      "description": "令牌背面刻有古宗阵纹，似乎对应三百年前失踪的灵脉",
      "target_resolve_seq": 75
    }
  ]
}
```

**强制标红（`severity = "high_risk"`）触发条件**：
1. 角色存活状态变更（死亡、失踪、夺舍、废除修为）；
2. 实力境界/身份阶层跨大阶跃升；
3. 核心道具/功法/公司股权发生易主或损毁；
4. 重要角色阵营关系翻转（盟友反目/投降归顺）；
5. 都市题材现金流单章变动幅度超过当前总资产 30%。

---

## 2. 引擎二：男频网文节奏编排与串行微批流水线

### 2.1 卷级节奏曲线与自动诊断
编排台以折线视图渲染当前卷内所有章节的情绪极性映射权重（`setup: 1`, `suppress: 0`, `turn: 2`, `burst: 4`, `harvest: 3`），并在后台实时执行 3 条规则校验：
- **压抑过载告警**：连续 `>= 3` 章处于 `suppress` 且无 `turn/burst`，提示“蓄压过长易劝退读者，建议插入小反转或利益预支”。
- **缺爽点告警**：连续 `>= 5` 章峰值未达 `burst`，提示“本单元缺少阶段高潮”。
- **伏笔超期告警**：`global_seq > target_resolve_seq` 且伏笔仍为 `planted/hinted`，在最近章纲顶部挂黄标提醒收网。

### 2.2 批量章纲与节拍生成
作者在编排台指定“从第 M 章起连续生成 N 章（默认 5 章）”及本小高潮期望走向：
1. `outline` 路由模型读取卷目标、近 15 章因果摘要、当前状态快照与超期伏笔清单。
2. 一次性输出结构化的 N 章列表（每章含 `title`, `emotion_tag`, `hook_type`, `hook_note`, 及 3~6 条 `chapter_beats`）。
3. 落库为 `outlined` 状态，作者在闸门 1 直接拖拽节拍顺序、增删出场人或重写核心反转，确认后才推入微批队列。

### 2.3 串行微批队列状态机

```text
  [queued] ──► [compiling] ──► [drafting] ──► [linting] ──► [critiquing]
                                   │                            │
                              (用户停止/断流)               (发现可修病灶)
                                   ▼                            ▼
                              [partial]                    [polishing]
                                                                │
                                                                ▼
                           [awaiting_review] ◄──────────── [extracting]
                                   │
                         (作者闸门 2 审定入库)
                                   ▼
                              [finalized]
```

- **崩溃与中断恢复**：若程序在 `drafting` 途中被关闭或用户主动按停止键，已流出的正文内容立即落入 `chapters.content`（写入前先存旧版快照），章节置为 `partial`，队列停止推进；下次可一键从断点章重新执行。
- **模式开关**：微批启动支持 3 档：`draft_only`（只出草稿+本地 Lint）、`draft_and_polish`（加跑 Critic 与局部润色）、`full_pipeline`（全套含状态抽取），供作者按场景控制 Token 消耗。

---

## 3. 引擎三：极致去 AI 味与文风贴合引擎

### 3.1 Style DNA 文风指纹与场景动态锚定
导入 3~5 章作者样章或标杆作品后，`style` 流程提取量化约束并切分 4 类场景片断入库 `style_samples`：
- **量化指纹**：平均句长（如 `16~22` 字）、单段上限（网文手机端建议 `<= 120` 汉字）、对话占比区间（如 `30%~50%`）、叙述语气关键词（如“冷峻克制、少形容词、动作先于心理”）。
- **场景动态锚定**：编译某章节拍时，识别本章节拍主导类型（战斗多选 `combat`、博弈谈话选 `dialogue`、过渡日常选 `daily`、探秘悬念选 `suspense`），从样章库选 1~2 段约 500 字原文作为“语感示范片断”注入 Prompt 尾部，只学用词节奏不抄内容。

### 3.2 0 成本本地确定性中文网文 AI 味探测器（`internal/linter`）

每次正文落入或编辑变动时，本地在 `< 20ms` 内完成全章扫描（0 API 费用），输出结构化问题定位：

```json
{
  "ruleId": "cliche-mouth-corner",
  "category": "cliche",
  "severity": "error",
  "paragraphIndex": 14,
  "startRune": 8,
  "endRune": 16,
  "matchedText": "嘴角勾起一抹弧度",
  "hint": "高频 AI 套话：直接写行动、台词或具体微表情，删去固定搭配"
}
```

#### 内置默认规则清单（可在 Atlas 中自由增删与调阈值）

| 类别 | 规则子项与示例匹配模式 | 触发阈值 / 级别 |
| --- | --- | --- |
| **A. 高频 AI 套话词库** | `嘴角勾起` / `一抹弧度` / `眼神微眯` / `眼中闪过一抹(精光\|寒芒\|玩味)` / `空气仿佛凝固` / `心中涌起一股` / `倒吸一口凉气` / `不由得` / `宛如` / `这一刻` / `深吸一口气` / `缓缓说道` / `嘴角泛起` / `令人窒息的` | `error`（单章出现 `>=1` 次即标红）或 `warn`（如 `不由得`/`缓缓` 允许最多 1 次，超出标红） |
| **B. 句式结构病** | 1. **密集对比句**：单章超过 2 次 `不是……而是……` 或 `与其说……不如说……`<br>2. **AI 三段式对称排比**：同段内连续 3 句结构高度对称且字数相近<br>3. **章末/段末强行鸡汤升华**：结尾两段匹配 `(他知道|他明白|这一刻他意识到).*(真正的考验\|才刚刚开始\|命运的齿轮\|新的篇章)` | `warn` / `error`（章末升华直接判 `error`） |
| **C. 网文排版与推进病** | 1. **文字墙告警**：单段长度 `> 160` 汉字（手机端极易劝退）<br>2. **纯心理灌水段**：连续 `>= 3` 段既无双引号对话 `“...”` 也无动作推进动词，全篇心理活动总结 | `warn`（提示拆段或改用动作外化情绪） |

### 3.3 独立 Critic 审校官（4 维核验）
由高逻辑模型（`critic` 路由，低温度 `0.20`）对照「本章作战简报 + 生成草稿」做独立盲审，严格输出 JSON：
1. **一致性防吃书**：对照实体快照，抓出境界/伤势/道具持有/资产数额前后打架之处（例如“左肩刚受重伤却双手挥重剑无事”）。
2. **男频毒点排查**：抓出无动机送人头、主角圣母说教、信息差强行降智、数字量纲崩溃。
3. **节拍落点核验**：逐条核对 `chapter_beats` 是否全部扎实写出、有无一笔带过敷衍核心反转。
4. **章末 200 字钩子强检**：单独提取最后 200 字验证是否真实兑现 `hook_type` 悬念，若出现平淡收尾或鸡汤总结则直接判 `hook_passed = false`。

### 3.4 手术式局部润色（Surgical Polish）
绝不做整章重写（避免把已经写顺的精彩段落洗坏）。系统仅把「本地 Lint 标红段落 + Critic 点名病灶段落」连同其前后各 1 段（`[prevPara, targetPara, nextPara]`）打包送给 `polish` 模型，严格要求输出**仅针对目标段落的改写建议**：

```json
{
  "diffs": [
    {
      "id": "diff-14",
      "paragraphIndex": 14,
      "reason": "命中 AI 套话「嘴角勾起一抹弧度」+ 心理水词",
      "original": "林渊嘴角勾起一抹弧度，心中不由得暗自冷笑，他知道对方已经上钩了。",
      "replacement": "林渊抬手掸了掸袖口灰尘，将灵石收进储物袋：“既然王执事执意要验货，那就现在吧。”"
    }
  ]
}
```

作者在审改台正文区直接看到原位绿色/红色高亮对比，逐条按快捷键或点击「采纳 / 忽略」，好句子毫发无伤。

---

> 下一步参考：[04-ui-ux-design.md](./04-ui-ux-design.md)（视觉规范、页面布局线框与交互细节）。
