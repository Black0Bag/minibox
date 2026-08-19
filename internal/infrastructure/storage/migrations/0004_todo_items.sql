-- 0004_todo_items.sql — Agent Run 持久化 + TODO 项（B5 崩溃续跑）
-- 依据：PRD B5 崩溃续跑（todo_items 表，每步落盘，重启后 Resume）
-- 设计：
--   - runs 表：Agent 运行元数据（状态/模式/步数/token/答案/错误）
--   - todo_items 表：运行中的 TODO 项（计划项，支持崩溃后恢复）
--   - messages 存 JSON（可续传对话历史，避免单独表 join）
--   - WAL 模式下崩溃不丢已提交数据（已由 Open 启用 journal_mode(WAL)）

CREATE TABLE IF NOT EXISTS runs (
    id           TEXT PRIMARY KEY,                      -- Run ID（ULID）
    session_id   TEXT NOT NULL,                        -- 会话 ID
    state        TEXT NOT NULL DEFAULT 'planning',     -- 状态机：planning/acting/awaiting_approval/awaiting_input/done/failed
    mode         TEXT NOT NULL DEFAULT 'build',         -- 模式：plan/build
    steps        INTEGER NOT NULL DEFAULT 0,            -- 已走步数
    tokens_spent INTEGER NOT NULL DEFAULT 0,            -- token 消耗
    messages     TEXT NOT NULL DEFAULT '[]',           -- JSON: llm.Message 数组（可续传对话）
    pending_tool TEXT NOT NULL DEFAULT '',              -- JSON: 待执行工具（空=无）
    plan         TEXT NOT NULL DEFAULT '',              -- JSON: Plan（空=无）
    seen_calls   TEXT NOT NULL DEFAULT '[]',            -- JSON: 已执行工具指纹数组
    answer       TEXT NOT NULL DEFAULT '',               -- 最终答案
    error        TEXT NOT NULL DEFAULT '',               -- 错误信息
    created_at   TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at   TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_runs_session ON runs(session_id);
CREATE INDEX IF NOT EXISTS idx_runs_state ON runs(state);

-- 更新触发器：自动更新 updated_at
CREATE TRIGGER IF NOT EXISTS runs_au AFTER UPDATE ON runs
BEGIN
    UPDATE runs SET updated_at = datetime('now') WHERE id = old.id;
END;

-- todo_items 表：运行中的 TODO 项（计划项）
CREATE TABLE IF NOT EXISTS todo_items (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id     TEXT NOT NULL,                           -- 所属 Run ID
    content    TEXT NOT NULL,                           -- TODO 内容
    status     TEXT NOT NULL DEFAULT 'pending',        -- pending/in_progress/completed
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (run_id) REFERENCES runs(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_todo_items_run ON todo_items(run_id);
