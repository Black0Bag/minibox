// Package setup 首次启动向导 + 安全默认（模块39，进度跟踪 20260812 子块G）。
// 设计：
//   - 向导状态机：Pending（未完成）→ Completed（完成，自动关闭入口）
//   - 设备凭据：crypto/rand 生成 + 常时比较校验，持久化 0600 文件
//   - 敏感路径拦截：agent 只读工具的黑名单（/etc、/root、.ssh、私钥等）
//   - 安全默认已在 config 层（Listen=127.0.0.1），本包负责行为侧收口
package setup

// Status 向导状态。
type Status int

const (
	// StatusPending 未完成首次向导（服务端处于锁定状态）。
	StatusPending Status = iota
	// StatusCompleted 向导已完成（正常服务）。
	StatusCompleted
)

// String 状态可读名。
func (s Status) String() string {
	switch s {
	case StatusPending:
		return "pending"
	case StatusCompleted:
		return "completed"
	default:
		return "unknown"
	}
}

// Store 向导状态存储（组合根注入实现）。
type Store interface {
	// Load 读取向导状态，文件不存在返回 StatusPending，nil 错误。
	Load() (Status, error)
	// Save 持久化向导状态。
	Save(Status) error
}

// Wizard 首次启动向导。
type Wizard struct {
	store Store
	done  bool // 本进程内是否已确认完成（内存缓存）
}

// New 创建向导。
func New(store Store) *Wizard {
	return &Wizard{store: store}
}

// Status 返回当前向导状态（内存缓存优先，回落存储）。
func (w *Wizard) Status() (Status, error) {
	if w.done {
		return StatusCompleted, nil
	}
	st, err := w.store.Load()
	if err != nil {
		return st, err
	}
	if st == StatusCompleted {
		w.done = true
	}
	return st, nil
}

// NeedWizard 是否需要先完成向导（服务端锁定判断）。
func (w *Wizard) NeedWizard() (bool, error) {
	st, err := w.Status()
	if err != nil {
		return false, err
	}
	return st != StatusCompleted, nil
}

// Complete 完成向导（幂等），返回 true 表示本次确实完成了（供关闭入口联动）。
func (w *Wizard) Complete() (bool, error) {
	st, err := w.Status()
	if err != nil {
		return false, err
	}
	if st == StatusCompleted {
		return false, nil // 早已完成
	}
	if err := w.store.Save(StatusCompleted); err != nil {
		return false, err
	}
	w.done = true
	return true, nil
}
