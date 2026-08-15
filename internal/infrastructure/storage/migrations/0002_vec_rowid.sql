-- 0002_vec_rowid.sql — 重建 kb_vec 向量表（加 doc_id 主键 + tier 元数据列 + 删除同步触发器）
-- 依据：进度跟踪文档 20260810 数据库设计（模块 18）——kb_vec 用 doc_id 关联 kb_store，孤儿用触发器同步
-- 原因：0001 版 kb_vec 缺 doc_id 主键（无法 JOIN kb_store）、缺删除同步（删主表留孤儿）
-- 虚表无法 ALTER，先 DROP 重建；向量数据由编译管道重灌，重建安全

DROP TRIGGER IF EXISTS kb_store_vec_ad;
DROP TRIGGER IF EXISTS kb_store_vec_ai;
DROP TRIGGER IF EXISTS kb_store_vec_au;
DROP TRIGGER IF EXISTS kb_cache_vec_ad;
DROP TRIGGER IF EXISTS kb_cache_vec_ai;
DROP TRIGGER IF EXISTS kb_cache_vec_au;

DROP TABLE IF EXISTS kb_vec;

CREATE VIRTUAL TABLE IF NOT EXISTS kb_vec USING vec0(
    doc_id integer primary key,
    tier text,
    embedding float[1024]
);

-- kb_store → kb_vec 同步触发器（删除孤儿）
-- 注意：kb_store 插入/更新时不自动写向量——embedding 由编译管道生成后经 Store.Embed 显式写入
CREATE TRIGGER IF NOT EXISTS kb_store_vec_ad AFTER DELETE ON kb_store BEGIN
    DELETE FROM kb_vec WHERE doc_id = old.rowid;
END;

-- kb_cache → kb_vec 同步触发器（缓存区过期清理走 DELETE，自动触发）
CREATE TRIGGER IF NOT EXISTS kb_cache_vec_ad AFTER DELETE ON kb_cache BEGIN
    DELETE FROM kb_vec WHERE doc_id = old.rowid AND tier = 'cache';
END;
