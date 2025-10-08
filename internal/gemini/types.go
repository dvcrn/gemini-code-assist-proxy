package gemini

// ContentPart represents a single part of a content message.
type ContentPart struct {
	Text             string            `json:"text,omitempty"`
	ThoughtSignature string            `json:"thoughtSignature,omitempty"`
	FunctionCall     *FunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *FunctionResponse `json:"functionResponse,omitempty"`
}

// Content represents a single message in the chat history for Gemini.
type Content struct {
	Role  string        `json:"role"`
	Parts []ContentPart `json:"parts"`
}

// SystemInstruction defines the system-level instructions for the model.
type SystemInstruction struct {
	Role  string        `json:"role"`
	Parts []ContentPart `json:"parts"`
}

// JSONSchema represents a JSON schema.
type JSONSchema map[string]interface{}

// FunctionCall represents a tool call emitted by the model.
type FunctionCall struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args,omitempty"`
}

// FunctionResponse represents the tool result returned by the client.
type FunctionResponse struct {
	Name     string                 `json:"name"`
	Response map[string]interface{} `json:"response,omitempty"`
}

// FunctionDeclaration defines a function that can be called by the model.
type FunctionDeclaration struct {
	Name                 string     `json:"name"`
	Description          string     `json:"description"`
	ParametersJsonSchema JSONSchema `json:"parametersJsonSchema"`
}

// Tool represents a collection of function declarations.
type Tool struct {
	FunctionDeclarations []FunctionDeclaration `json:"functionDeclarations"`
}

// ThinkingConfig configures the model's thinking process.
type ThinkingConfig struct {
	IncludeThoughts bool `json:"includeThoughts"`
	ThinkingBudget  int  `json:"thinkingBudget"`
}

// GeminiGenerationConfig configures the generation process.
type GeminiGenerationConfig struct {
	Temperature     float64         `json:"temperature,omitempty"`
	TopP            float64         `json:"topP,omitempty"`
	ThinkingConfig  *ThinkingConfig `json:"thinkingConfig,omitempty"`
	MaxOutputTokens int             `json:"maxOutputTokens,omitempty"`
}

// LoadCodeAssistRequest represents the request body for the loadCodeAssist endpoint.
type LoadCodeAssistRequest struct {
	Metadata Metadata `json:"metadata"`
}

// Metadata contains metadata about the IDE and platform.
type Metadata struct {
	IdeType    string `json:"ideType"`
	Platform   string `json:"platform"`
	PluginType string `json:"pluginType"`
}

// LoadCodeAssistResponse represents the response from the loadCodeAssist endpoint.
type LoadCodeAssistResponse struct {
	CurrentTier             Tier   `json:"currentTier"`
	AllowedTiers            []Tier `json:"allowedTiers"`
	CloudAICompanionProject string `json:"cloudaicompanionProject"`
	GCPManaged              bool   `json:"gcpManaged"`
	ManageSubscriptionURI   string `json:"manageSubscriptionUri"`
}

// Tier represents a tier of service.
type Tier struct {
	ID                                 string                 `json:"id"`
	Name                               string                 `json:"name"`
	Description                        string                 `json:"description"`
	UserDefinedCloudAICompanionProject bool                   `json:"userDefinedCloudaicompanionProject"`
	PrivacyNotice                      map[string]interface{} `json:"privacyNotice"`
	IsDefault                          bool                   `json:"isDefault,omitempty"`
}

// GenerateContentRequest represents the request body for the generateContent endpoint.
type GenerateContentRequest struct {
	Model        string                `json:"model"`
	Project      string                `json:"project"`
	UserPromptID string                `json:"user_prompt_id,omitempty"`
	Request      GeminiInternalRequest `json:"request"`
	SessionID    string                `json:"session_id,omitempty"`
}

type GeminiInternalRequest struct {
	Contents          []Content               `json:"contents"`
	SystemInstruction *SystemInstruction      `json:"systemInstruction,omitempty"`
	Tools             []Tool                  `json:"tools,omitempty"`
	GenerationConfig  *GeminiGenerationConfig `json:"generationConfig,omitempty"`
}

// GenerateContentResponse represents the response from the generateContent endpoint.
type GenerateContentResponse struct {
	Response map[string]interface{} `json:"response"`
}
