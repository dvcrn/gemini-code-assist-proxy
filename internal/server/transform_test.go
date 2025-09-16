package server

import (
	"encoding/json"
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
		name         string
		requestData  map[string]interface{}
		model        string
		wantContains []string
		wantErr      bool
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
		name         string
		requestData  map[string]interface{}
		model        string
		projectID    string
		wantContains []string
		wantErr      bool
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

func TestProcessQueryParams(t *testing.T) {
	tests := []struct {
		name              string
		originalQuery     string
		expectedQuery     string
		expectedHasAPIKey bool
	}{
		{
			name:              "query with API key",
			originalQuery:     "key=AIzaSyAbcdef123456&alt=sse",
			expectedQuery:     "alt=sse",
			expectedHasAPIKey: true,
		},
		{
			name:              "query with only API key",
			originalQuery:     "key=AIzaSyAbcdef123456",
			expectedQuery:     "",
			expectedHasAPIKey: true,
		},
		{
			name:              "query without API key",
			originalQuery:     "alt=sse&format=json",
			expectedQuery:     "alt=sse&format=json",
			expectedHasAPIKey: false,
		},
		{
			name:              "empty query",
			originalQuery:     "",
			expectedQuery:     "",
			expectedHasAPIKey: false,
		},
		{
			name:              "query with multiple params and API key in middle",
			originalQuery:     "alt=sse&key=AIzaSyAbcdef123456&format=json",
			expectedQuery:     "alt=sse&format=json",
			expectedHasAPIKey: true,
		},
		{
			name:              "malformed query",
			originalQuery:     "invalid%query%params",
			expectedQuery:     "invalid%query%params",
			expectedHasAPIKey: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			processedQuery, hasAPIKey := processQueryParams(tt.originalQuery)
			if processedQuery != tt.expectedQuery {
				t.Errorf("processQueryParams(%q) query = %q, want %q", tt.originalQuery, processedQuery, tt.expectedQuery)
			}
			if hasAPIKey != tt.expectedHasAPIKey {
				t.Errorf("processQueryParams(%q) hasAPIKey = %v, want %v", tt.originalQuery, hasAPIKey, tt.expectedHasAPIKey)
			}
		})
	}
}

func TestUnwrapCloudCodeResponse(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]interface{}
		expected map[string]interface{}
	}{
		{
			name: "response with wrapped content",
			input: map[string]interface{}{
				"response": map[string]interface{}{
					"candidates": []interface{}{
						map[string]interface{}{
							"content": map[string]interface{}{
								"parts": []interface{}{
									map[string]interface{}{"text": "Hello"},
								},
							},
						},
					},
					"promptFeedback": map[string]interface{}{
						"safetyRatings": []interface{}{},
					},
				},
				"metadata": map[string]interface{}{
					"requestId": "123",
				},
			},
			expected: map[string]interface{}{
				"candidates": []interface{}{
					map[string]interface{}{
						"content": map[string]interface{}{
							"parts": []interface{}{
								map[string]interface{}{"text": "Hello"},
							},
						},
					},
				},
				"promptFeedback": map[string]interface{}{
					"safetyRatings": []interface{}{},
				},
				"metadata": map[string]interface{}{
					"requestId": "123",
				},
			},
		},
		{
			name: "response without wrapped content",
			input: map[string]interface{}{
				"candidates": []interface{}{
					map[string]interface{}{
						"content": map[string]interface{}{
							"parts": []interface{}{
								map[string]interface{}{"text": "Direct response"},
							},
						},
					},
				},
			},
			expected: map[string]interface{}{
				"candidates": []interface{}{
					map[string]interface{}{
						"content": map[string]interface{}{
							"parts": []interface{}{
								map[string]interface{}{"text": "Direct response"},
							},
						},
					},
				},
			},
		},
		{
			name:     "empty response",
			input:    map[string]interface{}{},
			expected: map[string]interface{}{},
		},
		{
			name: "response field is not a map",
			input: map[string]interface{}{
				"response": "not a map",
				"other":    "field",
			},
			expected: map[string]interface{}{
				"response": "not a map",
				"other":    "field",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := unwrapCloudCodeResponse(tt.input)

			// Compare JSON representations for deep equality
			expectedJSON, _ := json.Marshal(tt.expected)
			resultJSON, _ := json.Marshal(result)

			if string(expectedJSON) != string(resultJSON) {
				t.Errorf("unwrapCloudCodeResponse() = %v, want %v", string(resultJSON), string(expectedJSON))
			}
		})
	}
}

func TestCountFunctionCalls(t *testing.T) {
	tests := []struct {
		name     string
		turn     map[string]interface{}
		expected int
	}{
		{
			name: "single function call",
			turn: map[string]interface{}{
				"role": "model",
				"parts": []interface{}{
					map[string]interface{}{
						"functionCall": map[string]interface{}{
							"name": "test_function",
							"args": map[string]interface{}{"key": "value"},
						},
					},
				},
			},
			expected: 1,
		},
		{
			name: "multiple function calls",
			turn: map[string]interface{}{
				"role": "model",
				"parts": []interface{}{
					map[string]interface{}{
						"functionCall": map[string]interface{}{
							"name": "function1",
						},
					},
					map[string]interface{}{
						"functionCall": map[string]interface{}{
							"name": "function2",
						},
					},
					map[string]interface{}{
						"functionCall": map[string]interface{}{
							"name": "function3",
						},
					},
				},
			},
			expected: 3,
		},
		{
			name: "no function calls",
			turn: map[string]interface{}{
				"role": "model",
				"parts": []interface{}{
					map[string]interface{}{
						"text": "This is just text",
					},
				},
			},
			expected: 0,
		},
		{
			name: "mixed parts with function calls",
			turn: map[string]interface{}{
				"role": "model",
				"parts": []interface{}{
					map[string]interface{}{
						"text": "Let me help you with that",
					},
					map[string]interface{}{
						"functionCall": map[string]interface{}{
							"name": "search",
						},
					},
					map[string]interface{}{
						"text": "And also this",
					},
					map[string]interface{}{
						"functionCall": map[string]interface{}{
							"name": "calculate",
						},
					},
				},
			},
			expected: 2,
		},
		{
			name:     "empty turn",
			turn:     map[string]interface{}{},
			expected: 0,
		},
		{
			name: "no parts field",
			turn: map[string]interface{}{
				"role": "model",
			},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := countFunctionCalls(tt.turn)
			if result != tt.expected {
				t.Errorf("countFunctionCalls() = %d, want %d", result, tt.expected)
			}
		})
	}
}

func TestCountFunctionResponses(t *testing.T) {
	tests := []struct {
		name     string
		turn     map[string]interface{}
		expected int
	}{
		{
			name: "single function response",
			turn: map[string]interface{}{
				"role": "user",
				"parts": []interface{}{
					map[string]interface{}{
						"functionResponse": map[string]interface{}{
							"name":     "test_function",
							"response": map[string]interface{}{"result": "success"},
						},
					},
				},
			},
			expected: 1,
		},
		{
			name: "multiple function responses",
			turn: map[string]interface{}{
				"role": "user",
				"parts": []interface{}{
					map[string]interface{}{
						"functionResponse": map[string]interface{}{
							"name": "function1",
						},
					},
					map[string]interface{}{
						"functionResponse": map[string]interface{}{
							"name": "function2",
						},
					},
					map[string]interface{}{
						"functionResponse": map[string]interface{}{
							"name": "function3",
						},
					},
				},
			},
			expected: 3,
		},
		{
			name: "no function responses",
			turn: map[string]interface{}{
				"role": "user",
				"parts": []interface{}{
					map[string]interface{}{
						"text": "This is just text",
					},
				},
			},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := countFunctionResponses(tt.turn)
			if result != tt.expected {
				t.Errorf("countFunctionResponses() = %d, want %d", result, tt.expected)
			}
		})
	}
}

func TestValidateFunctionCallParity(t *testing.T) {
	tests := []struct {
		name        string
		requestData map[string]interface{}
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid parity - single function call and response",
			requestData: map[string]interface{}{
				"contents": []interface{}{
					map[string]interface{}{
						"role": "model",
						"parts": []interface{}{
							map[string]interface{}{
								"functionCall": map[string]interface{}{
									"name": "search",
									"args": map[string]interface{}{"query": "test"},
								},
							},
						},
					},
					map[string]interface{}{
						"role": "user",
						"parts": []interface{}{
							map[string]interface{}{
								"functionResponse": map[string]interface{}{
									"name":     "search",
									"response": map[string]interface{}{"results": []interface{}{}},
								},
							},
						},
					},
				},
			},
			expectError: false,
		},
		{
			name: "valid parity - multiple function calls and responses",
			requestData: map[string]interface{}{
				"contents": []interface{}{
					map[string]interface{}{
						"role": "model",
						"parts": []interface{}{
							map[string]interface{}{
								"functionCall": map[string]interface{}{
									"name": "function1",
								},
							},
							map[string]interface{}{
								"functionCall": map[string]interface{}{
									"name": "function2",
								},
							},
						},
					},
					map[string]interface{}{
						"role": "user",
						"parts": []interface{}{
							map[string]interface{}{
								"functionResponse": map[string]interface{}{
									"name": "function1",
								},
							},
							map[string]interface{}{
								"functionResponse": map[string]interface{}{
									"name": "function2",
								},
							},
						},
					},
				},
			},
			expectError: false,
		},
		{
			name: "invalid parity - more calls than responses",
			requestData: map[string]interface{}{
				"contents": []interface{}{
					map[string]interface{}{
						"role": "model",
						"parts": []interface{}{
							map[string]interface{}{
								"functionCall": map[string]interface{}{
									"name": "function1",
								},
							},
							map[string]interface{}{
								"functionCall": map[string]interface{}{
									"name": "function2",
								},
							},
						},
					},
					map[string]interface{}{
						"role": "user",
						"parts": []interface{}{
							map[string]interface{}{
								"functionResponse": map[string]interface{}{
									"name": "function1",
								},
							},
						},
					},
				},
			},
			expectError: true,
			errorMsg:    "2 function calls, but following user turn has 1 function responses",
		},
		{
			name: "invalid parity - fewer calls than responses",
			requestData: map[string]interface{}{
				"contents": []interface{}{
					map[string]interface{}{
						"role": "model",
						"parts": []interface{}{
							map[string]interface{}{
								"functionCall": map[string]interface{}{
									"name": "function1",
								},
							},
						},
					},
					map[string]interface{}{
						"role": "user",
						"parts": []interface{}{
							map[string]interface{}{
								"functionResponse": map[string]interface{}{
									"name": "function1",
								},
							},
							map[string]interface{}{
								"functionResponse": map[string]interface{}{
									"name": "function2",
								},
							},
						},
					},
				},
			},
			expectError: true,
			errorMsg:    "1 function calls, but following user turn has 2 function responses",
		},
		{
			name: "valid - no function calls",
			requestData: map[string]interface{}{
				"contents": []interface{}{
					map[string]interface{}{
						"role": "user",
						"parts": []interface{}{
							map[string]interface{}{
								"text": "Hello",
							},
						},
					},
					map[string]interface{}{
						"role": "model",
						"parts": []interface{}{
							map[string]interface{}{
								"text": "Hi there!",
							},
						},
					},
				},
			},
			expectError: false,
		},
		{
			name: "valid - empty contents",
			requestData: map[string]interface{}{
				"contents": []interface{}{},
			},
			expectError: false,
		},
		{
			name:        "valid - no contents field",
			requestData: map[string]interface{}{},
			expectError: false,
		},
		{
			name: "invalid - function call at end (no response turn)",
			requestData: map[string]interface{}{
				"contents": []interface{}{
					map[string]interface{}{
						"role": "user",
						"parts": []interface{}{
							map[string]interface{}{
								"text": "Search for something",
							},
						},
					},
					map[string]interface{}{
						"role": "model",
						"parts": []interface{}{
							map[string]interface{}{
								"functionCall": map[string]interface{}{
									"name": "search",
								},
							},
						},
					},
				},
			},
			expectError: true, // Function calls MUST be followed by responses
			errorMsg:    "last turn) has 1 function calls but no following user turn",
		},
		{
			name: "invalid - function call followed by model turn instead of user",
			requestData: map[string]interface{}{
				"contents": []interface{}{
					map[string]interface{}{
						"role": "model",
						"parts": []interface{}{
							map[string]interface{}{
								"functionCall": map[string]interface{}{
									"name": "search",
								},
							},
						},
					},
					map[string]interface{}{
						"role": "model",
						"parts": []interface{}{
							map[string]interface{}{
								"text": "Still waiting for response",
							},
						},
					},
				},
			},
			expectError: true,
			errorMsg:    "followed by a model turn instead of a user turn",
		},
		{
			name: "invalid - multiple function calls at end",
			requestData: map[string]interface{}{
				"contents": []interface{}{
					map[string]interface{}{
						"role": "user",
						"parts": []interface{}{
							map[string]interface{}{
								"text": "Do multiple things",
							},
						},
					},
					map[string]interface{}{
						"role": "model",
						"parts": []interface{}{
							map[string]interface{}{
								"functionCall": map[string]interface{}{
									"name": "function1",
								},
							},
							map[string]interface{}{
								"functionCall": map[string]interface{}{
									"name": "function2",
								},
							},
							map[string]interface{}{
								"functionCall": map[string]interface{}{
									"name": "function3",
								},
							},
						},
					},
				},
			},
			expectError: true,
			errorMsg:    "last turn) has 3 function calls but no following user turn",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateFunctionCallParity(tt.requestData)
			if tt.expectError {
				if err == nil {
					t.Errorf("validateFunctionCallParity() expected error but got nil")
				} else if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("validateFunctionCallParity() error = %v, want error containing %v", err, tt.errorMsg)
				}
			} else {
				if err != nil {
					t.Errorf("validateFunctionCallParity() unexpected error: %v", err)
				}
			}
		})
	}
}
