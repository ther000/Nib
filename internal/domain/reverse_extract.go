package domain

import "strings"

// ReverseExtractRequest 是从旧稿反推设定与伏笔的采样范围。
type ReverseExtractRequest struct {
	BookID      int64 `json:"bookId"`
	StartSeq    int   `json:"startSeq"`
	EndSeq      int   `json:"endSeq"`
	MaxChapters int   `json:"maxChapters"`
}

func (r ReverseExtractRequest) Validate() error {
	if r.BookID <= 0 {
		return newValidationError("bookId", "书籍 ID 必须大于 0")
	}
	if r.StartSeq < 0 || r.EndSeq < 0 {
		return newValidationError("startSeq", "章节范围不能为负数")
	}
	if r.EndSeq > 0 && r.EndSeq < r.StartSeq {
		return newValidationError("endSeq", "结束章节不能小于起始章节")
	}
	if r.MaxChapters < 0 || r.MaxChapters > 100 {
		return newValidationError("maxChapters", "单次最多采样 100 章")
	}
	return nil
}

// ReverseExtractEntity 是从旧稿中抽取的角色/势力/地点/道具/体系/铁律候选。
type ReverseExtractEntity struct {
	Name          string            `json:"name"`
	Kind          EntityKind        `json:"kind"`
	Aliases       []string          `json:"aliases"`
	Summary       string            `json:"summary"`
	IsInvariant   bool              `json:"isInvariant"`
	Attributes    map[string]string `json:"attributes"`
	ExistingMatch bool              `json:"existingMatch"`
}

// ReverseExtractRelation 是抽取出的实体关系。
type ReverseExtractRelation struct {
	SourceName   string `json:"sourceName"`
	TargetName   string `json:"targetName"`
	RelationType string `json:"relationType"`
	Description  string `json:"description"`
}

// ReverseExtractForeshadowing 是抽取出的未回收伏笔。
type ReverseExtractForeshadowing struct {
	Title             string `json:"title"`
	Description       string `json:"description"`
	PlantedChapterSeq int    `json:"plantedChapterSeq"`
	TargetResolveSeq  int    `json:"targetResolveSeq"`
}

// ReverseExtractArcSummary 是抽取出的阶段/卷摘要。
type ReverseExtractArcSummary struct {
	Level    ArcSummaryLevel `json:"level"`
	StartSeq int             `json:"startSeq"`
	EndSeq   int             `json:"endSeq"`
	Summary  string          `json:"summary"`
}

// ReverseExtractPreview 是旧稿反推后供作者勾选确认的审阅清单。
type ReverseExtractPreview struct {
	Entities       []ReverseExtractEntity        `json:"entities"`
	Relations      []ReverseExtractRelation      `json:"relations"`
	Foreshadowings []ReverseExtractForeshadowing `json:"foreshadowings"`
	ArcSummaries   []ReverseExtractArcSummary    `json:"arcSummaries"`
	Warnings       []string                      `json:"warnings"`
}

// ReverseExtractChunk 是反推设定的流式进度事件。
type ReverseExtractChunk struct {
	Kind    string                 `json:"kind"`
	Delta   string                 `json:"delta,omitempty"`
	Preview *ReverseExtractPreview `json:"preview,omitempty"`
	Error   string                 `json:"error,omitempty"`
	Usage   *TokenUsage            `json:"usage,omitempty"`
}

// ApplyReverseExtractInput 是作者确认后的反推结果落库入参。
type ApplyReverseExtractInput struct {
	BookID  int64                 `json:"bookId"`
	Preview ReverseExtractPreview `json:"preview"`
}

func (in ApplyReverseExtractInput) Validate() error {
	if in.BookID <= 0 {
		return newValidationError("bookId", "书籍 ID 必须大于 0")
	}
	if len(in.Preview.Entities) == 0 && len(in.Preview.Relations) == 0 &&
		len(in.Preview.Foreshadowings) == 0 && len(in.Preview.ArcSummaries) == 0 {
		return newValidationError("preview", "没有可写入的抽取结果")
	}
	for _, entity := range in.Preview.Entities {
		if strings.TrimSpace(entity.Name) == "" {
			return newValidationError("entities", "实体名称不能为空")
		}
		if !entity.Kind.Valid() {
			return newValidationError("entities", "存在无效的实体分类")
		}
	}
	return nil
}

// ApplyReverseExtractResult 汇总反推结果的落库数量。
type ApplyReverseExtractResult struct {
	EntitiesCreated  int `json:"entitiesCreated"`
	EntitiesUpdated  int `json:"entitiesUpdated"`
	RelationsCreated int `json:"relationsCreated"`
	Foreshadowings   int `json:"foreshadowings"`
	ArcSummaries     int `json:"arcSummaries"`
}
