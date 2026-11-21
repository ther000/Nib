package domain

import (
	"fmt"
	"strings"
	"time"
)

// ScratchNote 表示书内灵感便签/废稿池中的一条记录（对应 scratch_notes 表）。
type ScratchNote struct {
	ID        int64    `json:"id"`
	BookID    int64    `json:"bookId"`
	Title     string   `json:"title"`
	Content   string   `json:"content"`
	Tags      []string `json:"tags"`
	Pinned    bool     `json:"pinned"`
	CreatedAt string   `json:"createdAt"`
	UpdatedAt string   `json:"updatedAt"`
}

// UpsertScratchNoteInput 是新建或编辑灵感便签的入参。
type UpsertScratchNoteInput struct {
	ID      int64    `json:"id"`
	BookID  int64    `json:"bookId"`
	Title   string   `json:"title"`
	Content string   `json:"content"`
	Tags    []string `json:"tags"`
	Pinned  bool     `json:"pinned"`
}

func (in UpsertScratchNoteInput) Validate() error {
	if in.BookID <= 0 {
		return newValidationError("bookId", "书籍 ID 必须大于 0")
	}
	if strings.TrimSpace(in.Title) == "" && strings.TrimSpace(in.Content) == "" {
		return newValidationError("title", "标题和内容不能同时为空")
	}
	if len(in.Content) > 20000 {
		return newValidationError("content", "单条便签不能超过 20,000 字")
	}
	if len(in.Tags) > 12 {
		return newValidationError("tags", "最多添加 12 个标签")
	}
	for _, tag := range in.Tags {
		if len([]rune(strings.TrimSpace(tag))) > 16 {
			return newValidationError("tags", "单个标签不能超过 16 个字")
		}
	}
	return nil
}

// TrashItemType 表示回收站条目的原始对象类型。
type TrashItemType string

const (
	TrashItemBook    TrashItemType = "book"
	TrashItemVolume  TrashItemType = "volume"
	TrashItemChapter TrashItemType = "chapter"
	TrashItemEntity  TrashItemType = "entity"
)

func (t TrashItemType) Valid() bool {
	switch t {
	case TrashItemBook, TrashItemVolume, TrashItemChapter, TrashItemEntity:
		return true
	}
	return false
}

// TrashItem 是回收站条目的前端视图。
type TrashItem struct {
	ID          int64         `json:"id"`
	ItemType    TrashItemType `json:"itemType"`
	BookID      int64         `json:"bookId"`
	Title       string        `json:"title"`
	Summary     string        `json:"summary"`
	DeletedAt   string        `json:"deletedAt"`
	RetentionAt string        `json:"retentionAt"`
}

// TrashRestoreResult 表示回收站条目恢复后的新对象信息。
type TrashRestoreResult struct {
	ItemType TrashItemType `json:"itemType"`
	ItemID   int64         `json:"itemId"`
	Title    string        `json:"title"`
	Message  string        `json:"message"`
}

// AutoBackupInfo 描述一份自动滚动备份文件。
type AutoBackupInfo struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	SizeBytes  int64  `json:"sizeBytes"`
	ModifiedAt string `json:"modifiedAt"`
}

// TrashRetentionDays 是回收站默认保留天数。
const TrashRetentionDays = 30

// TrashRetentionAt 根据删除时间计算保留截止时间。
func TrashRetentionAt(deletedAt string) string {
	t, err := time.Parse(time.RFC3339Nano, deletedAt)
	if err != nil {
		t, err = time.Parse("2006-01-02 15:04:05.999999999-07:00", deletedAt)
	}
	if err != nil {
		return ""
	}
	return t.AddDate(0, 0, TrashRetentionDays).Format(time.RFC3339)
}

// FormatTrashSummary 生成「X 卷 · X 章」之类的回收站摘要。
func FormatTrashSummary(volumes, chapters, entities int) string {
	parts := make([]string, 0, 3)
	if volumes > 0 {
		parts = append(parts, fmt.Sprintf("%d 卷", volumes))
	}
	if chapters > 0 {
		parts = append(parts, fmt.Sprintf("%d 章", chapters))
	}
	if entities > 0 {
		parts = append(parts, fmt.Sprintf("%d 个设定", entities))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " · ")
}
