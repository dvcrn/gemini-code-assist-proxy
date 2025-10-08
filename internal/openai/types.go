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

// Message represents a message in the chat history.
type Message struct {
	Content interface{} `json:"content"` // Can be a string or a slice of ContentPart
	Role    string      `json:"role"`
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
