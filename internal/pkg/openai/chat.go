package openai

import (
	"context"
	"fmt"
	"io"

	"github.com/sashabaranov/go-openai"
	"rag_robot/internal/pkg/config"
)

type ChatClient struct {
	client *openai.Client
	model  string
}

func (c *ChatClient) Model() string {
	return c.model
}

func NewChatClient(cfg config.OpenAIConfig) *ChatClient {
	clientConfig := openai.DefaultConfig(cfg.APIKey)
	if cfg.BaseURL != "" {
		clientConfig.BaseURL = cfg.BaseURL
	}
	model := cfg.Model
	if model == "" {
		model = "gpt-3.5-turbo"
	}
	return &ChatClient{
		client: openai.NewClientWithConfig(clientConfig),
		model:  model,
	}
}

// Complete 普通问答，返回完整答案字符串
func (c *ChatClient) Complete(ctx context.Context, messages []openai.ChatCompletionMessage) (string, error) {
	resp, err := c.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model:    c.model,
		Messages: messages,
	})
	if err != nil {
		return "", fmt.Errorf("调用 ChatCompletion 失败: %w", err)
	}
	//choices是GPT会给出多个候选答案
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("ChatCompletion 返回空结果")
	}
	//选择第一个答案的内容返回
	return resp.Choices[0].Message.Content, nil
}

// StreamComplete 流式问答，每生成一个 token 就调用一次 onChunk
// onChunk 返回 error 可中断流
func (c *ChatClient) StreamComplete(ctx context.Context, messages []openai.ChatCompletionMessage, onChunk func(string) error) error {

	//想openAI发起一个流式聊天
	stream, err := c.client.CreateChatCompletionStream(ctx, openai.ChatCompletionRequest{
		Model:    c.model,
		Messages: messages,
		//开启流式返回
		//如果不写 Stream: true，通常就是普通模式，要等完整结果。
		//写了它之后，模型会边生成边返回
		Stream: true,
	})
	if err != nil {
		return fmt.Errorf("创建流式请求失败: %w", err)
	}
	defer stream.Close()

	//不断从流里读取数据
	for {
		//一段会会从这里不断读出 直到读到结尾
		resp, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("流式接收失败: %w", err)
		}
		if len(resp.Choices) == 0 {
			continue
		}
		//一小块 一小块取数据
		delta := resp.Choices[0].Delta.Content
		if delta == "" {
			continue
		}
		if err = onChunk(delta); err != nil {
			return err
		}
	}
}
