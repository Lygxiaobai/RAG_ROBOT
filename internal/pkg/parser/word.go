package parser

import (
	"fmt"
	"github.com/nguyenthenguyen/docx"
)

type WordParser struct{}

func (p *WordParser) Parse(filePath string) (string, error) {
	r, err := docx.ReadDocxFile(filePath)
	if err != nil {
		return "", fmt.Errorf("打开Word文件失败: %w", err)
	}
	defer r.Close()

	doc := r.Editable()
	return doc.GetContent(), nil
}

func (p *WordParser) SupportedTypes() []string {
	return []string{"docx"}
}
