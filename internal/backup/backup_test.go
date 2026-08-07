package backup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeSnapper 模拟快照生成（返回文件名）
type fakeSnapper struct {
	calls int
}

func (f *fakeSnapper) Snapshot(dir string) (string, error) {
	f.calls++
	name := time.Now().Format("snapshot-20060102-150405") + "-" + itoa(f.calls) + ".db"
	return name, nil
}

func itoa(n int) string {
	if n == 1 {
		return "a"
	}
	return "b"
}

func TestManagerConfig(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir, &fakeSnapper{}, 3, time.Minute)
	if m.Keep() != 3 {
		t.Errorf("Keep 应为 3，得到 %d", m.Keep())
	}
	if m.Dir() != dir {
		t.Errorf("Dir 错误: %s", m.Dir())
	}
}

func TestRunCreatesSnapshot(t *testing.T) {
	dir := t.TempDir()
	snap := &fakeSnapper{}
	m := NewManager(dir, snap, 5, time.Minute)
	ctx := context.Background()
	_ = os.MkdirAll(dir, 0o755)
	// 预置一个旧快照（模拟历史）
	_ = os.WriteFile(filepath.Join(dir, "snapshot-20060101-000000.db"), []byte("old"), 0o644)

	name, err := m.Run(ctx)
	if err != nil {
		t.Fatalf("Run 失败: %v", err)
	}
	if name == "" {
		t.Fatal("应返回快照名")
	}
	if snap.calls != 1 {
		t.Errorf("应调用 Snapshot 1 次，得到 %d", snap.calls)
	}
	// 快照应记录在案
	recs, _ := m.Records()
	if len(recs) != 1 {
		t.Errorf("应有 1 条记录，得到 %d", len(recs))
	}
}

func TestRetentionKeepsLatest(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(dir, 0o755)
	// 创建 5 个真实文件（模拟历史快照）
	for i := 0; i < 5; i++ {
		_ = os.WriteFile(filepath.Join(dir, "snapshot-20260101-00000"+string(rune('0'+i))+".db"), []byte("x"), 0o644)
	}
	// 用真实文件 Snapshot 模拟
	snap := &fileSnapper{dir: dir}
	m := NewManager(dir, snap, 3, time.Minute)
	ctx := context.Background()
	if _, err := m.Run(ctx); err != nil {
		t.Fatalf("Run 失败: %v", err)
	}
	// 保留 3 份：新增 1 份后共 6 个 db 文件，应删到 3 个
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读取目录失败: %v", err)
	}
	dbCount := 0
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".db" {
			dbCount++
		}
	}
	if dbCount != 3 {
		t.Errorf("应保留 3 份快照，实际 %d 份", dbCount)
	}
}

// fileSnapper 用真实文件创建快照
type fileSnapper struct{ dir string }

func (f *fileSnapper) Snapshot(dir string) (string, error) {
	name := "snapshot-" + time.Now().Format("20060102-150405") + "-new.db"
	if err := os.WriteFile(filepath.Join(f.dir, name), []byte("new"), 0o644); err != nil {
		return "", err
	}
	return name, nil
}

func TestList(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir, &fakeSnapper{}, 5, time.Minute)
	_ = os.MkdirAll(dir, 0o755)
	names := []string{"snapshot-a.db", "snapshot-b.db"}
	for _, n := range names {
		_ = os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644)
	}
	list, err := m.List()
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("应有 2 个快照，得到 %d", len(list))
	}
}

func TestPersistence(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir, &fakeSnapper{}, 3, time.Minute)
	_, _ = m.Run(context.Background())
	// 重新加载
	m2 := NewManager(dir, &fakeSnapper{}, 3, time.Minute)
	recs, err := m2.Records()
	if err != nil {
		t.Fatalf("加载记录失败: %v", err)
	}
	if len(recs) != 1 {
		t.Errorf("重启后应 1 条记录，得到 %d", len(recs))
	}
}
