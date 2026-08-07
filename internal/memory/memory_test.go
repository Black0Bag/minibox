package memory

import (
	"strings"
	"testing"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open 失败: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestCreateAndGet(t *testing.T) {
	s := openTest(t)
	id, err := s.CreateEntry(ZoneCache, "text", "测试标题", "今天和用户讨论了 minibox 的架构设计", []string{"项目", "架构"}, "会话1")
	if err != nil {
		t.Fatalf("CreateEntry 失败: %v", err)
	}
	e, err := s.GetEntry(id)
	if err != nil {
		t.Fatalf("GetEntry 失败: %v", err)
	}
	if e.Zone != ZoneCache {
		t.Errorf("zone 应为 cache，得到 %s", e.Zone)
	}
	if e.Title != "测试标题" {
		t.Errorf("title 错误: %q", e.Title)
	}
	if len(e.Tags) != 2 {
		t.Errorf("tags 应为 2 个，得到 %v", e.Tags)
	}
	if e.CreatedAt == "" {
		t.Error("created_at 不应为空")
	}
}

func TestGetNotFound(t *testing.T) {
	s := openTest(t)
	_, err := s.GetEntry(99999)
	if err != ErrNotFound {
		t.Errorf("应返回 ErrNotFound，得到 %v", err)
	}
}

func TestSearchChinese(t *testing.T) {
	s := openTest(t)
	_, _ = s.CreateEntry(ZoneStore, "text", "", "本周完成了后端 M1 里程碑的 logme 留痕系统开发", nil, "s1")
	_, _ = s.CreateEntry(ZoneStore, "text", "", "前端准备采用 Kotlin 和 Compose 进行界面开发", nil, "s2")

	// 中文 4 字词组（trigram 有效）
	results, err := s.Search("留痕系统", "", 10)
	if err != nil {
		t.Fatalf("Search 失败: %v", err)
	}
	found := false
	for _, r := range results {
		if strings.Contains(r.Entry.Content, "logme") {
			found = true
		}
	}
	if !found {
		t.Errorf("应搜到包含 logme 的条目，结果: %d 条", len(results))
	}

	// 中文 2 字短词（LIKE 兜底）
	results, err = s.Search("前端", "", 10)
	if err != nil {
		t.Fatalf("短词 Search 失败: %v", err)
	}
	if len(results) == 0 {
		t.Error("短词「前端」应通过 LIKE 兜底搜到结果")
	}
}

func TestSearchEnglishWord(t *testing.T) {
	s := openTest(t)
	_, _ = s.CreateEntry(ZoneStore, "text", "", "we should use modernc sqlite driver for zero cgo", nil, "s1")
	results, err := s.Search("sqlite", "", 10)
	if err != nil {
		t.Fatalf("Search 失败: %v", err)
	}
	if len(results) == 0 {
		t.Error("英文词 sqlite 应搜到结果")
	}
}

func TestSearchZoneFilter(t *testing.T) {
	s := openTest(t)
	_, _ = s.CreateEntry(ZoneCache, "text", "", "临时笔记甲", nil, "")
	_, _ = s.CreateEntry(ZoneStore, "text", "", "正式文档乙", nil, "")
	// 只搜 store 区
	results, err := s.Search("正式文档", ZoneStore, 10)
	if err != nil {
		t.Fatalf("Search 失败: %v", err)
	}
	for _, r := range results {
		if r.Entry.Zone != ZoneStore {
			t.Errorf("应只返回 store 区，得到 %s", r.Entry.Zone)
		}
	}
}

func TestListAndCount(t *testing.T) {
	s := openTest(t)
	for i := 0; i < 5; i++ {
		_, _ = s.CreateEntry(ZoneCache, "text", "标题", "内容", nil, "")
	}
	_, _ = s.CreateEntry(ZoneStore, "text", "标题", "内容", nil, "")
	entries, total, err := s.ListEntries(ZoneCache, "", 10, 0)
	if err != nil {
		t.Fatalf("ListEntries 失败: %v", err)
	}
	if total != 5 {
		t.Errorf("cache 区应 5 条，得到 %d", total)
	}
	if len(entries) != 5 {
		t.Errorf("应返回 5 条，得到 %d", len(entries))
	}
	// 分页
	entries, total, _ = s.ListEntries(ZoneCache, "", 2, 0)
	if len(entries) != 2 || total != 5 {
		t.Errorf("分页异常: len=%d total=%d", len(entries), total)
	}
}

func TestDeleteAndUpdate(t *testing.T) {
	s := openTest(t)
	id, _ := s.CreateEntry(ZoneCache, "text", "标题", "内容", []string{"a"}, "")
	if err := s.UpdateEntry(id, "新标题", "新内容", []string{"b"}); err != nil {
		t.Fatalf("UpdateEntry 失败: %v", err)
	}
	e, _ := s.GetEntry(id)
	if e.Title != "新标题" || e.Content != "新内容" || e.Tags[0] != "b" {
		t.Errorf("更新后数据异常: %+v", e)
	}
	// 更新后仍可搜索到新内容
	res, _ := s.Search("新内容", "", 10)
	if len(res) == 0 {
		t.Error("更新后应能搜到新内容")
	}
	if err := s.DeleteEntry(id); err != nil {
		t.Fatalf("DeleteEntry 失败: %v", err)
	}
	if _, err := s.GetEntry(id); err != ErrNotFound {
		t.Errorf("删除后应 ErrNotFound，得到 %v", err)
	}
	// 删除后搜不到
	res, _ = s.Search("新内容", "", 10)
	for _, r := range res {
		if r.Entry.ID == id {
			t.Error("删除后不应搜到该条目")
		}
	}
}

func TestClearZone(t *testing.T) {
	s := openTest(t)
	_, _ = s.CreateEntry(ZoneCache, "text", "", "缓存内容", nil, "")
	n, err := s.ClearZone(ZoneCache)
	if err != nil {
		t.Fatalf("ClearZone 失败: %v", err)
	}
	if n != 1 {
		t.Errorf("应清空 1 条，得到 %d", n)
	}
	total := 0
	entries, total, _ := s.ListEntries(ZoneCache, "", 10, 0)
	_ = entries
	if total != 0 {
		t.Errorf("清空后 cache 区应 0 条，得到 %d", total)
	}
}

// fakeVec 生成 2048 维测试向量（前几位有语义，其余为 0）
func fakeVec(prefix []float32) []float32 {
	v := make([]float32, DefaultVecDim)
	copy(v, prefix)
	return v
}

func TestVectorSearch(t *testing.T) {
	s := openTest(t)
	id1, _ := s.CreateEntry(ZoneStore, "text", "", "喜欢编程和写代码", nil, "")
	id2, _ := s.CreateEntry(ZoneStore, "text", "", "热爱运动和健身", nil, "")
	if err := s.SetVector(id1, fakeVec([]float32{1, 0, 0, 0})); err != nil {
		t.Fatalf("SetVector id1 失败: %v", err)
	}
	if err := s.SetVector(id2, fakeVec([]float32{0, 1, 0, 0})); err != nil {
		t.Fatalf("SetVector id2 失败: %v", err)
	}
	hits, err := s.VectorSearch(fakeVec([]float32{0.9, 0.1, 0, 0}), 10)
	if err != nil {
		t.Fatalf("VectorSearch 失败: %v（vec0 可能不可用）", err)
	}
	if len(hits) == 0 {
		t.Fatal("向量检索无结果")
	}
	if hits[0].EntryID != id1 {
		t.Errorf("最相似应为 id1（编程），得到 id=%d", hits[0].EntryID)
	}
}

func TestHybridFuse(t *testing.T) {
	fts := map[int64]int{1: 1, 2: 2}
	vec := map[int64]int{2: 1, 3: 2}
	ids := hybridFuse(fts, vec)
	// 2 同时被两者命中，应排第一
	if len(ids) == 0 || ids[0] != 2 {
		t.Errorf("RRF 融合后 id2 应排第一，得到 %v", ids)
	}
}
