package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"nib/internal/domain"
)

// SearchMemory 执行 FTS5 trigram + 双字短词 LIKE 混合检索（遵循 AGENTS.md §3.4）。
// - 检索词长度 >= 3 runes：使用 chapters_fts / entities_fts / foreshadowings_fts / arc_summaries_fts（trigram）精确检索；
// - 检索词长度 < 3 runes：自动回退 LIKE '%query%'，避免 trigram 对双字人名（如“林渊”）漏召回；
// - maxChapterSeq > 0 时严格限制章节、伏笔埋设章号与摘要结束章号 <= maxChapterSeq，杜绝未来章节剧透。
func (s *Store) SearchMemory(ctx context.Context, bookID int64, query string, maxChapterSeq int) ([]domain.MemorySearchHit, error) {
	if bookID <= 0 {
		return nil, &domain.ValidationError{Field: "bookId", Message: "书籍 ID 必须大于 0"}
	}
	if maxChapterSeq < 0 {
		return nil, &domain.ValidationError{Field: "maxChapterSeq", Message: "最大章节号不能为负数"}
	}

	q := strings.TrimSpace(query)
	if q == "" {
		return []domain.MemorySearchHit{}, nil
	}

	var hits []domain.MemorySearchHit
	var err error
	if utf8.RuneCountInString(q) >= 3 {
		hits, err = s.searchMemoryFTS5(ctx, bookID, q, maxChapterSeq)
		if err == nil {
			attrHits, attrErr := s.searchEntityAttributesLike(ctx, bookID, q)
			if attrErr != nil {
				return nil, attrErr
			}
			if len(attrHits) > 0 {
				seenEntity := make(map[int64]bool, len(hits))
				for _, h := range hits {
					if h.Source == domain.MemorySearchSourceEntity && h.EntityID > 0 {
						seenEntity[h.EntityID] = true
					}
				}
				for _, ah := range attrHits {
					if !seenEntity[ah.EntityID] {
						seenEntity[ah.EntityID] = true
						hits = append(hits, ah)
					}
				}
			}
		}
	} else {
		hits, err = s.searchMemoryLike(ctx, bookID, q, maxChapterSeq)
	}
	if err != nil {
		return nil, err
	}

	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Score != hits[j].Score {
			return hits[i].Score > hits[j].Score
		}
		if hits[i].Source != hits[j].Source {
			return hits[i].Source < hits[j].Source
		}
		if hits[i].ChapterSeq != hits[j].ChapterSeq {
			return hits[i].ChapterSeq > hits[j].ChapterSeq
		}
		if hits[i].EntityID != hits[j].EntityID {
			return hits[i].EntityID < hits[j].EntityID
		}
		if hits[i].ForeshadowingID != hits[j].ForeshadowingID {
			return hits[i].ForeshadowingID < hits[j].ForeshadowingID
		}
		if hits[i].ArcSummaryID != hits[j].ArcSummaryID {
			return hits[i].ArcSummaryID < hits[j].ArcSummaryID
		}
		return hits[i].ChapterID < hits[j].ChapterID
	})
	if len(hits) > 30 {
		hits = hits[:30]
	}
	return hits, nil
}

func (s *Store) searchMemoryFTS5(ctx context.Context, bookID int64, q string, maxChapterSeq int) ([]domain.MemorySearchHit, error) {
	matchExpr := formatFTS5Phrase(q)
	hits := make([]domain.MemorySearchHit, 0)

	// 1. 章节 FTS5 检索
	chapRows, err := s.db.QueryContext(ctx, `
		SELECT c.id, c.global_seq, c.title,
		       snippet(chapters_fts, 1, '【', '】', '…', 24) AS snip,
		       c.content,
		       bm25(chapters_fts) AS rank
		FROM chapters_fts
		JOIN chapters c ON c.id = chapters_fts.rowid
		WHERE chapters_fts MATCH ?
		  AND c.book_id = ?
		  AND (? <= 0 OR c.global_seq <= ?)
		ORDER BY rank ASC, c.global_seq DESC
		LIMIT 20
	`, matchExpr, bookID, maxChapterSeq, maxChapterSeq)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer chapRows.Close()

	for chapRows.Next() {
		var (
			id, seq              int64
			title, snip, content string
			rank                 float64
		)
		if err := chapRows.Scan(&id, &seq, &title, &snip, &content, &rank); err != nil {
			return nil, mapSQLiteError(err)
		}
		if strings.TrimSpace(snip) == "" || !strings.Contains(snip, "【") {
			snip = buildContextSnippet(content, q, title)
		}
		hits = append(hits, domain.MemorySearchHit{
			Source:     domain.MemorySearchSourceChapter,
			ChapterID:  id,
			ChapterSeq: int(seq),
			Title:      fmt.Sprintf("第%d章 %s", seq, title),
			Snippet:    snip,
			Score:      bm25ToPositiveScore(rank),
		})
	}
	if err := chapRows.Err(); err != nil {
		return nil, err
	}

	// 2. 实体 FTS5 检索
	entRows, err := s.db.QueryContext(ctx, `
		SELECT e.id, e.name, e.aliases_json, e.summary,
		       bm25(entities_fts) AS rank
		FROM entities_fts
		JOIN entities e ON e.id = entities_fts.rowid
		WHERE entities_fts MATCH ?
		  AND e.book_id = ?
		ORDER BY rank ASC, e.is_invariant DESC, e.id ASC
		LIMIT 20
	`, matchExpr, bookID)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer entRows.Close()

	for entRows.Next() {
		var (
			id                         int64
			name, aliasesJSON, summary string
			rank                       float64
		)
		if err := entRows.Scan(&id, &name, &aliasesJSON, &summary, &rank); err != nil {
			return nil, mapSQLiteError(err)
		}
		combined := formatEntitySearchText(name, unmarshalJSONSlice[string](aliasesJSON), summary, nil)
		hits = append(hits, domain.MemorySearchHit{
			Source:   domain.MemorySearchSourceEntity,
			EntityID: id,
			Title:    name,
			Snippet:  buildContextSnippet(combined, q, name),
			Score:    bm25ToPositiveScore(rank),
		})
	}
	if err := entRows.Err(); err != nil {
		return nil, err
	}

	// 3. 伏笔 FTS5 检索（防剧透：maxChapterSeq > 0 时仅返回 planted_chapter_seq <= maxChapterSeq）
	fsRows, err := s.db.QueryContext(ctx, `
		SELECT f.id, f.planted_chapter_seq, f.title, f.description, f.status,
		       bm25(foreshadowings_fts) AS rank
		FROM foreshadowings_fts
		JOIN foreshadowings f ON f.id = foreshadowings_fts.rowid
		WHERE foreshadowings_fts MATCH ?
		  AND f.book_id = ?
		  AND (? <= 0 OR f.planted_chapter_seq <= ?)
		ORDER BY rank ASC, f.planted_chapter_seq DESC, f.id ASC
		LIMIT 20
	`, matchExpr, bookID, maxChapterSeq, maxChapterSeq)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer fsRows.Close()

	for fsRows.Next() {
		var (
			id, plantedSeq           int64
			title, description, stat string
			rank                     float64
		)
		if err := fsRows.Scan(&id, &plantedSeq, &title, &description, &stat, &rank); err != nil {
			return nil, mapSQLiteError(err)
		}
		combined := formatForeshadowSearchText(title, description, domain.ForeshadowingStatus(stat))
		hits = append(hits, domain.MemorySearchHit{
			Source:          domain.MemorySearchSourceForeshadowing,
			ForeshadowingID: id,
			ChapterSeq:      int(plantedSeq),
			Title:           title,
			Snippet:         buildContextSnippet(combined, q, title),
			Score:           bm25ToPositiveScore(rank),
		})
	}
	if err := fsRows.Err(); err != nil {
		return nil, err
	}

	// 4. 弧线/卷摘要 FTS5 检索（防剧透：maxChapterSeq > 0 时仅返回 end_seq <= maxChapterSeq）
	arcRows, err := s.db.QueryContext(ctx, `
		SELECT a.id, a.level, a.start_seq, a.end_seq, a.summary,
		       bm25(arc_summaries_fts) AS rank
		FROM arc_summaries_fts
		JOIN arc_summaries a ON a.id = arc_summaries_fts.rowid
		WHERE arc_summaries_fts MATCH ?
		  AND a.book_id = ?
		  AND (? <= 0 OR a.end_seq <= ?)
		ORDER BY rank ASC, a.end_seq DESC, a.id ASC
		LIMIT 20
	`, matchExpr, bookID, maxChapterSeq, maxChapterSeq)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer arcRows.Close()

	for arcRows.Next() {
		var (
			id, startSeq, endSeq int64
			level, summary       string
			rank                 float64
		)
		if err := arcRows.Scan(&id, &level, &startSeq, &endSeq, &summary, &rank); err != nil {
			return nil, mapSQLiteError(err)
		}
		title := formatArcSummarySearchTitle(domain.ArcSummaryLevel(level), int(startSeq), int(endSeq))
		hits = append(hits, domain.MemorySearchHit{
			Source:       domain.MemorySearchSourceArcSummary,
			ArcSummaryID: id,
			ChapterSeq:   int(endSeq),
			Title:        title,
			Snippet:      buildContextSnippet(summary, q, title),
			Score:        bm25ToPositiveScore(rank),
		})
	}
	if err := arcRows.Err(); err != nil {
		return nil, err
	}

	return hits, nil
}

// searchEntityAttributesLike 弥补 entities_fts 未索引 attributes_json 导致的 3+ 字属性/境界词漏召回。
func (s *Store) searchEntityAttributesLike(ctx context.Context, bookID int64, q string) ([]domain.MemorySearchHit, error) {
	pattern := "%" + escapeLikePattern(q) + "%"
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, aliases_json, summary, attributes_json
		FROM entities
		WHERE book_id = ?
		  AND attributes_json LIKE ? ESCAPE '\'
		ORDER BY is_invariant DESC, id ASC
		LIMIT 20
	`, bookID, pattern)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer rows.Close()

	hits := make([]domain.MemorySearchHit, 0)
	for rows.Next() {
		var (
			id                                    int64
			name, aliasesJSON, summary, attrsJSON string
		)
		if err := rows.Scan(&id, &name, &aliasesJSON, &summary, &attrsJSON); err != nil {
			return nil, mapSQLiteError(err)
		}
		combined := formatEntitySearchText(name, unmarshalJSONSlice[string](aliasesJSON), summary, unmarshalAttributes(attrsJSON))
		hits = append(hits, domain.MemorySearchHit{
			Source:   domain.MemorySearchSourceEntity,
			EntityID: id,
			Title:    name,
			Snippet:  buildContextSnippet(combined, q, name),
			Score:    1.25,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return hits, nil
}

func (s *Store) searchMemoryLike(ctx context.Context, bookID int64, q string, maxChapterSeq int) ([]domain.MemorySearchHit, error) {
	pattern := "%" + escapeLikePattern(q) + "%"
	hits := make([]domain.MemorySearchHit, 0)

	// 1. 章节短词 LIKE 回退检索
	chapRows, err := s.db.QueryContext(ctx, `
		SELECT id, global_seq, title, content
		FROM chapters
		WHERE book_id = ?
		  AND (? <= 0 OR global_seq <= ?)
		  AND (title LIKE ? ESCAPE '\' OR content LIKE ? ESCAPE '\')
		ORDER BY global_seq DESC, id DESC
		LIMIT 20
	`, bookID, maxChapterSeq, maxChapterSeq, pattern, pattern)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer chapRows.Close()

	for chapRows.Next() {
		var (
			id, seq        int64
			title, content string
		)
		if err := chapRows.Scan(&id, &seq, &title, &content); err != nil {
			return nil, mapSQLiteError(err)
		}
		score := 1.0
		if strings.Contains(title, q) {
			score += 2.0
		}
		if cnt := strings.Count(content, q); cnt > 0 {
			score += float64(min(cnt, 10)) * 0.25
		}
		hits = append(hits, domain.MemorySearchHit{
			Source:     domain.MemorySearchSourceChapter,
			ChapterID:  id,
			ChapterSeq: int(seq),
			Title:      fmt.Sprintf("第%d章 %s", seq, title),
			Snippet:    buildContextSnippet(content, q, title),
			Score:      score,
		})
	}
	if err := chapRows.Err(); err != nil {
		return nil, err
	}

	// 2. 实体短词 LIKE 回退检索
	entRows, err := s.db.QueryContext(ctx, `
		SELECT id, name, aliases_json, summary, attributes_json
		FROM entities
		WHERE book_id = ?
		  AND (
		      name LIKE ? ESCAPE '\'
		      OR aliases_json LIKE ? ESCAPE '\'
		      OR summary LIKE ? ESCAPE '\'
		      OR attributes_json LIKE ? ESCAPE '\'
		  )
		ORDER BY is_invariant DESC, id ASC
		LIMIT 20
	`, bookID, pattern, pattern, pattern, pattern)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer entRows.Close()

	for entRows.Next() {
		var (
			id                                    int64
			name, aliasesJSON, summary, attrsJSON string
		)
		if err := entRows.Scan(&id, &name, &aliasesJSON, &summary, &attrsJSON); err != nil {
			return nil, mapSQLiteError(err)
		}
		aliases := unmarshalJSONSlice[string](aliasesJSON)
		attrs := unmarshalAttributes(attrsJSON)
		score := 1.0
		if strings.EqualFold(strings.TrimSpace(name), q) {
			score += 4.0
		} else if strings.Contains(name, q) {
			score += 2.5
		}
		combined := formatEntitySearchText(name, aliases, summary, attrs)
		hits = append(hits, domain.MemorySearchHit{
			Source:   domain.MemorySearchSourceEntity,
			EntityID: id,
			Title:    name,
			Snippet:  buildContextSnippet(combined, q, name),
			Score:    score,
		})
	}
	if err := entRows.Err(); err != nil {
		return nil, err
	}

	// 3. 伏笔短词 LIKE 回退检索
	fsRows, err := s.db.QueryContext(ctx, `
		SELECT id, planted_chapter_seq, title, description, status
		FROM foreshadowings
		WHERE book_id = ?
		  AND (? <= 0 OR planted_chapter_seq <= ?)
		  AND (title LIKE ? ESCAPE '\' OR description LIKE ? ESCAPE '\')
		ORDER BY planted_chapter_seq DESC, id ASC
		LIMIT 20
	`, bookID, maxChapterSeq, maxChapterSeq, pattern, pattern)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer fsRows.Close()

	for fsRows.Next() {
		var (
			id, plantedSeq           int64
			title, description, stat string
		)
		if err := fsRows.Scan(&id, &plantedSeq, &title, &description, &stat); err != nil {
			return nil, mapSQLiteError(err)
		}
		score := 1.0
		if strings.Contains(title, q) {
			score += 2.0
		}
		if cnt := strings.Count(description, q); cnt > 0 {
			score += float64(min(cnt, 5)) * 0.25
		}
		combined := formatForeshadowSearchText(title, description, domain.ForeshadowingStatus(stat))
		hits = append(hits, domain.MemorySearchHit{
			Source:          domain.MemorySearchSourceForeshadowing,
			ForeshadowingID: id,
			ChapterSeq:      int(plantedSeq),
			Title:           title,
			Snippet:         buildContextSnippet(combined, q, title),
			Score:           score,
		})
	}
	if err := fsRows.Err(); err != nil {
		return nil, err
	}

	// 4. 弧线/卷摘要短词 LIKE 回退检索
	arcRows, err := s.db.QueryContext(ctx, `
		SELECT id, level, start_seq, end_seq, summary
		FROM arc_summaries
		WHERE book_id = ?
		  AND (? <= 0 OR end_seq <= ?)
		  AND summary LIKE ? ESCAPE '\'
		ORDER BY end_seq DESC, id ASC
		LIMIT 20
	`, bookID, maxChapterSeq, maxChapterSeq, pattern)
	if err != nil {
		return nil, mapSQLiteError(err)
	}
	defer arcRows.Close()

	for arcRows.Next() {
		var (
			id, startSeq, endSeq int64
			level, summary       string
		)
		if err := arcRows.Scan(&id, &level, &startSeq, &endSeq, &summary); err != nil {
			return nil, mapSQLiteError(err)
		}
		score := 1.0
		if cnt := strings.Count(summary, q); cnt > 0 {
			score += float64(min(cnt, 5)) * 0.25
		}
		title := formatArcSummarySearchTitle(domain.ArcSummaryLevel(level), int(startSeq), int(endSeq))
		hits = append(hits, domain.MemorySearchHit{
			Source:       domain.MemorySearchSourceArcSummary,
			ArcSummaryID: id,
			ChapterSeq:   int(endSeq),
			Title:        title,
			Snippet:      buildContextSnippet(summary, q, title),
			Score:        score,
		})
	}
	if err := arcRows.Err(); err != nil {
		return nil, err
	}

	return hits, nil
}

func formatFTS5Phrase(q string) string {
	return `"` + strings.ReplaceAll(q, `"`, `""`) + `"`
}

func bm25ToPositiveScore(rank float64) float64 {
	// SQLite FTS5 bm25() 越小（越负）表示相关性越高，统一转为正数越大越好。
	if rank < 0 {
		return 1.0 - rank
	}
	return 1.0 / (1.0 + rank)
}

func formatForeshadowSearchText(title, description string, status domain.ForeshadowingStatus) string {
	parts := make([]string, 0, 3)
	if label := status.Label(); label != "" {
		parts = append(parts, "["+label+"]")
	}
	if t := strings.TrimSpace(title); t != "" {
		parts = append(parts, t)
	}
	if d := strings.TrimSpace(description); d != "" {
		parts = append(parts, d)
	}
	return strings.Join(parts, " ")
}

func formatArcSummarySearchTitle(level domain.ArcSummaryLevel, startSeq, endSeq int) string {
	prefix := "弧线摘要"
	if level == domain.ArcSummaryLevelVolume {
		prefix = "卷摘要"
	}
	return fmt.Sprintf("%s 第%d~%d章", prefix, startSeq, endSeq)
}

func formatEntitySearchText(name string, aliases []string, summary string, attrs map[string]string) string {
	parts := make([]string, 0, 4)
	if len(aliases) > 0 {
		parts = append(parts, "别名："+strings.Join(aliases, "、"))
	}
	if s := strings.TrimSpace(summary); s != "" {
		parts = append(parts, s)
	}
	if len(attrs) > 0 {
		keys := make([]string, 0, len(attrs))
		for k := range attrs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		pairs := make([]string, 0, len(keys))
		for _, k := range keys {
			pairs = append(pairs, fmt.Sprintf("%s=%s", k, attrs[k]))
		}
		parts = append(parts, strings.Join(pairs, "；"))
	}
	if len(parts) == 0 {
		return name
	}
	return strings.Join(parts, " | ")
}

func buildContextSnippet(text, query, fallback string) string {
	clean := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", " "), "\n", " "))
	if clean == "" {
		return fallback
	}
	runes := []rune(clean)
	qRunes := []rune(query)

	matchIdx := -1
	if len(qRunes) > 0 {
		lowerClean := []rune(strings.ToLower(clean))
		lowerQ := []rune(strings.ToLower(query))
		for i := 0; i <= len(lowerClean)-len(lowerQ); i++ {
			matched := true
			for j := range lowerQ {
				if lowerClean[i+j] != lowerQ[j] {
					matched = false
					break
				}
			}
			if matched {
				matchIdx = i
				break
			}
		}
	}

	const radius = 36
	if matchIdx < 0 {
		if len(runes) <= radius*2 {
			return clean
		}
		return string(runes[:radius*2]) + "…"
	}

	matchIdx = min(matchIdx, len(runes))
	matchEnd := min(len(runes), matchIdx+len(qRunes))
	start := max(0, matchIdx-radius)
	end := min(len(runes), matchEnd+radius)

	var b strings.Builder
	if start > 0 {
		b.WriteString("…")
	}
	b.WriteString(string(runes[start:matchIdx]))
	b.WriteString("【")
	b.WriteString(string(runes[matchIdx:matchEnd]))
	b.WriteString("】")
	b.WriteString(string(runes[matchEnd:end]))
	if end < len(runes) {
		b.WriteString("…")
	}
	return b.String()
}
