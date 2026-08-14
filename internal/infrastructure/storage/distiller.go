package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Black0Bag/minibox/internal/domain/memory"
)

// SQLiteDistiller 概率蒸馏实现（B13）。
// 结果写入 kb_preferences 表。
type SQLiteDistiller struct {
	db *sql.DB
}

// NewDistiller 创建蒸馏器。
func NewDistiller(db *sql.DB) *SQLiteDistiller {
	return &SQLiteDistiller{db: db}
}

// Distill 执行蒸馏（当前骨架：扫描 kb_store 高频内容提取偏好）。
// Phase 3 后续：接入 LLM 提炼（5 机制：关键词+概率+来源强度+证据数+最近命中+重要性）。
func (d *SQLiteDistiller) Distill(ctx context.Context, opts memory.DistillOptions) (*memory.DistillResult, error) {
	if opts.MinEvidence == 0 {
		opts.MinEvidence = 2
	}

	// 骨架：统计 access_count 高且 importance 高的条目，作为候选偏好
	query := `
		SELECT content, importance, access_count
		FROM kb_store
		WHERE access_count >= ? AND importance >= ?
		ORDER BY access_count DESC`

	rows, err := d.db.QueryContext(ctx, query, opts.MinEvidence, opts.ImportanceThreshold)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// 先收集结果（读循环内不能写同一连接，否则单写者死锁）
	type candidate struct {
		content      string
		importance   float64
		accessCount  int
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.content, &c.importance, &c.accessCount); err != nil {
			return nil, err
		}
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close() // 释放读连接

	// 再写偏好（读循环结束后）
	extracted := 0
	for _, c := range candidates {
		if err := d.upsertPreference(ctx, c.content, c.importance, c.accessCount); err != nil {
			return nil, err
		}
		extracted++
	}

	return &memory.DistillResult{Extracted: extracted}, nil
}

// upsertPreference 写入偏好（概率蒸馏结果）。
func (d *SQLiteDistiller) upsertPreference(ctx context.Context, key string, importance float64, evidenceCount int) error {
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO kb_preferences (key, value, probability, evidence_count)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			probability = excluded.probability,
			evidence_count = kb_preferences.evidence_count + excluded.evidence_count,
			last_hit_at = ?`,
		key, key, importance, evidenceCount, time.Now().Format("2006-01-02 15:04:05"))
	return err
}

// ListPreferences 列出偏好。
func (d *SQLiteDistiller) ListPreferences(ctx context.Context, limit int) ([]memory.Preference, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT key, value, probability, evidence_count, COALESCE(last_hit_at, '')
		FROM kb_preferences
		ORDER BY probability DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

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

var _ = fmt.Sprintf
