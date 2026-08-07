package fsutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Black0Bag/minibox/internal/timestamp"
)

func testLogMe(t *testing.T, dir string) *LogMe {
	t.Helper()
	ts := timestamp.New("")
	seq, err := NewSeqStore("")
	if err != nil {
		t.Fatalf("NewSeqStore 失败: %v", err)
	}
	return NewLogMe(dir, seq, ts)
}

func TestSanitize(t *testing.T) {
	got := Sanitize("key=sk-dJc97HDfRL4zwdb5cipRoOD6pTgdS8")
	if strings.Contains(got, "sk-dJc97") {
		t.Errorf("敏感信息未脱敏: %q", got)
	}
	if !strings.Contains(got, "sk-***") {
		t.Errorf("应替换为 sk-***: %q", got)
	}
}

func TestSeqStoreMemory(t *testing.T) {
	seq, _ := NewSeqStore("")
	for i := uint64(1); i <= 5; i++ {
		got, err := seq.Next()
		if err != nil {
			t.Fatalf("Next 失败: %v", err)
		}
		if got != i {
			t.Errorf("第 %d 次取号应为 %d，得到 %d", i, i, got)
		}
	}
}

func TestSeqStorePersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "seq")
	seq, _ := NewSeqStore(path)
	_, _ = seq.Next() // 1
	_, _ = seq.Next() // 2
	// 重新加载应从 3 开始
	seq2, _ := NewSeqStore(path)
	n, _ := seq2.Next()
	if n != 3 {
		t.Errorf("重启后序号应接续为 3，得到 %d", n)
	}
}

func TestLogMeEnsureCreatesTemplate(t *testing.T) {
	dir := t.TempDir()
	lm := testLogMe(t, dir)
	if err := lm.Ensure(); err != nil {
		t.Fatalf("Ensure 失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, LogMeFile)); err != nil {
		t.Fatalf("logme 文件未创建: %v", err)
	}
	content, err := lm.Read()
	if err != nil {
		t.Fatalf("Read 失败: %v", err)
	}
	if !strings.Contains(content, "### 用途") {
		t.Errorf("语义区模板缺少「用途」节")
	}
	if !strings.Contains(content, "### 操作技巧") {
		t.Errorf("语义区模板缺少「操作技巧」节")
	}
	if !strings.Contains(content, "### 注意事项") {
		t.Errorf("语义区模板缺少「注意事项」节")
	}
	// Ensure 幂等：再次调用不报错
	if err := lm.Ensure(); err != nil {
		t.Errorf("Ensure 幂等失败: %v", err)
	}
}

func TestLogMeAppend(t *testing.T) {
	dir := t.TempDir()
	lm := testLogMe(t, dir)
	if err := lm.Append("agent:minibox", "写入文件 config.yaml"); err != nil {
		t.Fatalf("Append 失败: %v", err)
	}
	if err := lm.Append("agent:subagent-01", "遍历文件夹"); err != nil {
		t.Fatalf("Append 失败: %v", err)
	}
	ops, err := lm.ReadOperations()
	if err != nil {
		t.Fatalf("ReadOperations 失败: %v", err)
	}
	if len(ops) != 2 {
		t.Fatalf("应有 2 条操作记录，得到 %d", len(ops))
	}
	if !strings.Contains(ops[0], "[#000001]") {
		t.Errorf("第一条序号应为 000001: %s", ops[0])
	}
	if !strings.Contains(ops[1], "[#000002]") {
		t.Errorf("第二条序号应为 000002: %s", ops[1])
	}
	if !strings.Contains(ops[0], "[agent:minibox]") {
		t.Errorf("操作者标记缺失: %s", ops[0])
	}
	// 时间戳格式 19 字符
	if !strings.Contains(ops[0], "] [") {
		t.Errorf("操作记录格式异常: %s", ops[0])
	}
}

func TestLogMeSanitizeInRecord(t *testing.T) {
	dir := t.TempDir()
	lm := testLogMe(t, dir)
	if err := lm.Append("agent:minibox", "写入含 key 文件 sk-dJc97HDfRL4zwdb5cipRoOD6pTgdS8Xxt9ISA6eawT3UFWfe"); err != nil {
		t.Fatalf("Append 失败: %v", err)
	}
	content, _ := lm.Read()
	if strings.Contains(content, "sk-dJc97") {
		t.Error("logme 记录中不应出现明文 key")
	}
	if !strings.Contains(content, "sk-***") {
		t.Error("logme 记录应脱敏为 sk-***")
	}
}

func TestLogMeReadSemantic(t *testing.T) {
	dir := t.TempDir()
	lm := testLogMe(t, dir)
	_ = lm.Ensure()
	_ = lm.Append("agent:minibox", "操作1")
	sem, err := lm.ReadSemantic()
	if err != nil {
		t.Fatalf("ReadSemantic 失败: %v", err)
	}
	if strings.Contains(sem, "操作1") {
		t.Error("语义区不应包含操作记录")
	}
	if !strings.Contains(sem, "### 用途") {
		t.Error("语义区应包含用途节")
	}
}

func TestFSWriteNewPreventsOverwrite(t *testing.T) {
	dir := t.TempDir()
	lm := testLogMe(t, dir)
	fs := NewFS(lm, timestamp.New(""))
	path := filepath.Join(dir, "a.txt")
	if err := fs.WriteNew(path, []byte("hello")); err != nil {
		t.Fatalf("首次新建失败: %v", err)
	}
	if err := fs.WriteNew(path, []byte("overwrite")); err == nil {
		t.Error("第二次新建应被防火墙拒绝")
	}
	data, err := fs.ReadString(path)
	if err != nil {
		t.Fatalf("Read 失败: %v", err)
	}
	if data != "hello" {
		t.Errorf("原有内容应未被覆盖: %q", data)
	}
}

func TestFSAppendKeepsContent(t *testing.T) {
	dir := t.TempDir()
	lm := testLogMe(t, dir)
	fs := NewFS(lm, timestamp.New(""))
	path := filepath.Join(dir, "log.txt")
	if err := fs.WriteNew(path, []byte("第一行\n")); err != nil {
		t.Fatalf("WriteNew 失败: %v", err)
	}
	if err := fs.Append(path, []byte("第二行\n")); err != nil {
		t.Fatalf("Append 失败: %v", err)
	}
	content, _ := fs.ReadString(path)
	if content != "第一行\n第二行\n" {
		t.Errorf("追加后内容错误: %q", content)
	}
}

func TestFSListAndEnsureLogMe(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sub", "deep")
	lm := testLogMe(t, dir)
	fs := NewFS(lm, timestamp.New(""))
	if err := fs.EnsureDir(dir); err != nil {
		t.Fatalf("EnsureDir 失败: %v", err)
	}
	if err := fs.WriteNew(filepath.Join(dir, "x.txt"), []byte("x")); err != nil {
		t.Fatalf("WriteNew 失败: %v", err)
	}
	names, err := fs.List(dir)
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	if len(names) != 2 { // logme + x.txt
		t.Errorf("目录应含 logme 和 x.txt，得到 %v", names)
	}
}
