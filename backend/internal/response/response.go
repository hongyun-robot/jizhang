package response

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
)

// 统一业务错误码（{code,message,data} 包装，code=0 表示成功）。
const (
	CodeOK              = 0
	CodeInvalidParams   = 10001
	CodeUnauthorized    = 10002
	CodeForbidden       = 10003
	CodeNotFound        = 10004
	CodeTooManyReq      = 10005
	CodeConflict        = 10006
	CodeAccessExpired   = 10007
	CodeSessionEnded    = 10008
	CodeSessionReplaced = 10009
	CodeRefreshConflict = 10010
	CodeInternal        = 20000
	CodeAuthUnavailable = 20001
)

// Body 统一响应体。
type Body struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// OK 返回成功响应。
func OK(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, Body{Code: CodeOK, Message: "ok", Data: data})
}

// OKRaw data 已是序列化好的 JSON 时使用（统计缓存命中路径，避免二次编解码）。
func OKRaw(c *gin.Context, data json.RawMessage) {
	c.JSON(http.StatusOK, Body{Code: CodeOK, Message: "ok", Data: data})
}

// Fail 返回业务错误响应，HTTP 状态码与业务码分开表达。
func Fail(c *gin.Context, httpStatus, code int, msg string) {
	c.JSON(httpStatus, Body{Code: code, Message: msg})
}

// Invalid 参数校验失败。
func Invalid(c *gin.Context, msg string) {
	Fail(c, http.StatusBadRequest, CodeInvalidParams, msg)
}

// Unauthorized 未认证。
func Unauthorized(c *gin.Context, msg string) {
	Fail(c, http.StatusUnauthorized, CodeUnauthorized, msg)
}

// Forbidden 无权限。
func Forbidden(c *gin.Context, msg string) {
	Fail(c, http.StatusForbidden, CodeForbidden, msg)
}

// NotFound 资源不存在。
func NotFound(c *gin.Context, msg string) {
	Fail(c, http.StatusNotFound, CodeNotFound, msg)
}

// Conflict 资源冲突（重复、约束）。
func Conflict(c *gin.Context, msg string) {
	Fail(c, http.StatusConflict, CodeConflict, msg)
}

// MsgInvalidAmount 统一金额格式提示。
const MsgInvalidAmount = "金额格式无效，应为数字（如 12.50）"

// Internal 服务内部错误。
func Internal(c *gin.Context, msg string) {
	Fail(c, http.StatusInternalServerError, CodeInternal, msg)
}
