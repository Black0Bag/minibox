package memory

import (
	"fmt"
	"regexp"
	"strings"
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

// Search FTS5 全文搜索（trigram 中文分词 + 短词 LIKE 兜底）
// 说明：trigram tokenizer 要求查询项 >= 3 字符；1-2 字短词用 LIKE 兜底（已验证方案）。
func (s *Store) Search(query, zone string, limit int) ([]SearchResult, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}

	cjkLong, cjkShort := extractCJKBlocks(query)
	wordLong, wordShort := extractWords(query)

	// 1. FTS trigram 检索
	hits := map[int64]float64{}

	var matchParts []string
	for _, t := range cjkLong {
		matchParts = append(matchParts, `"`+t+`"`)
	}
	for _, t := range wordLong {
		matchParts = append(matchParts, `"`+t+`"`)
	}

	if len(matchParts) > 0 {
		match := strings.Join(matchParts, " OR ")
		where := "WHERE entries_fts MATCH ?"
		args := []any{match}
		if zone != "" {
			where += " AND entries.zone = ?"
			args = append(args, zone)
		}
		rows, err := s.db.Query(`SELECT entries_fts.rowid, bm25(entries_fts) AS score
			FROM entries_fts JOIN entries ON entries.id = entries_fts.rowid
			`+where+` ORDER BY score LIMIT ?`, append(args, limit*2)...)
		if err != nil {
			return nil, fmt.Errorf("全文检索: %w", err)
		}
		for rows.Next() {
			var id int64
			var sc float64
			if err := rows.Scan(&id, &sc); err != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("扫描全文结果: %w", err)
			}
			hits[id] = sc
		}
		_ = rows.Close()
	}

	// 2. 短词 LIKE 兜底
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
		rows, err := s.db.Query(`SELECT entries.id FROM entries `+where+` LIMIT ?`, append(args, limit)...)
		if err != nil {
			return nil, fmt.Errorf("短词检索: %w", err)
		}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				_ = rows.Close()
				return nil, err
			}
			if _, ok := hits[id]; !ok {
				hits[id] = 0 // LIKE 命中权重低
			}
		}
		_ = rows.Close()
	}

	// 3. 组装结果
	results := make([]SearchResult, 0, len(hits))
	for id := range hits {
		e, err := s.GetEntry(id)
		if err != nil {
			continue
		}
		results = append(results, SearchResult{Entry: e})
	}
	return results, nil
}
