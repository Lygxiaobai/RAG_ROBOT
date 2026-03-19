package parser

import (
	"fmt"
	"os"
)

type TxtParser struct{}

func (p *TxtParser) Parse(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("读取文件失败: %w", err)
	}
	return string(data), nil
}

func (p *TxtParser) SupportedTypes() []string {
	return []string{"txt", "md"}
}
