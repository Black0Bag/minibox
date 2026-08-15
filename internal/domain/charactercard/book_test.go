package charactercard

import (
	"strings"
	"testing"
)

// testBook 一份带 4 条目的角色书（含 selective/constant/大小写敏感场景）。
func testBook() *Book {
	return &Book{
		Name:        "测试书",
		TokenBudget: 100,
		Entries: []Entry{
			{Keys: []string{"图书馆"}, Content: "图书馆藏书十万册。", Enabled: true, InsertionOrder: 3, Name: "图书馆", Priority: 3},
			{Keys: []string{"禁书"}, SecondaryKeys: []string{"地下"}, Content: "禁书在地下三层。", Enabled: true, InsertionOrder: 1, Selective: true, Name: "禁书", Priority: 2},
			{Keys: []string{"魔法"}, Content: "魔法是灵魂的语言。", Enabled: true, InsertionOrder: 2, Constant: true, Name: "魔法本质", Priority: 1},
			{Keys: []string{"蛇"}, Content: "蛇是冷血动物。", Enabled: true, InsertionOrder: 0, CaseSensitive: true, Name: "蛇", Priority: 5},
			{Keys: []string{"旧条目"}, Content: "旧内容。", Enabled: false, InsertionOrder: 0, Name: "停用条目"},
		},
	}
}

func TestMatch_Basic(t *testing.T) {
	b := testBook()
	got := b.Match("我在图书馆里读魔法书")
	if len(got) != 2 {
		t.Fatalf("命中数 = %d, 期望 2: %+v", len(got), got)
	}
	names := map[string]bool{}
	for _, m := range got {
		names[m.Entry.Name] = true
	}
	if !names["图书馆"] || !names["魔法本质"] {
		t.Errorf("命中集合错误: %v", names)
	}
}

func TestMatch_Selective(t *testing.T) {
	b := testBook()
	// 只有主键 → 不触发
	if got := b.Match("禁书很危险"); len(got) != 0 {
		t.Errorf("selective 单键不应触发: %+v", got)
	}
	// 主键+次键 → 触发
	if got := b.Match("禁书藏在地下"); len(got) != 1 || got[0].Entry.Name != "禁书" {
		t.Errorf("selective 双键应触发: %+v", got)
	}
}

func TestMatch_CaseSensitive(t *testing.T) {
	bs := &Book{Entries: []Entry{
		{Keys: []string{"Snake"}, Content: "snake", Enabled: true, InsertionOrder: 0, CaseSensitive: true},
		{Keys: []string{"Dog"}, Content: "dog", Enabled: true, InsertionOrder: 0},
	}}
	if got := bs.Match("sNaKe"); len(got) != 0 {
		t.Errorf("大小写敏感条目不应命中 sNaKe: %+v", got)
	}
	if got := bs.Match("DOG"); len(got) != 1 {
		t.Errorf("大小写不敏感条目应命中 DOG: %+v", got)
	}
}

func TestMatch_Disabled(t *testing.T) {
	b := testBook()
	if got := b.Match("旧条目"); len(got) != 0 {
		t.Errorf("停用条目不应命中: %+v", got)
	}
}

func TestAssemble_OrderAndConstant(t *testing.T) {
	b := testBook()
	// 文本同时命中：禁书（主键禁书+次键地下）、图书馆、魔法（constant）
	blocks, unused := b.Assemble("禁书在地下的图书馆，魔法依旧", 0)
	if len(unused) != 0 {
		t.Errorf("不限预算不应有丢弃: %v", unused)
	}
	// constant（魔法本质）排前，其余按 insertion_order（禁书1 → 图书馆3）
	var order []string
	for _, blk := range blocks {
		order = append(order, firstLine(blk))
	}
	want := []string{"【魔法本质】", "【禁书】", "【图书馆】"}
	if len(order) != len(want) {
		t.Fatalf("块数 = %d, 期望 %d: %v", len(order), len(want), order)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Errorf("顺序[%d] = %q, 期望 %q（全部: %v）", i, order[i], want[i], order)
		}
	}
}

func TestAssemble_TokenBudget(t *testing.T) {
	b := testBook()
	// 预算 8 token ≈ 16 字符：只够装下常驻条目（魔法本质），非 constant 低值先丢
	blocks, unused := b.Assemble("禁书在地下的图书馆，魔法依旧", 8)
	if len(blocks) == 0 {
		t.Fatal("常驻条目不应被预算丢弃")
	}
	if blocks[0] != "【魔法本质】\n魔法是灵魂的语言。" {
		t.Errorf("常驻块错误: %q", blocks[0])
	}
	// 禁书 priority=2 先于 图书馆 priority=3 被丢
	unusedName := map[string]bool{}
	for _, u := range unused {
		unusedName[u] = true
	}
	if !unusedName["禁书"] || !unusedName["图书馆"] {
		t.Errorf("低值条目应被丢弃: %v", unused)
	}
	if unusedName["魔法本质"] {
		t.Error("constant 条目不应进入丢弃名单（预算内）")
	}
}

func TestAssemble_NoMatch(t *testing.T) {
	b := testBook()
	blocks, unused := b.Assemble("这里没有任何关键词", 0)
	if len(blocks) != 0 || len(unused) != 0 {
		t.Errorf("无命中时应为空: blocks=%v unused=%v", blocks, unused)
	}
}

func TestEnableDisableEntry(t *testing.T) {
	b := testBook()
	if err := b.DisableEntry(0, "图书馆"); err != nil {
		t.Fatalf("停用失败: %v", err)
	}
	if got := b.Match("我在图书馆"); len(got) != 0 {
		t.Errorf("停用后不应命中: %+v", got)
	}
	if err := b.EnableEntry(0, "图书馆"); err != nil {
		t.Fatalf("启用失败: %v", err)
	}
	if got := b.Match("我在图书馆"); len(got) != 1 {
		t.Errorf("启用后应命中: %+v", got)
	}
	if err := b.DisableEntry(999, "不存在"); err == nil {
		t.Error("不存在的条目应报错")
	}
}

// firstLine 取块首行（条目标题）。
func firstLine(block string) string {
	idx := strings.Index(block, "\n")
	if idx < 0 {
		return block
	}
	return block[:idx]
}
