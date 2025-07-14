package server

import (
	"strings"
	"testing"
)

func TestNormalizeModelName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Pro models
		{
			name:     "gemini-2.5-pro stays unchanged",
			input:    "gemini-2.5-pro",
			expected: "gemini-2.5-pro",
		},
		{
			name:     "gemini-1.5-pro becomes gemini-2.5-pro",
			input:    "gemini-1.5-pro",
			expected: "gemini-2.5-pro",
		},
		{
			name:     "gemini-2.0-pro-latest becomes gemini-2.5-pro",
			input:    "gemini-2.0-pro-latest",
			expected: "gemini-2.5-pro",
		},
		{
			name:     "gemini-pro becomes gemini-2.5-pro",
			input:    "gemini-pro",
			expected: "gemini-2.5-pro",
		},
		{
			name:     "gemini-pro-001 becomes gemini-2.5-pro",
			input:    "gemini-pro-001",
			expected: "gemini-2.5-pro",
		},
		// Flash models
		{
			name:     "gemini-2.5-flash stays unchanged",
			input:    "gemini-2.5-flash",
			expected: "gemini-2.5-flash",
		},
		{
			name:     "gemini-1.5-flash becomes gemini-2.5-flash",
			input:    "gemini-1.5-flash",
			expected: "gemini-2.5-flash",
		},
		{
			name:     "gemini-2.0-flash-lite becomes gemini-2.5-flash",
			input:    "gemini-2.0-flash-lite",
			expected: "gemini-2.5-flash",
		},
		{
			name:     "gemini-flash becomes gemini-2.5-flash",
			input:    "gemini-flash",
			expected: "gemini-2.5-flash",
		},
		{
			name:     "gemini-flash-001 becomes gemini-2.5-flash",
			input:    "gemini-flash-001",
			expected: "gemini-2.5-flash",
		},
		// Case insensitive
		{
			name:     "GEMINI-PRO becomes gemini-2.5-pro",
			input:    "GEMINI-PRO",
			expected: "gemini-2.5-pro",
		},
		{
			name:     "Gemini-Flash becomes gemini-2.5-flash",
			input:    "Gemini-Flash",
			expected: "gemini-2.5-flash",
		},
		{
			name:     "gEmInI-pRo becomes gemini-2.5-pro",
			input:    "gEmInI-pRo",
			expected: "gemini-2.5-pro",
		},
		// Models without pro or flash stay unchanged
		{
			name:     "gemini-nano stays unchanged",
			input:    "gemini-nano",
			expected: "gemini-nano",
		},
		{
			name:     "gemini-ultra stays unchanged",
			input:    "gemini-ultra",
			expected: "gemini-ultra",
		},
		{
			name:     "some-other-model stays unchanged",
			input:    "some-other-model",
			expected: "some-other-model",
		},
		// Edge cases
		{
			name:     "model with pro in middle",
			input:    "some-pro-model",
			expected: "gemini-2.5-pro",
		},
		{
			name:     "model with flash in middle",
			input:    "some-flash-model",
			expected: "gemini-2.5-flash",
		},
		{
			name:     "empty string stays unchanged",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeModelName(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeModelName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestParseGeminiPath(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		expectedModel  string
		expectedAction string
	}{
		{
			name:           "v1beta path with generateContent",
			path:           "/v1beta/models/gemini-pro:generateContent",
			expectedModel:  "gemini-pro",
			expectedAction: "generateContent",
		},
		{
			name:           "v1 path with streamGenerateContent",
			path:           "/v1/models/gemini-2.5-flash:streamGenerateContent",
			expectedModel:  "gemini-2.5-flash",
			expectedAction: "streamGenerateContent",
		},
		{
			name:           "path with countTokens",
			path:           "/v1beta/models/gemini-1.5-pro:countTokens",
			expectedModel:  "gemini-1.5-pro",
			expectedAction: "countTokens",
		},
		{
			name:           "model with version and variant",
			path:           "/v1beta/models/gemini-2.0-pro-latest:generateContent",
			expectedModel:  "gemini-2.0-pro-latest",
			expectedAction: "generateContent",
		},
		{
			name:           "invalid path - no model",
			path:           "/v1beta/generateContent",
			expectedModel:  "",
			expectedAction: "",
		},
		{
			name:           "invalid path - no action",
			path:           "/v1beta/models/gemini-pro",
			expectedModel:  "",
			expectedAction: "",
		},
		{
			name:           "invalid path - wrong format",
			path:           "/api/v2/models/gemini-pro:generateContent",
			expectedModel:  "",
			expectedAction: "",
		},
		{
			name:           "empty path",
			path:           "",
			expectedModel:  "",
			expectedAction: "",
		},
		{
			name:           "path with extra segments",
			path:           "/v1beta/models/gemini-pro:generateContent/extra",
			expectedModel:  "gemini-pro",
			expectedAction: "generateContent/extra",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model, action := parseGeminiPath(tt.path)
			if model != tt.expectedModel {
				t.Errorf("parseGeminiPath(%q) model = %q, want %q", tt.path, model, tt.expectedModel)
			}
			if action != tt.expectedAction {
				t.Errorf("parseGeminiPath(%q) action = %q, want %q", tt.path, action, tt.expectedAction)
			}
		})
	}
}

func TestBuildCountTokensRequest(t *testing.T) {
	tests := []struct {
		name        string
		requestData map[string]interface{}
		model       string
		wantContains []string
		wantErr     bool
	}{
		{
			name: "simple request",
			requestData: map[string]interface{}{
				"contents": []interface{}{
					map[string]interface{}{
						"parts": []interface{}{
							map[string]interface{}{"text": "Hello"},
						},
						"role": "user",
					},
				},
			},
			model: "gemini-2.5-pro",
			wantContains: []string{
				`"request":{`,
				`"model":"models/gemini-2.5-pro"`,
				`"contents":[`,
				`"text":"Hello"`,
			},
		},
		{
			name: "request with generateContentRequest wrapper",
			requestData: map[string]interface{}{
				"generateContentRequest": map[string]interface{}{
					"contents": []interface{}{
						map[string]interface{}{
							"parts": []interface{}{
								map[string]interface{}{"text": "Test"},
							},
							"role": "user",
						},
					},
				},
			},
			model: "gemini-2.5-flash",
			wantContains: []string{
				`"request":{`,
				`"model":"models/gemini-2.5-flash"`,
				`"contents":[`,
				`"text":"Test"`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildCountTokensRequest(tt.requestData, tt.model)
			if (err != nil) != tt.wantErr {
				t.Errorf("buildCountTokensRequest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err == nil {
				gotStr := string(got)
				for _, want := range tt.wantContains {
					if !strings.Contains(gotStr, want) {
						t.Errorf("buildCountTokensRequest() = %v, want to contain %v", gotStr, want)
					}
				}
			}
		})
	}
}

func TestBuildCloudCodeRequest(t *testing.T) {
	tests := []struct {
		name        string
		requestData map[string]interface{}
		model       string
		projectID   string
		wantContains []string
		wantErr     bool
	}{
		{
			name: "standard request",
			requestData: map[string]interface{}{
				"contents": []interface{}{
					map[string]interface{}{
						"parts": []interface{}{
							map[string]interface{}{"text": "Hello world"},
						},
						"role": "user",
					},
				},
			},
			model:     "gemini-2.5-pro",
			projectID: "test-project",
			wantContains: []string{
				`"model":"gemini-2.5-pro"`,
				`"project":"test-project"`,
				`"request":{`,
				`"contents":[`,
				`"text":"Hello world"`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildCloudCodeRequest(tt.requestData, tt.model, tt.projectID)
			if (err != nil) != tt.wantErr {
				t.Errorf("buildCloudCodeRequest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err == nil {
				gotStr := string(got)
				for _, want := range tt.wantContains {
					if !strings.Contains(gotStr, want) {
						t.Errorf("buildCloudCodeRequest() = %v, want to contain %v", gotStr, want)
					}
				}
			}
		})
	}
}
