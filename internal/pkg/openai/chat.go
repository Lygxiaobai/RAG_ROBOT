package openai

import (
	"context"
	"fmt"
	"io"

	"github.com/sashabaranov/go-openai"
	"rag_robot/internal/pkg/circuitbreaker"
	"rag_robot/internal/pkg/config"
)

type ChatClient struct {
	client  *openai.Client
	model   string
	breaker *circuitbreaker.CircuitBreaker
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

// WithBreaker 挂载熔断器，返回自身方便链式调用。
func (c *ChatClient) WithBreaker(cb *circuitbreaker.CircuitBreaker) *ChatClient {
	c.breaker = cb
	return c
}

// Complete 普通问答，返回完整答案字符串
func (c *ChatClient) Complete(ctx context.Context, messages []openai.ChatCompletionMessage) (string, error) {
	var answer string
	call := func() error {
		resp, err := c.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
			Model:    c.model,
			Messages: messages,
		})
		if err != nil {
			return fmt.Errorf("调用 ChatCompletion 失败: %w", err)
		}
		if len(resp.Choices) == 0 {
			return fmt.Errorf("ChatCompletion 返回空结果")
		}
		answer = resp.Choices[0].Message.Content
		return nil
	}

	if c.breaker != nil {
		if err := c.breaker.Call(call); err != nil {
			return "", err
		}
		return answer, nil
	}
	if err := call(); err != nil {
		return "", err
	}
	return answer, nil
}

// StreamComplete 流式问答，每生成一个 token 就调用一次 onChunk
// onChunk 返回 error 可中断流
func (c *ChatClient) StreamComplete(ctx context.Context, messages []openai.ChatCompletionMessage, onChunk func(string) error) error {
	call := func() error {
		stream, err := c.client.CreateChatCompletionStream(ctx, openai.ChatCompletionRequest{
			Model:    c.model,
			Messages: messages,
			Stream:   true,
		})
		if err != nil {
			return fmt.Errorf("创建流式请求失败: %w", err)
		}
		defer stream.Close()

		for {
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
			delta := resp.Choices[0].Delta.Content
			if delta == "" {
				continue
			}
			if err = onChunk(delta); err != nil {
				return err
			}
		}
	}

	if c.breaker != nil {
		return c.breaker.Call(call)
	}
	return call()
}
