package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dvcrn/gemini-code-assist-proxy/internal/logger"
	"github.com/dvcrn/gemini-code-assist-proxy/internal/openai"
	"github.com/dvcrn/gemini-code-assist-proxy/internal/transform"
)

// openAIChatCompletionsHandler handles OpenAI-compatible chat completion requests
func (s *Server) openAIChatCompletionsHandler(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	logger.Get().Info().
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Time("start_time", startTime).
		Msg("OpenAI chat completions request received")

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

	logger.Get().Info().
		Str("requested_model", req.Model).
		Bool("stream", req.Stream).
		Int("messages", len(req.Messages)).
		Int("tools", len(req.Tools)).
		Msg("Parsed OpenAI request")

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
	originalModel := gemReq.Model
	gemReq.Model = normalizeModelName(gemReq.Model)
	if gemReq.Model != originalModel {
		logger.Get().Info().
			Str("original_model", originalModel).
			Str("normalized_model", gemReq.Model).
			Msg("Normalized model for CloudCode")
	}

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
		logger.Get().Debug().Msg("SSE headers flushed (flusher available)")
	} else {
		logger.Get().Info().Msg("SSE flusher not available; relying on implicit streaming")
	}

	// Start upstream streaming from Gemini
	upstream := make(chan string, 32)
	logger.Get().Info().
		Str("model", gemReq.Model).
		Msg("Starting upstream StreamGenerateContent")
	if err := s.geminiClient.StreamGenerateContent(r.Context(), gemReq, upstream); err != nil {
		logger.Get().Error().Err(err).Msg("StreamGenerateContent call failed")
		http.Error(w, "Upstream streaming error", http.StatusInternalServerError)
		return
	}
	logger.Get().Info().Msg("Upstream StreamGenerateContent started")

	// Adapter: CloudCode SSE -> StreamChunk
	chunkIn := make(chan openai.StreamChunk, 32)
	go func() {
		defer close(chunkIn)
		firstUpstream := true
		for line := range upstream {
			// Process only data lines
			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			if firstUpstream {
				logger.Get().Info().
					Dur("time_to_first_upstream_line", time.Since(startTime)).
					Msg("First upstream SSE line received")
				firstUpstream = false
			}

			// Normalize CloudCode 'response' wrapper first
			transformed := TransformSSELine(line)
			data := strings.TrimSpace(strings.TrimPrefix(transformed, "data: "))

			// Handle upstream DONE
			if data == "" || data == "[DONE]" || data == "\"[DONE]\"" {
				logger.Get().Info().Msg("Received upstream DONE")
				break
			}

			// Try to parse JSON payload
			var obj map[string]interface{}
			if err := json.Unmarshal([]byte(data), &obj); err != nil {
				logger.Get().Debug().Err(err).Msg("Failed to parse SSE JSON; forwarding as text")
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
							rawName, _ := fc["name"].(string)
							name := strings.TrimSpace(rawName)

							// Robust args extraction: support args/argsJson/arguments/parameters (object or JSON string)
							var args map[string]interface{}
							var source string
							tryParse := func(val interface{}, key string) bool {
								switch v := val.(type) {
								case map[string]interface{}:
									args = v
									source = key
									return true
								case string:
									var m map[string]interface{}
									if err := json.Unmarshal([]byte(v), &m); err == nil {
										args = m
										source = key + " (json)"
										return true
									}
								}
								return false
							}

							if !tryParse(fc["args"], "args") &&
								!tryParse(fc["argsJson"], "argsJson") &&
								!tryParse(fc["arguments"], "arguments") &&
								!tryParse(fc["parameters"], "parameters") {
								args = map[string]interface{}{}
								source = "default_empty"
							}

							// Normalize common file_path variants for Droid and other clients
							if _, ok := args["file_path"]; !ok {
								var candidate interface{}
								for _, k := range []string{"filePath", "filepath", "path", "file", "filename", "file_name"} {
									if v, ok := args[k]; ok {
										candidate = v
										break
									}
								}
								if candidate != nil {
									switch v := candidate.(type) {
									case string:
										args["file_path"] = v
									default:
										if b, err := json.Marshal(v); err == nil {
											args["file_path"] = string(b)
										}
									}
								}
							}

							logger.Get().Info().
								Str("function", name).
								Str("args_source", source).
								Int("arg_keys", len(args)).
								Msg("Emitting tool call from model")

							chunkIn <- openai.StreamChunk{
								Type: "tool_code",
								Data: map[string]interface{}{
									"name": name,
									"args": args,
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
	firstWrite := true
	for sse := range out {
		if _, err := io.WriteString(w, sse); err != nil {
			logger.Get().Error().Err(err).Msg("Error writing SSE to client")
			return
		}
		if firstWrite {
			logger.Get().Info().
				Dur("time_to_first_client_write", time.Since(startTime)).
				Msg("First OpenAI SSE chunk written to client")
			firstWrite = false
		}
		if flusher != nil {
			flusher.Flush()
		}
	}

	logger.Get().Info().
		Str("model", gemReq.Model).
		Dur("total_duration", time.Since(startTime)).
		Msg("OpenAI streaming response completed")
}
