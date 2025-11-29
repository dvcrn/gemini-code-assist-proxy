package gemini

import (
	"encoding/json"
)

// ContentPart represents a single part of a content message.
type ContentPart struct {
	Text             string            `json:"text,omitempty"`
	ThoughtSignature string            `json:"thoughtSignature,omitempty"`
	FunctionCall     *FunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *FunctionResponse `json:"functionResponse,omitempty"`
}

// Content represents a single message in the chat history for Gemini.
type Content struct {
	Role  string        `json:"role,omitempty"`
	Parts []ContentPart `json:"parts,omitempty"`
}

// SystemInstruction defines the system-level instructions for the model.
type SystemInstruction struct {
	Role  string        `json:"role,omitempty"`
	Parts []ContentPart `json:"parts,omitempty"`
}

// GeminiParameterSchema defines the proprietary schema format for Gemini function parameters.
type GeminiParameterSchema struct {
	Type        string                            `json:"type,omitempty"`
	Description string                            `json:"description,omitempty"`
	Properties  map[string]*GeminiParameterSchema `json:"properties,omitempty"`
	Items       *GeminiParameterSchema            `json:"items,omitempty"`
	Required    []string                          `json:"required,omitempty"`
	Enum        []string                          `json:"enum,omitempty"`
}

// FunctionCall represents a tool call emitted by the model.
type FunctionCall struct {
	Name string                 `json:"name,omitempty"`
	Args map[string]interface{} `json:"args,omitempty"`
}

// FunctionResponse represents the tool result returned by the client.
type FunctionResponse struct {
	Name     string                 `json:"name,omitempty"`
	Response map[string]interface{} `json:"response,omitempty"`
}

// FunctionDeclaration defines a function that can be called by the model.
type FunctionDeclaration struct {
	Name        string                 `json:"name,omitempty"`
	Description string                 `json:"description,omitempty"`
	Parameters  *GeminiParameterSchema `json:"parameters,omitempty"`
}

// UnmarshalJSON: accept parametersJsonSchema (camelCase) and parameters (snake_case).
// Keeps compatibility with public v1beta tool schemas.
func (f *FunctionDeclaration) UnmarshalJSON(b []byte) error {
	// Attempt direct unmarshal for camelCase first
	type alias FunctionDeclaration
	var a alias
	if err := json.Unmarshal(b, &a); err == nil {
		*f = FunctionDeclaration(a)
		if f.Parameters != nil {
			return nil
		}
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	if v, ok := raw["name"]; ok {
		_ = json.Unmarshal(v, &f.Name)
	}
	if v, ok := raw["description"]; ok {
		_ = json.Unmarshal(v, &f.Description)
	}
	// snake_case key used by generativelanguage public API
	if v, ok := raw["parameters"]; ok {
		var schema GeminiParameterSchema
		if err := json.Unmarshal(v, &schema); err == nil {
			f.Parameters = &schema
			return nil
		}
	}
	// or explicit camelCase if present but empty earlier
	if v, ok := raw["parametersJsonSchema"]; ok && f.Parameters == nil {
		var schema GeminiParameterSchema
		if err := json.Unmarshal(v, &schema); err == nil {
			f.Parameters = &schema
		}
	}
	return nil
}

// Tool represents a collection of function declarations.
type Tool struct {
	FunctionDeclarations []FunctionDeclaration `json:"functionDeclarations,omitempty"`
}

// UnmarshalJSON: accept functionDeclarations (camelCase) and function_declarations (snake_case).
// Enables mixed client payloads without 400s.
func (t *Tool) UnmarshalJSON(b []byte) error {
	// Try camelCase first
	type alias Tool
	var a alias
	if err := json.Unmarshal(b, &a); err == nil && len(a.FunctionDeclarations) > 0 {
		*t = Tool(a)
		return nil
	}
	// Fallback to snake_case key function_declarations
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	if fdRaw, ok := raw["function_declarations"]; ok {
		var arr []json.RawMessage
		if err := json.Unmarshal(fdRaw, &arr); err != nil {
			return err
		}
		fds := make([]FunctionDeclaration, 0, len(arr))
		for _, item := range arr {
			var fd FunctionDeclaration
			if err := json.Unmarshal(item, &fd); err != nil {
				return err
			}
			fds = append(fds, fd)
		}
		t.FunctionDeclarations = fds
		return nil
	}
	// If neither present, keep empty
	t.FunctionDeclarations = nil
	return nil
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
	Model        string                `json:"model,omitempty"`
	Project      string                `json:"project,omitempty"`
	UserPromptID string                `json:"user_prompt_id,omitempty"`
	Request      GeminiInternalRequest `json:"request,omitempty"`
	SessionID    string                `json:"session_id,omitempty"`
}

type GeminiInternalRequest struct {
	Contents          []Content               `json:"contents,omitempty"`
	SystemInstruction *SystemInstruction      `json:"systemInstruction,omitempty"`
	Tools             []Tool                  `json:"tools,omitempty"`
	GenerationConfig  *GeminiGenerationConfig `json:"generationConfig,omitempty"`
	SessionID         string                  `json:"session_id,omitempty"`
}

// UnmarshalJSON: accept tools as array or single object (v1beta shape).
// Adds leniency for public API payloads while keeping CloudCode shape.
func (g *GeminiInternalRequest) UnmarshalJSON(b []byte) error {
	// Define a raw holder to inspect tools shape without failing early
	var raw struct {
		Contents          []Content               `json:"contents"`
		SystemInstruction *SystemInstruction      `json:"systemInstruction"`
		Tools             json.RawMessage         `json:"tools"`
		GenerationConfig  *GeminiGenerationConfig `json:"generationConfig"`
	}

	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}

	g.Contents = raw.Contents
	g.SystemInstruction = raw.SystemInstruction
	g.GenerationConfig = raw.GenerationConfig

	// If tools is absent or null, we're done
	if len(raw.Tools) == 0 || string(raw.Tools) == "null" {
		g.Tools = nil
		return nil
	}

	// First, try array of tools
	var toolsArr []Tool
	if err := json.Unmarshal(raw.Tools, &toolsArr); err == nil {
		g.Tools = toolsArr
		return nil
	}

	// Next, try single tool object
	var single Tool
	if err := json.Unmarshal(raw.Tools, &single); err == nil {
		g.Tools = []Tool{single}
		return nil
	}

	// If neither worked, fall back to strict error
	// Re-attempt full strict unmarshal to surface a helpful error
	type strict GeminiInternalRequest
	var s strict
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	*g = GeminiInternalRequest(s)
	return nil
}

// GenerateContentResponse represents the response from the generateContent endpoint.
type GenerateContentResponse struct {
	Response map[string]interface{} `json:"response"`
}
