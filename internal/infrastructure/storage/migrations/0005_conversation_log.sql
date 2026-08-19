-- 0005_conversation_log.sql — 对话日志表（TTL 90 天自动清理）
-- 依据：PRD B14 对话历史持久化 + 进度跟踪设计第 5 项
-- 设计：
--   - 记录每条对话消息（用户/助手/工具）
--   - TTL 90 天：通过 cron 定时清理过期记录
--   - 按会话分组，支持历史回放
--   - messages JSON 存完整消息体（含 metadata）

CREATE TABLE IF NOT EXISTS conversation_log (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id  TEXT NOT NULL,                          -- 会话 ID
    run_id      TEXT NOT NULL DEFAULT '',               -- 所属 Run ID（可空）
    role        TEXT NOT NULL,                           -- user/assistant/tool/system
    content     TEXT NOT NULL,                           -- 消息内容
    tokens      INTEGER NOT NULL DEFAULT 0,             -- token 消耗
    metadata    TEXT NOT NULL DEFAULT '{}',             -- JSON: 额外元数据（工具名/模型等）
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_convlog_session ON conversation_log(session_id);
CREATE INDEX IF NOT EXISTS idx_convlog_created ON conversation_log(created_at);

-- TTL 触发器：插入时检查（软提醒，实际清理由 cron 定时执行 DELETE）
-- 注意：SQLite 不支持 CREATE TRIGGER ... AFTER INSERT ... WHERE，
--       TTL 清理在 app 层 cron 调度中执行（DELETE FROM conversation_log WHERE created_at < datetime('now', '-90 days')）
