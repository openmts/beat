package handler

import (
	"encoding/json"
	"net/http"
	"reflect"
)

type Response struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func JSONResponse(w http.ResponseWriter, statusCode int, data interface{}) {
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(Response{
		Code:    statusCode,
		Message: "success",
		Data:    listSafeData(data),
	})
}

func JSONError(w http.ResponseWriter, statusCode int, message string) {
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(Response{
		Code:    statusCode,
		Message: message,
	})
}

// listSafeData 把 nil 切片改写成空切片：Go 的 nil 切片会编码成 JSON null，
// 列表接口一旦返回 null，前端对结果取 length/map 时就会直接崩溃。
func listSafeData(data interface{}) interface{} {
	value := reflect.ValueOf(data)
	if value.Kind() == reflect.Slice && value.IsNil() {
		return reflect.MakeSlice(value.Type(), 0, 0).Interface()
	}
	return data
}
