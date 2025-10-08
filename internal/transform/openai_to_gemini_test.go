package transform

import (
	"testing"

	"github.com/dvcrn/gemini-code-assist-proxy/internal/openai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToGeminiRequest(t *testing.T) {
	t.Run("simple request", func(t *testing.T) {
		openAIReq := &openai.ChatCompletionRequest{
			Model: "gemini-pro",
			Messages: []openai.Message{
				{Role: "user", Content: "Hello"},
			},
			Temperature: 0.8,
			MaxTokens:   100,
		}

		geminiReq, err := ToGeminiRequest(openAIReq, "test-project")
		require.NoError(t, err)

		assert.Equal(t, "gemini-pro", geminiReq.Model)
		assert.Equal(t, "test-project", geminiReq.Project)

		req := geminiReq.Request
		contents := req.Contents
		require.Len(t, contents, 1)

		firstContent := contents[0]
		assert.Equal(t, "user", firstContent.Role)
		parts := firstContent.Parts
		require.Len(t, parts, 1)
		assert.Equal(t, "Hello", parts[0].Text)

		require.NotNil(t, req.GenerationConfig)
		assert.Equal(t, 0.8, req.GenerationConfig.Temperature)
		assert.Equal(t, 100, req.GenerationConfig.MaxOutputTokens)
	})

	t.Run("with system message", func(t *testing.T) {
		openAIReq := &openai.ChatCompletionRequest{
			Model: "gemini-pro",
			Messages: []openai.Message{
				{Role: "system", Content: "You are a helpful assistant."},
				{Role: "user", Content: "Hello"},
			},
		}

		geminiReq, err := ToGeminiRequest(openAIReq, "test-project")
		require.NoError(t, err)

		req := geminiReq.Request

		// Check system instruction
		require.NotNil(t, req.SystemInstruction)
		sysParts := req.SystemInstruction.Parts
		require.Len(t, sysParts, 1)
		assert.Equal(t, "You are a helpful assistant.", sysParts[0].Text)

		// Check user message
		contents := req.Contents
		require.Len(t, contents, 1)
		firstContent := contents[0]
		assert.Equal(t, "user", firstContent.Role)
	})
}
