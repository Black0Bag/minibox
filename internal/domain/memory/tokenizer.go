package memory

// Tokenizer 中文分词接口。
// 设计：写入和查询用同一分词器，token 完全对齐（SinoMem 实证）。
// 预分词模式：写入触发器先用 jieba 切词 → 空格连接 → 存入 FTS5。
type Tokenizer interface {
	// TokenizeIndex 索引用分词（写入 kb_store 前调用）。
	TokenizeIndex(text string) (string, error)

	// TokenizeQuery 查询用分词（生成 FTS5 MATCH 语句）。
	// 每个词加引号 + AND 连接（防 FTS5 布尔注入，vstash 实证）。
	TokenizeQuery(text string) (string, error)
}
