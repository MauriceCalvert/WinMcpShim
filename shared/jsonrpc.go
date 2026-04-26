package shared

import (
	"encoding/json"
	"fmt"
)

// Request represents a JSON-RPC 2.0 request.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response represents a JSON-RPC 2.0 success response.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
}

// ErrorResponse represents a JSON-RPC 2.0 error response.
type ErrorResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Error   *RPCError       `json:"error"`
}

// RPCError is the error object in a JSON-RPC error response.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ToolResult is the MCP tool call result envelope.
type ToolResult struct {
	Content []ContentItem `json:"content"`
	IsError bool          `json:"isError"`
}

// ContentItem is a single content entry in a tool result.
type ContentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ToolAnnotations describes tool behaviour hints per MCP spec ToolAnnotations.
type ToolAnnotations struct {
	Title           string `json:"title"`
	ReadOnlyHint    bool   `json:"readOnlyHint"`
	DestructiveHint bool   `json:"destructiveHint"`
	IdempotentHint  bool   `json:"idempotentHint"`
	OpenWorldHint   bool   `json:"openWorldHint"`
}

// ToolSchema is the MCP tool declaration for tools/list.
type ToolSchema struct {
	Name        string           `json:"name"`
	Description string           `json:"description"`
	InputSchema json.RawMessage  `json:"inputSchema"`
	Annotations *ToolAnnotations `json:"annotations,omitempty"`
}

// MakeToolResult creates a ToolResult as json.RawMessage.
func MakeToolResult(text string, isError bool) json.RawMessage {
	tr := ToolResult{
		Content: []ContentItem{{Type: "text", Text: text}},
		IsError: isError,
	}
	data, _ := json.Marshal(tr)
	return data
}

// MakeErrorResponse creates a JSON-RPC level error response.
func MakeErrorResponse(id json.RawMessage, code int, message string) *ErrorResponse {
	return &ErrorResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &RPCError{Code: code, Message: message},
	}
}

// MakeSuccessResponse creates a JSON-RPC success response with a tool result.
func MakeSuccessResponse(id json.RawMessage, result json.RawMessage) *Response {
	return &Response{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
}

// RawJSON compacts a JSON string into a json.RawMessage.
func RawJSON(s string) json.RawMessage {
	var buf json.RawMessage
	if err := json.Unmarshal([]byte(s), &buf); err != nil {
		panic(fmt.Sprintf("invalid built-in schema JSON: %v", err))
	}
	compact, _ := json.Marshal(buf)
	return compact
}
