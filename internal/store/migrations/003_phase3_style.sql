-- 文风指纹档案（每本书可维护一套量化风格约束）
CREATE TABLE style_profiles (
    id                    INTEGER PRIMARY KEY,
    book_id               INTEGER NOT NULL UNIQUE REFERENCES books(id) ON DELETE CASCADE,
    avg_sentence_chars    REAL NOT NULL DEFAULT 18.0,
    max_paragraph_chars   INTEGER NOT NULL DEFAULT 140,
    dialogue_ratio_min    REAL NOT NULL DEFAULT 0.25,
    dialogue_ratio_max    REAL NOT NULL DEFAULT 0.55,
    pov_rule              TEXT NOT NULL DEFAULT 'third_limited',
    tone_directives       TEXT NOT NULL DEFAULT '',
    forbidden_tropes_note TEXT NOT NULL DEFAULT '',
    updated_at            TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
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
    id              INTEGER PRIMARY KEY,
    book_id         INTEGER NOT NULL DEFAULT 0,
    category        TEXT NOT NULL CHECK (category IN ('cliche', 'structure', 'formatting')),
    pattern         TEXT NOT NULL,
    is_regex        INTEGER NOT NULL DEFAULT 0,
    severity        TEXT NOT NULL DEFAULT 'warn' CHECK (severity IN ('info', 'warn', 'error')),
    max_per_chapter INTEGER NOT NULL DEFAULT 0,
    hint            TEXT NOT NULL DEFAULT '',
    enabled         INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX idx_deai_rules_book ON deai_rules(book_id, enabled);

-- 章节审校与手术润色报告
CREATE TABLE chapter_audits (
    id           INTEGER PRIMARY KEY,
    chapter_id   INTEGER NOT NULL UNIQUE REFERENCES chapters(id) ON DELETE CASCADE,
    lint_json    TEXT NOT NULL DEFAULT '[]',
    critic_json  TEXT NOT NULL DEFAULT '[]',
    diffs_json   TEXT NOT NULL DEFAULT '[]',
    hook_passed  INTEGER NOT NULL DEFAULT 0,
    hook_verdict TEXT NOT NULL DEFAULT '',
    deai_score   INTEGER NOT NULL DEFAULT 100,
    updated_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- 预置全局中文男频网文去 AI 味默认规则库（book_id = 0）
INSERT INTO deai_rules (book_id, category, pattern, is_regex, severity, max_per_chapter, hint, enabled) VALUES
    -- A. 高频 AI 套话词库 (cliche)
    (0, 'cliche', '嘴角勾起', 0, 'error', 0, '高频 AI 套话：直接写行动、台词或具体微表情，删去“嘴角勾起”固定搭配', 1),
    (0, 'cliche', '一抹弧度', 0, 'error', 0, '高频 AI 套话：删去“一抹弧度”，改用具体神态或肢体动作细节', 1),
    (0, 'cliche', '眼神微眯', 0, 'error', 0, '高频 AI 套话：改写视线落点、兵刃动作或台词交锋', 1),
    (0, 'cliche', '眼中闪过一抹(精光|寒芒|玩味)', 1, 'error', 0, '高频 AI 套话：避免“眼中闪过一抹…”，用人物实际出手或盘算细节替代', 1),
    (0, 'cliche', '空气仿佛凝固', 0, 'error', 0, '高频 AI 套话：通过环境声响、呼吸停顿或器物细节展现压迫感', 1),
    (0, 'cliche', '心中涌起一股', 0, 'error', 0, '高频 AI 套话：减少直白心理说明，用外在动作与生理反应外化情绪', 1),
    (0, 'cliche', '倒吸一口凉气', 0, 'error', 0, '高频 AI 套话：旁观者震惊用停顿、后退、改称呼等克制细节体现', 1),
    (0, 'cliche', '这一刻', 0, 'error', 0, '高频 AI 套话：删去“这一刻”空洞时间强调，直接推进动作', 1),
    (0, 'cliche', '深吸一口气', 0, 'error', 0, '高频 AI 套话：删去套路化深呼吸动作，直接切入人物决断或台词', 1),
    (0, 'cliche', '缓缓说道', 0, 'error', 0, '高频 AI 套话：减少“缓缓说道”水词，直接以动作带出台词或省略提示语', 1),
    (0, 'cliche', '嘴角泛起', 0, 'error', 0, '高频 AI 套话：避免“嘴角泛起”套路神态，改写具体动作反应', 1),
    (0, 'cliche', '令人窒息的', 0, 'error', 0, '高频 AI 套话：删去抽象形容词“令人窒息的”，改写具体威压或代价', 1),
    (0, 'cliche', '不由得', 0, 'warn', 1, '高频 AI 连接词：单章最多保留 1 次，多余处直接删去使句式更干脆', 1),
    (0, 'cliche', '宛如', 0, 'warn', 1, '高频 AI 比喻词：单章最多保留 1 次，优先使用直陈动作而非泛滥明喻', 1),
    -- B. 句式结构病 (structure)
    (0, 'structure', '不是[^，。！？\n]{1,30}而是', 1, 'warn', 2, '句式结构病：单章“不是……而是……”对比句超过 2 处，建议改为直陈句', 1),
    (0, 'structure', '与其说[^，。！？\n]{1,30}不如说', 1, 'warn', 1, '句式结构病：减少翻译腔“与其说……不如说……”，直接陈述事实', 1),
    (0, 'structure', '(他知道|他明白|这一刻他意识到)[^。！？\n]*(真正的考验|才刚刚开始|命运的齿轮|新的篇章)', 1, 'error', 0, '章末/段末强行鸡汤升华：严禁套路化总结，必须用具体动作、对话或悬念细节收尾', 1),
    -- C. 网文排版与推进病 (formatting)
    (0, 'formatting', '单段超长文字墙 (>160字)', 0, 'warn', 0, '移动端网文排版病：单段超过字数上限，建议在动作转折或对话处拆分为短段', 1),
    (0, 'formatting', '连续3段纯心理无对话动作推进', 0, 'warn', 0, '叙事推进病：连续 3 段以上缺乏对话与具体动作推进，建议改用行动外化情绪', 1);
