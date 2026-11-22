package pipeline

import (
	"encoding/json"
	"fmt"
	"strings"

	"nib/internal/domain"
	"nib/internal/llm"
)

type directorToolSpec struct {
	Name        string
	Label       string
	Description string
	PromptHint  string
	Parameters  map[string]any
}

func schemaObj(props map[string]any, required ...string) map[string]any {
	m := map[string]any{
		"type":       "object",
		"properties": props,
	}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}

func schemaStr(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

func schemaInt(desc string) map[string]any {
	return map[string]any{"type": "integer", "description": desc}
}

func schemaBool(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}

var directorToolSpecs = []directorToolSpec{
	{
		Name:        "get_book_overview",
		Label:       "查看全书概览",
		Description: "重新获取全书最新概览",
		PromptHint:  `{} — 重新获取全书最新概览`,
		Parameters:  schemaObj(map[string]any{}),
	},
	{
		Name:        "list_chapters_range",
		Label:       "查询指定区间目录",
		Description: "按起止章号查询全书目录（长篇超过 25 章时必用）",
		PromptHint:  `{"start_seq": 1, "end_seq": 50} — 按起止章号查询目录（长篇超过 25 章时必用）`,
		Parameters: schemaObj(map[string]any{
			"start_seq": schemaInt("起始章号"),
			"end_seq":   schemaInt("结束章号"),
		}),
	},
	{
		Name:        "read_chapter",
		Label:       "阅读章节内容",
		Description: "读取指定章节的大纲、节拍与正文",
		PromptHint:  `{"chapter_id": 0, "chapter_seq": 1} — 读取指定章节的大纲、节拍与正文`,
		Parameters: schemaObj(map[string]any{
			"chapter_id":  schemaInt("章节ID"),
			"chapter_seq": schemaInt("全局章号"),
		}),
	},
	{
		Name:        "get_entity_detail",
		Label:       "查看设定详情与关系",
		Description: "按名称查看角色/势力/道具的完整属性与关系网",
		PromptHint:  `{"name": "实体名称"} — 查看角色/势力/道具的完整属性与关系网`,
		Parameters: schemaObj(map[string]any{
			"name": schemaStr("实体名称或关键词"),
		}, "name"),
	},
	{
		Name:        "search_memory",
		Label:       "检索全书记忆",
		Description: "全文检索全书正文与设定记录",
		PromptHint:  `{"query": "关键词"} — 全文检索全书正文与设定记录`,
		Parameters: schemaObj(map[string]any{
			"query": schemaStr("检索关键词"),
		}, "query"),
	},
	{
		Name:        "update_book",
		Label:       "更新书籍设定",
		Description: "更新书籍名称、简介、金手指或目标章字数",
		PromptHint:  `{"title": "", "premise": "", "golden_finger": "", "target_chapter_words": 2500} — 更新书籍简介、金手指或目标字数`,
		Parameters: schemaObj(map[string]any{
			"title":                schemaStr("书名"),
			"premise":              schemaStr("简介"),
			"golden_finger":        schemaStr("金手指"),
			"target_chapter_words": schemaInt("每章目标字数"),
		}),
	},
	{
		Name:        "upsert_entity",
		Label:       "更新角色与设定",
		Description: "新增或更新角色、势力、地点、道具、体系或世界铁律",
		PromptHint:  `{"kind": "character|faction|location|item|system|rule", "name": "", "summary": "", "attributes": {}, "is_invariant": false} — 新增或更新设定`,
		Parameters: schemaObj(map[string]any{
			"kind":         schemaStr("character|faction|location|item|system|rule"),
			"name":         schemaStr("名称"),
			"summary":      schemaStr("简介"),
			"is_invariant": schemaBool("是否世界铁律"),
			"attributes":   map[string]any{"type": "object", "description": "属性键值"},
		}, "name"),
	},
	{
		Name:        "upsert_entity_relation",
		Label:       "更新角色/势力关系",
		Description: "记录两个实体之间的关系（如师徒、死敌、同盟、隶属）",
		PromptHint:  `{"source_name": "", "target_name": "", "relation_type": "", "description": ""} — 记录两个实体间的关系`,
		Parameters: schemaObj(map[string]any{
			"source_name":   schemaStr("源实体名"),
			"target_name":   schemaStr("目标实体名"),
			"relation_type": schemaStr("关系类型"),
			"description":   schemaStr("关系说明"),
		}, "source_name", "target_name", "relation_type"),
	},
	{
		Name:        "upsert_foreshadowing",
		Label:       "更新伏笔记录",
		Description: "新增或更新伏笔状态与目标回收章号",
		PromptHint:  `{"title": "", "description": "", "planted_seq": 1, "target_resolve_seq": 10, "status": "planted|hinted|resolved|abandoned"} — 新增或更新伏笔`,
		Parameters: schemaObj(map[string]any{
			"title":              schemaStr("伏笔标题"),
			"description":        schemaStr("伏笔内容"),
			"planted_seq":        schemaInt("埋设章号"),
			"target_resolve_seq": schemaInt("计划回收章号"),
			"status":             schemaStr("planted|hinted|resolved|abandoned"),
		}, "title"),
	},
	{
		Name:        "upsert_volume",
		Label:       "规划分卷",
		Description: "新建或更新分卷",
		PromptHint:  `{"seq": 1, "title": "", "goal": "", "climax_plan": ""} — 新建或更新分卷`,
		Parameters: schemaObj(map[string]any{
			"seq":         schemaInt("卷序"),
			"title":       schemaStr("卷名"),
			"goal":        schemaStr("卷目标"),
			"climax_plan": schemaStr("卷末高潮"),
		}, "title"),
	},
	{
		Name:        "upsert_chapter_outline",
		Label:       "调整章纲与节拍",
		Description: "新建或更新单章大纲、情绪、钩子与节拍卡",
		PromptHint:  `{"chapter_id": 0, "chapter_seq": 0, "volume_id": 0, "title": "", "emotion_tag": "setup|suppress|turn|burst|harvest", "hook_type": "mystery|crisis|counterattack_eve|reward_pending|new_arc", "hook_note": "", "outline_summary": "", "beats": [{"stage": "setup", "summary": "", "conflict_note": "", "target_words": 500}]} — 新建或更新单章大纲与节拍`,
		Parameters: schemaObj(map[string]any{
			"chapter_id":      schemaInt("章节ID"),
			"chapter_seq":     schemaInt("全局章号"),
			"volume_id":       schemaInt("分卷ID"),
			"title":           schemaStr("章名"),
			"emotion_tag":     schemaStr("setup|suppress|turn|burst|harvest"),
			"hook_type":       schemaStr("mystery|crisis|counterattack_eve|reward_pending|new_arc"),
			"hook_note":       schemaStr("钩子说明"),
			"outline_summary": schemaStr("章纲"),
			"beats":           map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
		}),
	},
	{
		Name:        "batch_generate_outlines",
		Label:       "批量生成章纲",
		Description: "一次性批量生成后续 1~10 章大纲与节拍",
		PromptHint:  `{"volume_id": 0, "count": 3, "arc_goal": ""} — 一次性批量生成后续 1~10 章大纲与节拍`,
		Parameters: schemaObj(map[string]any{
			"volume_id": schemaInt("分卷ID"),
			"count":     schemaInt("生成章数1-10"),
			"arc_goal":  schemaStr("阶段剧情目标"),
		}),
	},
	{
		Name:        "delete_chapter",
		Label:       "删除章节",
		Description: "删除指定章节并自动重排章序",
		PromptHint:  `{"chapter_id": 0, "chapter_seq": 1} — 删除指定章节并自动重排章序`,
		Parameters: schemaObj(map[string]any{
			"chapter_id":  schemaInt("章节ID"),
			"chapter_seq": schemaInt("全局章号"),
		}),
	},
	{
		Name:        "move_chapter",
		Label:       "调整章节顺序",
		Description: "上移/下移章节或跨卷移动",
		PromptHint:  `{"chapter_id": 0, "chapter_seq": 1, "direction": "up|down", "target_volume_id": 0} — 调整章节顺序或跨卷移动`,
		Parameters: schemaObj(map[string]any{
			"chapter_id":       schemaInt("章节ID"),
			"chapter_seq":      schemaInt("全局章号"),
			"direction":        schemaStr("up|down"),
			"target_volume_id": schemaInt("目标卷ID"),
		}),
	},
	{
		Name:        "write_chapter_content",
		Label:       "修改章节正文",
		Description: "直接写入或覆盖指定章节的完整正文（覆盖前自动归档快照）",
		PromptHint:  `{"chapter_id": 0, "chapter_seq": 1, "content": "..."} — 直接写入或修改某章完整正文（自动先存快照）`,
		Parameters: schemaObj(map[string]any{
			"chapter_id":  schemaInt("章节ID"),
			"chapter_seq": schemaInt("全局章号"),
			"content":     schemaStr("完整正文内容"),
		}, "content"),
	},
	{
		Name:        "generate_chapter",
		Label:       "生成章节正文",
		Description: "调用写作管线按作战简报生成某章完整正文",
		PromptHint:  `{"chapter_id": 0, "chapter_seq": 1} — 调用写作管线为某章生成正文`,
		Parameters: schemaObj(map[string]any{
			"chapter_id":  schemaInt("章节ID"),
			"chapter_seq": schemaInt("全局章号"),
		}),
	},
	{
		Name:        "replace_chapter_paragraphs",
		Label:       "局部修改章节段落",
		Description: "局部修改某章指定段落范围（优先用于局部修稿，比整章重写更稳）",
		PromptHint:  `{"chapter_id": 0, "chapter_seq": 1, "start_paragraph": 1, "end_paragraph": 2, "new_text": "..."} — 局部替换指定段落范围`,
		Parameters: schemaObj(map[string]any{
			"chapter_id":      schemaInt("章节ID"),
			"chapter_seq":     schemaInt("全局章号"),
			"start_paragraph": schemaInt("起始段落号，从1开始"),
			"end_paragraph":   schemaInt("结束段落号，从1开始"),
			"new_text":        schemaStr("替换后的段落文本"),
		}, "start_paragraph", "end_paragraph", "new_text"),
	},
	{
		Name:        "audit_and_polish_chapter",
		Label:       "质检并局部润色",
		Description: "对指定章节执行本地扫描 + AI 审校并自动应用局部润色",
		PromptHint:  `{"chapter_id": 0, "chapter_seq": 1} — 对某章执行本地扫描 + AI 审校并自动应用局部润色`,
		Parameters: schemaObj(map[string]any{
			"chapter_id":  schemaInt("章节ID"),
			"chapter_seq": schemaInt("全局章号"),
		}),
	},
	{
		Name:        "generate_arc_summary",
		Label:       "自动生成弧线/卷摘要",
		Description: "AI 汇总指定章号区间（arc_10）或整卷（volume）的长期记忆摘要",
		PromptHint:  `{"level": "arc_10|volume", "volume_id": 0, "start_seq": 1, "end_seq": 10} — 汇总弧线或整卷记忆摘要`,
		Parameters: schemaObj(map[string]any{
			"level":     schemaStr("arc_10|volume"),
			"volume_id": schemaInt("卷ID"),
			"start_seq": schemaInt("起始章号"),
			"end_seq":   schemaInt("结束章号"),
		}),
	},
	{
		Name:        "start_autopilot",
		Label:       "启动 AI 自动托管",
		Description: "启动后台全自动托管连续写稿",
		PromptHint:  `{"target_chapters": 3, "arc_goal": "", "auto_outline": true, "auto_polish": true, "auto_commit_memory": false} — 启动后台全自动托管连续写稿`,
		Parameters: schemaObj(map[string]any{
			"target_chapters":    schemaInt("章数1-20"),
			"arc_goal":           schemaStr("剧情目标"),
			"auto_outline":       schemaBool("自动续写章纲"),
			"auto_polish":        schemaBool("自动审校润色"),
			"auto_commit_memory": schemaBool("自动同步记忆"),
		}),
	},
	{
		Name:        "stop_autopilot",
		Label:       "停止 AI 自动托管",
		Description: "停止正在运行的后台托管",
		PromptHint:  `{} — 停止正在运行的后台托管`,
		Parameters:  schemaObj(map[string]any{}),
	},
}

func directorToolLabel(name string) string {
	for _, spec := range directorToolSpecs {
		if spec.Name == name {
			return spec.Label
		}
	}
	return name
}

func directorToolDefinitions() []llm.ToolDefinition {
	defs := make([]llm.ToolDefinition, 0, len(directorToolSpecs))
	for _, spec := range directorToolSpecs {
		defs = append(defs, llm.ToolDefinition{
			Name:        spec.Name,
			Description: spec.Description,
			Parameters:  spec.Parameters,
		})
	}
	return defs
}

func formatDirectorToolPromptList() string {
	var b strings.Builder
	for i, spec := range directorToolSpecs {
		fmt.Fprintf(&b, "%d. %s %s\n", i+1, spec.Name, spec.PromptHint)
	}
	return strings.TrimRight(b.String(), "\n")
}

func unmarshalDirectorArgs(rawArgs json.RawMessage, dst any) error {
	trimmed := strings.TrimSpace(string(rawArgs))
	if trimmed == "" || trimmed == "null" {
		trimmed = "{}"
	}
	if err := json.Unmarshal([]byte(trimmed), dst); err != nil {
		return fmt.Errorf("%w: 工具参数 JSON 格式无效: %v", domain.ErrValidation, err)
	}
	return nil
}
