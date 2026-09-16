# Changelog

All notable changes to minibox will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.2.1] - 2026-09-16

### Added
- 补齐 4 个 domain 包单元测试（agent/memory/scheduler/subagent，共 36 个测试用例）

## [0.2.0] - 2026-09-16

### Changed
- 拆分 http_handlers.go（1030 行）为 9 个域文件，提升可维护性

### Fixed
- 修复 GoReleaser 跨平台编译失败（v0.1.0-v0.1.2）

## [0.1.2] - 2026-09-16

### Fixed
- 修复 GoReleaser 配置错误：changelog.use: file 不是有效选项，移除后使用默认 git 模式

## [0.1.1] - 2026-09-16

### Fixed
- 修复 GoReleaser 跨平台编译失败：移除 windows 目标（syscall.Statfs 不支持 Windows）

## [0.1.0] - 2026-09-16

### Added
- 多供应商 OpenAI 兼容 LLM 路由与 SSE 解析
- SQLite + FTS5 中文预分词 + 向量混合检索
- Agent 状态机、Plan-first 工具权限链
- REST/SSE/WebSocket 三通道通信
- Bearer Token 认证与白名单
- GitHub Actions CI（test/race/lint/audit 四阶段）
- GoReleaser 跨平台单文件二进制发布

### Fixed
- 修复会话 Hub 并发安全缺陷
- 修复调度器非原子操作导致任务丢失
- 修复 HTTP Server 缺少 ReadHeaderTimeout