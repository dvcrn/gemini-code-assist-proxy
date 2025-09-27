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
