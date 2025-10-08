package transform

import (
	"testing"

	"github.com/dvcrn/gemini-code-assist-proxy/internal/openai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestToolParityAggregation ensures that when an assistant turn emits multiple tool_calls,
// and subsequent tool result messages follow, we aggregate the tool results into a single
// user turn with the same number of functionResponse parts as functionCall parts.
// This satisfies CloudCode's requirement that each function call is matched by exactly
// one function response in the following user turn.
func TestToolParityAggregation(t *testing.T) {
	req := &openai.ChatCompletionRequest{
		Model: "gemini-2.5-pro",
		Messages: []openai.Message{
			{
				Role:    "user",
				Content: "Please read poem.md and search for 'night'.",
			},
			{
				Role: "assistant",
				ToolCalls: []openai.OpenAIToolCall{
					{
						Index: 0,
						ID:    "call_1",
						Type:  "function",
						Function: openai.OpenAIFunctionCall{
							Name:      "read",
							Arguments: `{"file_path":"poem.md"}`,
						},
					},
					{
						Index: 1,
						ID:    "call_2",
						Type:  "function",
						Function: openai.OpenAIFunctionCall{
							Name:      "grep",
							Arguments: `{"file_path":"poem.md","pattern":"night"}`,
						},
					},
				},
			},
			{
				Role:       "tool",
				ToolCallID: "call_1",
				// Intentionally omit Name to validate resolution via tool_call_id -> function name
				Name:    "",
				Content: "Once upon a midnight dreary,\nWhile I pondered, weak and weary, ...",
			},
			{
				Role:       "tool",
				ToolCallID: "call_2",
				Name:       "",
				Content:    "Line 1: ... night ...\nLine 4: ... night ...",
			},
		},
		Stream: true,
	}

	got, err := ToGeminiRequest(req, "test-project")
	require.NoError(t, err, "ToGeminiRequest should succeed")
	require.NotNil(t, got, "GenerateContentRequest must not be nil")

	contents := got.Request.Contents
	// Expect three turns: user (prompt), model (tool calls), user (aggregated tool responses)
	require.Len(t, contents, 3, "expected user -> model -> user turns")

	// Validate assistant (model) turn contains two functionCall parts
	modelTurn := contents[1]
	assert.Equal(t, "model", modelTurn.Role, "second turn should be model (assistant)")

	callNames := make([]string, 0, 2)
	callCount := 0
	for _, p := range modelTurn.Parts {
		if p.FunctionCall != nil {
			callCount++
			callNames = append(callNames, p.FunctionCall.Name)
			require.NotNil(t, p.FunctionCall.Args, "functionCall args should not be nil")
		}
	}
	assert.Equal(t, 2, callCount, "expected exactly 2 functionCall parts in model turn")
	assert.Equal(t, []string{"read", "grep"}, callNames, "functionCall names should match tool_calls order")

	// Validate following user turn contains two functionResponse parts (aggregated)
	userToolRespTurn := contents[2]
	assert.Equal(t, "user", userToolRespTurn.Role, "third turn should be user (tool responses)")

	respNames := make([]string, 0, 2)
	respCount := 0
	for _, p := range userToolRespTurn.Parts {
		if p.FunctionResponse != nil {
			respCount++
			respNames = append(respNames, p.FunctionResponse.Name)
			require.NotNil(t, p.FunctionResponse.Response, "functionResponse response should not be nil")
			// Ensure we preserved some output (non-empty)
			if out, ok := p.FunctionResponse.Response["output"].(string); ok {
				assert.NotEmpty(t, out, "functionResponse.output should not be empty")
			} else {
				t.Fatalf("functionResponse.response.output missing or not string: %#v", p.FunctionResponse.Response)
			}
		}
	}
	assert.Equal(t, 2, respCount, "expected exactly 2 functionResponse parts in aggregated user turn")
	assert.Equal(t, []string{"read", "grep"}, respNames, "functionResponse names should resolve from tool_call_id and match call order")
}
