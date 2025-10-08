package openai

// ChatCompletionRequest represents a request payload for OpenAI-compatible chat completion endpoints.
type ChatCompletionRequest struct {
	MaxTokens   int       `json:"max_tokens"`
	Messages    []Message `json:"messages"`
	Model       string    `json:"model"`
	Stream      bool      `json:"stream"`
	Temperature float64   `json:"temperature"`
	Tools       []Tool    `json:"tools,omitempty"`
}

// Message represents a message in the chat history, including tool calls/results.
type Message struct {
	// Standard fields
	Content interface{} `json:"content"` // Can be a string or a slice of ContentPart
	Role    string      `json:"role"`

	// Tool calling (assistant -> tools). When present on an assistant message,
	// the assistant is requesting tool execution.
	ToolCalls []OpenAIToolCall `json:"tool_calls,omitempty"`

	// Tool result (tools -> assistant). When present on a tool message, this links
	// the tool output back to the originating assistant tool call.
	ToolCallID string `json:"tool_call_id,omitempty"`

	// Optional function name on tool messages (some clients include this)
	Name string `json:"name,omitempty"`
}

// ContentPart represents a part of a multi-modal message.
type ContentPart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// Tool represents a tool the model can call.
type Tool struct {
	Type     string   `json:"type"`
	Function Function `json:"function"`
}

// Function represents a function that can be called by the model.
type Function struct {
	Description string      `json:"description,omitempty"`
	Name        string      `json:"name"`
	Parameters  interface{} `json:"parameters"`
}

// ChatCompletionResponse represents a response payload for OpenAI-compatible chat completion endpoints.
// This is used for non-streaming responses.
type ChatCompletionResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

// Choice represents a single choice in a chat completion response.
type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

// Usage represents the token usage for a request.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}
