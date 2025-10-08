package openai

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCreateOpenAIStreamTransformer_BasicText(t *testing.T) {
	model := "gemini-2.5-pro"
	transformer := CreateOpenAIStreamTransformer(model)

	input := make(chan StreamChunk, 1)
	input <- StreamChunk{Type: "text", Data: "Hello, world!"}
	close(input)

	output := transformer(input)

	var chunks []string
	for chunk := range output {
		chunks = append(chunks, chunk)
	}

	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 chunks (data + final + DONE), got %d", len(chunks))
	}

	// First chunk should contain the text
	firstChunk := chunks[0]
	if !strings.Contains(firstChunk, "data: ") {
		t.Error("expected SSE format with 'data: ' prefix")
	}

	var parsed OpenAIChunk
	jsonStr := strings.TrimPrefix(firstChunk, "data: ")
	jsonStr = strings.TrimSpace(jsonStr)
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		t.Fatalf("failed to parse chunk JSON: %v", err)
	}

	if parsed.Model != model {
		t.Errorf("expected model %s, got %s", model, parsed.Model)
	}

	if len(parsed.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(parsed.Choices))
	}

	choice := parsed.Choices[0]
	if choice.Delta.Content == nil || *choice.Delta.Content != "Hello, world!" {
		t.Errorf("expected content 'Hello, world!', got %v", choice.Delta.Content)
	}

	if choice.Delta.Role == nil || *choice.Delta.Role != "assistant" {
		t.Error("expected first chunk to include role 'assistant'")
	}

	// Last chunk should be [DONE]
	lastChunk := chunks[len(chunks)-1]
	if !strings.Contains(lastChunk, "[DONE]") {
		t.Error("expected last chunk to contain [DONE]")
	}
}

func TestCreateOpenAIStreamTransformer_ThinkingContent(t *testing.T) {
	model := "gemini-2.5-pro"
	transformer := CreateOpenAIStreamTransformer(model)

	input := make(chan StreamChunk, 1)
	input <- StreamChunk{Type: "thinking_content", Data: "Let me think..."}
	close(input)

	output := transformer(input)

	for chunk := range output {
		if strings.Contains(chunk, "Let me think...") {
			var parsed OpenAIChunk
			jsonStr := strings.TrimPrefix(chunk, "data: ")
			jsonStr = strings.TrimSpace(jsonStr)
			if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
				t.Fatalf("failed to parse chunk: %v", err)
			}

			if parsed.Choices[0].Delta.Content == nil || *parsed.Choices[0].Delta.Content != "Let me think..." {
				t.Error("expected thinking_content to be in delta.content")
			}
			return
		}
	}

	t.Error("thinking_content chunk not found")
}

func TestCreateOpenAIStreamTransformer_RealThinking(t *testing.T) {
	model := "gemini-2.5-pro"
	transformer := CreateOpenAIStreamTransformer(model)

	input := make(chan StreamChunk, 1)
	input <- StreamChunk{Type: "real_thinking", Data: "Analyzing the problem..."}
	close(input)

	output := transformer(input)

	for chunk := range output {
		if strings.Contains(chunk, "Analyzing") {
			var parsed OpenAIChunk
			jsonStr := strings.TrimPrefix(chunk, "data: ")
			jsonStr = strings.TrimSpace(jsonStr)
			if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
				t.Fatalf("failed to parse chunk: %v", err)
			}

			if parsed.Choices[0].Delta.Reasoning == nil || *parsed.Choices[0].Delta.Reasoning != "Analyzing the problem..." {
				t.Error("expected real_thinking to be in delta.reasoning")
			}
			return
		}
	}

	t.Error("real_thinking chunk not found")
}

func TestCreateOpenAIStreamTransformer_ReasoningData(t *testing.T) {
	model := "gemini-2.5-pro"
	transformer := CreateOpenAIStreamTransformer(model)

	input := make(chan StreamChunk, 1)
	input <- StreamChunk{
		Type: "reasoning",
		Data: map[string]interface{}{
			"reasoning": "This is my reasoning",
			"toolCode":  "some code",
		},
	}
	close(input)

	output := transformer(input)

	for chunk := range output {
		if strings.Contains(chunk, "reasoning") {
			var parsed OpenAIChunk
			jsonStr := strings.TrimPrefix(chunk, "data: ")
			jsonStr = strings.TrimSpace(jsonStr)
			if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
				t.Fatalf("failed to parse chunk: %v", err)
			}

			if parsed.Choices[0].Delta.Reasoning == nil || *parsed.Choices[0].Delta.Reasoning != "This is my reasoning" {
				t.Error("expected reasoning data to be extracted")
			}
			return
		}
	}

	t.Error("reasoning chunk not found")
}

func TestCreateOpenAIStreamTransformer_ToolCall(t *testing.T) {
	model := "gemini-2.5-pro"
	transformer := CreateOpenAIStreamTransformer(model)

	input := make(chan StreamChunk, 1)
	input <- StreamChunk{
		Type: "tool_code",
		Data: map[string]interface{}{
			"name": "get_weather",
			"args": map[string]interface{}{
				"location": "San Francisco",
			},
		},
	}
	close(input)

	output := transformer(input)

	var foundToolCall bool
	var finalChunk string
	var allChunks []string

	for chunk := range output {
		allChunks = append(allChunks, chunk)
		if strings.Contains(chunk, "[DONE]") {
			continue
		}

		if !strings.HasPrefix(chunk, "data: ") {
			continue
		}

		jsonStr := strings.TrimPrefix(chunk, "data: ")
		jsonStr = strings.TrimSpace(jsonStr)

		var parsed OpenAIChunk
		if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
			var finalParsed OpenAIFinalChunk
			if err2 := json.Unmarshal([]byte(jsonStr), &finalParsed); err2 == nil {
				if finalParsed.Choices[0].FinishReason != "" {
					finalChunk = chunk
				}
			}
			continue
		}

		if len(parsed.Choices) > 0 && len(parsed.Choices[0].Delta.ToolCalls) > 0 {
			foundToolCall = true

			if len(parsed.Choices[0].Delta.ToolCalls) != 1 {
				t.Fatalf("expected 1 tool call, got %d", len(parsed.Choices[0].Delta.ToolCalls))
			}

			toolCall := parsed.Choices[0].Delta.ToolCalls[0]
			if toolCall.Function.Name != "get_weather" {
				t.Errorf("expected function name 'get_weather', got %s", toolCall.Function.Name)
			}

			if !strings.Contains(toolCall.ID, "call_") {
				t.Error("expected tool call ID to start with 'call_'")
			}

			if toolCall.Type != "function" {
				t.Errorf("expected type 'function', got %s", toolCall.Type)
			}

			if parsed.Choices[0].Delta.Role == nil || *parsed.Choices[0].Delta.Role != "assistant" {
				t.Error("expected first tool call chunk to have role")
			}
		}
	}

	if !foundToolCall {
		t.Errorf("tool call chunk not found. All chunks: %v", allChunks)
	}

	if finalChunk != "" {
		var parsed OpenAIFinalChunk
		jsonStr := strings.TrimPrefix(finalChunk, "data: ")
		jsonStr = strings.TrimSpace(jsonStr)
		if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
			t.Fatalf("failed to parse final chunk: %v", err)
		}

		if parsed.Choices[0].FinishReason != "tool_calls" {
			t.Errorf("expected finish_reason 'tool_calls', got %s", parsed.Choices[0].FinishReason)
		}
	}
}

func TestCreateOpenAIStreamTransformer_NativeTool(t *testing.T) {
	model := "gemini-2.5-pro"
	transformer := CreateOpenAIStreamTransformer(model)

	input := make(chan StreamChunk, 1)
	input <- StreamChunk{
		Type: "native_tool",
		Data: map[string]interface{}{
			"type": "code_execution",
			"data": map[string]interface{}{
				"result": "42",
			},
		},
	}
	close(input)

	output := transformer(input)

	for chunk := range output {
		if strings.Contains(chunk, "native_tool_calls") {
			var parsed OpenAIChunk
			jsonStr := strings.TrimPrefix(chunk, "data: ")
			jsonStr = strings.TrimSpace(jsonStr)
			if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
				t.Fatalf("failed to parse chunk: %v", err)
			}

			if len(parsed.Choices[0].Delta.NativeToolCalls) != 1 {
				t.Fatalf("expected 1 native tool call, got %d", len(parsed.Choices[0].Delta.NativeToolCalls))
			}
			return
		}
	}

	t.Error("native_tool chunk not found")
}

func TestCreateOpenAIStreamTransformer_Grounding(t *testing.T) {
	model := "gemini-2.5-pro"
	transformer := CreateOpenAIStreamTransformer(model)

	input := make(chan StreamChunk, 1)
	input <- StreamChunk{
		Type: "grounding_metadata",
		Data: map[string]interface{}{
			"sources": []string{"source1", "source2"},
		},
	}
	close(input)

	output := transformer(input)

	for chunk := range output {
		if strings.Contains(chunk, "grounding") {
			var parsed OpenAIChunk
			jsonStr := strings.TrimPrefix(chunk, "data: ")
			jsonStr = strings.TrimSpace(jsonStr)
			if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
				t.Fatalf("failed to parse chunk: %v", err)
			}

			if parsed.Choices[0].Delta.Grounding == nil {
				t.Error("expected grounding data")
			}
			return
		}
	}

	t.Error("grounding chunk not found")
}

func TestCreateOpenAIStreamTransformer_UsageData(t *testing.T) {
	model := "gemini-2.5-pro"
	transformer := CreateOpenAIStreamTransformer(model)

	input := make(chan StreamChunk, 2)
	input <- StreamChunk{Type: "text", Data: "Hello"}
	input <- StreamChunk{
		Type: "usage",
		Data: map[string]interface{}{
			"inputTokens":  float64(10),
			"outputTokens": float64(20),
		},
	}
	close(input)

	output := transformer(input)

	var foundUsage bool
	for chunk := range output {
		if strings.Contains(chunk, "prompt_tokens") {
			var parsed OpenAIFinalChunk
			jsonStr := strings.TrimPrefix(chunk, "data: ")
			jsonStr = strings.TrimSpace(jsonStr)
			if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
				t.Fatalf("failed to parse final chunk: %v", err)
			}

			if parsed.Usage == nil {
				t.Fatal("expected usage data in final chunk")
			}

			if parsed.Usage.PromptTokens != 10 {
				t.Errorf("expected prompt_tokens 10, got %d", parsed.Usage.PromptTokens)
			}

			if parsed.Usage.CompletionTokens != 20 {
				t.Errorf("expected completion_tokens 20, got %d", parsed.Usage.CompletionTokens)
			}

			if parsed.Usage.TotalTokens != 30 {
				t.Errorf("expected total_tokens 30, got %d", parsed.Usage.TotalTokens)
			}

			foundUsage = true
		}
	}

	if !foundUsage {
		t.Error("usage data not found in final chunk")
	}
}

func TestCreateOpenAIStreamTransformer_MultipleChunks(t *testing.T) {
	model := "gemini-2.5-pro"
	transformer := CreateOpenAIStreamTransformer(model)

	input := make(chan StreamChunk, 3)
	input <- StreamChunk{Type: "text", Data: "Hello"}
	input <- StreamChunk{Type: "text", Data: " world"}
	input <- StreamChunk{Type: "text", Data: "!"}
	close(input)

	output := transformer(input)

	var textChunks []string
	for chunk := range output {
		if strings.Contains(chunk, "data: {") {
			var parsed OpenAIChunk
			jsonStr := strings.TrimPrefix(chunk, "data: ")
			jsonStr = strings.TrimSpace(jsonStr)
			if err := json.Unmarshal([]byte(jsonStr), &parsed); err == nil {
				if len(parsed.Choices) > 0 && parsed.Choices[0].Delta.Content != nil {
					textChunks = append(textChunks, *parsed.Choices[0].Delta.Content)
				}
			}
		}
	}

	if len(textChunks) != 3 {
		t.Errorf("expected 3 text chunks, got %d", len(textChunks))
	}

	// Only first chunk should have role
	transformer2 := CreateOpenAIStreamTransformer(model)
	input2 := make(chan StreamChunk, 2)
	input2 <- StreamChunk{Type: "text", Data: "First"}
	input2 <- StreamChunk{Type: "text", Data: "Second"}
	close(input2)

	output2 := transformer2(input2)

	roleCount := 0
	chunkNum := 0
	for chunk := range output2 {
		if strings.Contains(chunk, "data: {") {
			var parsed OpenAIChunk
			jsonStr := strings.TrimPrefix(chunk, "data: ")
			jsonStr = strings.TrimSpace(jsonStr)
			if err := json.Unmarshal([]byte(jsonStr), &parsed); err == nil {
				if len(parsed.Choices) > 0 && parsed.Choices[0].Delta.Role != nil {
					roleCount++
					if chunkNum != 0 {
						t.Error("role should only be in first chunk")
					}
				}
				chunkNum++
			}
		}
	}

	if roleCount != 1 {
		t.Errorf("expected role in exactly 1 chunk, got %d", roleCount)
	}
}

func TestCreateOpenAIStreamTransformer_EmptyChannel(t *testing.T) {
	model := "gemini-2.5-pro"
	transformer := CreateOpenAIStreamTransformer(model)

	input := make(chan StreamChunk)
	close(input)

	output := transformer(input)

	var chunks []string
	for chunk := range output {
		chunks = append(chunks, chunk)
	}

	// Should still send final chunk and DONE
	if len(chunks) < 2 {
		t.Error("expected at least final chunk and DONE")
	}

	lastChunk := chunks[len(chunks)-1]
	if !strings.Contains(lastChunk, "[DONE]") {
		t.Error("expected [DONE] marker")
	}
}

func TestCreateOpenAIStreamTransformer_FinishReason(t *testing.T) {
	tests := []struct {
		name           string
		chunks         []StreamChunk
		expectedReason string
	}{
		{
			name: "stop for regular completion",
			chunks: []StreamChunk{
				{Type: "text", Data: "Hello"},
			},
			expectedReason: "stop",
		},
		{
			name: "tool_calls for function call",
			chunks: []StreamChunk{
				{
					Type: "tool_code",
					Data: map[string]interface{}{
						"name": "test_func",
						"args": map[string]interface{}{},
					},
				},
			},
			expectedReason: "tool_calls",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := "gemini-2.5-pro"
			transformer := CreateOpenAIStreamTransformer(model)

			input := make(chan StreamChunk, len(tt.chunks))
			for _, chunk := range tt.chunks {
				input <- chunk
			}
			close(input)

			output := transformer(input)

			for chunk := range output {
				if strings.Contains(chunk, "finish_reason") && !strings.Contains(chunk, "null") {
					var parsed OpenAIFinalChunk
					jsonStr := strings.TrimPrefix(chunk, "data: ")
					jsonStr = strings.TrimSpace(jsonStr)
					if err := json.Unmarshal([]byte(jsonStr), &parsed); err == nil {
						if parsed.Choices[0].FinishReason != tt.expectedReason {
							t.Errorf("expected finish_reason '%s', got '%s'", tt.expectedReason, parsed.Choices[0].FinishReason)
						}
						return
					}
				}
			}
		})
	}
}

func TestTypeConversionHelpers(t *testing.T) {
	t.Run("toReasoningData", func(t *testing.T) {
		testCases := []struct {
			name     string
			input    interface{}
			expected ReasoningData
			valid    bool
		}{
			{
				name:     "valid map",
				input:    map[string]interface{}{"reasoning": "test", "toolCode": "code"},
				expected: ReasoningData{Reasoning: "test", ToolCode: "code"},
				valid:    true,
			},
			{
				name:     "direct struct",
				input:    ReasoningData{Reasoning: "direct"},
				expected: ReasoningData{Reasoning: "direct"},
				valid:    true,
			},
			{
				name:  "nil input",
				input: nil,
				valid: false,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				result, ok := toReasoningData(tc.input)
				if ok != tc.valid {
					t.Errorf("expected valid=%v, got %v", tc.valid, ok)
				}
				if tc.valid && result.Reasoning != tc.expected.Reasoning {
					t.Errorf("expected reasoning=%s, got %s", tc.expected.Reasoning, result.Reasoning)
				}
			})
		}
	})

	t.Run("toGeminiFunctionCall", func(t *testing.T) {
		args := map[string]interface{}{"key": "value"}
		testCases := []struct {
			name     string
			input    interface{}
			expected GeminiFunctionCall
			valid    bool
		}{
			{
				name:     "valid map",
				input:    map[string]interface{}{"name": "func", "args": args},
				expected: GeminiFunctionCall{Name: "func", Args: args},
				valid:    true,
			},
			{
				name:     "direct struct",
				input:    GeminiFunctionCall{Name: "direct", Args: args},
				expected: GeminiFunctionCall{Name: "direct", Args: args},
				valid:    true,
			},
			{
				name:  "nil input",
				input: nil,
				valid: false,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				result, ok := toGeminiFunctionCall(tc.input)
				if ok != tc.valid {
					t.Errorf("expected valid=%v, got %v", tc.valid, ok)
				}
				if tc.valid && result.Name != tc.expected.Name {
					t.Errorf("expected name=%s, got %s", tc.expected.Name, result.Name)
				}
			})
		}
	})

	t.Run("toUsageData", func(t *testing.T) {
		testCases := []struct {
			name     string
			input    interface{}
			expected UsageData
			valid    bool
		}{
			{
				name:     "valid map with float",
				input:    map[string]interface{}{"inputTokens": float64(10), "outputTokens": float64(20)},
				expected: UsageData{InputTokens: 10, OutputTokens: 20},
				valid:    true,
			},
			{
				name:     "valid map with int",
				input:    map[string]interface{}{"inputTokens": 15, "outputTokens": 25},
				expected: UsageData{InputTokens: 15, OutputTokens: 25},
				valid:    true,
			},
			{
				name:     "direct struct",
				input:    UsageData{InputTokens: 5, OutputTokens: 10},
				expected: UsageData{InputTokens: 5, OutputTokens: 10},
				valid:    true,
			},
			{
				name:  "nil input",
				input: nil,
				valid: false,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				result, ok := toUsageData(tc.input)
				if ok != tc.valid {
					t.Errorf("expected valid=%v, got %v", tc.valid, ok)
				}
				if tc.valid {
					if result.InputTokens != tc.expected.InputTokens {
						t.Errorf("expected inputTokens=%d, got %d", tc.expected.InputTokens, result.InputTokens)
					}
					if result.OutputTokens != tc.expected.OutputTokens {
						t.Errorf("expected outputTokens=%d, got %d", tc.expected.OutputTokens, result.OutputTokens)
					}
				}
			})
		}
	})

	t.Run("toNativeToolResponse", func(t *testing.T) {
		testCases := []struct {
			name     string
			input    interface{}
			expected NativeToolResponse
			valid    bool
		}{
			{
				name: "valid map",
				input: map[string]interface{}{
					"type": "code_execution",
					"data": map[string]interface{}{"result": "42"},
				},
				expected: NativeToolResponse{Type: "code_execution", Data: map[string]interface{}{"result": "42"}},
				valid:    true,
			},
			{
				name: "direct struct",
				input: NativeToolResponse{
					Type: "search",
					Data: "search results",
				},
				expected: NativeToolResponse{Type: "search", Data: "search results"},
				valid:    true,
			},
			{
				name:  "nil input",
				input: nil,
				valid: false,
			},
			{
				name:  "missing type",
				input: map[string]interface{}{"data": "something"},
				valid: false,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				result, ok := toNativeToolResponse(tc.input)
				if ok != tc.valid {
					t.Errorf("expected valid=%v, got %v", tc.valid, ok)
				}
				if tc.valid && result.Type != tc.expected.Type {
					t.Errorf("expected type=%s, got %s", tc.expected.Type, result.Type)
				}
			})
		}
	})
}
