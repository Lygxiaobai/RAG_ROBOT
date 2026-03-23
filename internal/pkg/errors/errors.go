package errors

import (
	stderrors "errors" // 标准库 errors，避免与本包名冲突
	"fmt"
	"net/http"
)

// AppError 是业务层统一错误类型
// 包含：HTTP状态码、业务错误码、错误信息、原始错误
type AppError struct {
	HTTPCode int    // HTTP 响应状态码，如 400、500
	Code     int    // 业务错误码，如 40001
	Message  string // 面向用户的错误描述
	Cause    error  // 原始错误（内部日志用，不暴露给前端）
}

// Error 实现 error 接口
func (e *AppError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%d] %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%d] %s", e.Code, e.Message)
}

// Unwrap 支持 errors.Is / errors.As 链式判断
func (e *AppError) Unwrap() error {
	return e.Cause
}

// New 创建业务错误
func New(httpCode, code int, message string) *AppError {
	return &AppError{HTTPCode: httpCode, Code: code, Message: message}
}

// Wrap 包装原始错误
func (e *AppError) Wrap(cause error) *AppError {
	return &AppError{
		HTTPCode: e.HTTPCode,
		Code:     e.Code,
		Message:  e.Message,
		Cause:    cause,
	}
}

// ====== 预定义错误码 ======
// 规则：第1位=大类(1=通用,2=文档,3=问答,4=向量,5=外部服务)，后4位=具体错误

var (
	// 通用错误 1xxxx
	ErrInvalidParam    = New(http.StatusBadRequest, 10001, "参数错误")
	ErrUnauthorized    = New(http.StatusUnauthorized, 10002, "未授权")
	ErrForbidden       = New(http.StatusForbidden, 10003, "权限不足")
	ErrNotFound        = New(http.StatusNotFound, 10004, "资源不存在")
	ErrInternalServer  = New(http.StatusInternalServerError, 10005, "服务器内部错误")
	ErrTooManyRequests = New(http.StatusTooManyRequests, 10006, "请求频率超限")
	ErrServiceFallback = New(http.StatusServiceUnavailable, 10007, "服务降级中")

	// 文档错误 2xxxx
	ErrDocumentNotFound    = New(http.StatusNotFound, 20001, "文档不存在")
	ErrDocumentParseFailed = New(http.StatusUnprocessableEntity, 20002, "文档解析失败")
	ErrDocumentTooLarge    = New(http.StatusRequestEntityTooLarge, 20003, "文档超过大小限制")
	ErrUnsupportedFileType = New(http.StatusBadRequest, 20004, "不支持的文件类型")
	ErrDuplicateDocument   = New(http.StatusConflict, 20005, "文档已存在")
	ErrInvalidDocumentID   = New(http.StatusBadRequest, 20006, "文档 ID 无效")
	ErrInvalidKBID         = New(http.StatusBadRequest, 20007, "knowledge_base_id 无效")
	ErrFileGetFailed       = New(http.StatusBadRequest, 20008, "文件获取失败")

	// 问答错误 3xxxx
	ErrQuestionEmpty       = New(http.StatusBadRequest, 30001, "问题不能为空")
	ErrNoContextFound      = New(http.StatusOK, 30002, "未找到相关文档，无法回答") // 200但业务降级
	ErrQAFailed            = New(http.StatusInternalServerError, 30003, "问答生成失败")
	ErrConversationExpired = New(http.StatusGone, 30004, "会话已过期")
	ErrFeedbackFailed      = New(http.StatusInternalServerError, 30005, "反馈提交失败")

	// 向量服务错误 4xxxx
	ErrVectorStoreFailed  = New(http.StatusInternalServerError, 40001, "向量存储失败")
	ErrVectorSearchFailed = New(http.StatusInternalServerError, 40002, "向量检索失败")
	ErrEmbeddingFailed    = New(http.StatusInternalServerError, 40003, "向量化失败")

	// 外部服务错误 5xxxx
	ErrOpenAIUnavailable = New(http.StatusServiceUnavailable, 50001, "AI服务暂时不可用")
	ErrOpenAIRateLimit   = New(http.StatusTooManyRequests, 50002, "AI服务请求超限")
	ErrOpenAITimeout     = New(http.StatusGatewayTimeout, 50003, "AI服务响应超时")
	ErrQdrantUnavailable = New(http.StatusServiceUnavailable, 50004, "向量数据库不可用")
	ErrCacheUnavailable  = New(http.StatusServiceUnavailable, 50005, "缓存服务不可用")
)

// IsAppError 判断是否为业务错误
func IsAppError(err error) (*AppError, bool) {
	var appErr *AppError
	if stderrors.As(err, &appErr) {
		return appErr, true
	}
	return nil, false
}
