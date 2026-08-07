package device

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Device 设备代理（后端眼耳口手，Phase 3）
type Device struct {
	ID           string    `json:"id"`           // 设备唯一 ID
	Name         string    `json:"name"`         // 设备名称（配对时声明）
	Type         string    `json:"type"`         // 设备类型：安卓 / PC / 其他
	Capabilities []string  `json:"capabilities"` // 能力声明（无障碍/浏览器/语音...）
	Status       string    `json:"status"`       // 在线 / 离线
	RegisteredAt string    `json:"registered_at"`
	lastSeen     time.Time // 最近心跳时间（不序列化）
}

// Hub 设备注册中心：配对码认证 + 注册表 + 心跳 + 审计 + 持久化
type Hub struct {
	mu       sync.Mutex
	dir      string
	devices  map[string]*Device
	codes    map[string]string // 配对码 → 设备名（一次性）
	audit    []string
	offlineTTL time.Duration
}

// ErrNotFound 设备不存在
var ErrNotFound = errors.New("设备不存在")

// ErrBadCode 配对码错误
var ErrBadCode = errors.New("配对码无效或已使用")

// DefaultOfflineTTL 心跳超时阈值（默认 90 秒）
const DefaultOfflineTTL = 90 * time.Second

// NewHub 创建设备 Hub（dir 为持久化目录）
func NewHub(dir string) *Hub {
	h := &Hub{
		dir:        dir,
		devices:    map[string]*Device{},
		codes:      map[string]string{},
		offlineTTL: DefaultOfflineTTL,
	}
	_ = h.load()
	return h
}

// pairingPath 配对码持久化路径
func (h *Hub) pairingPath() string { return filepath.Join(h.dir, "pairing.json") }

// NewPairingCode 生成一次性配对码
func (h *Hub) NewPairingCode(name string) (string, error) {
	code, err := randomCode(8)
	if err != nil {
		return "", err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.codes[code] = name
	return code, h.saveLocked()
}

// Register 配对注册设备
func (h *Hub) Register(code, id, devType string, caps []string) (*Device, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	name, ok := h.codes[code]
	if !ok {
		return nil, ErrBadCode
	}
	if id == "" {
		id = randomID()
	}
	if _, exists := h.devices[id]; exists {
		return nil, fmt.Errorf("设备 %s 已注册", id)
	}
	dev := &Device{
		ID:           id,
		Name:         name,
		Type:         devType,
		Capabilities: caps,
		Status:       "在线",
		RegisteredAt: time.Now().Format("2006-01-02 15:04:05"),
		lastSeen:     time.Now(),
	}
	h.devices[id] = dev
	delete(h.codes, code) // 一次性
	h.auditLocked("设备注册", id)
	_ = h.saveLocked()
	_ = h.saveDevicesLocked()
	return dev, nil
}

// Get 获取设备
func (h *Hub) Get(id string) (*Device, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	d, ok := h.devices[id]
	if !ok {
		return nil, ErrNotFound
	}
	return clone(d), nil
}

// List 列出全部设备
func (h *Hub) List() ([]Device, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]Device, 0, len(h.devices))
	for _, d := range h.devices {
		out = append(out, *clone(d))
	}
	return out, nil
}

// Heartbeat 设备心跳更新
func (h *Hub) Heartbeat(id string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	d, ok := h.devices[id]
	if !ok {
		return ErrNotFound
	}
	d.lastSeen = time.Now()
	d.Status = "在线"
	h.auditLocked("心跳", id)
	return nil
}

// MarkOffline 手动标记离线（强制离线，即使 lastSeen 新鲜）
func (h *Hub) MarkOffline(id string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	d, ok := h.devices[id]
	if !ok {
		return ErrNotFound
	}
	d.Status = "离线"
	d.lastSeen = time.Time{} // 置零使 CheckOnline 判离线
	h.auditLocked("离线标记", id)
	return nil
}

// CheckOnline 检查设备是否在线（心跳超时自动判离线）
func (h *Hub) CheckOnline(id string) (bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	d, ok := h.devices[id]
	if !ok {
		return false, ErrNotFound
	}
	if time.Since(d.lastSeen) > h.offlineTTL {
		d.Status = "离线"
		return false, nil
	}
	d.Status = "在线"
	return true, nil
}

// Unregister 注销设备
func (h *Hub) Unregister(id string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.devices[id]; !ok {
		return ErrNotFound
	}
	delete(h.devices, id)
	h.auditLocked("注销", id)
	return h.saveDevicesLocked()
}

// AuditLog 审计日志（动作留痕）
func (h *Hub) AuditLog(limit int) ([]string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if limit <= 0 || limit > len(h.audit) {
		limit = len(h.audit)
	}
	start := len(h.audit) - limit
	if start < 0 {
		start = 0
	}
	out := make([]string, 0, limit)
	out = append(out, h.audit[start:]...)
	return out, nil
}

// ============ 内部 ============

func (h *Hub) auditLocked(action, id string) {
	h.audit = append(h.audit, fmt.Sprintf("[%s] %s 设备 %s", time.Now().Format("2006-01-02 15:04:05"), action, id))
	if len(h.audit) > 200 {
		h.audit = h.audit[len(h.audit)-200:]
	}
}

func clone(d *Device) *Device {
	c := *d
	c.Capabilities = append([]string{}, d.Capabilities...)
	return &c
}

func randomCode(n int) (string, error) {
	const chars = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = chars[int(b[i])%len(chars)]
	}
	return string(b), nil
}

func randomID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("dev-%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x", b)
}

// saveLocked 持久化配对码
func (h *Hub) saveLocked() error {
	if err := os.MkdirAll(h.dir, 0o755); err != nil {
		return err
	}
	data, _ := json.Marshal(h.codes)
	return os.WriteFile(h.pairingPath(), data, 0o600)
}

// saveDevicesLocked 持久化设备注册表
func (h *Hub) saveDevicesLocked() error {
	if err := os.MkdirAll(h.dir, 0o755); err != nil {
		return err
	}
	data, _ := json.Marshal(h.devices)
	return os.WriteFile(filepath.Join(h.dir, "devices.json"), data, 0o600)
}

// load 加载持久化数据
func (h *Hub) load() error {
	if data, err := os.ReadFile(h.pairingPath()); err == nil {
		var codes map[string]string
		if json.Unmarshal(data, &codes) == nil {
			h.codes = codes
		}
	}
	if data, err := os.ReadFile(filepath.Join(h.dir, "devices.json")); err == nil {
		var devs map[string]*Device
		if json.Unmarshal(data, &devs) == nil {
			for id, d := range devs {
				if d != nil {
					d.lastSeen = time.Now().Add(-h.offlineTTL) // 重启后视为可能离线
					h.devices[id] = d
				}
			}
		}
	}
	return nil
}
