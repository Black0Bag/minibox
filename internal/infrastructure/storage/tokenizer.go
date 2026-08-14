package storage

import (
	"strings"

	"github.com/lengzhao/jiebago"
	jiebaembed "github.com/lengzhao/jiebago/embed"
)

// JiebaTokenizer 基于 jiebago 的中文分词器。
// 纯 Go（MIT 协议），无 CGO，符合单文件红线。
// 写入和查询用同一分词器，token 完全对齐（SinoMem/Anamnesis 实证）。
type JiebaTokenizer struct {
	seg *jiebago.Segmenter
}

// NewJiebaTokenizer 创建 jieba 分词器（用内置 embed 字典，开箱即用）。
func NewJiebaTokenizer() (*JiebaTokenizer, error) {
	seg := &jiebago.Segmenter{}
	if err := seg.LoadDictionaryFromBytes(jiebaembed.DictData); err != nil {
		return nil, err
	}
	return &JiebaTokenizer{seg: seg}, nil
}

// TokenizeIndex 索引用分词：jieba 切词 → 去重 → 空格连接。
func (t *JiebaTokenizer) TokenizeIndex(text string) (string, error) {
	seen := make(map[string]struct{})
	var out []string
	for tok := range t.seg.CutForSearch(text, true) {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		if _, ok := seen[tok]; ok {
			continue
		}
		seen[tok] = struct{}{}
		out = append(out, tok)
	}
	return strings.Join(out, " "), nil
}

// TokenizeQuery 查询用分词：每个词加引号 + AND 连接。
// 防 FTS5 布尔操作符注入（vstash 实证）。
func (t *JiebaTokenizer) TokenizeQuery(text string) (string, error) {
	var out []string
	seen := make(map[string]struct{})
	for tok := range t.seg.CutForSearch(text, true) {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		if _, ok := seen[tok]; ok {
			continue
		}
		seen[tok] = struct{}{}
		// 转义引号 + 加引号
		escaped := strings.ReplaceAll(tok, `"`, `""`)
		out = append(out, `"`+escaped+`"`)
	}
	return strings.Join(out, " "), nil
}
