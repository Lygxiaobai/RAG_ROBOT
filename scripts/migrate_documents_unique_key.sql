-- ============================================================
-- 数据库迁移脚本：修改 documents 表唯一索引
-- 目的：允许同一文档存在于不同知识库中
-- 修改：UNIQUE KEY 从 (content_hash) 改为 (knowledge_base_id, content_hash)
-- 日期：2026-03-21
-- ============================================================

USE rag_robot;

-- 1. 检查当前索引
SHOW INDEX FROM documents WHERE Key_name = 'uk_documents_kb_hash' OR Key_name = 'content_hash';

-- 2. 删除旧的全局唯一索引（如果存在）
-- 注意：旧索引的名字可能是 content_hash 或其他名字，根据实际情况修改
SET @old_index_exists = (SELECT COUNT(*) FROM information_schema.statistics
                         WHERE table_schema = 'rag_robot'
                         AND table_name = 'documents'
                         AND index_name = 'content_hash');

SET @sql = IF(@old_index_exists > 0,
              'ALTER TABLE documents DROP INDEX content_hash',
              'SELECT "旧索引 content_hash 不存在，跳过删除" AS info');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

-- 3. 删除可能存在的其他 content_hash 相关索引
SET @uk_hash_exists = (SELECT COUNT(*) FROM information_schema.statistics
                       WHERE table_schema = 'rag_robot'
                       AND table_name = 'documents'
                       AND index_name = 'uk_documents_hash');

SET @sql = IF(@uk_hash_exists > 0,
              'ALTER TABLE documents DROP INDEX uk_documents_hash',
              'SELECT "旧索引 uk_documents_hash 不存在，跳过删除" AS info');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

-- 4. 检查新索引是否已存在
SET @new_index_exists = (SELECT COUNT(*) FROM information_schema.statistics
                         WHERE table_schema = 'rag_robot'
                         AND table_name = 'documents'
                         AND index_name = 'uk_documents_kb_hash');

-- 5. 如果新索引不存在，则创建
SET @sql = IF(@new_index_exists = 0,
              'ALTER TABLE documents ADD UNIQUE KEY uk_documents_kb_hash (knowledge_base_id, content_hash)',
              'SELECT "新索引 uk_documents_kb_hash 已存在，跳过创建" AS info');
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

-- 6. 验证索引修改结果
SELECT
    table_name,
    index_name,
    GROUP_CONCAT(column_name ORDER BY seq_in_index) AS columns,
    non_unique
FROM information_schema.statistics
WHERE table_schema = 'rag_robot'
  AND table_name = 'documents'
  AND (index_name LIKE '%hash%' OR index_name = 'uk_documents_kb_hash')
GROUP BY table_name, index_name, non_unique
ORDER BY index_name;

-- ============================================================
-- 执行完成后应该看到：
-- uk_documents_kb_hash | knowledge_base_id,content_hash | 0 (non_unique=0 表示唯一索引)
-- ============================================================
