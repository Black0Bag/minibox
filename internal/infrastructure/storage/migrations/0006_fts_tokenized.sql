-- 0006_fts_tokenized.sql — jieba 预分词索引修复
-- 目的：保留 kb_store.content 原文，同时把预分词文本写入 FTS5。
-- 依据：SQLite FTS5 external-content/trigger 约束 + 项目中文分词设计。
-- 迁移策略：保留主表数据，重建普通 FTS5 表；启动时由 Go 层用同一 tokenizer 回填索引。

ALTER TABLE kb_store ADD COLUMN tokenized_content TEXT NOT NULL DEFAULT '';

DROP TRIGGER IF EXISTS kb_store_ai;
DROP TRIGGER IF EXISTS kb_store_ad;
DROP TRIGGER IF EXISTS kb_store_au;
DROP TABLE IF EXISTS kb_fts;

-- 普通 FTS5 表：原文与预分词内容分离，避免 external-content 表读取原文导致索引词不一致。
CREATE VIRTUAL TABLE kb_fts USING fts5(
    content,
    tags,
    tokenize='unicode61 remove_diacritics 2'
);

-- 兼容绕过 Store 直接写入的旧调用：tokenized_content 为空时暂用原文，
-- 启动回填会把历史行替换为 jieba 预分词内容。
CREATE TRIGGER kb_store_ai AFTER INSERT ON kb_store BEGIN
    INSERT INTO kb_fts(rowid, content, tags)
    VALUES (new.rowid, COALESCE(NULLIF(new.tokenized_content, ''), new.content), new.tags);
END;

CREATE TRIGGER kb_store_ad AFTER DELETE ON kb_store BEGIN
    DELETE FROM kb_fts WHERE rowid = old.rowid;
END;

CREATE TRIGGER kb_store_au AFTER UPDATE ON kb_store BEGIN
    DELETE FROM kb_fts WHERE rowid = old.rowid;
    INSERT INTO kb_fts(rowid, content, tags)
    VALUES (new.rowid, COALESCE(NULLIF(new.tokenized_content, ''), new.content), new.tags);
END;
