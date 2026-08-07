package distill

import (
	"testing"

	"github.com/Black0Bag/minibox/internal/memory"
)

func newDist(t *testing.T) *Distiller {
	t.Helper()
	store, err := memory.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open 失败: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return NewDistiller(store)
}

func TestHitRaisesProbability(t *testing.T) {
	d := newDist(t)
	if err := d.Hit("喜欢简洁", "high", "s1"); err != nil {
		t.Fatalf("Hit 失败: %v", err)
	}
	if err := d.Hit("喜欢简洁", "high", "s1"); err != nil {
		t.Fatalf("二次 Hit 失败: %v", err)
	}
	prefs, err := d.List(10)
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	if len(prefs) != 1 {
		t.Fatalf("应有 1 条偏好，得到 %d", len(prefs))
	}
	if prefs[0].Keyword != "喜欢简洁" {
		t.Errorf("keyword 错误: %s", prefs[0].Keyword)
	}
	if prefs[0].Evidence != 2 {
		t.Errorf("evidence 应为 2，得到 %d", prefs[0].Evidence)
	}
	if prefs[0].Probability <= 0.5 {
		t.Errorf("命中两次后概率应大于 0.5，得到 %f", prefs[0].Probability)
	}
}

func TestNegativeLowersProbability(t *testing.T) {
	d := newDist(t)
	_ = d.Hit("喜欢长文", "medium", "")
	prefs, _ := d.List(10)
	before := prefs[0].Probability
	if err := d.Negative("喜欢长文"); err != nil {
		t.Fatalf("Negative 失败: %v", err)
	}
	prefs, _ = d.List(10)
	if prefs[0].Probability >= before {
		t.Errorf("反例后概率应下调: before=%f after=%f", before, prefs[0].Probability)
	}
}

func TestDelete(t *testing.T) {
	d := newDist(t)
	_ = d.Hit("临时偏好", "low", "")
	ok, err := d.Delete("临时偏好")
	if err != nil || !ok {
		t.Fatalf("Delete 失败: ok=%v err=%v", ok, err)
	}
	if _, err := d.Delete("临时偏好"); err != nil {
		// 已删除，不应再删到
	}
	prefs, _ := d.List(10)
	if len(prefs) != 0 {
		t.Errorf("删除后应为空，得到 %d 条", len(prefs))
	}
}

func TestImportancePersist(t *testing.T) {
	d := newDist(t)
	_ = d.Hit("永久约束", "permanent", "")
	prefs, _ := d.List(10)
	if prefs[0].Importance != "permanent" {
		t.Errorf("importance 应为 permanent，得到 %s", prefs[0].Importance)
	}
}
