package device

import "sync"

// Registry 设备注册表（D-04：多设备管理）。
type Registry struct {
	mu      sync.RWMutex
	aliases map[string]string // alias → deviceID
}

// NewRegistry 创建设备注册表。
func NewRegistry() *Registry {
	return &Registry{
		aliases: make(map[string]string),
	}
}

// SetAlias 设置设备别名。
func (r *Registry) SetAlias(deviceID, alias string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.aliases[alias] = deviceID
}

// Resolve 解析别名到设备 ID。
func (r *Registry) Resolve(name string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if id, ok := r.aliases[name]; ok {
		return id
	}
	return name
}

// RemoveAlias 移除别名。
func (r *Registry) RemoveAlias(alias string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.aliases, alias)
}
