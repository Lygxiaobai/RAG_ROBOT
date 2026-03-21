package parser

import (
	"strings"
)

// Chunker 文档分块器
type Chunker struct {
	ChunkSize    int // 每块最大字符数
	ChunkOverlap int // 重叠字符数
}

// NewChunker 创建分块器
func NewChunker(chunkSize, chunkOverlap int) *Chunker {
	if chunkSize <= 0 {
		chunkSize = 500
	}
	if chunkOverlap < 0 {
		chunkOverlap = 100
	}
	if chunkOverlap >= chunkSize {
		chunkOverlap = chunkSize / 5
	}
	return &Chunker{
		ChunkSize:    chunkSize,
		ChunkOverlap: chunkOverlap,
	}
}

// Split 将文本分割成多个块
func (c *Chunker) Split(text string) []string {
	// 1. 先按段落分割（尊重自然语义边界）
	paragraphs := splitByParagraph(text)

	var chunks []string
	var currentChunk strings.Builder

	for _, para := range paragraphs {
		//去掉字符串开头和结尾的字符
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}

		// 单个段落本身超过 ChunkSize，按字符强制切分后再处理
		if len([]rune(para)) > c.ChunkSize {
			subChunks := splitBySize(para, c.ChunkSize, c.ChunkOverlap)
			for _, sub := range subChunks {
				chunks = append(chunks, sub)
			}
			continue
		}

		// 如果当前块 + 新段落 超过限制，先保存当前块（用 rune 计数，正确处理中文）
		if len([]rune(currentChunk.String()))+len([]rune(para)) > c.ChunkSize && currentChunk.Len() > 0 {
			chunks = append(chunks, currentChunk.String())

			// 重叠：保留当前块末尾的一部分作为下一块的开头
			overlap := getOverlap(currentChunk.String(), c.ChunkOverlap)
			currentChunk.Reset()
			currentChunk.WriteString(overlap)
		}

		currentChunk.WriteString(para)
		currentChunk.WriteString("\n")
	}

	// 保存最后一块
	if currentChunk.Len() > 0 {
		chunks = append(chunks, strings.TrimSpace(currentChunk.String()))
	}

	return chunks
}

// splitByParagraph 按空行分割段落，兼容 Linux(\n\n) 和 Windows(\r\n\r\n) 换行符
func splitByParagraph(text string) []string {
	// 统一将 \r\n 替换为 \n，再按 \n\n 分割
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return strings.Split(text, "\n\n")
}

// getOverlap 取文本末尾 n 个字符作为重叠部分
func getOverlap(text string, n int) string {
	runes := []rune(text) // 用 rune 正确处理中文
	if len(runes) <= n {
		return text
	}
	return string(runes[len(runes)-n:])
}

// splitBySize 对超长单段落按字符数强制切分（带重叠）
func splitBySize(text string, size, overlap int) []string {
	runes := []rune(text)
	var chunks []string
	for start := 0; start < len(runes); start += size - overlap {
		end := start + size
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[start:end]))
		if end == len(runes) {
			break
		}
	}
	return chunks
}
