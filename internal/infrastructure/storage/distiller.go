package storage

import (
	"context"
	"database/sql"
	"time"

	"github.com/Black0Bag/minibox/internal/domain/memory"
)

// PrefExtractor LLM 偏好提取器（B13 LLM 机制，组合根注入）。
// 从候选内容中提炼结构化偏好（key=偏好主题，value=偏好内容）。
type PrefExtractor interface {
	// Extract 提取偏好（返回 key/value 对）。
	Extract(ctx context.Context, content string) ([]memory.Preference, error)
}

// SQLiteDistiller 概率蒸馏实现（B13）。
// 结果写入 kb_preferences 表。
// 机制（进度跟踪）：关键词/概率 + 来源强度 + 证据数 + 最近命中 + 重要性。
// LLM 提炼可选接入（SetPrefExtractor），否则用统计近似。
type SQLiteDistiller struct {
	db        *sql.DB
	extractor PrefExtractor // 可选 LLM 提炼
}

// NewDistiller 创建蒸馏器。
func NewDistiller(db *sql.DB) *SQLiteDistiller {
	return &SQLiteDistiller{db: db}
}

// SetPrefExtractor 注入 LLM 偏好提取器（B13 LLM 机制）。
func (d *SQLiteDistiller) SetPrefExtractor(e PrefExtractor) {
	d.extractor = e
}

// Distill 执行蒸馏：统计数据 → 过滤候选 → LLM 提炼（可选）或统计近似 → 写偏好。
func (d *SQLiteDistiller) Distill(ctx context.Context, opts memory.DistillOptions) (*memory.DistillResult, error) {
	if opts.MinEvidence == 0 {
		opts.MinEvidence = 2
	}

	// 统计候选：access_count 高且 importance 高的条目
	rows, err := d.db.QueryContext(ctx, `
		SELECT content, importance, access_count
		FROM kb_store
		WHERE access_count >= ? AND importance >= ?
		ORDER BY access_count DESC`,
		opts.MinEvidence, opts.ImportanceThreshold)
	if err != nil {
		return nil, err
	}

	// 先收集结果（读循环内不能写同一连接，否则单写者死锁）
	type candidate struct {
		content     string
		importance  float64
		accessCount int
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.content, &c.importance, &c.accessCount); err != nil {
			_ = rows.Close()
			return nil, err
		}
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close() // 释放读连接

	// 再写偏好（读循环结束后）
	extracted := 0
	for _, c := range candidates {
		if d.extractor != nil {
			// LLM 提炼：从内容中提取结构化偏好
			prefs, err := d.extractor.Extract(ctx, c.content)
			if err != nil || len(prefs) == 0 {
				// 提炼失败/无偏好 → 回退统计近似（失败不阻断蒸馏）
				if err := d.upsertPreference(ctx, c.content, c.content, c.importance, c.accessCount, false); err != nil {
					return nil, err
				}
				extracted++
				continue
			}
			for _, p := range prefs {
				if err := d.upsertPreference(ctx, p.Key, p.Value, p.Probability, 1, true); err != nil {
					return nil, err
				}
				extracted++
			}
			continue
		}
		// 统计近似：内容本身即偏好（无 LLM 时）
		if err := d.upsertPreference(ctx, c.content, c.content, c.importance, c.accessCount, false); err != nil {
			return nil, err
		}
		extracted++
	}

	return &memory.DistillResult{Extracted: extracted}, nil
}

// upsertPreference 写入偏好（概率蒸馏结果）。
// llmMode=true 时 key=偏好主题、value=偏好内容、probability=模型概率；
// false 时 key=value=内容、probability=importance（统计近似）。
func (d *SQLiteDistiller) upsertPreference(ctx context.Context, key, value string, probability float64, evidenceCount int, llmMode bool) error {
	if llmMode {
		_, err := d.db.ExecContext(ctx, `
			INSERT INTO kb_preferences (key, value, probability, evidence_count, last_hit_at)
			VALUES (?, ?, ?, 1, ?)
			ON CONFLICT(key) DO UPDATE SET
				value = excluded.value,
				probability = MAX(kb_preferences.probability, excluded.probability),
				evidence_count = kb_preferences.evidence_count + 1,
				last_hit_at = ?`,
			key, value, probability, time.Now().Format("2006-01-02 15:04:05"), time.Now().Format("2006-01-02 15:04:05"))
		return err
	}
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO kb_preferences (key, value, probability, evidence_count, last_hit_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			probability = excluded.probability,
			evidence_count = kb_preferences.evidence_count + excluded.evidence_count,
			last_hit_at = ?`,
		key, key, probability, evidenceCount, time.Now().Format("2006-01-02 15:04:05"), time.Now().Format("2006-01-02 15:04:05"))
	return err
}

// ListPreferences 列出偏好。

// ListPreferences 列出偏好。
func (d *SQLiteDistiller) ListPreferences(ctx context.Context, limit int) ([]memory.Preference, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := d.db.QueryContext(ctx, `
		SELECT key, value, probability, evidence_count, COALESCE(last_hit_at, '')
		FROM kb_preferences
		ORDER BY probability DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var prefs []memory.Preference
	for rows.Next() {
		var p memory.Preference
		if err := rows.Scan(&p.Key, &p.Value, &p.Probability, &p.EvidenceCnt, &p.LastHitAt); err != nil {
			return nil, err
		}
		prefs = append(prefs, p)
	}
	return prefs, rows.Err()
}
