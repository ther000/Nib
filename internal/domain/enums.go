package domain

import (
	"strings"
	"unicode/utf8"
)

// ProviderProtocol 定义支持的 LLM API 协议类型（对应 providers.protocol）。
type ProviderProtocol string

const (
	ProtocolOpenAICompat ProviderProtocol = "openai_compat"
	ProtocolAnthropic    ProviderProtocol = "anthropic"
	ProtocolGemini       ProviderProtocol = "gemini"
)

func (p ProviderProtocol) Valid() bool {
	switch p {
	case ProtocolOpenAICompat, ProtocolAnthropic, ProtocolGemini:
		return true
	default:
		return false
	}
}

func AllProviderProtocols() []ProviderProtocol {
	return []ProviderProtocol{ProtocolOpenAICompat, ProtocolAnthropic, ProtocolGemini}
}

// PipelineStage 定义 5 环节独立模型路由阶段（对应 model_routes.stage）。
type PipelineStage string

const (
	StageOutline PipelineStage = "outline"
	StageDraft   PipelineStage = "draft"
	StageCritic  PipelineStage = "critic"
	StagePolish  PipelineStage = "polish"
	StageExtract PipelineStage = "extract"
)

func (s PipelineStage) Valid() bool {
	switch s {
	case StageOutline, StageDraft, StageCritic, StagePolish, StageExtract:
		return true
	default:
		return false
	}
}

func AllPipelineStages() []PipelineStage {
	return []PipelineStage{StageOutline, StageDraft, StageCritic, StagePolish, StageExtract}
}

// GenreID 定义题材基底标识（对应 books.genre），内置 8 大题材基底并兼容作者自定义题材名。
type GenreID string

const (
	GenreXuanhuan GenreID = "xuanhuan"
	GenreXianxia  GenreID = "xianxia"
	GenreUrban    GenreID = "urban"
	GenreHistory  GenreID = "history"
	GenreSciFi    GenreID = "scifi"
	GenreMystery  GenreID = "mystery"
	GenreGame     GenreID = "game"
	GenreGeneral  GenreID = "general"
)

func (g GenreID) Valid() bool {
	trimmed := strings.TrimSpace(string(g))
	if trimmed == "" {
		return false
	}
	return utf8.RuneCountInString(trimmed) <= 32
}

func AllGenreIDs() []GenreID {
	return []GenreID{
		GenreXuanhuan,
		GenreXianxia,
		GenreUrban,
		GenreHistory,
		GenreSciFi,
		GenreMystery,
		GenreGame,
		GenreGeneral,
	}
}

// ChapterStatus 定义章节状态机（对应 chapters.status）。
type ChapterStatus string

const (
	ChapterStatusPlanned       ChapterStatus = "planned"
	ChapterStatusOutlined      ChapterStatus = "outlined"
	ChapterStatusDrafting      ChapterStatus = "drafting"
	ChapterStatusPartial       ChapterStatus = "partial"
	ChapterStatusReviewing     ChapterStatus = "reviewing"
	ChapterStatusFinalized     ChapterStatus = "finalized"
	ChapterStatusUpstreamDirty ChapterStatus = "upstream_dirty"
)

func (s ChapterStatus) Valid() bool {
	switch s {
	case ChapterStatusPlanned,
		ChapterStatusOutlined,
		ChapterStatusDrafting,
		ChapterStatusPartial,
		ChapterStatusReviewing,
		ChapterStatusFinalized,
		ChapterStatusUpstreamDirty:
		return true
	default:
		return false
	}
}

// EmotionTag 定义章节主情绪极性（对应 chapters.emotion_tag）。
type EmotionTag string

const (
	EmotionSetup    EmotionTag = "setup"
	EmotionSuppress EmotionTag = "suppress"
	EmotionTurn     EmotionTag = "turn"
	EmotionBurst    EmotionTag = "burst"
	EmotionHarvest  EmotionTag = "harvest"
)

func (e EmotionTag) Valid() bool {
	switch e {
	case EmotionSetup, EmotionSuppress, EmotionTurn, EmotionBurst, EmotionHarvest:
		return true
	default:
		return false
	}
}

// Weight 返回卷级情绪折线权重（setup:1, suppress:0, turn:2, burst:4, harvest:3）。
func (e EmotionTag) Weight() int {
	switch e {
	case EmotionSuppress:
		return 0
	case EmotionSetup:
		return 1
	case EmotionTurn:
		return 2
	case EmotionHarvest:
		return 3
	case EmotionBurst:
		return 4
	default:
		return 1
	}
}

func (e EmotionTag) Label() string {
	switch e {
	case EmotionSetup:
		return "铺垫建场"
	case EmotionSuppress:
		return "蓄压受阻"
	case EmotionTurn:
		return "反转破局"
	case EmotionBurst:
		return "高潮爆发"
	case EmotionHarvest:
		return "落袋留悬"
	default:
		return string(e)
	}
}

// HookType 定义章末钩子类型（对应 chapters.hook_type）。
type HookType string

const (
	HookMystery         HookType = "mystery"
	HookCrisis          HookType = "crisis"
	HookCounterattackEve HookType = "counterattack_eve"
	HookRewardPending   HookType = "reward_pending"
	HookNewArc          HookType = "new_arc"
)

func (h HookType) Valid() bool {
	switch h {
	case HookMystery, HookCrisis, HookCounterattackEve, HookRewardPending, HookNewArc:
		return true
	default:
		return false
	}
}

func (h HookType) Label() string {
	switch h {
	case HookMystery:
		return "悬念揭露"
	case HookCrisis:
		return "突发危机"
	case HookCounterattackEve:
		return "反击前夜"
	case HookRewardPending:
		return "利益待拆"
	case HookNewArc:
		return "转场开篇"
	default:
		return string(h)
	}
}

// BeatStage 定义章节内部节拍阶段（对应 chapter_beats.stage）。
type BeatStage string

const (
	BeatStageSetup    BeatStage = "setup"
	BeatStageTarget   BeatStage = "target"
	BeatStageSuppress BeatStage = "suppress"
	BeatStageTurn     BeatStage = "turn"
	BeatStageBurst    BeatStage = "burst"
	BeatStageHarvest  BeatStage = "harvest"
	BeatStageHook     BeatStage = "hook"
)

func (b BeatStage) Valid() bool {
	switch b {
	case BeatStageSetup,
		BeatStageTarget,
		BeatStageSuppress,
		BeatStageTurn,
		BeatStageBurst,
		BeatStageHarvest,
		BeatStageHook:
		return true
	default:
		return false
	}
}

func (b BeatStage) Label() string {
	switch b {
	case BeatStageSetup:
		return "起手建场"
	case BeatStageTarget:
		return "立靶冲突"
	case BeatStageSuppress:
		return "压制试探"
	case BeatStageTurn:
		return "转折破局"
	case BeatStageBurst:
		return "动作爆发"
	case BeatStageHarvest:
		return "收割结算"
	case BeatStageHook:
		return "章末留钩"
	default:
		return string(b)
	}
}

// VersionSource 定义章节版本快照来源（对应 chapter_versions.source）。
type VersionSource string

const (
	VersionSourceManualBackup   VersionSource = "manual_backup"
	VersionSourceAIDraft        VersionSource = "ai_draft"
	VersionSourceSurgicalPolish VersionSource = "surgical_polish"
	VersionSourceRestoreBackup  VersionSource = "restore_backup"
)

func (v VersionSource) Valid() bool {
	switch v {
	case VersionSourceManualBackup,
		VersionSourceAIDraft,
		VersionSourceSurgicalPolish,
		VersionSourceRestoreBackup:
		return true
	default:
		return false
	}
}

// EntityKind 定义设定实体分类（对应 entities.kind）。
type EntityKind string

const (
	EntityKindCharacter EntityKind = "character"
	EntityKindFaction   EntityKind = "faction"
	EntityKindLocation  EntityKind = "location"
	EntityKindItem      EntityKind = "item"
	EntityKindSystem    EntityKind = "system"
	EntityKindRule      EntityKind = "rule"
)

func (k EntityKind) Valid() bool {
	switch k {
	case EntityKindCharacter,
		EntityKindFaction,
		EntityKindLocation,
		EntityKindItem,
		EntityKindSystem,
		EntityKindRule:
		return true
	default:
		return false
	}
}

func (k EntityKind) Label() string {
	switch k {
	case EntityKindCharacter:
		return "角色"
	case EntityKindFaction:
		return "势力"
	case EntityKindLocation:
		return "地点"
	case EntityKindItem:
		return "道具"
	case EntityKindSystem:
		return "体系"
	case EntityKindRule:
		return "铁律"
	default:
		return string(k)
	}
}

// GenChunkKind 定义流式生成事件类型。
type GenChunkKind string

const (
	GenChunkStarted   GenChunkKind = "started"
	GenChunkThinking  GenChunkKind = "thinking"
	GenChunkDelta     GenChunkKind = "delta"
	GenChunkUsage     GenChunkKind = "usage"
	GenChunkCompleted GenChunkKind = "completed"
	GenChunkPartial   GenChunkKind = "partial"
	GenChunkError     GenChunkKind = "error"
)

func (k GenChunkKind) Valid() bool {
	switch k {
	case GenChunkStarted,
		GenChunkThinking,
		GenChunkDelta,
		GenChunkUsage,
		GenChunkCompleted,
		GenChunkPartial,
		GenChunkError:
		return true
	default:
		return false
	}
}
