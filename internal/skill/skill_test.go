package skill

import (
	"strings"
	"testing"
)

func TestCreateSkillValidation(t *testing.T) {
	s := NewStore(t.TempDir())
	if _, err := s.Create(Skill{Name: ""}); err == nil {
		t.Error("空名应报错")
	}
	if _, err := s.Create(Skill{Name: "x"}); err == nil {
		t.Error("空描述应报错")
	}
}

func TestCreateGetAndLoad(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	sk, err := s.Create(Skill{Name: "编译知识库", Desc: "把文本编译成知识库条目", Steps: []string{"提炼", "入库"}, Tags: []string{"编译", "知识库"}})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if sk.ID == "" || len(sk.Steps) != 2 {
		t.Errorf("skill 异常: %+v", sk)
	}
	// 重新加载
	s2 := NewStore(dir)
	got, err := s2.Get(sk.ID)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if got.Name != "编译知识库" {
		t.Errorf("持久化丢失: %+v", got)
	}
}

func TestRecordSuccessFromWorkflow(t *testing.T) {
	s := NewStore(t.TempDir())
	// 一次成功工作流沉淀为 skill（步骤按顺序执行成功）
	sk, err := s.RecordSuccess("写周报", "把本周工作整理成周报", []string{"收集工作项", "生成周报"}, nil)
	if err != nil {
		t.Fatalf("沉淀失败: %v", err)
	}
	if sk.SuccessCount != 1 {
		t.Errorf("成功计数应为 1，得到 %d", sk.SuccessCount)
	}
	// 再次成功 → 计数增加 + 触发热度
	sk2, _ := s.RecordSuccess("写周报", "把本周工作整理成周报", []string{"收集工作项", "生成周报"}, nil)
	if sk2.SuccessCount != 2 {
		t.Errorf("第二次成功计数应为 2，得到 %d", sk2.SuccessCount)
	}
}

func TestMatchByName(t *testing.T) {
	s := NewStore(t.TempDir())
	_, _ = s.Create(Skill{Name: "编译知识库", Desc: "把文本编译成知识库条目", Steps: []string{"提炼"}})
	got, ok := s.Match("编译知识库")
	if !ok {
		t.Fatal("应按名字匹配")
	}
	if got.Name != "编译知识库" {
		t.Errorf("匹配错误: %s", got.Name)
	}
	// 模糊匹配（描述关键词）
	got, ok = s.Match("帮我编译一下这个文档")
	if !ok {
		t.Fatal("应模糊匹配到编译 skill")
	}
	if got.Name != "编译知识库" {
		t.Errorf("模糊匹配错误: %s", got.Name)
	}
}

func TestNoMatch(t *testing.T) {
	s := NewStore(t.TempDir())
	if _, ok := s.Match("完全不相关的事情"); ok {
		t.Error("不应匹配到任何 skill")
	}
}

func TestRankingBySuccess(t *testing.T) {
	s := NewStore(t.TempDir())
	// 低热度 skill
	_, _ = s.Create(Skill{Name: "冷门", Desc: "冷门领域", Steps: []string{"a"}})
	// 高热度 skill（成功 3 次）
	_, _ = s.RecordSuccess("热门", "热门领域", []string{"a"}, nil)
	_, _ = s.RecordSuccess("热门", "热门领域", []string{"a"}, nil)
	_, _ = s.RecordSuccess("热门", "热门领域", []string{"a"}, nil)

	got, ok := s.Match("热门领域")
	if !ok {
		t.Fatal("应匹配热门")
	}
	if got.Name != "热门" {
		t.Errorf("应优先匹配高热度: %s", got.Name)
	}
}

func TestList(t *testing.T) {
	s := NewStore(t.TempDir())
	_, _ = s.Create(Skill{Name: "a", Desc: "x", Steps: []string{"s"}})
	_, _ = s.Create(Skill{Name: "b", Desc: "y", Steps: []string{"s"}})
	list, err := s.List()
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("应有 2 个，得到 %d", len(list))
	}
}

func TestSkillContainsRequiredFields(t *testing.T) {
	s := NewStore(t.TempDir())
	sk, _ := s.Create(Skill{Name: "带边界", Desc: "带边界描述", Steps: []string{"步骤1"}, Boundaries: "只读操作"})
	if sk.Boundaries != "只读操作" {
		t.Errorf("边界丢失: %s", sk.Boundaries)
	}
	if !strings.Contains(sk.Desc, "带边界") {
		t.Errorf("描述丢失: %s", sk.Desc)
	}
}
