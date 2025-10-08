package transform

import (
	"fmt"
	"strings"

	"github.com/dvcrn/gemini-code-assist-proxy/internal/gemini"
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
			parts = append(parts, gemini.ContentPart{Text: content})
		case []interface{}:
			for _, part := range content {
				if p, ok := part.(map[string]interface{}); ok && p["type"] == "text" {
					if txt, ok2 := p["text"].(string); ok2 {
						parts = append(parts, gemini.ContentPart{Text: txt})
					}
				}
				// TODO: Handle other part types like images
			}
		default:
			// Ignore unsupported content types for now
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
