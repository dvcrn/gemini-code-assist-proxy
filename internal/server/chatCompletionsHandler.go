package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/dvcrn/gemini-code-assist-proxy/internal/logger"
	"github.com/dvcrn/gemini-code-assist-proxy/internal/openai"
	"github.com/dvcrn/gemini-code-assist-proxy/internal/transform"
)

// openAIChatCompletionsHandler handles OpenAI-compatible chat completion requests
func (s *Server) openAIChatCompletionsHandler(w http.ResponseWriter, r *http.Request) {
	logger.Get().Info().Msg("openAIChatCompletionsHandler called")

	// Read the request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		logger.Get().Error().Err(err).Msg("Error reading request body")
		http.Error(w, "Error reading request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Parse the request body
	var req openai.ChatCompletionRequest
	if err := json.Unmarshal(body, &req); err != nil {
		logger.Get().Error().Err(err).Msg("Error parsing request body")
		http.Error(w, "Error parsing request body", http.StatusBadRequest)
		return
	}

	// Require streaming mode for now
	if !req.Stream {
		logger.Get().Warn().Msg("Non-streaming OpenAI chat completion not supported yet")
		http.Error(w, "Only stream=true is supported for this endpoint", http.StatusBadRequest)
		return
	}

	// Transform OpenAI request to Gemini request
	gemReq, err := transform.ToGeminiRequest(&req, s.projectID)
	if err != nil {
		logger.Get().Error().Err(err).Msg("Failed to transform OpenAI request to Gemini request")
		http.Error(w, "Failed to transform request", http.StatusInternalServerError)
		return
	}

	// Normalize model name for CloudCode compatibility
	gemReq.Model = normalizeModelName(gemReq.Model)

	// Prepare SSE response headers
	w.Header().Del("Content-Length")
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	// Flush headers if supported
	var flusher http.Flusher
	if f, ok := w.(http.Flusher); ok {
		flusher = f
		flusher.Flush()
	}

	// Start upstream streaming from Gemini
	upstream := make(chan string, 32)
	if err := s.geminiClient.StreamGenerateContent(r.Context(), gemReq, upstream); err != nil {
		logger.Get().Error().Err(err).Msg("StreamGenerateContent call failed")
		http.Error(w, "Upstream streaming error", http.StatusInternalServerError)
		return
	}

	// Adapter: CloudCode SSE -> StreamChunk
	chunkIn := make(chan openai.StreamChunk, 32)
	go func() {
		defer close(chunkIn)
		for line := range upstream {
			// Process only data lines
			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			// Normalize CloudCode 'response' wrapper first
			transformed := TransformSSELine(line)
			data := strings.TrimSpace(strings.TrimPrefix(transformed, "data: "))

			// Handle upstream DONE
			if data == "" || data == "[DONE]" || data == "\"[DONE]\"" {
				break
			}

			// Try to parse JSON payload
			var obj map[string]interface{}
			if err := json.Unmarshal([]byte(data), &obj); err != nil {
				// Fallback: forward as plain text chunk
				chunkIn <- openai.StreamChunk{Type: "text", Data: data}
				continue
			}

			// Usage metadata (optional)
			if um, ok := obj["usageMetadata"].(map[string]interface{}); ok {
				payload := map[string]interface{}{}
				if v, ok := um["promptTokenCount"]; ok {
					payload["inputTokens"] = v
				}
				if v, ok := um["candidatesTokenCount"]; ok {
					payload["outputTokens"] = v
				}
				chunkIn <- openai.StreamChunk{Type: "usage", Data: payload}
			}

			// Extract candidate content parts
			if cands, ok := obj["candidates"].([]interface{}); ok {
				for _, c := range cands {
					cand, _ := c.(map[string]interface{})

					// Optional grounding metadata passthrough
					if gm, ok := cand["groundingMetadata"]; ok && gm != nil {
						chunkIn <- openai.StreamChunk{Type: "grounding_metadata", Data: gm}
					}

					// Find parts either under candidate.content.parts or candidate.parts
					var parts []interface{}
					if content, ok := cand["content"].(map[string]interface{}); ok {
						if ps, ok := content["parts"].([]interface{}); ok {
							parts = ps
						}
					}
					if len(parts) == 0 {
						if ps, ok := cand["parts"].([]interface{}); ok {
							parts = ps
						}
					}

					for _, p := range parts {
						part, _ := p.(map[string]interface{})

						// Text parts
						if txt, ok := part["text"].(string); ok && txt != "" {
							chunkIn <- openai.StreamChunk{Type: "text", Data: txt}
						}

						// Function call parts
						if fc, ok := part["functionCall"].(map[string]interface{}); ok {
							chunkIn <- openai.StreamChunk{
								Type: "tool_code",
								Data: map[string]interface{}{
									"name": fc["name"],
									"args": fc["args"],
								},
							}
						}
					}
				}
			}
		}
	}()

	// Transform chunks into OpenAI-compatible SSE
	transformer := openai.CreateOpenAIStreamTransformer(req.Model)
	out := transformer(chunkIn)

	// Stream transformed SSE to client
	for sse := range out {
		if _, err := io.WriteString(w, sse); err != nil {
			logger.Get().Error().Err(err).Msg("Error writing SSE to client")
			return
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
}
