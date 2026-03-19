package parser

import (
	"bytes"
	"fmt"
	"github.com/ledongthuc/pdf"
)

type PdfParser struct{}

func (p *PdfParser) Parse(filePath string) (string, error) {
	// 打开 PDF 文件
	f, reader, err := pdf.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("打开PDF失败: %w", err)
	}
	defer f.Close()

	var buf bytes.Buffer
	totalPages := reader.NumPage()

	// 逐页提取文本
	for i := 1; i <= totalPages; i++ {
		page := reader.Page(i)
		if page.V.IsNull() {
			continue
		}
		text, err := page.GetPlainText(nil)
		if err != nil {
			// 单页解析失败不中断，继续下一页
			continue
		}
		buf.WriteString(text)
		buf.WriteString("\n") // 页与页之间加换行
	}

	return buf.String(), nil
}

func (p *PdfParser) SupportedTypes() []string {
	return []string{"pdf"}
}
