package openai

import (
	"context"
	"fmt"
	"github.com/sashabaranov/go-openai"
	"rag_robot/internal/pkg/config"
)

// EmbeddingClient 向量化客户端
type EmbeddingClient struct {
	client *openai.Client
	model  openai.EmbeddingModel
}

// NewEmbeddingClient 创建向量化客户端
func NewEmbeddingClient(cfg config.OpenAIConfig) *EmbeddingClient {
	clientConfig := openai.DefaultConfig(cfg.APIKey)
	// 支持自定义 BaseURL（国内代理/中转）
	if cfg.BaseURL != "" {
		clientConfig.BaseURL = cfg.BaseURL
	}

	return &EmbeddingClient{
		client: openai.NewClientWithConfig(clientConfig),
		model:  openai.AdaEmbeddingV2, // text-embedding-ada-002，1536维
	}
}

// GetEmbedding 对单个文本生成向量
func (e *EmbeddingClient) GetEmbedding(ctx context.Context, text string) ([]float32, error) {
	resp, err := e.client.CreateEmbeddings(ctx, openai.EmbeddingRequest{
		Input: []string{text},
		Model: e.model,
	})
	if err != nil {
		return nil, fmt.Errorf("调用Embeddings API失败: %w", err)
	}
	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("Embeddings API 返回空数据")
	}
	return resp.Data[0].Embedding, nil
}

// GetEmbeddingBatch 批量生成向量（减少API调用次数）
func (e *EmbeddingClient) GetEmbeddingBatch(ctx context.Context, texts []string) ([][]float32, error) {
	// OpenAI 单次最多 2048 个输入，这里做安全限制
	if len(texts) > 100 {
		return nil, fmt.Errorf("批量向量化单次最多支持100条")
	}

	resp, err := e.client.CreateEmbeddings(ctx, openai.EmbeddingRequest{
		Input: texts,
		Model: e.model,
	})
	if err != nil {
		return nil, fmt.Errorf("批量调用Embeddings API失败: %w", err)
	}

	if len(resp.Data) != len(texts) {
		return nil, fmt.Errorf("批量向量化返回数量不匹配: 期望 %d，实际 %d", len(texts), len(resp.Data))
	}

	// 注意：API 返回的顺序和输入顺序一致
	result := make([][]float32, len(resp.Data))
	for _, item := range resp.Data {
		result[item.Index] = item.Embedding
	}
	return result, nil
}
