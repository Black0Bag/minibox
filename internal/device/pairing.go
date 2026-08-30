package device

import (
	"crypto/rand"
	"fmt"
	"sync"
	"time"
)

// PairingManager 配对管理器（D-03：6 位配对码）。
type PairingManager struct {
	mu       sync.RWMutex
	codes    map[string]pairingEntry // deviceID → code
	pendings map[string]string       // code → deviceID
}

type pairingEntry struct {
	Code      string
	DeviceID  string
	ExpiresAt time.Time
}

// NewPairingManager 创建配对管理器。
func NewPairingManager() *PairingManager {
	return &PairingManager{
		codes:    make(map[string]pairingEntry),
		pendings: make(map[string]string),
	}
}

// GenerateCode 生成 6 位配对码（有效期 5 分钟）。
func (pm *PairingManager) GenerateCode(deviceID string) (string, error) {
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("生成配对码失败: %w", err)
	}
	code := fmt.Sprintf("%06d", (int(b[0])<<16|int(b[1])<<8|int(b[2]))%1000000)

	pm.mu.Lock()
	entry := pairingEntry{
		Code:      code,
		DeviceID:  deviceID,
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}
	pm.codes[deviceID] = entry
	pm.pendings[code] = deviceID
	pm.mu.Unlock()

	return code, nil
}

// VerifyCode 校验配对码。
func (pm *PairingManager) VerifyCode(code string) (string, bool) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	deviceID, ok := pm.pendings[code]
	if !ok {
		return "", false
	}

	entry, ok := pm.codes[deviceID]
	if !ok || time.Now().After(entry.ExpiresAt) {
		delete(pm.pendings, code)
		return "", false
	}

	// 配对成功后清理
	delete(pm.pendings, code)
	delete(pm.codes, deviceID)
	return deviceID, true
}

// RemoveCode 移除配对码。
func (pm *PairingManager) RemoveCode(deviceID string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if entry, ok := pm.codes[deviceID]; ok {
		delete(pm.pendings, entry.Code)
		delete(pm.codes, deviceID)
	}
}
