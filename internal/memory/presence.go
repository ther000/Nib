package memory

import (
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"nib/internal/domain"
)

// ComputeEntityPresence 统计指定书籍截至 upToSeq（<0 表示全书最新章，0 表示开篇初始态）
// 各设定实体的出场章数、正文提及次数、节拍绑定次数与掉线章数。
func ComputeEntityPresence(
	bookID int64,
	entities []domain.Entity,
	chapters []domain.Chapter,
	beatsByChapter map[int64][]domain.Beat,
	upToSeq int,
) domain.EntityPresenceReport {
	sortedChaps := make([]domain.Chapter, len(chapters))
	copy(sortedChaps, chapters)
	sort.Slice(sortedChaps, func(i, j int) bool {
		if sortedChaps[i].GlobalSeq != sortedChaps[j].GlobalSeq {
			return sortedChaps[i].GlobalSeq < sortedChaps[j].GlobalSeq
		}
		return sortedChaps[i].ID < sortedChaps[j].ID
	})

	latestSeq := 0
	for _, ch := range sortedChaps {
		if ch.GlobalSeq > latestSeq {
			latestSeq = ch.GlobalSeq
		}
	}

	referenceSeq := upToSeq
	if upToSeq < 0 {
		referenceSeq = latestSeq
	} else if upToSeq == 0 {
		referenceSeq = 0
	} else if latestSeq > 0 && upToSeq > latestSeq {
		referenceSeq = latestSeq
	}

	activeChaps := make([]domain.Chapter, 0, len(sortedChaps))
	if referenceSeq > 0 {
		for _, ch := range sortedChaps {
			if ch.GlobalSeq > 0 && ch.GlobalSeq <= referenceSeq {
				activeChaps = append(activeChaps, ch)
			}
		}
	}

	stats := make([]domain.EntityPresenceStat, len(entities))
	for i, ent := range entities {
		terms := buildEntitySearchTerms(ent)
		stat := domain.EntityPresenceStat{
			EntityID:            ent.ID,
			EntityName:          ent.Name,
			EntityKind:          ent.Kind,
			IsInvariant:         ent.IsInvariant,
			AppearedChapterSeqs: make([]int, 0),
		}

		for _, ch := range activeChaps {
			mentions := countNonOverlappingTerms(ch.Content, terms)
			beatHits := countBeatAppearancesForEntity(beatsByChapter[ch.ID], ent.ID)
			if mentions == 0 && beatHits == 0 {
				continue
			}
			stat.AppearanceChapters++
			stat.MentionCount += mentions
			stat.BeatAppearances += beatHits
			stat.AppearedChapterSeqs = append(stat.AppearedChapterSeqs, ch.GlobalSeq)
			if stat.FirstSeenSeq == 0 {
				stat.FirstSeenSeq = ch.GlobalSeq
			}
			stat.LastSeenSeq = ch.GlobalSeq
			stat.LastSeenChapterID = ch.ID
			stat.LastSeenChapterTitle = ch.Title
		}

		if stat.LastSeenSeq > 0 {
			if referenceSeq > stat.LastSeenSeq {
				stat.AbsentChapters = referenceSeq - stat.LastSeenSeq
			}
			stat.NeverAppeared = false
		} else {
			stat.AbsentChapters = 0
			stat.NeverAppeared = true
		}

		stats[i] = stat
	}

	return domain.EntityPresenceReport{
		BookID:           bookID,
		ReferenceSeq:     referenceSeq,
		LatestChapterSeq: latestSeq,
		Stats:            stats,
	}
}

func buildEntitySearchTerms(ent domain.Entity) []string {
	seen := make(map[string]struct{}, 1+len(ent.Aliases))
	terms := make([]string, 0, 1+len(ent.Aliases))

	appendTerm := func(raw string) {
		term := strings.TrimSpace(raw)
		if utf8.RuneCountInString(term) < 2 {
			return
		}
		if _, dup := seen[term]; dup {
			return
		}
		seen[term] = struct{}{}
		terms = append(terms, term)
	}

	appendTerm(ent.Name)
	for _, alias := range ent.Aliases {
		appendTerm(alias)
	}

	// 按字节长度降序、字典序稳定排列，确保最长优先匹配（如「青云宗」先于「青云」）。
	sort.Slice(terms, func(i, j int) bool {
		if len(terms[i]) != len(terms[j]) {
			return len(terms[i]) > len(terms[j])
		}
		return terms[i] < terms[j]
	})
	return terms
}

func countNonOverlappingTerms(text string, sortedTerms []string) int {
	if text == "" || len(sortedTerms) == 0 {
		return 0
	}
	count := 0
	for i := 0; i < len(text); {
		matchedLen := 0
		rest := text[i:]
		for _, term := range sortedTerms {
			if len(rest) >= len(term) && strings.HasPrefix(rest, term) {
				matchedLen = len(term)
				break
			}
		}
		if matchedLen > 0 {
			count++
			i += matchedLen
			continue
		}
		_, width := utf8.DecodeRuneInString(rest)
		if width <= 0 {
			width = 1
		}
		i += width
	}
	return count
}

func countBeatAppearancesForEntity(beats []domain.Beat, entityID int64) int {
	if len(beats) == 0 || entityID <= 0 {
		return 0
	}
	hits := 0
	for _, b := range beats {
		if b.POVEntityID != nil && *b.POVEntityID == entityID {
			hits++
			continue
		}
		if slices.Contains(b.EntityIDs, entityID) {
			hits++
		}
	}
	return hits
}
