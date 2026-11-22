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
// - 检索词长度 >= 3 runes：使用 chapters_fts 与 entities_fts（trigram）精确检索；
// - 检索词长度 < 3 runes：自动回退 LIKE '%query%'，避免 trigram 对双字人名（如“林渊”）漏召回；
// - maxChapterSeq > 0 时严格限制 chapters.global_seq <= maxChapterSeq，杜绝未来章节剧透。
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
		return hits[i].EntityID < hits[j].EntityID
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
