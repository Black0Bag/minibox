// Package charactercard 角色卡解析（SillyTavern 格式 V1/V2/V3）+ 角色书管理（B10）。
// 设计（联网校准 character-card-spec-v2 2023-06 + SillyTavern 2026 现行规范）：
//   - V1：扁平 JSON（name/description/personality/scenario/first_mes/mes_example）
//   - V2：{"spec":"chara_card_v2","spec_version":"2.0","data":{...}}
//   - V3：V2 外壳 + nickname/assets/source/group_only_greetings 等新字段（机械迁移）
//   - V2+ 卡片常带顶层 V1 兼容垫片（duplicated V1-style top level），解析时回退补全
//   - character_book（角色书）是内嵌在卡片里的世界书，取优先于整机世界书（规范条款）
package charactercard

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Card 统一角色卡（V1/V2/V3 归一后的内部结构）。
type Card struct {
	Spec    string // chara_card_v1 / chara_card_v2 / chara_card_v3
	Version string // spec_version

	Name        string
	Description string
	Personality string
	Scenario    string
	FirstMes    string
	MesExample  string

	CreatorNotes            string
	SystemPrompt            string
	PostHistoryInstructions string
	AlternateGreetings      []string
	Tags                    []string
	Creator                 string
	CharacterVersion        string
	Book                    *Book // 内嵌角色书（可空）
	Extensions              map[string]any

	// V3 新增
	Nickname                 string
	Assets                   []map[string]any // 多媒体资产声明（type/name/path，仅保留引用不下载）
	Source                   []string
	GroupOnlyGreetings       []string
	CreatorNotesMultilingual map[string]string
}

// v2Envelope V2/V3 外壳（data 为剩余字段的原始 JSON）。
type v2Envelope struct {
	Spec    string          `json:"spec"`
	Version string          `json:"spec_version"`
	Data    json.RawMessage `json:"data"`
}

// v1Body V1（扁平）与 V2/V3 data 共用的字段体，同时承担 V2+ 顶层兼容垫片。
type v1Body struct {
	Name                     string            `json:"name"`
	Description              string            `json:"description"`
	Personality              string            `json:"personality"`
	Scenario                 string            `json:"scenario"`
	FirstMes                 string            `json:"first_mes"`
	MesExample               string            `json:"mes_example"`
	CreatorNotes             string            `json:"creator_notes"`
	SystemPrompt             string            `json:"system_prompt"`
	PostHistoryInstructions  string            `json:"post_history_instructions"`
	AlternateGreetings       []string          `json:"alternate_greetings"`
	Tags                     []string          `json:"tags"`
	Creator                  string            `json:"creator"`
	CharacterVersion         string            `json:"character_version"`
	CharacterBook            *Book             `json:"character_book"`
	Extensions               map[string]any    `json:"extensions"`
	Nickname                 string            `json:"nickname"`
	Assets                   []map[string]any  `json:"assets"`
	Source                   []string          `json:"source"`
	GroupOnlyGreetings       []string          `json:"group_only_greetings"`
	CreatorNotesMultilingual map[string]string `json:"creator_notes_multilingual"`
}

// Parse 解析角色卡 JSON（自动检测 V1/V2/V3）。
// V2/V3：优先取 data 内字段，空值时回退顶层兼容垫片（规范要求不破坏未知字段）。
func Parse(data []byte) (*Card, error) {
	var env v2Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("解析角色卡失败: %w", err)
	}

	card := &Card{Extensions: map[string]any{}}

	// 无 spec 字段 → V1 扁平卡
	if env.Spec == "" {
		var body v1Body
		if err := json.Unmarshal(data, &body); err != nil {
			return nil, fmt.Errorf("解析 V1 角色卡失败: %w", err)
		}
		card.Spec = "chara_card_v1"
		applyBody(card, body)
		if card.Name == "" {
			return nil, fmt.Errorf("角色卡缺 name")
		}
		return card, nil
	}

	// V2/V3：data 为真身，顶层为兼容垫片
	card.Spec = env.Spec
	card.Version = env.Version
	var body, shim v1Body
	if err := json.Unmarshal(env.Data, &body); err != nil {
		return nil, fmt.Errorf("解析 %s data 失败: %w", env.Spec, err)
	}
	if err := json.Unmarshal(data, &shim); err != nil {
		return nil, fmt.Errorf("解析 %s 兼容垫片失败: %w", env.Spec, err)
	}
	applyBodyWithShim(card, body, shim)
	if card.Name == "" {
		return nil, fmt.Errorf("角色卡缺 name")
	}
	return card, nil
}

// ParseFile 从文件读取并解析角色卡。
func ParseFile(path string) (*Card, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path 由调用方（组合根/导入器）指定，非网络不可信输入
	if err != nil {
		return nil, fmt.Errorf("读取角色卡失败: %w", err)
	}
	return Parse(data)
}

// applyBody V1 直填。
func applyBody(c *Card, b v1Body) {
	c.Name = b.Name
	c.Description = b.Description
	c.Personality = b.Personality
	c.Scenario = b.Scenario
	c.FirstMes = b.FirstMes
	c.MesExample = b.MesExample
	c.CreatorNotes = b.CreatorNotes
	c.SystemPrompt = b.SystemPrompt
	c.PostHistoryInstructions = b.PostHistoryInstructions
	c.AlternateGreetings = b.AlternateGreetings
	c.Tags = b.Tags
	c.Creator = b.Creator
	c.CharacterVersion = b.CharacterVersion
	c.Book = b.CharacterBook
	c.Nickname = b.Nickname
	c.Assets = b.Assets
	c.Source = b.Source
	c.GroupOnlyGreetings = b.GroupOnlyGreetings
	c.CreatorNotesMultilingual = b.CreatorNotesMultilingual
	if len(b.Extensions) > 0 {
		c.Extensions = b.Extensions
	}
}

// applyBodyWithShim V2/V3 填写，data 空字段回退顶层垫片。
func applyBodyWithShim(c *Card, b, shim v1Body) {
	applyBody(c, b)
	// 兼容垫片回退：data 为空时才取顶层（垫片通常只含 V1 六字段）
	c.Description = pick(b.Description, shim.Description)
	c.Personality = pick(b.Personality, shim.Personality)
	c.Scenario = pick(b.Scenario, shim.Scenario)
	c.FirstMes = pick(b.FirstMes, shim.FirstMes)
	c.MesExample = pick(b.MesExample, shim.MesExample)
}

// pick 首选 a，空则回退 b。
func pick(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// DisplayName 显示名：V3 nickname 优先，否则 name。
func (c *Card) DisplayName() string {
	if c.Nickname != "" {
		return c.Nickname
	}
	return c.Name
}

// SystemPromptSection 组装 system prompt 段。
// original 为无角色卡时的全局 system prompt（替换 {{original}} 占位符）。
// 规范：system_prompt 空字符串时 MUST 使用 fallback（调用方决定），此处原样返回空。
func (c *Card) SystemPromptSection(original string) string {
	sp := c.SystemPrompt
	if sp == "" {
		return ""
	}
	return strings.ReplaceAll(sp, "{{original}}", original)
}

// PostHistorySection 组装 post_history_instructions 段（替换 {{original}}）。
func (c *Card) PostHistorySection(original string) string {
	phi := c.PostHistoryInstructions
	if phi == "" {
		return ""
	}
	return strings.ReplaceAll(phi, "{{original}}", original)
}

// CharacterDefinition 组装人物定义段（name/description/personality/scenario/mes_example）。
// 供 system prompt 装配器使用。
func (c *Card) CharacterDefinition() string {
	var sb strings.Builder
	sb.WriteString("【角色】" + c.DisplayName() + "\n")
	if c.Description != "" {
		sb.WriteString("【设定】" + c.Description + "\n")
	}
	if c.Personality != "" {
		sb.WriteString("【性格】" + c.Personality + "\n")
	}
	if c.Scenario != "" {
		sb.WriteString("【场景】" + c.Scenario + "\n")
	}
	if c.MesExample != "" {
		sb.WriteString("【示例对话】\n" + c.MesExample + "\n")
	}
	return sb.String()
}
