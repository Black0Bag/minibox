package storage

import (
	"context"
	"fmt"
)

// RebuildFTS 使用当前 tokenizer 重建存储区全文索引。
// 原文保留在 kb_store.content，预分词结果写入 tokenized_content，
// UPDATE 触发器负责把预分词文本同步到 kb_fts。
func (s *SQLiteStore) RebuildFTS(ctx context.Context) error {
	if s.tokenizer == nil {
		return fmt.Errorf("知识库分词器未就绪")
	}

	rows, err := s.db.QueryContext(ctx, "SELECT id, content FROM kb_store ORDER BY id")
	if err != nil {
		return fmt.Errorf("读取全文索引回填源数据失败: %w", err)
	}
	type sourceRow struct {
		id      int64
		content string
	}
	var sources []sourceRow
	for rows.Next() {
		var row sourceRow
		if err := rows.Scan(&row.id, &row.content); err != nil {
			_ = rows.Close()
			return fmt.Errorf("读取全文索引回填数据失败: %w", err)
		}
		sources = append(sources, row)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("遍历全文索引回填数据失败: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("关闭全文索引回填读取失败: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始全文索引回填事务失败: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, "DELETE FROM kb_fts"); err != nil {
		return fmt.Errorf("清空全文索引失败: %w", err)
	}
	for _, row := range sources {
		tokenized, err := s.tokenizer.TokenizeIndex(row.content)
		if err != nil {
			return fmt.Errorf("回填条目 %d 分词失败: %w", row.id, err)
		}
		if _, err := tx.ExecContext(ctx,
			"UPDATE kb_store SET tokenized_content = ?, updated_at = updated_at WHERE id = ?",
			tokenized, row.id); err != nil {
			return fmt.Errorf("回填条目 %d 全文索引失败: %w", row.id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交全文索引回填失败: %w", err)
	}
	return nil
}
