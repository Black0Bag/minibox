package skill

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Black0Bag/minibox/internal/fsutil"
)

// Skill 沉淀技能（Phase 4：成功工作流 → skill → 同类任务直接调取）
type Skill struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`     // 技能名（简短）
	Desc         string   `json:"desc"`     // 描述（含触发关键词）
	Steps        []string `json:"steps"`    // 执行步骤
	Boundaries   string   `json:"boundaries,omitempty"` // 边界（安全约束）
	Tags         []string `json:"tags,omitempty"`
	SuccessCount int      `json:"success_count"` // 成功次数（热度）
	CreatedAt    string   `json:"created_at"`
	UpdatedAt    string   `json:"updated_at"`
}

// Store Skill 沉淀存储
type Store struct {
	mu    sync.Mutex
	dir   string
	skills map[string]*Skill
}

// NewStore 创建 Skill 存储
func NewStore(dir string) *Store {
	s := &Store{dir: dir, skills: map[string]*Skill{}}
	_ = s.load()
	return s
}

// Create 创建 skill
func (s *Store) Create(sk Skill) (*Skill, error) {
	if strings.TrimSpace(sk.Name) == "" {
		return nil, errors.New("技能名不能为空")
	}
	if strings.TrimSpace(sk.Desc) == "" {
		return nil, errors.New("技能描述不能为空")
	}
	if len(sk.Steps) == 0 {
		return nil, errors.New("技能至少一步")
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	sk.ID = fsutil.NewID()
	sk.CreatedAt = now
	sk.UpdatedAt = now
	s.mu.Lock()
	defer s.mu.Unlock()
	s.skills[sk.ID] = &sk
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return &sk, nil
}

// Get 获取 skill
func (s *Store) Get(id string) (*Skill, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sk, ok := s.skills[id]
	if !ok {
		return nil, errors.New("技能不存在")
	}
	c := *sk
	return &c, nil
}

// List 列出全部 skill
func (s *Store) List() ([]Skill, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Skill, 0, len(s.skills))
	for _, sk := range s.skills {
		out = append(out, *sk)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SuccessCount > out[j].SuccessCount })
	return out, nil
}

// RecordSuccess 记录一次成功工作流（沉淀或强化）：同名存在则热度+1，否则新建
func (s *Store) RecordSuccess(name, desc string, steps []string, tags []string) (*Skill, error) {
	// 先查重名
	for _, sk := range s.skills {
		if sk.Name == name {
			sk.SuccessCount++
			sk.UpdatedAt = time.Now().Format("2006-01-02 15:04:05")
			_ = s.saveLocked()
			c := *sk
			return &c, nil
		}
	}
	return s.Create(Skill{Name: name, Desc: desc, Steps: steps, Tags: tags, SuccessCount: 1})
}

// Match 匹配同类任务：精确名 > 描述关键词 > 标签，命中返回热度最高者
func (s *Store) Match(input string) (*Skill, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	in := strings.ToLower(input)

	// 精确名匹配
	for _, sk := range s.skills {
		if strings.ToLower(sk.Name) == in {
			return clone(sk), true
		}
	}

	// 描述/标签关键词匹配（取热度最高）
	var best *Skill
	for _, sk := range s.skills {
		haystack := strings.ToLower(sk.Name + " " + sk.Desc + " " + strings.Join(sk.Tags, " "))
		nameL := strings.ToLower(sk.Name)
		// 输入是 skill 名的超集（"帮我编译一下这个文档" 含 "编译知识库" 的部分）
		if containsAny(input, splitTokens(nameL)) {
			if best == nil || sk.SuccessCount > best.SuccessCount {
				best = sk
			}
			continue
		}
		if strings.Contains(haystack, in) || strings.Contains(in, nameL) {
			if best == nil || sk.SuccessCount > best.SuccessCount {
				best = sk
			}
		}
	}
	if best != nil {
		return clone(best), true
	}
	return nil, false
}

// containsAny 输入是否包含任一关键词
func containsAny(input string, tokens []string) bool {
	low := strings.ToLower(input)
	for _, tk := range tokens {
		if tk != "" && strings.Contains(low, tk) {
			return true
		}
	}
	return false
}

// splitTokens 中文名按 2 字符滑动窗口切 token（如 "编译知识库" → "编译" "译知" "知识" "识库"）
func splitTokens(name string) []string {
	runes := []rune(name)
	if len(runes) < 2 {
		return []string{name}
	}
	var out []string
	for i := 0; i+1 < len(runes); i++ {
		out = append(out, string(runes[i:i+2]))
	}
	return out
}

func clone(sk *Skill) *Skill {
	c := *sk
	c.Steps = append([]string{}, sk.Steps...)
	c.Tags = append([]string{}, sk.Tags...)
	return &c
}

// ============ 持久化 ============

func (s *Store) path() string { return filepath.Join(s.dir, "skills.json") }

func (s *Store) saveLocked() error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(s.skills, "", "  ")
	return os.WriteFile(s.path(), data, 0o600)
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path())
	if err != nil {
		return nil
	}
	var skills map[string]*Skill
	if err := json.Unmarshal(data, &skills); err != nil {
		return fmt.Errorf("解析 skills: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.skills = skills
	return nil
}
