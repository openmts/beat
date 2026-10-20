package handler

import (
	"io"
	"net/http"
	"net/http/httptest"
)

// jsonRequest 构造带 application/json 媒体类型的请求。管理面与认证面解码器会强制校验
// 媒体类型，用例统一走这里，避免每个夹具各自重复设置请求头而漏掉校验分支。
func jsonRequest(method, target string, body io.Reader) *http.Request {
	request := httptest.NewRequest(method, target, body)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	return request
}
