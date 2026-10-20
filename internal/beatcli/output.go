package beatcli

import (
	"bytes"
	"encoding/json"
	"io"
)

// envelope 是 Beat 服务端的统一响应信封。
type envelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// writeResponse 默认只输出信封中的 data；raw 时输出完整响应。
func writeResponse(output io.Writer, body []byte, raw bool) error {
	return writePayload(output, unwrap(body, raw))
}

// unwrap 解出 data 字段；非信封响应保持原样，避免吞掉代理页面等非标准内容。
func unwrap(body []byte, raw bool) []byte {
	if raw {
		return body
	}
	var decoded envelope
	if err := json.Unmarshal(body, &decoded); err != nil {
		return body
	}
	if len(decoded.Data) == 0 || string(decoded.Data) == "null" {
		return body
	}
	return decoded.Data
}

// writePayload 缩进输出 JSON，非 JSON 负载原样输出。
func writePayload(output io.Writer, payload []byte) error {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 {
		return nil
	}
	var buffer bytes.Buffer
	if err := json.Indent(&buffer, trimmed, "", "  "); err != nil {
		if _, err := output.Write(append(trimmed, '\n')); err != nil {
			return err
		}
		return nil
	}
	buffer.WriteByte('\n')
	if _, err := buffer.WriteTo(output); err != nil {
		return err
	}
	return nil
}
