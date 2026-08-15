package charactercard

import (
	"strings"
	"testing"
)

// v2Card 一份标准 V2 卡（含顶层 V1 兼容垫片 + 内嵌角色书）。
const v2Card = `{
  "spec": "chara_card_v2",
  "spec_version": "2.0",
  "name": "魔导师艾莲娜", "description": "顶层垫片描述", "personality": "顶层垫片性格",
  "first_mes": "顶层垫片问候",
  "data": {
    "name": "艾莲娜",
    "description": "银发魔导师，掌管古老魔法图书馆。",
    "personality": "睿智、耐心、神秘。喜欢谜语。",
    "scenario": "你为寻求禁忌知识来到阴影图书馆。",
    "first_mes": "*艾莲娜从古卷中抬起头，翠绿眼眸打量着你。* 欢迎，求知者。",
    "mes_example": "{{user}}: 魔法是什么？\n{{char}}: *扶了扶眼镜* 魔法既非善也非恶，只是工具。",
    "creator_notes": "本卡为测试卡",
    "system_prompt": "你是图书馆长艾莲娜。{{original}}",
    "post_history_instructions": "始终保持角色设定。",
    "alternate_greetings": ["*艾莲娜正擦拭书架。* 啊，来客人了。"],
    "tags": ["fantasy", "library"],
    "creator": "test-creator",
    "character_version": "1.0.0",
    "extensions": {"test": {"flag": true}},
    "character_book": {
      "name": "阴影图书馆设定",
      "scan_depth": 10,
      "token_budget": 200,
      "entries": [
        {"keys": ["禁忌之书"], "content": "禁忌之书藏在图书馆地下三层。", "enabled": true, "insertion_order": 2, "id": 1, "name": "禁忌之书"},
        {"keys": ["魔法"], "content": "魔法是灵魂的语言。", "enabled": true, "insertion_order": 1, "constant": true, "id": 2, "name": "魔法本质"}
      ]
    }
  }
}`

// v3Card 一份 V3 卡（含 V3 新字段）。
const v3Card = `{
  "spec": "chara_card_v3",
  "spec_version": "3.0",
  "data": {
    "name": "林晓",
    "nickname": "小晓",
    "first_mes": "你好！",
    "assets": [{"type": "expression", "name": "happy", "path": "happy.png"}],
    "source": ["https://example.com/origin"],
    "group_only_greetings": ["*对全组说话* 大家好。"],
    "creator_notes_multilingual": {"en": "test card", "zh": "测试卡"}
  }
}`

func TestParse_V1(t *testing.T) {
	c, err := Parse([]byte(`{"name":"测试角色","description":"描述","first_mes":"你好"}`))
	if err != nil {
		t.Fatalf("解析 V1 失败: %v", err)
	}
	if c.Spec != "chara_card_v1" {
		t.Errorf("Spec = %q, 期望 chara_card_v1", c.Spec)
	}
	if c.Name != "测试角色" || c.FirstMes != "你好" {
		t.Errorf("字段解析错误: %+v", c)
	}
}

func TestParse_V2(t *testing.T) {
	c, err := Parse([]byte(v2Card))
	if err != nil {
		t.Fatalf("解析 V2 失败: %v", err)
	}
	if c.Spec != "chara_card_v2" || c.Version != "2.0" {
		t.Errorf("spec/version = %q/%q", c.Spec, c.Version)
	}
	// data 内字段优先
	if c.Name != "艾莲娜" {
		t.Errorf("Name = %q, 期望 data 内艾莲娜", c.Name)
	}
	if c.Description != "银发魔导师，掌管古老魔法图书馆。" {
		t.Errorf("Description 应取 data: %q", c.Description)
	}
	if c.CharacterVersion != "1.0.0" || len(c.Tags) != 2 || c.CreatorNotes == "" {
		t.Errorf("V2 新字段解析错误: %+v", c)
	}
	// 内嵌角色书
	if c.Book == nil || c.Book.Name != "阴影图书馆设定" || len(c.Book.Entries) != 2 {
		t.Fatalf("角色书解析错误: %+v", c.Book)
	}
	// extensions 保留未知字段
	flag, ok := c.Extensions["test"].(map[string]any)
	if !ok || flag["flag"] != true {
		t.Errorf("extensions 未保留: %v", c.Extensions)
	}
}

func TestParse_V2_ShimFallback(t *testing.T) {
	// data 缺字段时回退顶层垫片（V1 shim）
	card := `{"spec":"chara_card_v2","data":{"name":"A","personality":""},"personality":"顶层性格","first_mes":"顶层问候"}`
	c, err := Parse([]byte(card))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if c.Personality != "顶层性格" {
		t.Errorf("Personality 应回退垫片 = %q", c.Personality)
	}
	if c.FirstMes != "顶层问候" {
		t.Errorf("FirstMes 应回退垫片 = %q", c.FirstMes)
	}
}

func TestParse_V3(t *testing.T) {
	c, err := Parse([]byte(v3Card))
	if err != nil {
		t.Fatalf("解析 V3 失败: %v", err)
	}
	if c.Spec != "chara_card_v3" || c.Version != "3.0" {
		t.Errorf("spec/version = %q/%q", c.Spec, c.Version)
	}
	if c.DisplayName() != "小晓" {
		t.Errorf("DisplayName = %q, 期望 V3 nickname 小晓", c.DisplayName())
	}
	if len(c.Assets) == 0 || len(c.Source) == 0 || len(c.GroupOnlyGreetings) == 0 {
		t.Errorf("V3 新字段未解析: %+v", c)
	}
	if c.CreatorNotesMultilingual["zh"] != "测试卡" {
		t.Errorf("多语言创作者备注未解析: %v", c.CreatorNotesMultilingual)
	}
}

func TestParse_MissingName(t *testing.T) {
	if _, err := Parse([]byte(`{"description":"无名卡"}`)); err == nil {
		t.Error("缺 name 应报错")
	}
	if _, err := Parse([]byte(`{"spec":"chara_card_v2","data":{"description":"无名卡"}}`)); err == nil {
		t.Error("V2 缺 name 应报错")
	}
}

func TestParse_InvalidJSON(t *testing.T) {
	if _, err := Parse([]byte(`{invalid`)); err == nil {
		t.Error("非法 JSON 应报错")
	}
}

func TestSystemPromptSection(t *testing.T) {
	c, err := Parse([]byte(v2Card))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	sp := c.SystemPromptSection("【全局默认】")
	if !strings.Contains(sp, "你是图书馆长艾莲娜") || !strings.Contains(sp, "【全局默认】") {
		t.Errorf("{{original}} 未替换: %q", sp)
	}
	if c.PostHistorySection("x") == "" {
		t.Error("post_history_instructions 应为非空")
	}
	// 空 system_prompt → 返回空（调用方 fallback）
	plain, _ := Parse([]byte(`{"name":"A","first_mes":"hi"}`))
	if plain.SystemPromptSection("orig") != "" {
		t.Error("空 system_prompt 应返回空串")
	}
}

func TestCharacterDefinition(t *testing.T) {
	c, err := Parse([]byte(v2Card))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	def := c.CharacterDefinition()
	for _, want := range []string{"【角色】艾莲娜", "【设定】", "【性格】", "【场景】", "【示例对话】"} {
		if !strings.Contains(def, want) {
			t.Errorf("人物定义缺少 %q: %s", want, def)
		}
	}
}
