-- ============================================================
-- 清空 rag_robot 数据库所有表数据
-- 注意：按照外键依赖关系的反向顺序删除，避免外键约束错误
-- 日期：2026-03-21
-- ============================================================

USE rag_robot;

-- 临时禁用外键检查（更安全的方式）
SET FOREIGN_KEY_CHECKS = 0;

-- 清空所有表数据（按依赖关系反向删除）
TRUNCATE TABLE qa_feedback;
TRUNCATE TABLE qa_sources;
TRUNCATE TABLE qa_records;
TRUNCATE TABLE messages;
TRUNCATE TABLE conversations;
TRUNCATE TABLE ingestion_tasks;
TRUNCATE TABLE document_chunks;
TRUNCATE TABLE documents;
TRUNCATE TABLE knowledge_bases;
TRUNCATE TABLE users;
TRUNCATE TABLE organizations;

-- 重新启用外键检查
SET FOREIGN_KEY_CHECKS = 1;

-- 验证清空结果
SELECT 'qa_feedback' AS table_name, COUNT(*) AS row_count FROM qa_feedback
UNION ALL
SELECT 'qa_sources', COUNT(*) FROM qa_sources
UNION ALL
SELECT 'qa_records', COUNT(*) FROM qa_records
UNION ALL
SELECT 'messages', COUNT(*) FROM messages
UNION ALL
SELECT 'conversations', COUNT(*) FROM conversations
UNION ALL
SELECT 'ingestion_tasks', COUNT(*) FROM ingestion_tasks
UNION ALL
SELECT 'document_chunks', COUNT(*) FROM document_chunks
UNION ALL
SELECT 'documents', COUNT(*) FROM documents
UNION ALL
SELECT 'knowledge_bases', COUNT(*) FROM knowledge_bases
UNION ALL
SELECT 'users', COUNT(*) FROM users
UNION ALL
SELECT 'organizations', COUNT(*) FROM organizations;

-- ============================================================
-- 执行完成后所有表的 row_count 应该都是 0
-- ============================================================
