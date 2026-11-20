package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"nib/internal/domain"
)

func openMemoryStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open(:memory:) failed: %v", err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Errorf("Close() failed: %v", err)
		}
	})
	return st
}

func TestOpenPragmasAndDefaultRoutes(t *testing.T) {
	ctx := context.Background()
	dbFile := filepath.Join(t.TempDir(), "nested", "nib.db")

	st, err := Open(dbFile)
	if err != nil {
		t.Fatalf("Open(file) failed: %v", err)
	}
	defer st.Close()

	if st.Path() != dbFile {
		t.Fatalf("Path() = %q, want %q", st.Path(), dbFile)
	}

	var journalMode string
	if err := st.db.QueryRowContext(ctx, `PRAGMA journal_mode;`).Scan(&journalMode); err != nil {
		t.Fatalf("query journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Fatalf("journal_mode = %q, want wal", journalMode)
	}

	var foreignKeys int
	if err := st.db.QueryRowContext(ctx, `PRAGMA foreign_keys;`).Scan(&foreignKeys); err != nil {
		t.Fatalf("query foreign_keys: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d, want 1", foreignKeys)
	}

	var busyTimeout int
	if err := st.db.QueryRowContext(ctx, `PRAGMA busy_timeout;`).Scan(&busyTimeout); err != nil {
		t.Fatalf("query busy_timeout: %v", err)
	}
	if busyTimeout != 5000 {
		t.Fatalf("busy_timeout = %d, want 5000", busyTimeout)
	}

	var syncMode int
	if err := st.db.QueryRowContext(ctx, `PRAGMA synchronous;`).Scan(&syncMode); err != nil {
		t.Fatalf("query synchronous: %v", err)
	}
	if syncMode != 1 { // NORMAL == 1
		t.Fatalf("synchronous = %d, want 1 (NORMAL)", syncMode)
	}

	// 验证 5 环节默认模型路由已幂等写入。
	routes, err := st.ListModelRoutes(ctx)
	if err != nil {
		t.Fatalf("ListModelRoutes: %v", err)
	}
	wantStages := domain.AllPipelineStages()
	if len(routes) != len(wantStages) {
		t.Fatalf("len(routes) = %d, want %d", len(routes), len(wantStages))
	}
	for i, stage := range wantStages {
		if routes[i].Stage != stage {
			t.Fatalf("routes[%d].Stage = %q, want %q", i, routes[i].Stage, stage)
		}
		if routes[i].ProviderID != nil {
			t.Fatalf("routes[%d].ProviderID = %v, want nil", i, *routes[i].ProviderID)
		}
	}
}

func TestMigrationIdempotencyAndUpgradeBackup(t *testing.T) {
	ctx := context.Background()
	dbFile := filepath.Join(t.TempDir(), "nib.db")

	// 1. 首次打开：建表且不应在全新空库产生 .bak.1。
	st1, err := Open(dbFile)
	if err != nil {
		t.Fatalf("first Open failed: %v", err)
	}
	createdBook, err := st1.CreateBook(ctx, domain.CreateBookInput{
		Title: "凡人问道录",
		Genre: domain.GenreXianxia,
	})
	if err != nil {
		t.Fatalf("CreateBook: %v", err)
	}
	// 自定义一条路由，确认再次打开不会覆盖用户配置。
	if _, err := st1.SaveModelRoute(ctx, domain.ModelRoute{
		Stage:       domain.StageDraft,
		ModelName:   "deepseek-chat",
		Temperature: 0.9,
		MaxTokens:   8192,
	}); err != nil {
		t.Fatalf("SaveModelRoute: %v", err)
	}
	if err := st1.Close(); err != nil {
		t.Fatalf("Close st1: %v", err)
	}
	if _, err := os.Stat(dbFile + ".bak.1"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fresh DB should not create .bak.1, stat err = %v", err)
	}

	// 2. 幂等重开：不应重复迁移，不生成备份，用户路由与书籍完好。
	st2, err := Open(dbFile)
	if err != nil {
		t.Fatalf("re-Open failed: %v", err)
	}
	route, err := st2.GetModelRoute(ctx, domain.StageDraft)
	if err != nil {
		t.Fatalf("GetModelRoute: %v", err)
	}
	if route.ModelName != "deepseek-chat" || route.MaxTokens != 8192 {
		t.Fatalf("re-Open overwrote custom route: %+v", route)
	}
	gotBook, err := st2.GetBook(ctx, createdBook.ID)
	if err != nil || gotBook.Title != "凡人问道录" {
		t.Fatalf("book not preserved across reopen: book=%+v err=%v", gotBook, err)
	}
	if err := st2.Close(); err != nil {
		t.Fatalf("Close st2: %v", err)
	}

	// 3. 模拟升级到 v2：必须在执行 v2 迁移前自动创建 nib.db.bak.2 备份。
	embedded, err := loadEmbeddedMigrations()
	if err != nil {
		t.Fatalf("loadEmbeddedMigrations: %v", err)
	}
	migsV2 := append(embedded, migration{
		Version: 2,
		Name:    "002_test_upgrade.sql",
		SQL:     `CREATE TABLE test_v2_marker (id INTEGER PRIMARY KEY, note TEXT NOT NULL);`,
	})
	st3, err := openWithMigrations(dbFile, migsV2)
	if err != nil {
		t.Fatalf("openWithMigrations v2 failed: %v", err)
	}
	defer st3.Close()

	bakPath := dbFile + ".bak.2"
	fi, err := os.Stat(bakPath)
	if err != nil {
		t.Fatalf("expected upgrade backup %q to exist: %v", bakPath, err)
	}
	if fi.Size() == 0 {
		t.Fatalf("upgrade backup %q is empty", bakPath)
	}

	// 验证备份文件本身是可独立打开且包含升级前书籍数据的有效 SQLite 数据库。
	bakStore, err := openWithMigrations(bakPath, embedded)
	if err != nil {
		t.Fatalf("backup DB %q should be valid sqlite: %v", bakPath, err)
	}
	defer bakStore.Close()
	bakBook, err := bakStore.GetBook(ctx, createdBook.ID)
	if err != nil || bakBook.Title != "凡人问道录" {
		t.Fatalf("backup DB missing pre-upgrade data: book=%+v err=%v", bakBook, err)
	}

	// 验证 v2 表已成功落库。
	if _, err := st3.db.ExecContext(ctx, `INSERT INTO test_v2_marker (note) VALUES ('ok')`); err != nil {
		t.Fatalf("v2 table missing after migration: %v", err)
	}
}

func TestProviderCRUDAndKeyPreservation(t *testing.T) {
	ctx := context.Background()
	st := openMemoryStore(t)

	// 创建供应商
	created, err := st.UpsertProvider(ctx, domain.UpsertProviderInput{
		Name:     "DeepSeek 官方",
		Protocol: domain.ProtocolOpenAICompat,
		BaseURL:  "https://api.deepseek.com/v1",
		APIKey:   "sk-secret-original-9999",
		Models:   []string{"deepseek-chat", "deepseek-reasoner", "  "},
		Enabled:  true,
	})
	if err != nil {
		t.Fatalf("UpsertProvider(create): %v", err)
	}
	if created.ID <= 0 || created.APIKey != "sk-secret-original-9999" {
		t.Fatalf("unexpected created provider: %+v", created)
	}
	if len(created.Models) != 2 {
		t.Fatalf("expected 2 cleaned models, got %v", created.Models)
	}

	// 更新时 APIKey 留空，必须保留原有密钥。
	updated, err := st.UpsertProvider(ctx, domain.UpsertProviderInput{
		ID:       created.ID,
		Name:     "DeepSeek 主力",
		Protocol: domain.ProtocolOpenAICompat,
		BaseURL:  "https://api.deepseek.com/v1",
		APIKey:   "",
		Models:   []string{"deepseek-chat"},
		Enabled:  false,
	})
	if err != nil {
		t.Fatalf("UpsertProvider(update keep key): %v", err)
	}
	if updated.APIKey != "sk-secret-original-9999" {
		t.Fatalf("expected existing APIKey preserved when empty, got %q", updated.APIKey)
	}
	if updated.Name != "DeepSeek 主力" || updated.Enabled {
		t.Fatalf("updated fields mismatch: %+v", updated)
	}

	// 更新时传入新密钥，必须覆盖。
	rotated, err := st.UpsertProvider(ctx, domain.UpsertProviderInput{
		ID:       created.ID,
		Name:     "DeepSeek 主力",
		Protocol: domain.ProtocolOpenAICompat,
		BaseURL:  "https://api.deepseek.com/v1",
		APIKey:   "sk-rotated-key-8888",
		Models:   []string{"deepseek-chat"},
		Enabled:  true,
	})
	if err != nil {
		t.Fatalf("UpsertProvider(rotate key): %v", err)
	}
	if rotated.APIKey != "sk-rotated-key-8888" {
		t.Fatalf("expected rotated APIKey, got %q", rotated.APIKey)
	}

	// 绑定模型路由后删除供应商，外键 ON DELETE SET NULL 应将 provider_id 自动置空。
	pid := created.ID
	savedRoute, err := st.SaveModelRoute(ctx, domain.ModelRoute{
		Stage:       domain.StageDraft,
		ProviderID:  &pid,
		ModelName:   "deepseek-chat",
		Temperature: 0.8,
		MaxTokens:   4096,
	})
	if err != nil {
		t.Fatalf("SaveModelRoute: %v", err)
	}
	if savedRoute.ProviderID == nil || *savedRoute.ProviderID != pid {
		t.Fatalf("route providerId = %v, want %d", savedRoute.ProviderID, pid)
	}

	if err := st.DeleteProvider(ctx, pid); err != nil {
		t.Fatalf("DeleteProvider: %v", err)
	}
	if err := st.DeleteProvider(ctx, pid); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("second DeleteProvider err = %v, want ErrNotFound", err)
	}
	routeAfterDelete, err := st.GetModelRoute(ctx, domain.StageDraft)
	if err != nil {
		t.Fatalf("GetModelRoute after provider delete: %v", err)
	}
	if routeAfterDelete.ProviderID != nil {
		t.Fatalf("expected route.ProviderID nil after FK ON DELETE SET NULL, got %d", *routeAfterDelete.ProviderID)
	}

	list, err := st.ListProviders(ctx)
	if err != nil {
		t.Fatalf("ListProviders: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 providers, got %d", len(list))
	}
}

func TestBooksVolumesChaptersFKCascadeAndSeq(t *testing.T) {
	ctx := context.Background()
	st := openMemoryStore(t)

	book, err := st.CreateBook(ctx, domain.CreateBookInput{
		Title:        "渊海剑主",
		Genre:        domain.GenreXuanhuan,
		Premise:      "边军小卒得残缺剑碑，步步登天",
		GoldenFinger: "残剑演武碑",
	})
	if err != nil {
		t.Fatalf("CreateBook: %v", err)
	}
	if book.TargetChapterWords != domain.DefaultTargetChapterWords {
		t.Fatalf("TargetChapterWords = %d, want %d", book.TargetChapterWords, domain.DefaultTargetChapterWords)
	}

	updatedBook, err := st.UpdateBook(ctx, domain.UpdateBookInput{
		ID:                 book.ID,
		Title:              "渊海剑主（修订）",
		Genre:              domain.GenreXuanhuan,
		Premise:            book.Premise,
		GoldenFinger:       book.GoldenFinger,
		TargetChapterWords: 3000,
		ExportDir:          `E:\Exports\Nib`,
	})
	if err != nil {
		t.Fatalf("UpdateBook: %v", err)
	}
	if updatedBook.TargetChapterWords != 3000 || updatedBook.ExportDir != `E:\Exports\Nib` {
		t.Fatalf("UpdateBook mismatch: %+v", updatedBook)
	}

	vol1, err := st.UpsertVolume(ctx, domain.UpsertVolumeInput{
		BookID: book.ID,
		Title:  "北境寒霜",
		Goal:   "破关入宗",
	})
	if err != nil {
		t.Fatalf("UpsertVolume 1: %v", err)
	}
	vol2, err := st.UpsertVolume(ctx, domain.UpsertVolumeInput{
		BookID: book.ID,
		Title:  "天渊潜龙",
	})
	if err != nil {
		t.Fatalf("UpsertVolume 2: %v", err)
	}
	if vol1.Seq != 1 || vol2.Seq != 2 {
		t.Fatalf("volume seqs = (%d, %d), want (1, 2)", vol1.Seq, vol2.Seq)
	}

	// 同书重复卷序号必须报 ErrConflict。
	_, err = st.UpsertVolume(ctx, domain.UpsertVolumeInput{
		BookID: book.ID,
		Seq:    1,
		Title:  "冲突卷",
	})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate volume seq err = %v, want ErrConflict", err)
	}

	// 跨卷新建章节，global_seq 连续递增，vol_seq 按卷各自递增。
	ch1, err := st.CreateChapter(ctx, domain.CreateChapterInput{BookID: book.ID, VolumeID: vol1.ID, Title: "寒夜拾碑"})
	if err != nil {
		t.Fatalf("CreateChapter 1: %v", err)
	}
	ch2, err := st.CreateChapter(ctx, domain.CreateChapterInput{BookID: book.ID, VolumeID: vol1.ID, Title: "校场立威"})
	if err != nil {
		t.Fatalf("CreateChapter 2: %v", err)
	}
	ch3, err := st.CreateChapter(ctx, domain.CreateChapterInput{BookID: book.ID, VolumeID: vol2.ID, Title: "初入天渊"})
	if err != nil {
		t.Fatalf("CreateChapter 3: %v", err)
	}
	if ch1.GlobalSeq != 1 || ch1.VolSeq != 1 || ch2.GlobalSeq != 2 || ch2.VolSeq != 2 || ch3.GlobalSeq != 3 || ch3.VolSeq != 1 {
		t.Fatalf("unexpected seqs: ch1=(%d,%d) ch2=(%d,%d) ch3=(%d,%d)",
			ch1.GlobalSeq, ch1.VolSeq, ch2.GlobalSeq, ch2.VolSeq, ch3.GlobalSeq, ch3.VolSeq)
	}

	// GetPreviousChapters 应按 global_seq DESC 返回紧邻前 2 章。
	prev, err := st.GetPreviousChapters(ctx, book.ID, ch3.GlobalSeq, 2)
	if err != nil {
		t.Fatalf("GetPreviousChapters: %v", err)
	}
	if len(prev) != 2 || prev[0].ID != ch2.ID || prev[1].ID != ch1.ID {
		t.Fatalf("GetPreviousChapters = %+v, want [ch2, ch1]", prev)
	}

	// 挂上节拍、版本与设定实体，然后删除 Book 验证外键级联删除。
	if _, err := st.ReplaceBeats(ctx, ch1.ID, []domain.Beat{
		{Stage: domain.BeatStageSetup, Summary: "寒夜巡营"},
	}); err != nil {
		t.Fatalf("ReplaceBeats: %v", err)
	}
	if _, err := st.SaveChapterContent(ctx, ch1.ID, "第一版正文", domain.ChapterStatusReviewing, domain.VersionSourceAIDraft, "gpt-test", `{"slot":"p0"}`); err != nil {
		t.Fatalf("SaveChapterContent 1: %v", err)
	}
	if _, err := st.SaveChapterContent(ctx, ch1.ID, "第二版正文", domain.ChapterStatusReviewing, domain.VersionSourceManualBackup, "", ""); err != nil {
		t.Fatalf("SaveChapterContent 2: %v", err)
	}
	if _, err := st.UpsertEntity(ctx, domain.UpsertEntityInput{
		BookID:      book.ID,
		Kind:        domain.EntityKindRule,
		Name:        "灵力守恒铁律",
		IsInvariant: true,
	}); err != nil {
		t.Fatalf("UpsertEntity: %v", err)
	}

	if err := st.DeleteBook(ctx, book.ID); err != nil {
		t.Fatalf("DeleteBook: %v", err)
	}

	// 校验所有关联子表全部被外键级联清空。
	for _, table := range []string{"volumes", "chapters", "chapter_beats", "chapter_versions", "entities"} {
		var count int
		if err := st.db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("table %s still has %d rows after book cascade delete", table, count)
		}
	}
}

func TestChapterSnapshotBeforeOverwriteAndVersionRestore(t *testing.T) {
	ctx := context.Background()
	st := openMemoryStore(t)

	book, err := st.CreateBook(ctx, domain.CreateBookInput{Title: "测试书", Genre: domain.GenreGeneral})
	if err != nil {
		t.Fatalf("CreateBook: %v", err)
	}
	vol, err := st.UpsertVolume(ctx, domain.UpsertVolumeInput{BookID: book.ID, Title: "第一卷"})
	if err != nil {
		t.Fatalf("UpsertVolume: %v", err)
	}
	chap, err := st.CreateChapter(ctx, domain.CreateChapterInput{BookID: book.ID, VolumeID: vol.ID, Title: "初战"})
	if err != nil {
		t.Fatalf("CreateChapter: %v", err)
	}

	// 1. 首次从空正文写入：不应产生空快照。
	v1Text := "风雪夜，林渊拔剑出鞘。"
	chap, err = st.SaveChapterContent(ctx, chap.ID, v1Text, domain.ChapterStatusReviewing, domain.VersionSourceAIDraft, "claude-sonnet", `{"total":1200}`)
	if err != nil {
		t.Fatalf("SaveChapterContent initial: %v", err)
	}
	if chap.WordCount != domain.CountWords(v1Text) || chap.LastBriefingJSON != `{"total":1200}` {
		t.Fatalf("initial save mismatch: %+v", chap)
	}
	versions, err := st.ListChapterVersions(ctx, chap.ID)
	if err != nil {
		t.Fatalf("ListChapterVersions 1: %v", err)
	}
	if len(versions) != 0 {
		t.Fatalf("expected 0 snapshots after initial write from empty content, got %d", len(versions))
	}

	// 2. 相同内容再次保存：不应产生冗余快照。
	chap, err = st.SaveChapterContent(ctx, chap.ID, v1Text, domain.ChapterStatusReviewing, domain.VersionSourceManualBackup, "", "")
	if err != nil {
		t.Fatalf("SaveChapterContent same content: %v", err)
	}
	versions, _ = st.ListChapterVersions(ctx, chap.ID)
	if len(versions) != 0 {
		t.Fatalf("expected 0 snapshots when content unchanged, got %d", len(versions))
	}

	// 3. 覆写新内容：必须先将 v1Text 归档到 chapter_versions，同时保留未传的 last_briefing_json。
	v2Text := "风雪骤停，林渊一剑斩断玄铁锁链，火星四溅。"
	chap, err = st.SaveChapterContent(ctx, chap.ID, v2Text, domain.ChapterStatusReviewing, domain.VersionSourceSurgicalPolish, "deepseek-chat", "")
	if err != nil {
		t.Fatalf("SaveChapterContent overwrite: %v", err)
	}
	if chap.Content != v2Text || chap.WordCount != domain.CountWords(v2Text) {
		t.Fatalf("overwritten chapter mismatch: %+v", chap)
	}
	if chap.LastBriefingJSON != `{"total":1200}` {
		t.Fatalf("empty briefingJSON should preserve previous briefing, got %q", chap.LastBriefingJSON)
	}

	versions, err = st.ListChapterVersions(ctx, chap.ID)
	if err != nil {
		t.Fatalf("ListChapterVersions 2: %v", err)
	}
	if len(versions) != 1 {
		t.Fatalf("expected 1 snapshot before overwrite, got %d", len(versions))
	}
	v1Snapshot := versions[0]
	if v1Snapshot.Content != v1Text || v1Snapshot.Source != domain.VersionSourceSurgicalPolish || v1Snapshot.ModelName != "deepseek-chat" {
		t.Fatalf("unexpected archived snapshot: %+v", v1Snapshot)
	}

	// 4. SaveChapterDraft（带可选标题修改）同样先存 v2Text 快照再覆写。
	v3Text := "林渊收剑入鞘，转身望向山门。"
	chap, err = st.SaveChapterDraft(ctx, domain.SaveChapterDraftInput{
		ChapterID:    chap.ID,
		Title:        "初战告捷",
		Content:      v3Text,
		BackupSource: domain.VersionSourceManualBackup,
	})
	if err != nil {
		t.Fatalf("SaveChapterDraft: %v", err)
	}
	if chap.Title != "初战告捷" || chap.Content != v3Text {
		t.Fatalf("SaveChapterDraft mismatch: %+v", chap)
	}

	// 5. 回滚到 v1Snapshot：必须先将当前 v3Text 以 source='restore_backup' 归档，再将正文恢复为 v1Text。
	restored, err := st.RestoreChapterVersion(ctx, v1Snapshot.ID)
	if err != nil {
		t.Fatalf("RestoreChapterVersion: %v", err)
	}
	if restored.Content != v1Text || restored.WordCount != domain.CountWords(v1Text) {
		t.Fatalf("restored chapter mismatch: %+v", restored)
	}

	versions, err = st.ListChapterVersions(ctx, chap.ID)
	if err != nil {
		t.Fatalf("ListChapterVersions after restore: %v", err)
	}
	if len(versions) != 3 {
		t.Fatalf("expected 3 snapshots (v1, v2, restore_backup of v3), got %d", len(versions))
	}
	// 最新快照（索引 0）必须是回滚前的 v3Text，且 source == restore_backup。
	if versions[0].Source != domain.VersionSourceRestoreBackup || versions[0].Content != v3Text {
		t.Fatalf("latest snapshot after restore = %+v, want restore_backup of v3", versions[0])
	}

	// 6. 定稿章节
	finalized, err := st.FinalizeChapter(ctx, chap.ID)
	if err != nil {
		t.Fatalf("FinalizeChapter: %v", err)
	}
	if finalized.Status != domain.ChapterStatusFinalized {
		t.Fatalf("status = %q, want finalized", finalized.Status)
	}
}

func TestBeatsReplacementAndMetaUpdate(t *testing.T) {
	ctx := context.Background()
	st := openMemoryStore(t)

	book, _ := st.CreateBook(ctx, domain.CreateBookInput{Title: "都市商战", Genre: domain.GenreUrban})
	vol, _ := st.UpsertVolume(ctx, domain.UpsertVolumeInput{BookID: book.ID, Title: "起势"})
	chap, _ := st.CreateChapter(ctx, domain.CreateChapterInput{BookID: book.ID, VolumeID: vol.ID, Title: "第一桶金"})

	povID := int64(42)
	beats, err := st.ReplaceBeats(ctx, chap.ID, []domain.Beat{
		{Stage: domain.BeatStageSetup, Summary: "交代窘境", ConflictNote: "房租到期", POVEntityID: &povID, EntityIDs: []int64{42, 7, 42}, TargetWords: 450},
		{Stage: domain.BeatStageTurn, Summary: "情报触发", EntityIDs: []int64{7}, TargetWords: 0}, // 0 -> default 500
		{Stage: domain.BeatStageHook, Summary: "神秘来电"},
	})
	if err != nil {
		t.Fatalf("ReplaceBeats 1: %v", err)
	}
	if len(beats) != 3 {
		t.Fatalf("len(beats) = %d, want 3", len(beats))
	}
	if beats[0].Seq != 1 || beats[1].Seq != 2 || beats[2].Seq != 3 {
		t.Fatalf("beats seq not 1..3: %+v", beats)
	}
	if beats[0].POVEntityID == nil || *beats[0].POVEntityID != 42 {
		t.Fatalf("beats[0].POVEntityID = %v, want 42", beats[0].POVEntityID)
	}
	if len(beats[0].EntityIDs) != 2 {
		t.Fatalf("expected deduplicated EntityIDs [42,7], got %v", beats[0].EntityIDs)
	}
	if beats[1].TargetWords != domain.DefaultBeatTargetWords {
		t.Fatalf("beats[1].TargetWords = %d, want %d", beats[1].TargetWords, domain.DefaultBeatTargetWords)
	}

	// 整体替换（含删减与重排）
	replaced, err := st.ReplaceBeats(ctx, chap.ID, []domain.Beat{
		{Stage: domain.BeatStageBurst, Summary: "签约打脸", TargetWords: 800},
	})
	if err != nil {
		t.Fatalf("ReplaceBeats 2: %v", err)
	}
	if len(replaced) != 1 || replaced[0].Seq != 1 || replaced[0].Summary != "签约打脸" {
		t.Fatalf("replaced beats mismatch: %+v", replaced)
	}

	// 非法节拍应拒绝且不破坏旧节拍（事务原子性）。
	_, err = st.ReplaceBeats(ctx, chap.ID, []domain.Beat{
		{Stage: domain.BeatStageSetup, Summary: ""},
	})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("invalid beat err = %v, want ErrValidation", err)
	}
	stillThere, _ := st.ListBeats(ctx, chap.ID)
	if len(stillThere) != 1 || stillThere[0].Summary != "签约打脸" {
		t.Fatalf("failed ReplaceBeats corrupted existing beats: %+v", stillThere)
	}

	// 更新章节元信息（空 status/emotion/hook 保留原值）。
	updated, err := st.UpdateChapterMeta(ctx, domain.UpdateChapterMetaInput{
		ID:             chap.ID,
		Title:          "首单破局",
		EmotionTag:     domain.EmotionBurst,
		HookType:       domain.HookCrisis,
		HookNote:       "竞争对手堵门",
		OutlineSummary: "拿下首单并反将一军",
	})
	if err != nil {
		t.Fatalf("UpdateChapterMeta: %v", err)
	}
	if updated.Title != "首单破局" || updated.EmotionTag != domain.EmotionBurst || updated.HookType != domain.HookCrisis || updated.Status != domain.ChapterStatusPlanned {
		t.Fatalf("UpdateChapterMeta mismatch: %+v", updated)
	}
}

func TestEntitiesFilterInvariantsAndBatchLookup(t *testing.T) {
	ctx := context.Background()
	st := openMemoryStore(t)

	book, _ := st.CreateBook(ctx, domain.CreateBookInput{Title: "修真录", Genre: domain.GenreXianxia})

	rule, err := st.UpsertEntity(ctx, domain.UpsertEntityInput{
		BookID:      book.ID,
		Kind:        domain.EntityKindRule,
		Name:        "筑基不可御空",
		IsInvariant: true,
		Summary:     "金丹之前不得离地飞行超过三息",
	})
	if err != nil {
		t.Fatalf("UpsertEntity rule: %v", err)
	}
	hero, err := st.UpsertEntity(ctx, domain.UpsertEntityInput{
		BookID:      book.ID,
		Kind:        domain.EntityKindCharacter,
		Name:        "林渊",
		Aliases:     []string{"林执事", "小渊"},
		IsInvariant: false,
		Summary:     "青云宗杂役出身，行事谨慎",
		Attributes:  map[string]string{"cultivation": "筑基初期", "weapon": "青冥剑"},
	})
	if err != nil {
		t.Fatalf("UpsertEntity hero: %v", err)
	}
	sword, err := st.UpsertEntity(ctx, domain.UpsertEntityInput{
		BookID:      book.ID,
		Kind:        domain.EntityKindItem,
		Name:        "青冥剑",
		IsInvariant: false,
		Summary:     "中品灵器，剑身刻有敛息阵纹",
		Attributes:  map[string]string{"owner": "林渊"},
	})
	if err != nil {
		t.Fatalf("UpsertEntity sword: %v", err)
	}

	// ListInvariants 只返回 is_invariant = 1
	invariants, err := st.ListInvariants(ctx, book.ID)
	if err != nil {
		t.Fatalf("ListInvariants: %v", err)
	}
	if len(invariants) != 1 || invariants[0].ID != rule.ID {
		t.Fatalf("ListInvariants = %+v, want [%d]", invariants, rule.ID)
	}

	// 按 Kind 过滤
	chars, err := st.ListEntities(ctx, book.ID, domain.EntityFilter{Kind: domain.EntityKindCharacter})
	if err != nil {
		t.Fatalf("ListEntities(character): %v", err)
	}
	if len(chars) != 1 || chars[0].ID != hero.ID || chars[0].Attributes["cultivation"] != "筑基初期" {
		t.Fatalf("ListEntities(character) = %+v", chars)
	}

	// 按 Query 跨别名/属性检索（如“青冥剑”同时命中角色属性与道具名称）
	matched, err := st.ListEntities(ctx, book.ID, domain.EntityFilter{Query: "青冥剑"})
	if err != nil {
		t.Fatalf("ListEntities(query): %v", err)
	}
	if len(matched) != 2 {
		t.Fatalf("expected 2 matches for 青冥剑, got %d: %+v", len(matched), matched)
	}

	// 按别名检索
	byAlias, err := st.ListEntities(ctx, book.ID, domain.EntityFilter{Query: "林执事"})
	if err != nil {
		t.Fatalf("ListEntities(alias): %v", err)
	}
	if len(byAlias) != 1 || byAlias[0].ID != hero.ID {
		t.Fatalf("alias search mismatch: %+v", byAlias)
	}

	// GetEntitiesByIDs 去重并忽略无效 ID
	fetched, err := st.GetEntitiesByIDs(ctx, []int64{sword.ID, 0, -1, hero.ID, sword.ID})
	if err != nil {
		t.Fatalf("GetEntitiesByIDs: %v", err)
	}
	if len(fetched) != 2 {
		t.Fatalf("GetEntitiesByIDs len = %d, want 2", len(fetched))
	}

	// 更新实体并删除
	updatedHero, err := st.UpsertEntity(ctx, domain.UpsertEntityInput{
		ID:          hero.ID,
		BookID:      book.ID,
		Kind:        domain.EntityKindCharacter,
		Name:        "林渊",
		Aliases:     []string{"林长老"},
		IsInvariant: false,
		Summary:     hero.Summary,
		Attributes:  map[string]string{"cultivation": "筑基中期"},
	})
	if err != nil {
		t.Fatalf("UpsertEntity update: %v", err)
	}
	if updatedHero.Attributes["cultivation"] != "筑基中期" || len(updatedHero.Aliases) != 1 || updatedHero.Aliases[0] != "林长老" {
		t.Fatalf("updated entity mismatch: %+v", updatedHero)
	}

	if err := st.DeleteEntity(ctx, sword.ID); err != nil {
		t.Fatalf("DeleteEntity: %v", err)
	}
	if err := st.DeleteEntity(ctx, sword.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("second DeleteEntity err = %v, want ErrNotFound", err)
	}
}

func TestWALConcurrentReadsAndWrites(t *testing.T) {
	ctx := context.Background()
	dbFile := filepath.Join(t.TempDir(), "concurrent.db")
	st, err := Open(dbFile)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()

	book, err := st.CreateBook(ctx, domain.CreateBookInput{Title: "并发测试", Genre: domain.GenreGeneral})
	if err != nil {
		t.Fatalf("CreateBook: %v", err)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 40)

	for i := 0; i < 10; i++ {
		idx := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := st.UpsertEntity(ctx, domain.UpsertEntityInput{
				BookID: book.ID,
				Kind:   domain.EntityKindCharacter,
				Name:   fmt.Sprintf("并发角色-%d", idx),
			}); err != nil {
				errCh <- err
			}
		}()
	}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := st.ListEntities(ctx, book.ID, domain.EntityFilter{}); err != nil {
				errCh <- err
			}
		}()
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent WAL operation failed: %v", err)
	}

	all, err := st.ListEntities(ctx, book.ID, domain.EntityFilter{})
	if err != nil {
		t.Fatalf("ListEntities final: %v", err)
	}
	if len(all) != 10 {
		t.Fatalf("expected 10 concurrently inserted entities, got %d", len(all))
	}
}
