package transform

import (
	"fmt"
	"time"

	"github.com/dvcrn/gemini-code-assist-proxy/internal/gemini"
	"github.com/dvcrn/gemini-code-assist-proxy/internal/openai"
	"github.com/google/uuid"
)

func ToOpenAIChatCompletionResponse(geminiResp *gemini.GenerateContentResponse, model string) (*openai.ChatCompletionResponse, error) {
	choices := []openai.Choice{}
	for i, candidate := range geminiResp.Response["candidates"].([]interface{}) {
		candidateMap, ok := candidate.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("failed to cast candidate to map")
		}

		content, ok := candidateMap["content"].(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("failed to cast content to map")
		}

		parts, ok := content["parts"].([]interface{})
		if !ok {
			return nil, fmt.Errorf("failed to cast parts to slice")
		}

		var contentText string
		for _, part := range parts {
			partMap, ok := part.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("failed to cast part to map")
			}
			text, ok := partMap["text"].(string)
			if ok {
				contentText += text
			}
		}

		choices = append(choices, openai.Choice{
			Index: i,
			Message: openai.Message{
				Role:    "assistant",
				Content: contentText,
			},
			FinishReason: "stop", // TODO: Map finish reason
		})
	}

	var promptTokens, completionTokens, totalTokens int
	if usage, ok := geminiResp.Response["usageMetadata"].(map[string]interface{}); ok {
		if pt, ok := usage["promptTokenCount"].(float64); ok {
			promptTokens = int(pt)
		}
		if ct, ok := usage["candidatesTokenCount"].(float64); ok {
			completionTokens = int(ct)
		}
		if tt, ok := usage["totalTokenCount"].(float64); ok {
			totalTokens = int(tt)
		} else {
			totalTokens = promptTokens + completionTokens
		}
	}

	return &openai.ChatCompletionResponse{
		ID:      fmt.Sprintf("chatcmpl-%s", uuid.New().String()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: choices,
		Usage: openai.Usage{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      totalTokens,
		},
	}, nil
}
