package transform

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dvcrn/gemini-code-assist-proxy/internal/gemini"
	"github.com/dvcrn/gemini-code-assist-proxy/internal/logger"
	"github.com/dvcrn/gemini-code-assist-proxy/internal/openai"
)

// ToGeminiRequest converts an OpenAI chat completion request to a Gemini generateContent request.
func ToGeminiRequest(openAIReq *openai.ChatCompletionRequest, projectID string) (*gemini.GenerateContentRequest, error) {
	var internalReq gemini.GeminiInternalRequest

	// Handle messages and system instructions
	geminiContents, systemInstruction, err := convertMessagesToGeminiContents(openAIReq.Messages)
	if err != nil {
		return nil, fmt.Errorf("failed to convert messages: %w", err)
	}

	// Handle tools
	geminiTools := convertToolsToGeminiTools(openAIReq.Tools)

	// Handle generation config
	var genCfg *gemini.GeminiGenerationConfig
	if openAIReq.Temperature > 0 || openAIReq.MaxTokens > 0 {
		genCfg = &gemini.GeminiGenerationConfig{
			Temperature:     openAIReq.Temperature,
			MaxOutputTokens: openAIReq.MaxTokens,
		}
	}

	internalReq = gemini.GeminiInternalRequest{
		Contents:          geminiContents,
		SystemInstruction: systemInstruction,
		Tools:             geminiTools,
		GenerationConfig:  genCfg,
	}

	geminiReq := &gemini.GenerateContentRequest{
		Model:   openAIReq.Model,
		Project: projectID,
		Request: internalReq,
	}

	return geminiReq, nil
}

// convertMessagesToGeminiContents converts OpenAI messages to Gemini's content format.
// It also extracts the system message as a separate systemInstruction.
func convertMessagesToGeminiContents(messages []openai.Message) (geminiContents []gemini.Content, systemInstruction *gemini.SystemInstruction, err error) {
	// Build tool_call_id -> function name map from assistant tool calls
	toolCallNameByID := map[string]string{}
	for _, m := range messages {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			for _, tc := range m.ToolCalls {
				if tc.ID != "" && tc.Function.Name != "" {
					toolCallNameByID[tc.ID] = tc.Function.Name
				}
			}
		}
	}

	for _, msg := range messages {
		if msg.Role == "system" {
			// Allow multiple system messages by concatenating their parts
			if systemInstruction == nil {
				systemInstruction = &gemini.SystemInstruction{
					Role:  "system",
					Parts: []gemini.ContentPart{},
				}
			}

			switch content := msg.Content.(type) {
			case string:
				if content != "" {
					systemInstruction.Parts = append(systemInstruction.Parts, gemini.ContentPart{Text: content})
				}
			case []interface{}:
				// Support array content for system messages (e.g., [{"type":"text","text":"..."}])
				for _, part := range content {
					if p, ok := part.(map[string]interface{}); ok && p["type"] == "text" {
						if txt, ok2 := p["text"].(string); ok2 && txt != "" {
							systemInstruction.Parts = append(systemInstruction.Parts, gemini.ContentPart{Text: txt})
						}
					}
				}
			default:
				// Ignore unsupported content types for system messages
			}
			continue // System message is not part of contents
		}

		var role string
		switch msg.Role {
		case "user":
			role = "user"
		case "assistant":
			role = "model"
		// TODO: handle tool role
		default:
			role = "user" // Default to user
		}

		var parts []gemini.ContentPart
		switch content := msg.Content.(type) {
		case string:
			if msg.Role == "tool" {
				resolvedName := msg.Name
				if resolvedName == "" && msg.ToolCallID != "" {
					if n, ok := toolCallNameByID[msg.ToolCallID]; ok {
						resolvedName = n
					}
				}
				if resolvedName == "" {
					return nil, nil, fmt.Errorf("tool response missing function name and unresolved tool_call_id")
				}

				// Log forwarding of tool response (string content) with preview
				preview := content
				if len(preview) > 300 {
					preview = preview[:300] + "..."
				}
				logger.Get().Info().
					Str("function", resolvedName).
					Str("tool_call_id", msg.ToolCallID).
					Int("response_len", len(content)).
					Str("response_preview", preview).
					Msg("Forwarding tool response to Gemini")

				resp := map[string]interface{}{"output": content}
				parts = append(parts, gemini.ContentPart{
					FunctionResponse: &gemini.FunctionResponse{
						Name:     resolvedName,
						Response: resp,
					},
				})
			} else {
				parts = append(parts, gemini.ContentPart{Text: content})
			}
		case []interface{}:
			if msg.Role == "tool" {
				var buf strings.Builder
				for _, part := range content {
					if p, ok := part.(map[string]interface{}); ok && p["type"] == "text" {
						if txt, ok2 := p["text"].(string); ok2 && txt != "" {
							if buf.Len() > 0 {
								buf.WriteString("\n")
							}
							buf.WriteString(txt)
						}
					}
				}
				resolvedName := msg.Name
				if resolvedName == "" && msg.ToolCallID != "" {
					if n, ok := toolCallNameByID[msg.ToolCallID]; ok {
						resolvedName = n
					}
				}
				if resolvedName == "" {
					return nil, nil, fmt.Errorf("tool response missing function name and unresolved tool_call_id")
				}

				// Log forwarding of tool response (aggregated text parts) with preview
				full := buf.String()
				preview := full
				if len(preview) > 300 {
					preview = preview[:300] + "..."
				}
				logger.Get().Info().
					Str("function", resolvedName).
					Str("tool_call_id", msg.ToolCallID).
					Int("response_len", len(full)).
					Str("response_preview", preview).
					Msg("Forwarding tool response to Gemini")

				resp := map[string]interface{}{"output": full}
				parts = append(parts, gemini.ContentPart{
					FunctionResponse: &gemini.FunctionResponse{
						Name:     resolvedName,
						Response: resp,
					},
				})
			} else {
				for _, part := range content {
					if p, ok := part.(map[string]interface{}); ok && p["type"] == "text" {
						if txt, ok2 := p["text"].(string); ok2 {
							parts = append(parts, gemini.ContentPart{Text: txt})
						}
					}
					// TODO: Handle other part types like images
				}
			}
		default:
			// Ignore unsupported content types for now
		}

		// Map assistant tool calls to functionCall parts
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			for _, tc := range msg.ToolCalls {
				var args map[string]interface{}
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
					args = map[string]interface{}{}
				}
				parts = append(parts, gemini.ContentPart{
					FunctionCall: &gemini.FunctionCall{
						Name: tc.Function.Name,
						Args: args,
					},
				})
			}
		}

		if len(parts) > 0 {
			geminiContents = append(geminiContents, gemini.Content{
				Role:  role,
				Parts: parts,
			})
		}
	}

	return geminiContents, systemInstruction, nil
}

func convertToolsToGeminiTools(tools []openai.Tool) []gemini.Tool {
	if len(tools) == 0 {
		return nil
	}

	var fns []gemini.FunctionDeclaration
	for _, t := range tools {
		if strings.ToLower(t.Type) != "function" {
			continue
		}

		var schema gemini.JSONSchema
		if m, ok := t.Function.Parameters.(map[string]interface{}); ok {
			schema = m
		}

		fns = append(fns, gemini.FunctionDeclaration{
			Name:                 t.Function.Name,
			Description:          t.Function.Description,
			ParametersJsonSchema: schema,
		})
	}

	if len(fns) == 0 {
		return nil
	}

	return []gemini.Tool{
		{FunctionDeclarations: fns},
	}
}
