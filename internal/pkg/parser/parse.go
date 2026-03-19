package parser

import "fmt"

// Parser 文档解析器接口
// 所有格式的解析器都必须实现这个接口
type Parser interface {
	// Parse 解析文档，返回纯文本内容
	Parse(filePath string) (string, error)
	// SupportedTypes 返回该解析器支持的文件类型
	SupportedTypes() []string
}

// GetParser 根据文件类型获取对应的解析器（工厂函数）
func GetParser(fileType string) (Parser, error) {
	switch fileType {
	case "txt", "md":
		return &TxtParser{}, nil
	case "pdf":
		return &PdfParser{}, nil
	case "docx":
		return &WordParser{}, nil
	default:
		return nil, fmt.Errorf("不支持的文件类型: %s", fileType)
	}
}
