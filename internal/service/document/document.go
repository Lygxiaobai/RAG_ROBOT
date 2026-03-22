package document

import (
	"context"
	"crypto/md5"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"
	"rag_robot/internal/model"
	"rag_robot/internal/pkg/logger"
	"rag_robot/internal/pkg/openai"
	"rag_robot/internal/pkg/parser"
	"rag_robot/internal/repository/database"
	"rag_robot/internal/repository/qdrant"
)

type Service struct {
	docRepo      *database.DocumentRepo
	embedClient  *openai.EmbeddingClient
	qdrantClient *qdrant.Client
	chunker      *parser.Chunker
	uploadDir    string
}

func NewService(
	docRepo *database.DocumentRepo,
	embedClient *openai.EmbeddingClient,
	qdrantClient *qdrant.Client,
) *Service {
	return &Service{
		docRepo:      docRepo,
		embedClient:  embedClient,
		qdrantClient: qdrantClient,
		chunker:      parser.NewChunker(500, 100),
		uploadDir:    "uploads",
	}
}

// ProcessDocument 处理文档的完整流程：保存 -> 解析 -> 分块 -> 向量化 -> 入库。
func (s *Service) ProcessDocument(ctx context.Context, file io.Reader, fileName string, fileSize int64, kbID int64) (*model.UploadDocumentResponse, error) {
	fileType := getFileType(fileName)
	if fileType == "" {
		return nil, fmt.Errorf("不支持的文件类型: %s", filepath.Ext(fileName))
	}

	filePath, contentHash, err := s.saveFile(file, fileName)
	if err != nil {
		return nil, fmt.Errorf("保存文件失败: %w", err)
	}

	doc := &model.Document{
		KnowledgeBaseID: kbID,
		Name:            fileName,
		FileType:        fileType,
		FileSize:        fileSize,
		FilePath:        filePath,
		ContentHash:     contentHash,
	}

	docID, created, err := s.docRepo.CreateDocument(ctx, doc)
	if err != nil {
		_ = os.Remove(filePath)
		return nil, fmt.Errorf("创建文档记录失败: %w", err)
	}
	doc.ID = docID

	if !created {
		// 命中 content_hash 去重：同一知识库中已存在相同内容的文档
		// 策略：直接复用已有文档，保留历史问答记录和用户反馈
		existingDoc, getErr := s.docRepo.GetDocumentByID(ctx, docID)
		_ = os.Remove(filePath) // 删除新上传的文件（因为要复用旧的）
		if getErr != nil {
			return nil, fmt.Errorf("查询已存在文档失败: %w", getErr)
		}
		if existingDoc == nil {
			return nil, fmt.Errorf("重复上传命中已存在文档，但未找到文档记录: %d", docID)
		}

		logger.Info("检测到重复上传，直接复用已有文档",
			zap.Int64("doc_id", existingDoc.ID),
			zap.Int64("knowledge_base_id", existingDoc.KnowledgeBaseID),
			zap.String("status", existingDoc.Status),
			zap.Int("chunk_count", existingDoc.ChunkCount))

		// 如果旧文档处理失败，则重新处理
		if existingDoc.Status == "failed" || existingDoc.Status == "processing" {
			logger.Info("旧文档状态异常，尝试重新处理",
				zap.Int64("doc_id", docID),
				zap.String("old_status", existingDoc.Status))

			// 删除旧的失败数据
			if existingDoc.Status == "failed" {
				_ = s.qdrantClient.DeleteByDocumentID(ctx, docID)
				_ = s.docRepo.DeleteChunksByDocumentID(ctx, docID)
			}

			// 继续后续处理流程（不 return）
		} else {
			// 文档已完成处理，直接返回
			return &model.UploadDocumentResponse{
				DocumentID: existingDoc.ID,
				Name:       existingDoc.Name,
				FileType:   existingDoc.FileType,
				FileSize:   existingDoc.FileSize,
				Status:     existingDoc.Status,
				ChunkCount: existingDoc.ChunkCount,
			}, nil
		}
	}

	_ = s.docRepo.UpdateDocumentStatus(ctx, docID, "processing", 0)

	p, err := parser.GetParser(fileType)
	if err != nil {
		_ = s.docRepo.UpdateDocumentStatus(ctx, docID, "failed", 0)
		return nil, err
	}
	text, err := p.Parse(filePath)
	if err != nil {
		_ = s.docRepo.UpdateDocumentStatus(ctx, docID, "failed", 0)
		return nil, fmt.Errorf("解析文档失败: %w", err)
	}
	logger.Info("文档解析完成", zap.Int64("doc_id", docID), zap.Int("text_length", len(text)))

	rawChunks := s.chunker.Split(text)
	logger.Info("文档分块完成",
		zap.Int64("doc_id", docID),
		zap.Int("chunk_count", len(rawChunks)),
		zap.Int("text_length", len([]rune(text))),
		zap.Int("avg_chunk_size", func() int {
			if len(rawChunks) == 0 {
				return 0
			}
			total := 0
			for _, c := range rawChunks {
				total += len([]rune(c))
			}
			return total / len(rawChunks)
		}()))

	embeddings, err := s.batchEmbedding(ctx, rawChunks)
	if err != nil {
		_ = s.docRepo.UpdateDocumentStatus(ctx, docID, "failed", 0)
		return nil, fmt.Errorf("向量化失败: %w", err)
	}

	var dbChunks []*model.DocumentChunk
	for i, content := range rawChunks {
		dbChunks = append(dbChunks, &model.DocumentChunk{
			DocumentID:      docID,
			KnowledgeBaseID: kbID,
			ChunkIndex:      i,
			Content:         content,
		})
	}

	if err = s.docRepo.BatchCreateChunks(ctx, dbChunks); err != nil {
		_ = s.docRepo.UpdateDocumentStatus(ctx, docID, "failed", 0)
		return nil, fmt.Errorf("保存分块失败: %w", err)
	}

	qdrantPoints := make([]*qdrant.ChunkPoint, 0, len(dbChunks))
	for i, chunk := range dbChunks {
		// pointID 继续沿用 document_id + chunk_index 的稳定组合，保证向量点幂等可覆盖。
		pointID := uint64(docID)*100000 + uint64(i)
		qdrantPoints = append(qdrantPoints, &qdrant.ChunkPoint{
			ID:              pointID,
			ChunkID:         chunk.ID,
			Vector:          embeddings[i],
			DocumentID:      docID,
			KnowledgeBaseID: kbID,
			ChunkIndex:      i,
			Content:         chunk.Content,
		})
	}
	if err = s.qdrantClient.UpsertPoints(ctx, qdrantPoints); err != nil {
		_ = s.docRepo.UpdateDocumentStatus(ctx, docID, "failed", 0)
		return nil, fmt.Errorf("写入向量数据库失败: %w", err)
	}
	logger.Info("向量写入 Qdrant 完成", zap.Int64("doc_id", docID), zap.Int("point_count", len(qdrantPoints)))

	// 回写 qdrant_point_id 到 document_chunks，便于后续追踪分块和向量点的映射关系。
	for _, p := range qdrantPoints {
		pointID := fmt.Sprintf("%d", p.ID)
		if err = s.docRepo.UpdateChunkQdrantID(ctx, p.ChunkID, pointID); err != nil {
			logger.Error("回写 qdrant_point_id 失败", zap.Int64("chunk_id", p.ChunkID), zap.Error(err))
		}
	}

	_ = s.docRepo.UpdateDocumentStatus(ctx, docID, "completed", len(dbChunks))
	logger.Info("文档处理完成", zap.Int64("doc_id", docID))

	return &model.UploadDocumentResponse{
		DocumentID: docID,
		Name:       fileName,
		FileType:   fileType,
		FileSize:   fileSize,
		Status:     "completed",
		ChunkCount: len(dbChunks),
	}, nil
}

// batchEmbedding 分批向量化文本。
func (s *Service) batchEmbedding(ctx context.Context, texts []string) ([][]float32, error) {
	const batchSize = 50
	var allEmbeddings [][]float32

	for i := 0; i < len(texts); i += batchSize {
		end := i + batchSize
		if end > len(texts) {
			end = len(texts)
		}
		batch := texts[i:end]

		embeddings, err := s.embedClient.GetEmbeddingBatch(ctx, batch)
		if err != nil {
			return nil, err
		}
		allEmbeddings = append(allEmbeddings, embeddings...)
	}
	return allEmbeddings, nil
}

// saveFile 保存文件到本地，返回文件路径和内容哈希。
func (s *Service) saveFile(file io.Reader, fileName string) (string, string, error) {
	dir := filepath.Join(s.uploadDir, time.Now().Format("2006/01/02"))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", "", err
	}

	ext := filepath.Ext(fileName)
	uniqueName := fmt.Sprintf("%d%s", time.Now().UnixNano(), ext)
	filePath := filepath.Join(dir, uniqueName)

	dst, err := os.Create(filePath)
	if err != nil {
		return "", "", err
	}
	defer dst.Close()

	h := md5.New()
	w := io.MultiWriter(dst, h)
	if _, err = io.Copy(w, file); err != nil {
		dst.Close()
		_ = os.Remove(filePath)
		return "", "", err
	}

	hash := fmt.Sprintf("%x", h.Sum(nil))
	return filePath, hash, nil
}

// getFileType 根据文件名获取支持的文件类型。
func getFileType(fileName string) string {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(fileName), "."))
	switch ext {
	case "txt", "md", "pdf", "docx":
		return ext
	default:
		return ""
	}
}

// DeleteDocument 删除文档：先删向量，再删数据库和本地文件。
func (s *Service) DeleteDocument(ctx context.Context, docID int64) error {
	doc, err := s.docRepo.GetDocumentByID(ctx, docID)
	if err != nil {
		return fmt.Errorf("查询文档失败: %w", err)
	}
	if doc == nil {
		return fmt.Errorf("文档不存在: id=%d", docID)
	}

	if err = s.qdrantClient.DeleteByDocumentID(ctx, docID); err != nil {
		return fmt.Errorf("删除向量数据失败: %w", err)
	}

	_ = os.Remove(doc.FilePath)

	if err = s.docRepo.DeleteDocument(ctx, docID); err != nil {
		return fmt.Errorf("删除数据库记录失败: %w", err)
	}

	logger.Info("文档删除完成", zap.Int64("doc_id", docID))
	return nil
}
