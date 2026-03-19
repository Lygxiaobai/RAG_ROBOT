-- ============================================================
-- RAG_ROBOT 数据库初始化脚本
-- 适配版本: 第二阶段完成后（文档上传 + 解析 + 分块 + 向量化已落库）
-- 更新日期: 2026-03-19
-- ============================================================

CREATE DATABASE IF NOT EXISTS rag_robot
  DEFAULT CHARACTER SET utf8mb4
  DEFAULT COLLATE utf8mb4_unicode_ci;

USE rag_robot;

SET NAMES utf8mb4;

-- ============================================================
-- 1. documents 表
--    对应 internal/model/document.go -> Document 结构体
--    对应 internal/repository/database/document_repo.go 的 INSERT/UPDATE 语句
-- ============================================================
CREATE TABLE IF NOT EXISTS documents (
  id               BIGINT UNSIGNED  NOT NULL AUTO_INCREMENT        COMMENT '文档ID',
  knowledge_base_id BIGINT UNSIGNED NOT NULL                       COMMENT '所属知识库ID（第三阶段前为业务分组标识）',
  name             VARCHAR(255)     NOT NULL                       COMMENT '原始文件名',
  file_type        VARCHAR(32)      NOT NULL                       COMMENT '文件类型: pdf/word/txt/md',
  file_size        BIGINT UNSIGNED  NOT NULL DEFAULT 0             COMMENT '文件大小(字节)',
  file_path        VARCHAR(500)     DEFAULT NULL                   COMMENT '服务器存储路径',
  status           VARCHAR(32)      NOT NULL DEFAULT 'pending'     COMMENT '解析状态: pending/processing/completed/failed',
  chunk_count      INT UNSIGNED     NOT NULL DEFAULT 0             COMMENT '切片数量',
  content_hash     CHAR(64)         DEFAULT NULL                   COMMENT '文件内容MD5，用于去重',
  error_message    VARCHAR(1000)    DEFAULT NULL                   COMMENT '失败原因',
  created_at       DATETIME         NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  updated_at       DATETIME         NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  deleted_at       DATETIME         DEFAULT NULL                   COMMENT '软删除时间',
  PRIMARY KEY (id),
  UNIQUE  KEY uk_documents_content_hash  (content_hash),
  KEY             idx_documents_kb_id    (knowledge_base_id),
  KEY             idx_documents_status   (status),
  KEY             idx_documents_deleted_at (deleted_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='文档主表';

-- ============================================================
-- 2. document_chunks 表
--    对应 internal/model/document.go -> DocumentChunk 结构体
--    对应 document_repo.go -> BatchCreateChunks 中的 INSERT 语句
--    字段精确匹配: document_id / knowledge_base_id / chunk_index /
--                  content / content_length / qdrant_point_id / created_at
-- ============================================================
CREATE TABLE IF NOT EXISTS document_chunks (
  id                BIGINT UNSIGNED  NOT NULL AUTO_INCREMENT       COMMENT '切片ID',
  document_id       BIGINT UNSIGNED  NOT NULL                      COMMENT '所属文档ID',
  knowledge_base_id BIGINT UNSIGNED  NOT NULL                      COMMENT '所属知识库ID',
  chunk_index       INT UNSIGNED     NOT NULL                      COMMENT '切片顺序（从0开始）',
  content           MEDIUMTEXT       NOT NULL                      COMMENT '切片文本内容',
  content_length    INT UNSIGNED     NOT NULL DEFAULT 0            COMMENT '内容字符数（Unicode字符计数）',
  qdrant_point_id   VARCHAR(64)      DEFAULT NULL                  COMMENT 'Qdrant 向量点 ID（第三阶段写入后更新）',
  created_at        DATETIME         NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  PRIMARY KEY (id),
  UNIQUE KEY uk_chunks_doc_index       (document_id, chunk_index),
  KEY        idx_chunks_kb_id          (knowledge_base_id),
  KEY        idx_chunks_qdrant_point_id (qdrant_point_id),
  CONSTRAINT fk_chunks_document_id
    FOREIGN KEY (document_id) REFERENCES documents (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='文档切片表（RAG核心）';

-- ============================================================
-- 以下各表为后续阶段预建，当前阶段暂不使用。
-- 建表不影响第二阶段功能，方便后续阶段直接使用。
-- ============================================================

-- ============================================================
-- 3. organizations 表（第七阶段：多租户鉴权）
-- ============================================================
CREATE TABLE IF NOT EXISTS organizations (
  id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT   COMMENT '组织ID',
  org_code   VARCHAR(64)     NOT NULL                  COMMENT '组织编码',
  name       VARCHAR(128)    NOT NULL                  COMMENT '组织名称',
  status     TINYINT         NOT NULL DEFAULT 1        COMMENT '状态: 1启用 0禁用',
  created_at DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  updated_at DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  PRIMARY KEY (id),
  UNIQUE KEY uk_organizations_org_code (org_code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='组织表（第七阶段启用）';

-- ============================================================
-- 4. users 表（第七阶段：JWT鉴权）
-- ============================================================
CREATE TABLE IF NOT EXISTS users (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT  COMMENT '用户ID',
  org_id        BIGINT UNSIGNED NOT NULL                 COMMENT '所属组织ID',
  username      VARCHAR(64)     NOT NULL                 COMMENT '登录名',
  display_name  VARCHAR(128)    NOT NULL                 COMMENT '显示名',
  email         VARCHAR(128)    DEFAULT NULL             COMMENT '邮箱',
  password_hash VARCHAR(255)    DEFAULT NULL             COMMENT '密码哈希',
  role          VARCHAR(32)     NOT NULL DEFAULT 'member' COMMENT '角色: owner/admin/member',
  status        TINYINT         NOT NULL DEFAULT 1       COMMENT '状态: 1启用 0禁用',
  created_at    DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  updated_at    DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  PRIMARY KEY (id),
  UNIQUE KEY uk_users_org_username (org_id, username),
  UNIQUE KEY uk_users_email        (email),
  KEY           idx_users_org_id   (org_id),
  CONSTRAINT fk_users_org_id FOREIGN KEY (org_id) REFERENCES organizations (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='用户表（第七阶段启用）';

-- ============================================================
-- 5. knowledge_bases 表（第三阶段：Qdrant集成）
-- ============================================================
CREATE TABLE IF NOT EXISTS knowledge_bases (
  id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '知识库ID',
  org_id             BIGINT UNSIGNED DEFAULT NULL            COMMENT '所属组织ID（第七阶段关联）',
  name               VARCHAR(128)    NOT NULL                COMMENT '知识库名称',
  description        VARCHAR(500)    DEFAULT NULL            COMMENT '知识库描述',
  embedding_provider VARCHAR(64)     DEFAULT 'openai'        COMMENT '向量化提供方',
  embedding_model    VARCHAR(128)    DEFAULT NULL            COMMENT '向量模型名称',
  vector_collection  VARCHAR(128)    NOT NULL                COMMENT 'Qdrant Collection 名称',
  status             TINYINT         NOT NULL DEFAULT 1      COMMENT '状态: 1启用 0禁用',
  created_at         DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  updated_at         DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  PRIMARY KEY (id),
  UNIQUE KEY uk_kb_vector_collection (vector_collection),
  KEY        idx_kb_org_id           (org_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='知识库表（第三阶段启用）';

-- ============================================================
-- 6. ingestion_tasks 表（第二/三阶段：异步任务跟踪，可选）
-- ============================================================
CREATE TABLE IF NOT EXISTS ingestion_tasks (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '任务ID',
  document_id   BIGINT UNSIGNED DEFAULT NULL            COMMENT '关联文档ID',
  task_type     VARCHAR(32)     NOT NULL                COMMENT '任务类型: parse/embedding/index',
  status        VARCHAR(32)     NOT NULL DEFAULT 'pending' COMMENT '状态: pending/running/success/failed',
  retry_count   INT UNSIGNED    NOT NULL DEFAULT 0      COMMENT '重试次数',
  error_message VARCHAR(1000)   DEFAULT NULL            COMMENT '失败原因',
  started_at    DATETIME        DEFAULT NULL            COMMENT '开始时间',
  finished_at   DATETIME        DEFAULT NULL            COMMENT '结束时间',
  created_at    DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  updated_at    DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  PRIMARY KEY (id),
  KEY idx_ingestion_tasks_status      (status),
  KEY idx_ingestion_tasks_document_id (document_id),
  CONSTRAINT fk_ingestion_tasks_document_id
    FOREIGN KEY (document_id) REFERENCES documents (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='文档入库任务表（异步处理跟踪）';

-- ============================================================
-- 7. conversations 表（第四阶段：多轮对话）
-- ============================================================
CREATE TABLE IF NOT EXISTS conversations (
  id               BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '会话ID',
  user_id          BIGINT UNSIGNED DEFAULT NULL            COMMENT '用户ID',
  knowledge_base_id BIGINT UNSIGNED DEFAULT NULL           COMMENT '知识库ID',
  session_id       VARCHAR(64)     NOT NULL                COMMENT '会话唯一标识',
  title            VARCHAR(255)    DEFAULT NULL            COMMENT '会话标题',
  status           VARCHAR(32)     NOT NULL DEFAULT 'active' COMMENT '状态: active/archived/closed',
  last_question_at DATETIME        DEFAULT NULL            COMMENT '最后提问时间',
  created_at       DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  updated_at       DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  PRIMARY KEY (id),
  UNIQUE KEY uk_conversations_session_id (session_id),
  KEY        idx_conversations_user_id   (user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='对话会话表（第四阶段启用）';

-- ============================================================
-- 8. messages 表（第四阶段：消息历史）
-- ============================================================
CREATE TABLE IF NOT EXISTS messages (
  id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '消息ID',
  conversation_id BIGINT UNSIGNED NOT NULL                COMMENT '会话ID',
  role            VARCHAR(32)     NOT NULL                COMMENT '角色: system/user/assistant',
  content         LONGTEXT        NOT NULL                COMMENT '消息内容',
  token_count     INT UNSIGNED    NOT NULL DEFAULT 0      COMMENT '消耗token数',
  prompt_version  VARCHAR(64)     DEFAULT NULL            COMMENT 'Prompt版本',
  retrieval_json  JSON            DEFAULT NULL            COMMENT '检索上下文快照（可追溯）',
  created_at      DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  PRIMARY KEY (id),
  KEY idx_messages_conversation_id (conversation_id),
  KEY idx_messages_created_at      (created_at),
  CONSTRAINT fk_messages_conversation_id
    FOREIGN KEY (conversation_id) REFERENCES conversations (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='会话消息表（第四阶段启用）';

-- ============================================================
-- 9. qa_records 表（第四阶段：问答审计）
-- ============================================================
CREATE TABLE IF NOT EXISTS qa_records (
  id                BIGINT UNSIGNED  NOT NULL AUTO_INCREMENT COMMENT '问答记录ID',
  knowledge_base_id BIGINT UNSIGNED  DEFAULT NULL            COMMENT '知识库ID',
  conversation_id   BIGINT UNSIGNED  DEFAULT NULL            COMMENT '会话ID',
  user_id           BIGINT UNSIGNED  DEFAULT NULL            COMMENT '用户ID',
  question_text     TEXT             NOT NULL                COMMENT '用户问题',
  question_hash     CHAR(64)         DEFAULT NULL            COMMENT '问题哈希（Redis缓存Key）',
  answer_text       LONGTEXT         DEFAULT NULL            COMMENT '答案内容',
  answer_model      VARCHAR(128)     DEFAULT NULL            COMMENT '回答使用的模型',
  answer_latency_ms INT UNSIGNED     NOT NULL DEFAULT 0      COMMENT '回答耗时(ms)',
  retrieval_count   INT UNSIGNED     NOT NULL DEFAULT 0      COMMENT '召回切片数',
  top_score         DECIMAL(6,4)     DEFAULT NULL            COMMENT '最高相似度分数',
  status            VARCHAR(32)      NOT NULL DEFAULT 'success' COMMENT '状态: success/partial/failed',
  error_message     VARCHAR(1000)    DEFAULT NULL            COMMENT '失败原因',
  created_at        DATETIME         NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  PRIMARY KEY (id),
  KEY idx_qa_records_kb_id           (knowledge_base_id),
  KEY idx_qa_records_conversation_id (conversation_id),
  KEY idx_qa_records_question_hash   (question_hash)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='问答记录表（第四阶段启用）';

-- ============================================================
-- 10. qa_sources 表（第四阶段：答案溯源）
-- ============================================================
CREATE TABLE IF NOT EXISTS qa_sources (
  id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '来源记录ID',
  qa_record_id BIGINT UNSIGNED NOT NULL                COMMENT '问答记录ID',
  document_id  BIGINT UNSIGNED NOT NULL                COMMENT '来源文档ID',
  chunk_id     BIGINT UNSIGNED NOT NULL                COMMENT '来源切片ID',
  rank_no      INT UNSIGNED    NOT NULL                COMMENT '召回排序名次',
  score        DECIMAL(6,4)    DEFAULT NULL            COMMENT '相似度分数',
  created_at   DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  PRIMARY KEY (id),
  UNIQUE KEY uk_qa_sources_rank      (qa_record_id, rank_no),
  KEY        idx_qa_sources_chunk_id (chunk_id),
  CONSTRAINT fk_qa_sources_qa_record_id
    FOREIGN KEY (qa_record_id) REFERENCES qa_records (id) ON DELETE CASCADE,
  CONSTRAINT fk_qa_sources_document_id
    FOREIGN KEY (document_id) REFERENCES documents (id),
  CONSTRAINT fk_qa_sources_chunk_id
    FOREIGN KEY (chunk_id) REFERENCES document_chunks (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='问答引用来源表（第四阶段启用）';

-- ============================================================
-- 11. qa_feedback 表（第四阶段：用户反馈）
-- ============================================================
CREATE TABLE IF NOT EXISTS qa_feedback (
  id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '反馈ID',
  qa_record_id BIGINT UNSIGNED NOT NULL                COMMENT '问答记录ID',
  user_id      BIGINT UNSIGNED DEFAULT NULL            COMMENT '用户ID',
  rating       TINYINT         NOT NULL                COMMENT '评分: 1差评 2中立 3好评',
  comment      VARCHAR(1000)   DEFAULT NULL            COMMENT '反馈说明',
  created_at   DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',
  PRIMARY KEY (id),
  KEY idx_qa_feedback_qa_record_id (qa_record_id),
  CONSTRAINT fk_qa_feedback_qa_record_id
    FOREIGN KEY (qa_record_id) REFERENCES qa_records (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='问答反馈表（第四阶段启用）';
