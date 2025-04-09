package mcp

// JSONRPCRequest represents a JSON-RPC request.
type JSONRPCRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      interface{}   `json:"id"`
	Method  string        `json:"method"`
	Params  RequestParams `json:"params"`
}

// RequestParams holds the parameters for different MCP methods.
// Note: This might be further refined later based on specific method needs.
type RequestParams struct {
	// Common params
	ProtocolVersion string                 `json:"protocolVersion,omitempty"`
	ClientInfo      map[string]interface{} `json:"clientInfo,omitempty"`
	Capabilities    map[string]interface{} `json:"capabilities,omitempty"`

	// tools/call params
	Name      string                 `json:"name,omitempty"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`

	// resources/list, resources/read, resources/subscribe params
	URI    string `json:"uri,omitempty"`
	Cursor string `json:"cursor,omitempty"`

	// Deprecated/Removed (kept for reference during refactor, remove later)
	// PAT             string                 `json:"pat,omitempty"`
	// ImageBytes      string                 `json:"image_bytes,omitempty"`
	// ImageURL        string                 `json:"image_url,omitempty"`
	// ModelID         string                 `json:"model_id,omitempty"`
	// AppID           string                 `json:"app_id,omitempty"`
	// UserID          string                 `json:"user_id,omitempty"`
	// TextPrompt      string                 `json:"text_prompt,omitempty"`
}

// JSONRPCResponse represents a JSON-RPC response.
type JSONRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
}

// RPCError represents a JSON-RPC error object.
type RPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// NewErrorResponse creates a JSONRPCResponse populated with an error.
func NewErrorResponse(id interface{}, code int, message string, data interface{}) JSONRPCResponse {
	return JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &RPCError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
}
