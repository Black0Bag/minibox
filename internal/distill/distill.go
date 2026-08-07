package distill

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/Black0Bag/minibox/internal/memory"
)

// Pref 用户蒸馏偏好条目（T-07）
type Pref struct {
	ID          int64   `json:"id"`
	Keyword     string  `json:"keyword"`
	Probability float64 `json:"probability"`
	Evidence    int     `json:"evidence"`
	Importance  string  `json:"importance"` // permanent/high/medium/low
	Source      string  `json:"source"`
	LastHit     string  `json:"last_hit"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

// Distiller 概率蒸馏管理器（T-07 五机制：命中上调/反例下调/重要性/老化/证据计数）
type Distiller struct {
	db *sql.DB
}

// New 创建蒸馏器
func NewDistiller(store *memory.Store) *Distiller {
	return &Distiller{db: store.DB()}
}

// Hit 正向证据：命中关键词，概率上调（证据越多步长越小，防泡沫）
func (d *Distiller) Hit(keyword, importance, source string) error {
	now := time.Now().Format("2006-01-02 15:04:05")
	var prob float64
	var ev int
	err := d.db.QueryRow(`SELECT probability, evidence FROM user_prefs WHERE keyword=?`, keyword).
		Scan(&prob, &ev)
	if err == sql.ErrNoRows {
		// 新建，初始 0.5
		if importance == "" {
			importance = "medium"
		}
		_, err := d.db.Exec(`INSERT INTO user_prefs(keyword,probability,evidence,importance,source,last_hit,created_at,updated_at)
			VALUES(?,?,?,?,?,?,?,?)`, keyword, 0.5, 1, importance, source, now, now, now)
		return err
	}
	if err != nil {
		return fmt.Errorf("查询偏好: %w", err)
	}
	ev++
	step := 0.1 / float64(ev) // 证据越多，单次上调越小
	prob += step
	if prob > 0.99 {
		prob = 0.99
	}
	_, err = d.db.Exec(`UPDATE user_prefs SET probability=?, evidence=?, importance=?, source=?, last_hit=?, updated_at=?
		WHERE keyword=?`, prob, ev, importance, source, now, now, keyword)
	return err
}

// Negative 反向证据：用户否定，概率下调（强负信号）
func (d *Distiller) Negative(keyword string) error {
	now := time.Now().Format("2006-01-02 15:04:05")
	res, err := d.db.Exec(`UPDATE user_prefs SET probability = MAX(0.01, probability - 0.15), updated_at=? WHERE keyword=?`,
		now, keyword)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("偏好 %q 不存在", keyword)
	}
	return nil
}

// List 列出偏好（按概率降序）
func (d *Distiller) List(limit int) ([]Pref, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := d.db.Query(`SELECT id,keyword,probability,evidence,importance,source,last_hit,created_at,updated_at
		FROM user_prefs ORDER BY probability DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("列出偏好: %w", err)
	}
	defer func() { _ = rows.Close() }()
	prefs := []Pref{}
	for rows.Next() {
		var p Pref
		if err := rows.Scan(&p.ID, &p.Keyword, &p.Probability, &p.Evidence, &p.Importance, &p.Source, &p.LastHit, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		prefs = append(prefs, p)
	}
	return prefs, rows.Err()
}

// Delete 删除偏好
func (d *Distiller) Delete(keyword string) (bool, error) {
	res, err := d.db.Exec(`DELETE FROM user_prefs WHERE keyword=?`, keyword)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// Decay 老化：长期未命中的偏好概率自然下调（T-07 TTL，permanent 不衰减）
// 按天计：未命中 30 天以上的 medium 衰减 0.05，low 衰减 0.1
func (d *Distiller) Decay() (int, error) {
	cutoff30 := time.Now().AddDate(0, 0, -30).Format("2006-01-02 15:04:05")
	res, err := d.db.Exec(`UPDATE user_prefs SET probability = MAX(0.05, probability - 0.05), updated_at=?
		WHERE importance != 'permanent' AND last_hit < ?`, time.Now().Format("2006-01-02 15:04:05"), cutoff30)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
