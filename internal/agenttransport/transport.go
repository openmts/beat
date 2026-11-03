package agenttransport

import "net/http"

const maximumResponseHeaderBytes = 64 << 10

// NoProxyTransport 返回 Agent 协议专用传输层；进程级代理变量不得截获 Agent 令牌。
func NoProxyTransport() *http.Transport {
	transport := &http.Transport{}
	if base, ok := http.DefaultTransport.(*http.Transport); ok {
		transport = base.Clone()
	}
	transport.Proxy = nil
	transport.MaxResponseHeaderBytes = maximumResponseHeaderBytes
	return transport
}
