package gemini

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
	Model            string                 `json:"model"`
	Project          string                 `json:"project"`
	UserPromptID     string                 `json:"user_prompt_id,omitempty"`
	Request          map[string]interface{} `json:"request"`
	GenerationConfig map[string]interface{} `json:"generationConfig,omitempty"`
	SessionID        string                 `json:"session_id,omitempty"`
}

// GenerateContentResponse represents the response from the generateContent endpoint.
type GenerateContentResponse struct {
	Response map[string]interface{} `json:"response"`
}
