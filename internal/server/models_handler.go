package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ModelSupports defines the capabilities of a model.
type ModelSupports struct {
	ParallelToolCalls bool `json:"parallel_tool_calls"`
	Streaming         bool `json:"streaming"`
	StructuredOutputs bool `json:"structured_outputs"`
	ToolCalls         bool `json:"tool_calls"`
	Vision            bool `json:"vision"`
}

// ModelLimits defines the token limits for a model.
type ModelLimits struct {
	MaxContextWindowTokens int `json:"max_context_window_tokens"`
	MaxOutputTokens        int `json:"max_output_tokens"`
	MaxPromptTokens        int `json:"max_prompt_tokens"`
}

// ModelCapabilities defines the technical capabilities of a model.
type ModelCapabilities struct {
	Family    string        `json:"family"`
	Limits    ModelLimits   `json:"limits"`
	Object    string        `json:"object"`
	Supports  ModelSupports `json:"supports"`
	Tokenizer string        `json:"tokenizer"`
	Type      string        `json:"type"`
}

// ModelInfo represents a single model in the list.
type ModelInfo struct {
	ID                  string            `json:"id"`
	Object              string            `json:"object"`
	Name                string            `json:"name"`
	Version             string            `json:"version"`
	Capabilities        ModelCapabilities `json:"capabilities"`
	ModelPickerCategory string            `json:"model_picker_category"`
	ModelPickerEnabled  bool              `json:"model_picker_enabled"`
	Preview             bool              `json:"preview"`
	Vendor              string            `json:"vendor"`
}

// ModelsListResponse is the top-level response for the models endpoint.
type ModelsListResponse struct {
	Object string      `json:"object"`
	Data   []ModelInfo `json:"data"`
}

func (s *Server) modelsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	defaultSupports := ModelSupports{
		ParallelToolCalls: true,
		Streaming:         true,
		StructuredOutputs: true,
		ToolCalls:         true,
		Vision:            false, // Vision is not supported via this proxy
	}

	defaultTokenizer := "o200k_base"
	defaultType := "chat"
	defaultCapabilitiesObject := "model_capabilities"
	defaultModelObject := "model"
	defaultVendor := "Google"

	models := []ModelInfo{
		{
			ID:      "gemini-2.5-pro",
			Object:  defaultModelObject,
			Name:    "Gemini 2.5 Pro",
			Version: "2.5.0",
			Capabilities: ModelCapabilities{
				Family: "gemini-2.5-pro",
				Limits: ModelLimits{
					MaxContextWindowTokens: 10240,
					MaxOutputTokens:        2048,
					MaxPromptTokens:        8192,
				},
				Object:    defaultCapabilitiesObject,
				Supports:  defaultSupports,
				Tokenizer: defaultTokenizer,
				Type:      defaultType,
			},
			ModelPickerCategory: "powerful",
			ModelPickerEnabled:  true,
			Preview:             false,
			Vendor:              defaultVendor,
		},
		{
			ID:      "gemini-2.5-flash",
			Object:  defaultModelObject,
			Name:    "Gemini 2.5 Flash",
			Version: "2.5.0",
			Capabilities: ModelCapabilities{
				Family: "gemini-2.5-flash",
				Limits: ModelLimits{
					MaxContextWindowTokens: 10240,
					MaxOutputTokens:        2048,
					MaxPromptTokens:        8192,
				},
				Object:    defaultCapabilitiesObject,
				Supports:  defaultSupports,
				Tokenizer: defaultTokenizer,
				Type:      defaultType,
			},
			ModelPickerCategory: "versatile",
			ModelPickerEnabled:  true,
			Preview:             false,
			Vendor:              defaultVendor,
		},
		{
			ID:      "gemini-2.5-flash-lite",
			Object:  defaultModelObject,
			Name:    "Gemini 2.5 Flash Lite",
			Version: "2.5.0",
			Capabilities: ModelCapabilities{
				Family: "gemini-2.5-flash-lite",
				Limits: ModelLimits{
					MaxContextWindowTokens: 10240,
					MaxOutputTokens:        2048,
					MaxPromptTokens:        8192,
				},
				Object:    defaultCapabilitiesObject,
				Supports:  defaultSupports,
				Tokenizer: defaultTokenizer,
				Type:      defaultType,
			},
			ModelPickerCategory: "lightweight",
			ModelPickerEnabled:  true,
			Preview:             true,
			Vendor:              defaultVendor,
		},
	}

	// Handle request for a single model, e.g., /v1/models/gemini-2.5-pro
	pathParts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(pathParts) > 2 {
		requestedModelID := pathParts[2]
		for _, model := range models {
			if model.ID == requestedModelID {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(model)
				return
			}
		}
		http.NotFound(w, r)
		return
	}

	response := ModelsListResponse{
		Object: "list",
		Data:   models,
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Date", time.Now().UTC().Format(http.TimeFormat))
	w.Header().Set("Server", "Google")
	w.Header().Set("Cache-Control", "public, max-age=600")

	jsonResponse, err := json.Marshal(response)
	if err != nil {
		http.Error(w, "Failed to marshal response", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Length", strconv.Itoa(len(jsonResponse)))
	w.Write(jsonResponse)
}
