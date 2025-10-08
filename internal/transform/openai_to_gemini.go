package transform

import (
	"fmt"

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

	// TODO: Handle tools

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
			if systemInstruction != nil {
				return nil, nil, fmt.Errorf("multiple system messages are not supported")
			}

			switch content := msg.Content.(type) {
			case string:
				systemInstruction = &gemini.SystemInstruction{
					Parts: []gemini.ContentPart{{Text: content}},
				}
			default:
				return nil, nil, fmt.Errorf("unsupported system message content type: %T", msg.Content)
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
