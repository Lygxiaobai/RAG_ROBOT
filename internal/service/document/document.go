package document

import (
	"context"
	"crypto/md5"
	"fmt"
	"rag_robot/internal/repository/qdrant"

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
)

type Service struct {
	docRepo      *database.DocumentRepo
	embedClient  *openai.EmbeddingClient
	qdrantClient *qdrant.Client //将向量化数据插入到qdrant数据库中
	chunker      *parser.Chunker
	uploadDir    string // 文件上传保存目录
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

// ProcessDocument 处理文档的完整流程：保存 → 解析 → 分块 → 向量化 → 存库
func (s *Service) ProcessDocument(ctx context.Context, file io.Reader, fileName string, fileSize int64, kbID int64) (*model.UploadDocumentResponse, error) {
	// 1. 获取文件类型
	fileType := getFileType(fileName)
	if fileType == "" {
		return nil, fmt.Errorf("不支持的文件类型: %s", filepath.Ext(fileName))
	}

	// 2. 保存文件到本地
	filePath, contentHash, err := s.saveFile(file, fileName)
	if err != nil {
		return nil, fmt.Errorf("保存文件失败: %w", err)
	}

	// 3. 在数据库创建文档记录（状态=pending）
	doc := &model.Document{
		KnowledgeBaseID: kbID,
		Name:            fileName,
		FileType:        fileType,
		FileSize:        fileSize,
		FilePath:        filePath,
		ContentHash:     contentHash,
	}
	docID, err := s.docRepo.CreateDocument(ctx, doc)
	if err != nil {
		os.Remove(filePath)
		return nil, fmt.Errorf("创建文档记录失败: %w", err)
	}
	doc.ID = docID

	// 4. 更新状态为 processing，开始异步处理
	_ = s.docRepo.UpdateDocumentStatus(ctx, docID, "processing", 0)

	// 5. 解析文档 → 获取纯文本
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

	// 6. 文档分块
	rawChunks := s.chunker.Split(text)

	logger.Info("文档分块完成", zap.Int64("doc_id", docID), zap.Int("chunk_count", len(rawChunks)))

	// 7. 批量向量化（每批最多50个）
	embeddings, err := s.batchEmbedding(ctx, rawChunks)
	if err != nil {
		_ = s.docRepo.UpdateDocumentStatus(ctx, docID, "failed", 0)
		return nil, fmt.Errorf("向量化失败: %w", err)
	}

	// 8. 构建分块记录并批量写入 MySQL
	var dbChunks []*model.DocumentChunk
	for i, content := range rawChunks {
		dbChunks = append(dbChunks, &model.DocumentChunk{
			DocumentID:      docID,
			KnowledgeBaseID: kbID,
			ChunkIndex:      i,
			Content:         content,
		})
	}

	// 9. 批量写入数据库（获得每个 chunk 的自增 ID）
	if err = s.docRepo.BatchCreateChunks(ctx, dbChunks); err != nil {
		_ = s.docRepo.UpdateDocumentStatus(ctx, docID, "failed", 0)
		return nil, fmt.Errorf("保存分块失败: %w", err)
	}

	// 10. 批量写入 Qdrant
	qdrantPoints := make([]*qdrant.ChunkPoint, 0, len(dbChunks))
	for i, chunk := range dbChunks {
		pointID := uint64(docID)*100000 + uint64(i)
		qdrantPoints = append(qdrantPoints, &qdrant.ChunkPoint{
			ID:              pointID,
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
	logger.Info("向量写入Qdrant完成", zap.Int64("doc_id", docID), zap.Int("point_count", len(qdrantPoints)))

	// 11. 更新状态为 completed
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

// batchEmbedding 分批向量化（每批50条） 把大分片批量切成小分片返回
func (s *Service) batchEmbedding(ctx context.Context, texts []string) ([][]float32, error) {
	const batchSize = 50
	var allEmbeddings [][]float32

	//len(texts) = 200   batchSize= 50
	//end 50 100
	//i   0 50
	//第一批 0-49   50-99
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

// saveFile 保存文件到本地，返回文件路径和 MD5 哈希
func (s *Service) saveFile(file io.Reader, fileName string) (string, string, error) {
	// 按日期组织目录
	dir := filepath.Join(s.uploadDir, time.Now().Format("2006/01/02"))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", "", err
	}

	// 生成唯一文件名（防冲突）
	ext := filepath.Ext(fileName)
	uniqueName := fmt.Sprintf("%d%s", time.Now().UnixNano(), ext)
	filePath := filepath.Join(dir, uniqueName)

	dst, err := os.Create(filePath)
	if err != nil {
		return "", "", err
	}
	defer dst.Close()

	// 同时计算 MD5（边写边算）
	h := md5.New()
	w := io.MultiWriter(dst, h)
	if _, err = io.Copy(w, file); err != nil {
		dst.Close()
		os.Remove(filePath)
		return "", "", err
	}

	hash := fmt.Sprintf("%x", h.Sum(nil))
	return filePath, hash, nil
}

// getFileType 从文件名获取类型
func getFileType(fileName string) string {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(fileName), "."))
	switch ext {
	case "txt", "md", "pdf", "docx":
		return ext
	default:
		return ""
	}
}
