-- 0003_system_config.sql — 系统配置表（B6 功能级模型独立配置 + 通用 key-value 持久化）
-- 依据：PRD B6 每个需调用 LLM 的功能都可独立配置模型
-- 设计：通用 key-value 表，value 为 JSON 字符串，可扩展任意系统配置项
-- 与 0001_init.sql 注释中的 "system_config 表后续迁移追加" 对齐

CREATE TABLE IF NOT EXISTS system_config (
    key         TEXT PRIMARY KEY,               -- 配置键，如 "feature.agent.model" / "feature.pref_extract.provider"
    value       TEXT NOT NULL,                  -- JSON 值
    description TEXT NOT NULL DEFAULT '',       -- 中文描述（供前端/CLI 显示）
    updated_at  TEXT NOT NULL DEFAULT (datetime('now')),
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

-- 默认配置：功能级模型映射（空值=使用默认供应商/模型）
INSERT OR IGNORE INTO system_config (key, value, description) VALUES
    ('feature.agent.provider',       '""', '对话/Agent 引擎使用的供应商标识（空=默认）'),
    ('feature.agent.model',          '""', '对话/Agent 引擎使用的模型ID（空=默认）'),
    ('feature.pref_extract.provider','""', '偏好蒸馏使用的供应商标识（空=默认）'),
    ('feature.pref_extract.model',   '""', '偏好蒸馏使用的模型ID（空=默认）'),
    ('feature.subagent.provider',    '""', '子代理使用的供应商标识（空=默认）'),
    ('feature.subagent.model',       '""', '子代理使用的模型ID（空=默认）');

-- 更新触发器：自动更新 updated_at
CREATE TRIGGER IF NOT EXISTS system_config_au AFTER UPDATE ON system_config
BEGIN
    UPDATE system_config SET updated_at = datetime('now') WHERE key = old.key;
END;
