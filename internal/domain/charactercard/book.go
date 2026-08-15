package charactercard

// 角色书（character_book）管理：条目匹配 + 预算内组装（B10 世界书管理）。
// 规范依据：character-card-spec-v2（entries 语义：keys 触发/content 注入/insertion_order
// 决定插入次序/constant 常驻/selective 需主次双键/priority 预算内优先保留）。

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

// Book 角色书（内嵌世界书）。
type Book struct {
	Name              string         `json:"name"`
	Description       string         `json:"description"`
	ScanDepth         int            `json:"scan_depth"`         // 语境深度（前瞻消息数）
	TokenBudget       int            `json:"token_budget"`       // 语境预算（token 上限）
	RecursiveScanning bool           `json:"recursive_scanning"` // 命中条目内容是否可再触发其他条目
	Extensions        map[string]any `json:"extensions"`
	Entries           []Entry        `json:"entries"`
}

// Entry 角色书条目。
type Entry struct {
	Keys           []string       `json:"keys"`
	SecondaryKeys  []string       `json:"secondary_keys,omitempty"`
	Content        string         `json:"content"`
	Extensions     map[string]any `json:"extensions,omitempty"`
	Enabled        bool           `json:"enabled"`
	InsertionOrder int            `json:"insertion_order"` // 越小越靠前
	CaseSensitive  bool           `json:"case_sensitive,omitempty"`
	Name           string         `json:"name,omitempty"`
	Priority       int            `json:"priority,omitempty"` // 预算不足时低值先丢
	ID             int            `json:"id,omitempty"`
	Comment        string         `json:"comment,omitempty"`
	Selective      bool           `json:"selective,omitempty"` // 需 keys+secondary_keys 双中才触发
	Constant       bool           `json:"constant,omitempty"`  // 常驻注入（预算内）
	Position       string         `json:"position,omitempty"`  // before_char / after_char
}

// MatchResult 一次命中。
type MatchResult struct {
	Entry Entry
	Hit   string // 命中的关键词
}

// Match 在文本中匹配启用的条目，返回命中的条目列表（保持 entries 原顺序）。
// 语义：主键中任意命中即触发；selective 条目需主键+次键同时命中。
func (b *Book) Match(text string) []MatchResult {
	if b == nil {
		return nil
	}
	var out []MatchResult
	for _, e := range b.Entries {
		if !e.Enabled || len(e.Keys) == 0 {
			continue
		}
		hit, ok := matchEntry(e, text)
		if !ok {
			continue
		}
		out = append(out, MatchResult{Entry: e, Hit: hit})
	}
	return out
}

// matchEntry 单条目匹配（按大小写敏感标志）。
func matchEntry(e Entry, text string) (string, bool) {
	hay := text
	if !e.CaseSensitive {
		hay = strings.ToLower(text)
	}
	for _, k := range e.Keys {
		key := k
		if !e.CaseSensitive {
			key = strings.ToLower(k)
		}
		if key == "" {
			continue
		}
		if strings.Contains(hay, key) {
			if !e.Selective {
				return k, true
			}
			// selective：还需次键命中
			for _, sk := range e.SecondaryKeys {
				skey := sk
				if !e.CaseSensitive {
					skey = strings.ToLower(sk)
				}
				if skey != "" && strings.Contains(hay, skey) {
					return k, true
				}
			}
		}
	}
	return "", false
}

// assembleEntry 条目注入文本（含条目标题行，便于模型辨认来源）。
func assembleEntry(e Entry) string {
	var sb strings.Builder
	if e.Name != "" {
		sb.WriteString("【" + e.Name + "】\n")
	}
	sb.WriteString(e.Content)
	return sb.String()
}

// Assemble 匹配 + 排序 + 预算封顶，产出待注入的知识块（B10 世界书管理主入口）。
// order：命中条目按 insertion_order 升序（小的在上）；constant 条目优先保留且不计入淘汰；
// budgetTokens 为 token 上限（近似 2 字符 ≈ 1 token，中文偏保守、英文偏宽松），0 表示不限。
// 返回：注入块列表（已按注入次序排列）；unused 为预算不足被丢弃的条目名（供前端提示）。
func (b *Book) Assemble(text string, budgetTokens int) (blocks []string, unused []string) {
	hits := b.Match(text)
	if len(hits) == 0 {
		return nil, nil
	}

	// 常驻条目（constant）优先入列
	var ordered []MatchResult
	for _, h := range hits {
		if h.Entry.Constant {
			ordered = append(ordered, h)
		}
	}
	// 其余按 insertion_order 升序
	var rest []MatchResult
	for _, h := range hits {
		if !h.Entry.Constant {
			rest = append(rest, h)
		}
	}
	sort.SliceStable(rest, func(i, j int) bool {
		return rest[i].Entry.InsertionOrder < rest[j].Entry.InsertionOrder
	})
	ordered = append(ordered, rest...)

	// 预算封顶：先全部估算，超出则按 priority 丢弃（低值先丢，保持顺序）
	total := 0
	for _, h := range ordered {
		total += estTokens(assembleEntry(h.Entry))
	}
	budget := budgetTokens
	if budget <= 0 {
		budget = total // 不限预算时全收
	}
	used := make([]bool, len(ordered))
	if total > budget {
		// 淘汰次序：非 constant 先丢（按 priority 低值先丢）；constant 最后丢
		idx := make([]int, len(ordered))
		for i := range ordered {
			idx[i] = i
		}
		sort.SliceStable(idx, func(i, j int) bool {
			a, b := ordered[idx[i]].Entry, ordered[idx[j]].Entry
			if a.Constant != b.Constant {
				return !a.Constant // 非 constant 排前，先被丢
			}
			return a.Priority < b.Priority
		})
		cur := total
		for _, i := range idx {
			if used[i] || cur <= budget {
				continue
			}
			cur -= estTokens(assembleEntry(ordered[i].Entry))
			used[i] = true
		}
	}

	for i, h := range ordered {
		if used[i] {
			unused = append(unused, h.Entry.Name)
			continue
		}
		blocks = append(blocks, assembleEntry(h.Entry))
	}
	return blocks, unused
}

// estTokens token 粗估：2 个 UTF-8 字符 ≈ 1 token（中文 1 字≈1 token 偏保守，英文 4 字符≈1 token 偏宽松）。
func estTokens(s string) int {
	n := utf8.RuneCountInString(s)
	if n == 0 {
		return 0
	}
	return (n + 1) / 2
}

// EnableEntry 启用条目（按名或 ID）。
func (b *Book) EnableEntry(id int, name string) error {
	return setEntryEnabled(b, id, name, true)
}

// DisableEntry 停用条目（按名或 ID）。
func (b *Book) DisableEntry(id int, name string) error {
	return setEntryEnabled(b, id, name, false)
}

func setEntryEnabled(b *Book, id int, name string, enabled bool) error {
	if b == nil {
		return fmt.Errorf("角色书为空")
	}
	for i := range b.Entries {
		e := &b.Entries[i]
		if (id > 0 && e.ID == id) || (name != "" && e.Name == name) {
			e.Enabled = enabled
			return nil
		}
	}
	return fmt.Errorf("未找到条目 id=%d name=%s", id, name)
}
