package memory

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/Black0Bag/minibox/internal/embed"
)

// SearchResult 搜索结果
type SearchResult struct {
	Entry *Entry  `json:"entry"`
	Score float64 `json:"score"`
}

var (
	cjkRe  = regexp.MustCompile(`[\p{Han}]+`)
	wordRe = regexp.MustCompile(`[A-Za-z0-9_]+`)
)

// extractCJKBlocks 提取查询中的连续中文块（>=3 字走 trigram；<3 字走 LIKE 兜底）
func extractCJKBlocks(q string) (long []string, short []string) {
	for _, m := range cjkRe.FindAllString(q, -1) {
		if len([]rune(m)) >= 3 {
			long = append(long, m)
		} else {
			short = append(short, m)
		}
	}
	return
}

// extractWords 提取英文/数字词（>=3 走 trigram，<3 走 LIKE）
func extractWords(q string) (long []string, short []string) {
	for _, m := range wordRe.FindAllString(q, -1) {
		if len(m) >= 3 {
			long = append(long, m)
		} else {
			short = append(short, m)
		}
	}
	return
}

// ftsRanked FTS5 全文检索，返回 id → rank（rank 0 最相关；trigram 中文 + 短词 LIKE 兜底）
func (s *Store) ftsRanked(query, zone string) (map[int64]int, error) {
	ranked := map[int64]int{}
	query = strings.TrimSpace(query)
	if query == "" {
		return ranked, nil
	}

	cjkLong, cjkShort := extractCJKBlocks(query)
	wordLong, wordShort := extractWords(query)

	var matchParts []string
	for _, t := range cjkLong {
		matchParts = append(matchParts, `"`+t+`"`)
	}
	for _, t := range wordLong {
		matchParts = append(matchParts, `"`+t+`"`)
	}

	nextRank := 0
	if len(matchParts) > 0 {
		match := strings.Join(matchParts, " OR ")
		where := "WHERE entries_fts MATCH ?"
		args := []any{match}
		if zone != "" {
			where += " AND entries.zone = ?"
			args = append(args, zone)
		}
		rows, err := s.db.Query(`SELECT entries_fts.rowid
			FROM entries_fts JOIN entries ON entries.id = entries_fts.rowid
			`+where+` ORDER BY bm25(entries_fts) LIMIT 200`, args...)
		if err != nil {
			return nil, fmt.Errorf("全文检索: %w", err)
		}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				_ = rows.Close()
				return nil, err
			}
			if _, ok := ranked[id]; !ok {
				ranked[id] = nextRank
				nextRank++
			}
		}
		_ = rows.Close()
	}

	// 短词 LIKE 兜底
	shorts := append(append([]string{}, cjkShort...), wordShort...)
	if len(shorts) > 0 {
		var likes []string
		var args []any
		for _, t := range shorts {
			likes = append(likes, "(entries.content LIKE ? OR entries.title LIKE ?)")
			args = append(args, "%"+t+"%", "%"+t+"%")
		}
		where := "WHERE (" + strings.Join(likes, " OR ") + ")"
		if zone != "" {
			where += " AND entries.zone = ?"
			args = append(args, zone)
		}
		rows, err := s.db.Query(`SELECT entries.id FROM entries `+where+` LIMIT 100`, args...)
		if err != nil {
			return nil, fmt.Errorf("短词检索: %w", err)
		}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				_ = rows.Close()
				return nil, err
			}
			if _, ok := ranked[id]; !ok {
				ranked[id] = nextRank
				nextRank++
			}
		}
		_ = rows.Close()
	}
	return ranked, nil
}

// Search FTS5 全文搜索（trigram 中文分词 + 短词 LIKE 兜底）
func (s *Store) Search(query, zone string, limit int) ([]SearchResult, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	ranked, err := s.ftsRanked(query, zone)
	if err != nil {
		return nil, err
	}
	return s.resultsFromRanked(ranked, limit)
}

// HybridSearch FTS5 + 向量 RRF 融合检索
// 说明：embedding 查询用真实 API（NVIDIA nemotron-3-embed-1b），与 FTS5 结果按 RRF(k=60) 融合。
func (s *Store) HybridSearch(query, zone string, limit int, ec *embed.Client) ([]SearchResult, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	fts, err := s.ftsRanked(query, zone)
	if err != nil {
		return nil, err
	}
	vecIDs := map[int64]int{}
	if ec != nil {
		vecs, eerr := ec.Embed(context.Background(), []string{query})
		if eerr == nil && len(vecs) > 0 {
			hits, verr := s.VectorSearch(vecs[0], limit*2)
			if verr == nil {
				for i, h := range hits {
					vecIDs[h.EntryID] = i
				}
			}
		}
	}
	ids := hybridFuse(fts, vecIDs)
	results := make([]SearchResult, 0, len(ids))
	count := 0
	for _, id := range ids {
		e, gerr := s.GetEntry(id)
		if gerr != nil {
			continue
		}
		if zone != "" && e.Zone != zone {
			continue
		}
		results = append(results, SearchResult{Entry: e})
		count++
		if count >= limit {
			break
		}
	}
	return results, nil
}

// resultsFromRanked 从 rank map 组装排序结果
func (s *Store) resultsFromRanked(ranked map[int64]int, limit int) ([]SearchResult, error) {
	ids := make([]int64, 0, len(ranked))
	for id := range ranked {
		ids = append(ids, id)
	}
	// 按 rank 排序
	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			if ranked[ids[j]] < ranked[ids[i]] {
				ids[i], ids[j] = ids[j], ids[i]
			}
		}
	}
	results := make([]SearchResult, 0, len(ids))
	for i, id := range ids {
		if i >= limit {
			break
		}
		e, err := s.GetEntry(id)
		if err != nil {
			continue
		}
		results = append(results, SearchResult{Entry: e})
	}
	return results, nil
}
