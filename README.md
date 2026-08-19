# minibox（后端仓库）

minibox 项目的后端——Go 单文件二进制"中枢大脑"。

## 分支说明

- `opencode`（默认）：本次从零推进任务的开发分支，所有开发在此进行
- `OmniBot`：历史遗留分支，仅存档，不再维护

## 当前状态

编码阶段 · 全部 8 个 Phase 已编码（详见 minibox-dev 仓库 `opencode` 分支的《编码任务追踪.md》）。

## 技术栈

Go 模块化单体 + transport/domain/infrastructure 三层 + SQLite（知识库）+ REST/SSE/WS 三通道。

## 安全规则

敏感信息（API key 等）绝不写入代码，仅本地测试用临时配置文件调用，测完删除。
