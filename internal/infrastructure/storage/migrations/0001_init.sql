-- 0001_init.sql — minibox 初始建表（最小集）
-- 依据：设计阶段第 1 项数据库设计（进度跟踪文档 20260810）
-- 本迁移先建核心表，其余表（conversation_log/todo_items/system_config/等）后续迁移追加

-- ========== schema_meta：schema 版本 + embedding 模型记录 ==========
CREATE TABLE IF NOT EXISTS schema_meta (
    id              INTEGER PRIMARY KEY CHECK (id = 1),
    schema_version  INTEGER NOT NULL DEFAULT 1,
    embedding_model TEXT,
    embedding_dim   INTEGER,
    migrated_at     TEXT NOT NULL DEFAULT (datetime('now'))
);

-- ========== kb_store：正式知识柜（编译沉淀后的真知识） ==========
CREATE TABLE IF NOT EXISTS kb_store (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    content         TEXT NOT NULL,
    source          TEXT,
    tags            TEXT,
    source_hash     TEXT UNIQUE,              -- 内容哈希，防重复入库（幂等）
    importance      REAL NOT NULL DEFAULT 0.5, -- 重要性 0.0-1.0
    access_count    INTEGER NOT NULL DEFAULT 0, -- LRU 降权用
    last_accessed_at TEXT,
    created_at      TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at      TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_kb_store_importance ON kb_store(importance DESC);
CREATE INDEX IF NOT EXISTS idx_kb_store_created ON kb_store(created_at DESC);

-- ========== kb_cache：临时抽屉（未沉淀片段，强制 TTL） ==========
CREATE TABLE IF NOT EXISTS kb_cache (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    content         TEXT NOT NULL,
    source          TEXT,
    tags            TEXT,
    source_hash     TEXT,
    importance      REAL NOT NULL DEFAULT 0.5,
    access_count    INTEGER NOT NULL DEFAULT 0,
    last_accessed_at TEXT,
    expires_at      TEXT NOT NULL,            -- 强制 TTL（临时内容必须有过期）
    created_at      TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at      TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_kb_cache_expires ON kb_cache(expires_at);

-- ========== kb_fts：FTS5 全文索引（外部内容表 + 触发器同步） ==========
-- 中文分词方案：纯 Go jieba 预分词，FTS5 用 unicode61 切空格
CREATE VIRTUAL TABLE IF NOT EXISTS kb_fts USING fts5(
    content, tags,
    content='kb_store',
    content_rowid='rowid',
    tokenize='unicode61 remove_diacritics 2'
);

-- kb_store → kb_fts 同步触发器
CREATE TRIGGER IF NOT EXISTS kb_store_ai AFTER INSERT ON kb_store BEGIN
    INSERT INTO kb_fts(rowid, content, tags) VALUES (new.rowid, new.content, new.tags);
END;
CREATE TRIGGER IF NOT EXISTS kb_store_ad AFTER DELETE ON kb_store BEGIN
    INSERT INTO kb_fts(kb_fts, rowid, content, tags) VALUES ('delete', old.rowid, old.content, old.tags);
END;
CREATE TRIGGER IF NOT EXISTS kb_store_au AFTER UPDATE ON kb_store BEGIN
    INSERT INTO kb_fts(kb_fts, rowid, content, tags) VALUES ('delete', old.rowid, old.content, old.tags);
    INSERT INTO kb_fts(rowid, content, tags) VALUES (new.rowid, new.content, new.tags);
END;

-- ========== kb_vec：向量索引（sqlite-vec vec0 虚表，1024 维 float32） ==========
-- 说明：modernc.org/sqlite 原生支持 sqlite-vec（空白导入 _ "modernc.org/sqlite/vec"）
-- 决策记录：sqlite-vec pure-Go 兼容性已解决（Gorse 2026-03 实证 + 本地 vec_version 验证 v0.1.9）
CREATE VIRTUAL TABLE IF NOT EXISTS kb_vec USING vec0(
    embedding float[1024]
);

-- ========== kb_snapshots：快照表（VACUUM INTO 版本管理） ==========
CREATE TABLE IF NOT EXISTS kb_snapshots (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    snapshot_id TEXT NOT NULL UNIQUE,
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    size_bytes  INTEGER NOT NULL DEFAULT 0,
    path        TEXT NOT NULL
);

-- ========== kb_preferences：偏好表（概率蒸馏结果） ==========
CREATE TABLE IF NOT EXISTS kb_preferences (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    key             TEXT NOT NULL,
    value           TEXT NOT NULL,
    probability     REAL NOT NULL DEFAULT 0.0,
    evidence_count  INTEGER NOT NULL DEFAULT 0,
    last_hit_at     TEXT,
    created_at      TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(key)
);

-- ========== 初始化 schema_meta ==========
INSERT OR IGNORE INTO schema_meta (id, schema_version) VALUES (1, 1);
