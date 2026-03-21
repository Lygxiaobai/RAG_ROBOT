package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"rag_robot/internal/pkg/config"
)

// EmbeddingClient 向量化客户端（直接 HTTP，兼容千问 text-embedding-v2 只接受字符串 input）
type EmbeddingClient struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	model      string
}

// NewEmbeddingClient 创建向量化客户端
func NewEmbeddingClient(cfg config.OpenAIConfig) *EmbeddingClient {
	baseURL := "https://api.openai.com/v1"
	if cfg.BaseURL != "" {
		baseURL = strings.TrimRight(cfg.BaseURL, "/")
	}

	model := "text-embedding-ada-002"
	if cfg.EmbeddingModel != "" {
		model = cfg.EmbeddingModel
	}

	return &EmbeddingClient{
		httpClient: &http.Client{Timeout: 60 * time.Second},
		baseURL:    baseURL,
		apiKey:     cfg.APIKey,
		model:      model,
	}
}

// embeddingRequest 发给 API 的请求体（input 用 any，可以是字符串或数组）
type embeddingRequest struct {
	Model string `json:"model"`
	Input any    `json:"input"`
}

// embeddingResponse API 响应体
type embeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"error"`
}

// GetEmbedding 对单个文本生成向量，input 发送为字符串（兼容千问 text-embedding-v2）
// 原来用 go-openai SDK 发请求，SDK 把 input 序列化成数组 ["文本"]，千问 v2 只接受字符串 "文本"，所以报400。
//
//	改为直接 HTTP 请求，input 字段用 any 类型发字符串。
func (e *EmbeddingClient) GetEmbedding(ctx context.Context, text string) ([]float32, error) {
	body, err := json.Marshal(embeddingRequest{
		Model: e.model,
		Input: text, // 字符串，不是数组
	})
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		e.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("调用Embeddings API失败: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	var result embeddingResponse
	if err = json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w, body=%s", err, string(data))
	}

	if result.Error != nil {
		return nil, fmt.Errorf("Embeddings API 返回错误: %s (code=%s)", result.Error.Message, result.Error.Code)
	}

	if len(result.Data) == 0 {
		return nil, fmt.Errorf("Embeddings API 返回空数据, body=%s", string(data))
	}
	return result.Data[0].Embedding, nil
}

// GetEmbeddingBatch 批量生成向量，逐条调用兼容千问等不支持数组批量输入的接口。
func (e *EmbeddingClient) GetEmbeddingBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) > 100 {
		return nil, fmt.Errorf("批量向量化单次最多支持100条")
	}

	result := make([][]float32, len(texts))
	for i, text := range texts {
		vec, err := e.GetEmbedding(ctx, text)
		if err != nil {
			return nil, fmt.Errorf("批量调用Embeddings API失败 (index=%d): %w", i, err)
		}
		result[i] = vec
	}
	return result, nil
}
