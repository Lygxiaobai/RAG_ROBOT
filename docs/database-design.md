# 数据库设计

## 设计目标

这套表结构是按当前 `RAG_ROBOT` 项目的开发计划书来设计的，重点覆盖下面几块能力：

- B2B SaaS 场景下的组织隔离
- 知识库和文档管理
- 文档切片与向量索引映射
- 多轮对话与问答记录
- 结果追踪和用户反馈

当前项目已经接入了 MySQL 连接池，所以这里先以 MySQL 8 为目标数据库。

## 设计原则

- 关系型数据库只保存业务元数据，不保存向量本体
- 向量数据继续放到 Qdrant，MySQL 只保存 `qdrant_point_id`
- 所有核心业务表都带 `created_at` / `updated_at`
- 需要级联清理的子表使用外键级联删除
- 先保证第一版能支撑后续开发，再逐步细化权限和审计

## 核心实体

### 1. organizations

组织表，对应 B2B SaaS 的租户概念。后续所有文档、用户、知识库、会话都挂在组织下面。

### 2. users

用户表，保存组织成员信息。即使你现在还没做登录，这张表也值得先留好，因为上传人、会话发起人、反馈人都要引用它。

### 3. knowledge_bases

知识库表。一个组织可以有多个知识库，比如“人事制度库”“技术文档库”“售后知识库”。

这里额外存了：

- `embedding_provider`
- `embedding_model`
- `vector_collection`

这样后面切换向量模型或切换 collection 时更容易追踪。

### 4. documents

文档主表，保存上传后的元数据，不直接承担向量检索。

重点字段：

- `parse_status`：解析流程状态
- `content_hash`：去重依据
- `chunk_count`：切片数量
- `token_count`：整体 token 数
- `error_message`：失败原因

### 5. document_chunks

文档切片表，是 RAG 的核心桥梁之一。它负责把“一个文档”拆成“多个可检索片段”。

重点字段：

- `chunk_index`：切片在文档中的顺序
- `content`：切片文本
- `qdrant_point_id`：和 Qdrant 中向量点的映射
- `metadata_json`：保留可扩展元数据

### 6. ingestion_tasks

文档入库任务表，用来跟踪解析、embedding、索引等异步步骤。

这张表能帮助你以后做：

- 上传后异步处理
- 重试失败任务
- 展示任务进度

### 7. conversations

对话会话表，对应一次聊天上下文。

重点字段：

- `session_id`：前后端交互时更方便用字符串会话标识
- `kb_id`：这次会话主要依赖哪个知识库
- `last_question_at`：便于做会话排序和归档

### 8. messages

消息表，保存多轮对话内容。

这里把 `retrieval_json` 也留出来了，方便以后追踪“本轮回答用了哪些检索上下文”。

### 9. qa_records

问答记录表，是“检索 + 大模型回答”这一动作的审计主表。

重点字段：

- `question_text`
- `answer_text`
- `answer_model`
- `answer_latency_ms`
- `retrieval_count`
- `top_score`
- `status`

后面做统计报表、缓存命中分析、效果评估时，这张表会很有用。

### 10. qa_sources

问答引用来源表，保存一次回答具体引用了哪些文档切片。

它的作用很重要：

- 给前端展示“答案来源”
- 做问答可解释性
- 回头检查召回质量

### 11. qa_feedback

问答反馈表，用于收集用户对答案的满意度。

这是后面做评估闭环的基础表。

## 当前最先会用到的表

如果按你的开发阶段继续往下写，最先真正用上的一般是这几张：

- `documents`
- `document_chunks`
- `ingestion_tasks`
- `conversations`
- `messages`
- `qa_records`

`organizations`、`users`、`knowledge_bases` 虽然现在不一定立刻写接口，但先设计出来能避免后期返工。

## 初始化方式

初始化 SQL 放在：

- `scripts/init_mysql.sql`

你可以先在本地 MySQL 执行这份脚本，先把 `rag_boot` 库和表都建起来。这样当前服务启动时，至少不会再因为库不存在而报：

- `Unknown database 'rag_boot'`

## 一个重要提醒

你当前配置文件里数据库名是 `rag_boot`。从项目名看，它也可能本来想写成 `rag_robot`。我这次为了和你现有配置保持一致，SQL 也用了 `rag_boot`。如果你想统一命名，后面可以一起改：

- `configs/config.dev.yaml`
- `configs/config.yaml`
- `scripts/init_mysql.sql`

## 下一步建议

设计完库以后，最适合继续往下做的是：

1. 先执行 `scripts/init_mysql.sql`
2. 写一个最小的数据库健康检查
3. 先建 `documents` 的 Go model 和 repository
4. 再做文档上传接口
5. 再做文档切片和入库任务
