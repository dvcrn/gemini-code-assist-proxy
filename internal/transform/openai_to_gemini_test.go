package transform

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/dvcrn/gemini-code-assist-proxy/internal/gemini"
)

func TestConvertToGeminiSchema(t *testing.T) {
	testCases := []struct {
		name           string
		inputSchema    map[string]interface{}
		expectedSchema *gemini.GeminiParameterSchema
	}{
		{
			name: "Simple Schema",
			inputSchema: map[string]interface{}{
				"type":        "object",
				"description": "A simple object.",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "The name.",
					},
				},
				"required": []interface{}{"name"},
			},
			expectedSchema: &gemini.GeminiParameterSchema{
				Type:        "OBJECT",
				Description: "A simple object.",
				Properties: map[string]*gemini.GeminiParameterSchema{
					"name": {
						Type:        "STRING",
						Description: "The name.",
					},
				},
				Required: []string{"name"},
			},
		},
		{
			name: "TodoWrite Schema with anyOf",
			inputSchema: map[string]interface{}{
				"description": "The updated todo list",
				"anyOf": []interface{}{
					map[string]interface{}{
						"type":     "array",
						"maxItems": 50.0,
						"items": map[string]interface{}{
							"type":     "object",
							"required": []interface{}{"content", "status"},
							"properties": map[string]interface{}{
								"content": map[string]interface{}{
									"type": "string",
								},
								"status": map[string]interface{}{
									"type": "string",
									"enum": []interface{}{"pending", "completed"},
								},
							},
						},
					},
					map[string]interface{}{
						"type": "string",
					},
				},
			},
			expectedSchema: &gemini.GeminiParameterSchema{
				Type:        "ARRAY",
				Description: "The updated todo list",
				Items: &gemini.GeminiParameterSchema{
					Type:     "OBJECT",
					Required: []string{"content", "status"},
					Properties: map[string]*gemini.GeminiParameterSchema{
						"content": {
							Type: "STRING",
						},
						"status": {
							Type: "STRING",
							Enum: []string{"pending", "completed"},
						},
					},
				},
			},
		},
		{
			name: "Schema with oneOf",
			inputSchema: map[string]interface{}{
				"description": "A parameter that can be one of several types.",
				"oneOf": []interface{}{
					map[string]interface{}{
						"type": "string",
					},
					map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "number",
						},
					},
				},
			},
			expectedSchema: &gemini.GeminiParameterSchema{
				Type:        "ARRAY",
				Description: "A parameter that can be one of several types.",
				Items: &gemini.GeminiParameterSchema{
					Type: "NUMBER",
				},
			},
		},
		{
			name: "Schema with unsupported keywords",
			inputSchema: map[string]interface{}{
				"$schema":              "http://json-schema.org/draft-07/schema#",
				"type":                 "object",
				"additionalProperties": false,
				"description":          "An object with extra keywords.",
				"properties": map[string]interface{}{
					"value": map[string]interface{}{
						"type":             "number",
						"exclusiveMinimum": 0,
					},
				},
			},
			expectedSchema: &gemini.GeminiParameterSchema{
				Type:        "OBJECT",
				Description: "An object with extra keywords.",
				Properties: map[string]*gemini.GeminiParameterSchema{
					"value": {
						Type: "NUMBER",
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actualSchema := convertToGeminiSchema(tc.inputSchema)

			if !reflect.DeepEqual(actualSchema, tc.expectedSchema) {
				actualJSON, _ := json.MarshalIndent(actualSchema, "", "  ")
				expectedJSON, _ := json.MarshalIndent(tc.expectedSchema, "", "  ")
				t.Errorf("Schema conversion failed.\nExpected:\n%s\n\nGot:\n%s", string(expectedJSON), string(actualJSON))
			}
		})
	}
}
