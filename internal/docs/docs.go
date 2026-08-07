package docs

import (
	"sort"
	"strings"
)

// Route 路由文档条目
type Route struct {
	Method  string `json:"method"`
	Path    string `json:"path"`
	Summary string `json:"summary"`
}

// APIDoc 中文 API 文档（M8：API 文档，中文优先）
type APIDoc struct {
	Title   string  `json:"title"`
	Version string  `json:"version"`
	BaseURL string  `json:"base_url"`
	Routes  []Route `json:"routes"`
}

// Route 列表（按方法分组、路径排序）
var routes = []Route{
	{Method: "GET", Path: "/api/v1/healthz", Summary: "健康检查（watchdog 依赖，豁免限流）"},
	{Method: "GET", Path: "/api/v1/状态", Summary: "系统状态（版本/端口/内存/供应商数）"},
	{Method: "GET", Path: "/api/v1/版本", Summary: "版本信息（版本号单一来源）"},
	{Method: "GET", Path: "/api/v1/logs", Summary: "日志查询（level/limit/since，自动脱敏）"},
	{Method: "GET", Path: "/api/v1/监控", Summary: "系统性能指标（CPU/内存/磁盘/网络/进程）"},
	{Method: "GET", Path: "/api/v1/降级", Summary: "当前四级降级状态（L0-L3）"},

	{Method: "GET", Path: "/api/v1/供应商/", Summary: "供应商列表（key 脱敏）"},
	{Method: "POST", Path: "/api/v1/供应商/", Summary: "新增 LLM 供应商"},
	{Method: "GET", Path: "/api/v1/供应商/{name}", Summary: "供应商详情"},
	{Method: "PUT", Path: "/api/v1/供应商/{name}", Summary: "更新供应商"},
	{Method: "DELETE", Path: "/api/v1/供应商/{name}", Summary: "删除供应商"},

	{Method: "POST", Path: "/api/v1/知识库/", Summary: "写入知识条目（默认缓存区）"},
	{Method: "GET", Path: "/api/v1/知识库/", Summary: "条目列表（zone/type/limit/offset）"},
	{Method: "GET", Path: "/api/v1/知识库/搜索", Summary: "全文搜索（配置 embedding 自动 RRF 融合）"},
	{Method: "GET", Path: "/api/v1/知识库/{id}", Summary: "读取条目"},
	{Method: "PUT", Path: "/api/v1/知识库/{id}", Summary: "更新条目"},
	{Method: "DELETE", Path: "/api/v1/知识库/{id}", Summary: "删除条目"},
	{Method: "POST", Path: "/api/v1/知识库/{id}/向量", Summary: "写入条目向量"},
	{Method: "DELETE", Path: "/api/v1/知识库/", Summary: "清空区域（zone=cache 默认）"},

	{Method: "POST", Path: "/api/v1/对话/流式", Summary: "SSE 流式对话（thinking/answer 事件）"},
	{Method: "POST", Path: "/api/v1/对话", Summary: "非流式 Agent 对话（工具调用闭环）"},

	{Method: "POST", Path: "/api/v1/编译/", Summary: "提交编译任务（text/url→LLM提炼→入库）"},
	{Method: "GET", Path: "/api/v1/编译/", Summary: "编译任务列表"},
	{Method: "GET", Path: "/api/v1/编译/{id}", Summary: "编译任务状态"},

	{Method: "POST", Path: "/api/v1/蒸馏/命中", Summary: "正向证据：命中关键词上调概率"},
	{Method: "POST", Path: "/api/v1/蒸馏/反例", Summary: "反向证据：用户否定下调概率"},
	{Method: "GET", Path: "/api/v1/蒸馏/", Summary: "蒸馏偏好列表"},
	{Method: "POST", Path: "/api/v1/蒸馏/老化", Summary: "触发老化衰减（TTL）"},
	{Method: "DELETE", Path: "/api/v1/蒸馏/{keyword}", Summary: "删除偏好"},

	{Method: "POST", Path: "/api/v1/subagent/", Summary: "批量派发 subagent（Fan-out/Fan-in）"},
	{Method: "GET", Path: "/api/v1/subagent/{id}/日志", Summary: "subagent 侧链日志"},

	{Method: "POST", Path: "/api/v1/计划/", Summary: "创建长程计划（to-do-list）"},
	{Method: "GET", Path: "/api/v1/计划/", Summary: "计划列表"},
	{Method: "GET", Path: "/api/v1/计划/{id}", Summary: "计划详情（中断续跑入口）"},
	{Method: "POST", Path: "/api/v1/计划/{id}/执行", Summary: "执行/续跑计划"},
	{Method: "POST", Path: "/api/v1/计划/{id}/回滚", Summary: "回滚到指定步骤"},
	{Method: "DELETE", Path: "/api/v1/计划/{id}", Summary: "删除计划"},

	{Method: "POST", Path: "/api/v1/调度/", Summary: "创建调度任务（alarm/schedule/calendar）"},
	{Method: "GET", Path: "/api/v1/调度/", Summary: "调度任务列表"},
	{Method: "GET", Path: "/api/v1/调度/{id}", Summary: "调度任务详情"},
	{Method: "POST", Path: "/api/v1/调度/{id}/执行", Summary: "执行调度任务"},
	{Method: "POST", Path: "/api/v1/调度/{id}/取消", Summary: "取消调度任务"},
	{Method: "DELETE", Path: "/api/v1/调度/{id}", Summary: "删除调度任务"},

	{Method: "POST", Path: "/api/v1/心跳/", Summary: "订阅心跳任务（绑定用户边界）"},
	{Method: "GET", Path: "/api/v1/心跳/", Summary: "心跳任务列表"},
	{Method: "POST", Path: "/api/v1/心跳/{id}/执行", Summary: "立即执行心跳任务"},
	{Method: "POST", Path: "/api/v1/心跳/{id}/退订", Summary: "退订心跳任务"},

	{Method: "POST", Path: "/api/v1/技能/", Summary: "创建技能"},
	{Method: "GET", Path: "/api/v1/技能/", Summary: "技能列表（按热度排序）"},
	{Method: "GET", Path: "/api/v1/技能/匹配", Summary: "匹配同类技能（q=任务描述）"},
	{Method: "POST", Path: "/api/v1/技能/沉淀", Summary: "沉淀成功工作流为技能"},

	{Method: "POST", Path: "/api/v1/设备/配对码", Summary: "生成设备配对码"},
	{Method: "POST", Path: "/api/v1/设备/", Summary: "设备配对注册"},
	{Method: "GET", Path: "/api/v1/设备/", Summary: "设备列表"},
	{Method: "GET", Path: "/api/v1/设备/{id}", Summary: "设备详情"},
	{Method: "POST", Path: "/api/v1/设备/{id}/心跳", Summary: "设备心跳"},
	{Method: "POST", Path: "/api/v1/设备/{id}/离线", Summary: "标记设备离线"},
	{Method: "GET", Path: "/api/v1/设备/审计", Summary: "设备审计日志"},
	{Method: "DELETE", Path: "/api/v1/设备/{id}", Summary: "注销设备"},

	{Method: "POST", Path: "/api/v1/快照/", Summary: "创建一致性快照（VACUUM INTO）"},
	{Method: "GET", Path: "/api/v1/快照/", Summary: "快照列表"},
	{Method: "POST", Path: "/api/v1/快照/恢复", Summary: "从快照恢复"},

	{Method: "POST", Path: "/api/v1/备份/", Summary: "手动备份（快照+保留策略）"},
	{Method: "GET", Path: "/api/v1/备份/", Summary: "备份列表"},
	{Method: "GET", Path: "/api/v1/备份/记录", Summary: "备份记录（大小/时间）"},

	{Method: "GET", Path: "/api/v1/升级/", Summary: "升级/watchdog 状态"},
	{Method: "POST", Path: "/api/v1/升级/检查", Summary: "健康检查"},
	{Method: "POST", Path: "/api/v1/升级/应用", Summary: "应用升级（保守两阶段）"},
}

// Build 生成完整 API 文档
func Build(version string) *APIDoc {
	doc := &APIDoc{
		Title:   "minibox 后端 API 文档",
		Version: version,
		BaseURL: "http://<后端地址>:8086",
	}
	doc.Routes = append([]Route{}, routes...)
	sort.Slice(doc.Routes, func(i, j int) bool {
		if doc.Routes[i].Path == doc.Routes[j].Path {
			return doc.Routes[i].Method < doc.Routes[j].Method
		}
		return doc.Routes[i].Path < doc.Routes[j].Path
	})
	return doc
}

// Markdown 生成 Markdown 格式文档
func (d *APIDoc) Markdown() string {
	var sb strings.Builder
	sb.WriteString("# " + d.Title + "\n\n")
	sb.WriteString("- 版本：" + d.Version + "\n")
	sb.WriteString("- 基础地址：" + d.BaseURL + "\n")
	sb.WriteString("- 协议：REST + SSE，统一前缀 `/api/v1`\n")
	sb.WriteString("- 错误格式：RFC 7807（type/title/status/detail/code）\n")
	sb.WriteString("- 限流：60/min（/healthz 豁免）\n\n")
	sb.WriteString("## 路由一览\n\n")
	sb.WriteString("| 方法 | 路径 | 说明 |\n")
	sb.WriteString("|---|---|---|\n")
	for _, r := range d.Routes {
		sb.WriteString("| " + r.Method + " | `" + r.Path + "` | " + r.Summary + " |\n")
	}
	return sb.String()
}
