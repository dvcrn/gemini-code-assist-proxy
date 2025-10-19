package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dvcrn/gemini-code-assist-proxy/internal/logger"
	"github.com/dvcrn/gemini-code-assist-proxy/internal/openai"
	"github.com/dvcrn/gemini-code-assist-proxy/internal/transform"
)

// openAIChatCompletionsHandler handles OpenAI-compatible chat completion requests.
// It logs structured tool inputs/outputs and emits per-token SSE debug logs.
// No client-specific normalization is applied; arguments are passed through as-is.
func (s *Server) openAIChatCompletionsHandler(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	logger.Get().Info().
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Time("start_time", startTime).
		Msg("OpenAI chat completions request received")

	// Read body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		logger.Get().Error().Err(err).Msg("Error reading request body")
		http.Error(w, "Error reading request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Parse request
	var req openai.ChatCompletionRequest
	if err := json.Unmarshal(body, &req); err != nil {
		logger.Get().Error().Err(err).Msg("Error parsing request body")
		http.Error(w, "Error parsing request body", http.StatusBadRequest)
		return
	}

	// Request overview
	logger.Get().Info().
		Str("requested_model", req.Model).
		Bool("stream", req.Stream).
		Int("messages", len(req.Messages)).
		Int("tools", len(req.Tools)).
		Msg("Parsed OpenAI request")

	// Log tool result messages present in the request (tool outputs from client)
	toolMsgCount := 0
	for i, m := range req.Messages {
		if strings.ToLower(m.Role) != "tool" {
			continue
		}
		toolMsgCount++

		kind := "unknown"
		var preview string
		switch v := m.Content.(type) {
		case string:
			kind = "string"
			preview = v
		case []interface{}:
			kind = "array"
			var b strings.Builder
			for _, part := range v {
				if pm, ok := part.(map[string]interface{}); ok && pm["type"] == "text" {
					if txt, ok := pm["text"].(string); ok && txt != "" {
						if b.Len() > 0 {
							b.WriteString("\n")
						}
						b.WriteString(txt)
					}
				}
			}
			preview = b.String()
		default:
			kind = "unknown"
		}

		if len(preview) > 300 {
			preview = preview[:300] + "..."
		}

		logger.Get().Info().
			Int("index", i).
			Str("tool_call_id", m.ToolCallID).
			Str("name", m.Name).
			Str("content_kind", kind).
			Int("content_len", len(preview)).
			Str("content_preview", preview).
			Msg("Tool result message received")
	}
	logger.Get().Debug().
		Int("tool_messages", toolMsgCount).
		Msg("Tool result message count")

	// Delegate to stream or non-stream handler
	if req.Stream {
		s.chatCompletionRequestStream(w, r, req, startTime)
		return
	}
	s.chatCompletionRequest(w, r, req, startTime)
}

// chatCompletionRequestStream handles the streaming variant (existing behavior).
func (s *Server) chatCompletionRequestStream(w http.ResponseWriter, r *http.Request, req openai.ChatCompletionRequest, startTime time.Time) {
	// Transform OpenAI -> Gemini
	gemReq, err := transform.ToGeminiRequest(&req, s.projectID)
	if err != nil {
		logger.Get().Error().Err(err).Msg("Failed to transform OpenAI request to Gemini request")
		http.Error(w, "Failed to transform request", http.StatusInternalServerError)
		return
	}

	// Normalize model name for CloudCode compatibility
	normalizedModelName := normalizeModelName(gemReq.Model)
	originalModel := gemReq.Model
	gemReq.Model = normalizedModelName
	if gemReq.Model != originalModel {
		logger.Get().Info().
			Str("original_model", originalModel).
			Str("normalized_model", normalizedModelName).
			Msg("Normalized model for CloudCode")
	}

	// Prepare SSE response
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

	// Pinger to keep connection alive
	pingerCtx, cancelPinger := context.WithCancel(r.Context())
	defer cancelPinger()

	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				logger.Get().Debug().Msg("Sending SSE ping to keep connection alive")
				if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
					logger.Get().Warn().Err(err).Msg("Failed to write SSE ping")
					return
				}
				if flusher, ok := w.(http.Flusher); ok {
					flusher.Flush()
				}
			case <-pingerCtx.Done():
				return
			}
		}
	}()

	if err := s.geminiClient.StreamGenerateContent(r.Context(), gemReq, upstream); err != nil {
		logger.Get().Error().Err(err).Msg("StreamGenerateContent call failed")
		http.Error(w, "Upstream streaming error", http.StatusInternalServerError)
		return
	}
	logger.Get().Info().Msg("Upstream StreamGenerateContent started")

	// Adapter: CloudCode SSE -> StreamChunk (model text, tool calls, usage, etc.)
	chunkIn := make(chan openai.StreamChunk, 32)
	go func() {
		defer close(chunkIn)
		firstUpstream := true
		firstThoughtSeen := false
		for line := range upstream {
			if firstUpstream {
				cancelPinger() // Stop pinger on first data
			}
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

			// Transform CloudCode wrapper to standard Gemini-format event
			transformed := TransformSSELine(line)
			data := strings.TrimSpace(strings.TrimPrefix(transformed, "data: "))

			// Handle upstream DONE
			if data == "" || data == "[DONE]" || data == "\"[DONE]\"" {
				logger.Get().Info().Msg("Received upstream DONE")
				break
			}

			// Parse JSON payload
			var obj map[string]interface{}
			if err := json.Unmarshal([]byte(data), &obj); err != nil {
				// Fallback: forward as plain text chunk
				logger.Get().Debug().Err(err).Msg("Failed to parse SSE JSON; forwarding as text")
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
					cand, ok := c.(map[string]interface{})
					if !ok {
						logger.Get().Warn().Interface("candidate", c).Msg("Skipping invalid candidate in Gemini stream")
						continue
					}

					// Optional grounding metadata passthrough
					if gm, ok := cand["groundingMetadata"]; ok && gm != nil {
						chunkIn <- openai.StreamChunk{Type: "grounding_metadata", Data: gm}
					}

					// Retrieve parts
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

					// Process parts
					for _, p := range parts {
						part, ok := p.(map[string]interface{})
						if !ok {
							logger.Get().Warn().Interface("part", p).Msg("Skipping invalid part in Gemini stream")
							continue
						}

						// Thought tokens (reasoning) — map to OpenAI reasoning stream
						if isThought, ok := part["thought"].(bool); ok && isThought {
							if txt, ok := part["text"].(string); ok && txt != "" {
								if !firstThoughtSeen {
									preview := txt
									if len(preview) > 300 {
										preview = preview[:300] + "..."
									}
									logger.Get().Info().
										Int("len", len(txt)).
										Str("preview", preview).
										Msg("Streaming thinking tokens detected")
									firstThoughtSeen = true
								}
								logger.Get().Debug().
									Str("token", txt).
									Msg("SSE thought token received")
								chunkIn <- openai.StreamChunk{Type: "real_thinking", Data: txt}
							}
							// Skip normal text handling to avoid duplicating this token
							continue
						}

						// Text tokens — log per token at DEBUG
						if txt, ok := part["text"].(string); ok && txt != "" {
							logger.Get().Debug().
								Str("token", txt).
								Msg("SSE text token received")
							chunkIn <- openai.StreamChunk{Type: "text", Data: txt}
						}

						// Function call parts
						if fc, ok := part["functionCall"].(map[string]interface{}); ok {
							rawName, _ := fc["name"].(string)
							name := strings.TrimSpace(rawName)

							// Robust args extraction without client-specific normalization
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

							// Log tool call inputs (preview at INFO, full JSON at DEBUG)
							argsJSON, _ := json.Marshal(args)
							argsPreview := string(argsJSON)
							if len(argsPreview) > 300 {
								argsPreview = argsPreview[:300] + "..."
							}
							logger.Get().Info().
								Str("function", name).
								Int("arg_keys", len(args)).
								Str("args_preview", argsPreview).
								Msg("Tool call inputs")
							logger.Get().Debug().
								Str("function", name).
								RawJSON("args", argsJSON).
								Str("args_source", source).
								Msg("Tool call full args")

							logger.Get().Info().
								Str("function", name).
								Str("args_source", source).
								Int("arg_keys", len(args)).
								Msg("Emitting tool call from model")

							// Emit tool call to OpenAI transformer
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

	// Transform chunks into OpenAI-compatible SSE and stream to client
	transformer := openai.CreateOpenAIStreamTransformer(req.Model)
	out := transformer(chunkIn)

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

// chatCompletionRequest handles the non-streaming variant via GenerateContent and returns OpenAI-style JSON.
func (s *Server) chatCompletionRequest(w http.ResponseWriter, r *http.Request, req openai.ChatCompletionRequest, startTime time.Time) {
	// Transform OpenAI -> Gemini
	gemReq, err := transform.ToGeminiRequest(&req, s.projectID)
	if err != nil {
		logger.Get().Error().Err(err).Msg("Failed to transform OpenAI request to Gemini request")
		http.Error(w, "Failed to transform request", http.StatusInternalServerError)
		return
	}

	// Normalize model name
	normalizedModelName := normalizeModelName(gemReq.Model)
	originalModel := gemReq.Model
	gemReq.Model = normalizedModelName
	if gemReq.Model != originalModel {
		logger.Get().Info().
			Str("original_model", originalModel).
			Str("normalized_model", normalizedModelName).
			Msg("Normalized model for CloudCode")
	}

	// Call non-streaming GenerateContent
	apiStart := time.Now()
	resp, err := s.geminiClient.GenerateContent(gemReq)
	if err != nil {
		logger.Get().Error().Err(err).Dur("api_call_duration", time.Since(apiStart)).Msg("GenerateContent failed")
		http.Error(w, "Error calling GenerateContent", http.StatusInternalServerError)
		return
	}

	// Extract assistant text content from first candidate
	var contentText string
	if resp != nil && resp.Response != nil {
		if cands, ok := resp.Response["candidates"].([]interface{}); ok && len(cands) > 0 {
			if first, ok := cands[0].(map[string]interface{}); ok {
				// parts may be under content.parts or parts
				var parts []interface{}
				if c, ok := first["content"].(map[string]interface{}); ok {
					if ps, ok := c["parts"].([]interface{}); ok {
						parts = ps
					}
				}
				if len(parts) == 0 {
					if ps, ok := first["parts"].([]interface{}); ok {
						parts = ps
					}
				}
				var b strings.Builder
				for _, p := range parts {
					if pm, ok := p.(map[string]interface{}); ok {
						if txt, ok := pm["text"].(string); ok && txt != "" {
							if b.Len() > 0 {
								b.WriteString("\n")
							}
							b.WriteString(txt)
						}
					}
				}
				contentText = b.String()
			}
		}
	}

	// Build OpenAI-style response
	created := time.Now().Unix()
	openAIResp := map[string]interface{}{
		"id":      "chatcmpl",
		"object":  "chat.completion",
		"created": created,
		"model":   req.Model,
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": contentText,
				},
				"finish_reason": "stop",
			},
		},
	}

	// Include usage if available
	if resp != nil && resp.Response != nil {
		if um, ok := resp.Response["usageMetadata"].(map[string]interface{}); ok {
			prompt := 0
			comp := 0
			if v, ok := um["promptTokenCount"].(float64); ok {
				prompt = int(v)
			}
			if v, ok := um["candidatesTokenCount"].(float64); ok {
				comp = int(v)
			}
			openAIResp["usage"] = map[string]interface{}{
				"prompt_tokens":     prompt,
				"completion_tokens": comp,
				"total_tokens":      prompt + comp,
			}
		}
	}

	// Write response
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(openAIResp); err != nil {
		logger.Get().Error().Err(err).Msg("Error writing non-streaming response")
		return
	}

	logger.Get().Info().
		Str("model", gemReq.Model).
		Dur("api_call_duration", time.Since(apiStart)).
		Dur("total_duration", time.Since(startTime)).
		Msg("OpenAI non-streaming response completed")
}
