package openai

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/dvcrn/gemini-code-assist-proxy/internal/logger"
	"github.com/google/uuid"
)

// StreamChunk represents a chunk of data from Gemini
type StreamChunk struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

// ReasoningData contains reasoning information
type ReasoningData struct {
	Reasoning string `json:"reasoning,omitempty"`
	ToolCode  string `json:"toolCode,omitempty"`
}

// GeminiFunctionCall represents a function call from Gemini
type GeminiFunctionCall struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args"`
}

// UsageData contains token usage information
type UsageData struct {
	InputTokens  int `json:"inputTokens"`
	OutputTokens int `json:"outputTokens"`
}

// NativeToolResponse represents a native tool response
type NativeToolResponse struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

// OpenAIToolCall represents a tool call in OpenAI format
type OpenAIToolCall struct {
	Index    int                `json:"index"`
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function OpenAIFunctionCall `json:"function"`
}

// OpenAIFunctionCall represents the function part of a tool call
type OpenAIFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// OpenAIDelta represents the delta object in a streaming response
type OpenAIDelta struct {
	Role             *string              `json:"role,omitempty"`
	Content          *string              `json:"content,omitempty"`
	Reasoning        *string              `json:"reasoning,omitempty"`
	ReasoningContent *string              `json:"reasoning_content,omitempty"`
	ToolCalls        []OpenAIToolCall     `json:"tool_calls,omitempty"`
	NativeToolCalls  []NativeToolResponse `json:"native_tool_calls,omitempty"`
	Grounding        interface{}          `json:"grounding,omitempty"`
}

// OpenAIChoice represents a choice in the streaming response
type OpenAIChoice struct {
	Index        int          `json:"index"`
	Delta        OpenAIDelta  `json:"delta"`
	FinishReason *string      `json:"finish_reason"`
	Logprobs     *interface{} `json:"logprobs,omitempty"`
	MatchedStop  *interface{} `json:"matched_stop,omitempty"`
}

// OpenAIChunk represents a chunk in OpenAI streaming format
type OpenAIChunk struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []OpenAIChoice `json:"choices"`
	Usage   *interface{}   `json:"usage"`
}

// OpenAIUsage represents token usage in the final chunk
type OpenAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// OpenAIFinalChoice represents a choice in the final chunk
type OpenAIFinalChoice struct {
	Index        int                    `json:"index"`
	Delta        map[string]interface{} `json:"delta"`
	FinishReason string                 `json:"finish_reason"`
}

// OpenAIFinalChunk represents the final chunk with usage data
type OpenAIFinalChunk struct {
	ID      string              `json:"id"`
	Object  string              `json:"object"`
	Created int64               `json:"created"`
	Model   string              `json:"model"`
	Choices []OpenAIFinalChoice `json:"choices"`
	Usage   *OpenAIUsage        `json:"usage,omitempty"`
}

const (
	OpenAIChatCompletionChunkObject = "chat.completion.chunk"
)

// CreateOpenAIStreamTransformer creates a transformer that converts Gemini StreamChunks
// into OpenAI-compatible SSE formatted strings.
// It returns a function that accepts an input channel and returns an output channel.
func CreateOpenAIStreamTransformer(model string) func(<-chan StreamChunk) <-chan string {
	return func(input <-chan StreamChunk) <-chan string {
		output := make(chan string, 10)

		go func() {
			defer close(output)

			chatID := fmt.Sprintf("chatcmpl-%s", uuid.New().String())
			creationTime := time.Now().Unix()
			firstChunk := true
			var toolCallID *string
			var usageData *UsageData

			// Process each chunk
			for chunk := range input {
				logger.Get().Info().Interface("chunk", chunk).Msg("Processing Gemini stream chunk")

				delta := OpenAIDelta{}
				shouldSend := false

				switch chunk.Type {
				case "text", "thinking_content":
					if text, ok := chunk.Data.(string); ok {
						delta.Content = &text
						if firstChunk {
							role := "assistant"
							delta.Role = &role
							firstChunk = false
						}
						shouldSend = true
					}

				case "real_thinking":
					if text, ok := chunk.Data.(string); ok {
						delta.Reasoning = &text
						shouldSend = true
					}

				case "reasoning":
					if reasoningData, ok := toReasoningData(chunk.Data); ok {
						delta.Reasoning = &reasoningData.Reasoning
						shouldSend = true
					}

				case "tool_code":
					if funcCall, ok := toGeminiFunctionCall(chunk.Data); ok {
						callID := fmt.Sprintf("call_%s", uuid.New().String())
						toolCallID = &callID

						argsJSON, _ := json.Marshal(funcCall.Args)
						delta.ToolCalls = []OpenAIToolCall{
							{
								Index: 0,
								ID:    callID,
								Type:  "function",
								Function: OpenAIFunctionCall{
									Name:      funcCall.Name,
									Arguments: string(argsJSON),
								},
							},
						}

						if firstChunk {
							role := "assistant"
							delta.Role = &role
							nullContent := ""
							delta.Content = &nullContent
							firstChunk = false
						}
						shouldSend = true
					}

				case "native_tool":
					if toolResp, ok := toNativeToolResponse(chunk.Data); ok {
						delta.NativeToolCalls = []NativeToolResponse{toolResp}
						shouldSend = true
					}

				case "grounding_metadata":
					if chunk.Data != nil {
						delta.Grounding = chunk.Data
						shouldSend = true
					}

				case "usage":
					if usage, ok := toUsageData(chunk.Data); ok {
						usageData = &usage
					}
					// Don't send a chunk for usage data
					continue
				}

				if shouldSend {
					openAIChunk := OpenAIChunk{
						ID:      chatID,
						Object:  OpenAIChatCompletionChunkObject,
						Created: creationTime,
						Model:   model,
						Choices: []OpenAIChoice{
							{
								Index:        0,
								Delta:        delta,
								FinishReason: nil,
								Logprobs:     nil,
								MatchedStop:  nil,
							},
						},
						Usage: nil,
					}

					if jsonBytes, err := json.Marshal(openAIChunk); err == nil {
						sse := fmt.Sprintf("data: %s\n\n", string(jsonBytes))
						logger.Get().Info().Str("sse", sse).Msg("Sending OpenAI SSE chunk")
						output <- sse
					}
				}
			}

			// Send final chunk
			finishReason := "stop"
			if toolCallID != nil {
				finishReason = "tool_calls"
			}

			finalChunk := OpenAIFinalChunk{
				ID:      chatID,
				Object:  OpenAIChatCompletionChunkObject,
				Created: creationTime,
				Model:   model,
				Choices: []OpenAIFinalChoice{
					{
						Index:        0,
						Delta:        map[string]interface{}{},
						FinishReason: finishReason,
					},
				},
			}

			if usageData != nil {
				finalChunk.Usage = &OpenAIUsage{
					PromptTokens:     usageData.InputTokens,
					CompletionTokens: usageData.OutputTokens,
					TotalTokens:      usageData.InputTokens + usageData.OutputTokens,
				}
			}

			if jsonBytes, err := json.Marshal(finalChunk); err == nil {
				output <- fmt.Sprintf("data: %s\n\n", string(jsonBytes))
			}

			output <- "data: [DONE]\n\n"
		}()

		return output
	}
}

// Type conversion helpers

func toReasoningData(data interface{}) (ReasoningData, bool) {
	if data == nil {
		return ReasoningData{}, false
	}

	// Try direct type assertion
	if rd, ok := data.(ReasoningData); ok {
		return rd, true
	}

	// Try map conversion
	if m, ok := data.(map[string]interface{}); ok {
		rd := ReasoningData{}
		if reasoning, ok := m["reasoning"].(string); ok {
			rd.Reasoning = reasoning
		}
		if toolCode, ok := m["toolCode"].(string); ok {
			rd.ToolCode = toolCode
		}
		return rd, rd.Reasoning != "" || rd.ToolCode != ""
	}

	return ReasoningData{}, false
}

func toGeminiFunctionCall(data interface{}) (GeminiFunctionCall, bool) {
	if data == nil {
		return GeminiFunctionCall{}, false
	}

	// Try direct type assertion
	if fc, ok := data.(GeminiFunctionCall); ok {
		return fc, true
	}

	// Try map conversion
	if m, ok := data.(map[string]interface{}); ok {
		fc := GeminiFunctionCall{}
		if name, ok := m["name"].(string); ok {
			fc.Name = name
		}
		if args, ok := m["args"].(map[string]interface{}); ok {
			fc.Args = args
		}
		return fc, fc.Name != "" && fc.Args != nil
	}

	return GeminiFunctionCall{}, false
}

func toUsageData(data interface{}) (UsageData, bool) {
	if data == nil {
		return UsageData{}, false
	}

	// Try direct type assertion
	if ud, ok := data.(UsageData); ok {
		return ud, true
	}

	// Try map conversion
	if m, ok := data.(map[string]interface{}); ok {
		ud := UsageData{}
		if inputTokens, ok := m["inputTokens"].(int); ok {
			ud.InputTokens = inputTokens
		} else if inputTokens, ok := m["inputTokens"].(float64); ok {
			ud.InputTokens = int(inputTokens)
		}
		if outputTokens, ok := m["outputTokens"].(int); ok {
			ud.OutputTokens = outputTokens
		} else if outputTokens, ok := m["outputTokens"].(float64); ok {
			ud.OutputTokens = int(outputTokens)
		}
		return ud, true
	}

	return UsageData{}, false
}

func toNativeToolResponse(data interface{}) (NativeToolResponse, bool) {
	if data == nil {
		return NativeToolResponse{}, false
	}

	// Try direct type assertion
	if ntr, ok := data.(NativeToolResponse); ok {
		return ntr, true
	}

	// Try map conversion
	if m, ok := data.(map[string]interface{}); ok {
		ntr := NativeToolResponse{}
		if typeVal, ok := m["type"].(string); ok {
			ntr.Type = typeVal
		}
		if dataVal, ok := m["data"]; ok {
			ntr.Data = dataVal
		}
		return ntr, ntr.Type != "" && ntr.Data != nil
	}

	return NativeToolResponse{}, false
}
