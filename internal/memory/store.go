package memory

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// 错误定义
var (
	ErrNotFound = errors.New("条目不存在")
	ErrExists   = errors.New("条目已存在")
)

// 区域常量（双区制）
const (
	ZoneCache = "cache" // 缓存区：临时记录，随时可清
	ZoneStore = "store" // 存储区：正式文件，编译沉淀
)

// Entry 知识条目
type Entry struct {
	ID          int64    `json:"id"`
	Zone        string   `json:"zone"`
	Type        string   `json:"type"`
	Title       string   `json:"title"`
	Content     string   `json:"content"`
	Tags        []string `json:"tags,omitempty"`
	Source      string   `json:"source,omitempty"`
	Probability float64  `json:"probability,omitempty"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
}

// CreateEntry 写入条目（默认入缓存区；返回新 ID）
func (s *Store) CreateEntry(zone, etype, title, content string, tags []string, source string) (int64, error) {
	if zone == "" {
		zone = ZoneCache
	}
	if etype == "" {
		etype = "text"
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("开始事务: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// 主表
	res, err := tx.Exec(`INSERT INTO entries(zone,type,title,content,tags,source,probability,created_at,updated_at)
		VALUES(?,?,?,?,?,?,1.0,?,?)`,
		zone, etype, title, content, strings.Join(tags, ","), source, now, now)
	if err != nil {
		return 0, fmt.Errorf("写入条目: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("获取条目 ID: %w", err)
	}
	// FTS 全文索引同步
	if _, err := tx.Exec(`INSERT INTO entries_fts(rowid, content, title, tags) VALUES(?,?,?,?)`,
		id, content, title, strings.Join(tags, " ")); err != nil {
		return 0, fmt.Errorf("写入全文索引: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("提交: %w", err)
	}
	return id, nil
}

// GetEntry 读取条目
func (s *Store) GetEntry(id int64) (*Entry, error) {
	var e Entry
	var tags string
	err := s.db.QueryRow(`SELECT id,zone,type,title,content,tags,source,probability,created_at,updated_at
		FROM entries WHERE id=?`, id).
		Scan(&e.ID, &e.Zone, &e.Type, &e.Title, &e.Content, &tags, &e.Source, &e.Probability, &e.CreatedAt, &e.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("读取条目 %d: %w", id, err)
	}
	if tags != "" {
		e.Tags = strings.Split(tags, ",")
	}
	return &e, nil
}

// ListEntries 列出条目（按区域/类型/limit/offset）
func (s *Store) ListEntries(zone, etype string, limit, offset int) ([]Entry, int, error) {
	if limit <= 0 || limit > 1000 {
		limit = 50
	}
	var where []string
	var args []any
	if zone != "" {
		where = append(where, "zone=?")
		args = append(args, zone)
	}
	if etype != "" {
		where = append(where, "type=?")
		args = append(args, etype)
	}
	w := ""
	if len(where) > 0 {
		w = "WHERE " + strings.Join(where, " AND ")
	}

	var total int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM entries "+w, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("统计条目: %w", err)
	}

	rows, err := s.db.Query(`SELECT id,zone,type,title,content,tags,source,probability,created_at,updated_at
		FROM entries `+w+` ORDER BY id DESC LIMIT ? OFFSET ?`,
		append(args, limit, offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("列出条目: %w", err)
	}
	defer func() { _ = rows.Close() }()

	entries := []Entry{}
	for rows.Next() {
		var e Entry
		var tags string
		if err := rows.Scan(&e.ID, &e.Zone, &e.Type, &e.Title, &e.Content, &tags, &e.Source, &e.Probability, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("扫描条目: %w", err)
		}
		if tags != "" {
			e.Tags = strings.Split(tags, ",")
		}
		entries = append(entries, e)
	}
	return entries, total, rows.Err()
}

// DeleteEntry 删除条目（同步删 FTS）
func (s *Store) DeleteEntry(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("开始事务: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.Exec("DELETE FROM entries WHERE id=?", id)
	if err != nil {
		return fmt.Errorf("删除条目: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec("DELETE FROM entries_fts WHERE rowid=?", id); err != nil {
		return fmt.Errorf("删除全文索引: %w", err)
	}
	if _, err := tx.Exec("DELETE FROM entry_vectors WHERE entry_id=?", id); err != nil {
		return fmt.Errorf("删除向量: %w", err)
	}
	return tx.Commit()
}

// UpdateEntry 更新条目（重新索引 FTS）
func (s *Store) UpdateEntry(id int64, title, content string, tags []string) error {
	now := time.Now().Format("2006-01-02 15:04:05")
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("开始事务: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.Exec(`UPDATE entries SET title=?, content=?, tags=?, updated_at=? WHERE id=?`,
		title, content, strings.Join(tags, ","), now, id)
	if err != nil {
		return fmt.Errorf("更新条目: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	// 重建 FTS 行
	if _, err := tx.Exec("DELETE FROM entries_fts WHERE rowid=?", id); err != nil {
		return fmt.Errorf("删除旧索引: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO entries_fts(rowid, content, title, tags) VALUES(?,?,?,?)`,
		id, content, title, strings.Join(tags, " ")); err != nil {
		return fmt.Errorf("重建索引: %w", err)
	}
	return tx.Commit()
}

// ClearZone 清空指定区域（缓存区可随时清）
func (s *Store) ClearZone(zone string) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("开始事务: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.Exec("DELETE FROM entries WHERE zone=?", zone)
	if err != nil {
		return 0, fmt.Errorf("清空区域 %s: %w", zone, err)
	}
	n, _ := res.RowsAffected()
	if _, err := tx.Exec("DELETE FROM entries_fts WHERE rowid IN (SELECT id FROM entries WHERE zone=?)", zone); err != nil {
		return 0, fmt.Errorf("清空区域索引: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return n, nil
}
