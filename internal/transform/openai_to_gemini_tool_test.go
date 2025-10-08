package transform

import (
	"testing"

	"github.com/dvcrn/gemini-code-assist-proxy/internal/openai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Ensures tool responses (role: "tool") without an explicit name populate
// FunctionResponse.Name by resolving the assistant's tool_calls via tool_call_id.
// This prevents INVALID_ARGUMENT errors like:
// contents[n].parts[0].function_response.name: [REQUIRED_FIELD_MISSING]
func TestToolResponseNameResolutionViaToolCallID(t *testing.T) {
	req := &openai.ChatCompletionRequest{
		Model: "gemini-2.5-pro",
		Messages: []openai.Message{
			{
				Role:    "user",
				Content: "Please read poem.md",
			},
			{
				Role: "assistant",
				// Tool call emitted by the assistant. Note the ID "call_1".
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
				},
			},
			{
				// Tool result coming back. Name intentionally empty to force resolution via tool_call_id.
				Role:       "tool",
				ToolCallID: "call_1",
				Name:       "",
				Content:    "Once upon a midnight dreary...",
			},
		},
		Stream: true,
	}

	got, err := ToGeminiRequest(req, "test-project")
	require.NoError(t, err, "ToGeminiRequest should not error for tool response resolution")

	// We expect three contents turns (user, model, user[tool-response])
	require.Len(t, got.Request.Contents, 3, "expected 3 content turns (user, assistant, tool-response)")

	toolRespTurn := got.Request.Contents[2]
	// The transform maps "tool" role to "user" turn with FunctionResponse part
	assert.Equal(t, "user", toolRespTurn.Role, "tool responses are encoded as a user turn with functionResponse part")
	require.Len(t, toolRespTurn.Parts, 1, "expected a single part carrying the functionResponse")

	part := toolRespTurn.Parts[0]
	require.NotNil(t, part.FunctionResponse, "expected functionResponse to be populated")
	assert.Equal(t, "read", part.FunctionResponse.Name, "functionResponse.name must be resolved from tool_call_id")

	// Ensure the tool output was captured (we package it under a response map with 'output')
	require.NotNil(t, part.FunctionResponse.Response, "functionResponse.response should be non-nil")
	if out, ok := part.FunctionResponse.Response["output"].(string); ok {
		assert.Contains(t, out, "midnight dreary", "tool output should contain tool content")
	} else {
		t.Fatalf("functionResponse.response.output missing or not a string: %#v", part.FunctionResponse.Response)
	}
}
