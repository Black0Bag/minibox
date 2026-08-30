package backup

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite" // 注册 "sqlite" 驱动（与生产同一实现）
)

// writeMinimalSQLite 建一个最小可用的 SQLite 库，供 VACUUM INTO 测试使用。
func writeMinimalSQLite(t *testing.T, dbPath string) error {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)`); err != nil {
		return err
	}
	_, err = db.Exec(`INSERT INTO t (v) VALUES ('x')`)
	return err
}

// TestValidateSnapName 快照名校验：拒绝路径穿越，只放行裸 .db 文件名。
func TestValidateSnapName(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"合法裸文件名", "minibox_20260830_010203.db", false},
		{"空名", "", true},
		{"父目录穿越", "../minibox.db", true},
		{"多级穿越", "../../etc/passwd.db", true},
		{"绝对路径", "/tmp/evil.db", true},
		{"含子目录", "sub/minibox.db", true},
		{"反斜杠分隔符", `sub\minibox.db`, true},
		{"仅点点", "..", true},
		{"中间含点点", "a..b.db", true},
		{"扩展名不对", "minibox.sqlite", true},
		{"无扩展名", "minibox", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSnapName(tc.input)
			if tc.wantErr && err == nil {
				t.Fatalf("input=%q 应报错，实际通过", tc.input)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("input=%q 应通过，实际 err=%v", tc.input, err)
			}
		})
	}
}

// TestRestore_RejectsTraversal 回归：Restore 必须拒绝穿越路径，
// 且拒绝时不得删除目标数据库文件。
func TestRestore_RejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "minibox.db")
	backupDir := filepath.Join(dir, "backups")

	if err := os.WriteFile(dbPath, []byte("live-db"), 0o600); err != nil {
		t.Fatalf("准备数据库文件失败: %v", err)
	}
	if err := os.MkdirAll(backupDir, 0o750); err != nil {
		t.Fatalf("准备备份目录失败: %v", err)
	}
	// 备份目录之外的"敏感"文件，穿越成功就会被搬走
	outside := filepath.Join(dir, "outside.db")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatalf("准备外部文件失败: %v", err)
	}

	m := NewManager(dbPath, backupDir)

	for _, bad := range []string{"../outside.db", "/etc/hosts", `..\outside.db`, ""} {
		err := m.Restore(bad)
		if err == nil {
			t.Fatalf("snapName=%q 应被拒绝", bad)
		}
		if !errors.Is(err, ErrInvalidSnapshotName) {
			t.Errorf("snapName=%q 错误应可用 errors.Is 判定为 ErrInvalidSnapshotName，实际=%v", bad, err)
		}
	}

	// 原数据库必须完好（拒绝路径不能走到 os.Remove）
	got, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("原数据库应仍存在: %v", err)
	}
	if string(got) != "live-db" {
		t.Errorf("原数据库内容被改动: %q", got)
	}
	// 外部文件必须还在原处
	if _, err := os.Stat(outside); err != nil {
		t.Errorf("外部文件被移动/删除: %v", err)
	}
}

// TestSnapshotListRestore 主流程：快照 → 列出 → 恢复。
func TestSnapshotListRestore(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "minibox.db")
	backupDir := filepath.Join(dir, "backups")

	// 用真实 SQLite 建库（modernc 驱动已随 storage 包注册）
	if err := writeMinimalSQLite(t, dbPath); err != nil {
		t.Skipf("无法准备 SQLite 测试库，跳过: %v", err)
	}

	m := NewManager(dbPath, backupDir)

	snapPath, err := m.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot err=%v", err)
	}
	if !strings.HasPrefix(filepath.Base(snapPath), "minibox_") {
		t.Errorf("快照名前缀异常: %s", filepath.Base(snapPath))
	}
	// 权限应收紧到 0600
	info, err := os.Stat(snapPath)
	if err != nil {
		t.Fatalf("快照文件不存在: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("快照权限=%o, 期望 600", perm)
	}

	list, err := m.List()
	if err != nil {
		t.Fatalf("List err=%v", err)
	}
	if len(list) != 1 {
		t.Fatalf("应有 1 个快照，实际 %d: %v", len(list), list)
	}

	// 恢复（裸文件名，走白名单校验）
	if err := m.Restore(list[0]); err != nil {
		t.Fatalf("Restore err=%v", err)
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Errorf("恢复后数据库应存在: %v", err)
	}
}

// TestList_MissingDirIsEmpty 备份目录不存在时 List 返回空而非报错。
func TestList_MissingDirIsEmpty(t *testing.T) {
	m := NewManager(filepath.Join(t.TempDir(), "x.db"), filepath.Join(t.TempDir(), "nope"))
	list, err := m.List()
	if err != nil {
		t.Fatalf("目录不存在时不应报错: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("应返回空列表，实际 %v", list)
	}
}
