// Package types —— application 层共享值（子包可安全引用，避免与根包容器成环）。
package types

// RequestParams 接口层传入的请求上下文参数（ip/ua 由 handler 提取）。
type RequestParams struct {
	IP string
	UA string
}
