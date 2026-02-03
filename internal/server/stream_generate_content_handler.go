package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dvcrn/gemini-code-assist-proxy/internal/gemini"
	"github.com/dvcrn/gemini-code-assist-proxy/internal/logger"
)

func (s *Server) streamGenerateContentHandler(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	model, action := parseGeminiPath(r.URL.Path)

	normalizedModel := normalizeModelName(model)

	logger.Get().Info().
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Str("query", r.URL.RawQuery).
		Str("requested_model", model).
		Str("normalized_model", normalizedModel).
		Str("action", action).
		Time("start_time", startTime).
		Msg("Gemini API request received")

	logger.Get().Debug().
		Str("content_type", r.Header.Get("Content-Type")).
		Str("user_agent", r.Header.Get("User-Agent")).
		Int64("content_length", r.ContentLength).
		Msg("Request headers")

	if model == "" || action == "" {
		logger.Get().Error().
			Str("path", r.URL.Path).
			Msg("Invalid path format")
		http.Error(w, "Invalid path format", http.StatusBadRequest)
		return
	}

	switch action {
	case "streamGenerateContent":
		s.handleStreamGenerateContent(w, r, normalizedModel)

	case "generateContent":
		s.handleGenerateContent(w, r, normalizedModel)

	default:
		logger.Get().Warn().
			Str("action", action).
			Msg("Unknown action")
		http.Error(w, "Unknown action: "+action, http.StatusBadRequest)
		return
	}

	logger.Get().Info().
		Dur("duration", time.Since(startTime)).
		Str("action", action).
		Msg("Gemini API request completed")
}

func (s *Server) handleGenerateContent(w http.ResponseWriter, r *http.Request, model string) {
	startTime := time.Now()

	logger.Get().Info().
		Str("model", model).
		Msg("Handling generateContent")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		logger.Get().Error().Err(err).Msg("Failed to read request body")
		http.Error(w, "Error reading request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var requestBody gemini.GeminiInternalRequest
	if err := json.Unmarshal(body, &requestBody); err != nil {
		logger.Get().Error().Err(err).Msg("Failed to parse request body")
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Sanitize request for CloudCode compatibility
	sanitizeGeminiRequest(&requestBody, model)

	logger.Get().Debug().
		Str("model", model).
		Int("body_size", len(body)).
		Msg("Calling gemini client GenerateContent")

	genReq := &gemini.GenerateContentRequest{
		Model:   model,
		Project: s.projectID,
		Request: requestBody,
	}

	apiCallStart := time.Now()
	resp, err := s.geminiClient.GenerateContent(genReq)
	if err != nil {
		logger.Get().Error().
			Err(err).
			Str("model", model).
			Dur("api_call_duration", time.Since(apiCallStart)).
			Msg("GenerateContent failed")
		http.Error(w, fmt.Sprintf("Error calling GenerateContent: %v", err), http.StatusInternalServerError)
		return
	}

	logger.Get().Debug().
		Dur("api_call_duration", time.Since(apiCallStart)).
		Msg("GenerateContent successful")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(resp.Response); err != nil {
		logger.Get().Error().Err(err).Msg("Failed to encode response")
		return
	}

	logger.Get().Info().
		Str("model", model).
		Dur("total_duration", time.Since(startTime)).
		Dur("api_call_duration", time.Since(apiCallStart)).
		Msg("generateContent completed")
}

func (s *Server) handleStreamGenerateContent(w http.ResponseWriter, r *http.Request, model string) {
	startTime := time.Now()
	logger.Get().Info().
		Str("model", model).
		Msg("Handling streamGenerateContent")

	// Read and parse request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		logger.Get().Error().Err(err).Msg("Failed to read request body")
		http.Error(w, "Error reading request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var requestBody gemini.GeminiInternalRequest
	if err := json.Unmarshal(body, &requestBody); err != nil {
		logger.Get().Error().Err(err).Msg("Failed to parse request body")
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Sanitize request for CloudCode compatibility
	sanitizeGeminiRequest(&requestBody, model)

	// Build CloudCode request wrapper
	genReq := &gemini.GenerateContentRequest{
		Model:   model,
		Project: s.projectID,
		Request: requestBody,
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
	}

	// Start upstream streaming and pipe raw lines
	lines := make(chan string, 16)
	apiCallStart := time.Now()
	if err := s.geminiClient.StreamGenerateContent(r.Context(), genReq, lines); err != nil {
		logger.Get().Error().
			Err(err).
			Str("model", model).
			Dur("api_call_duration", time.Since(apiCallStart)).
			Msg("StreamGenerateContent failed")
		// Emit concise request summary to aid debugging without flooding logs
		req := genReq.Request
		totalTextChars := 0
		maxContentChars := 0
		userMsgs := 0
		modelMsgs := 0
		for _, c := range req.Contents {
			if c.Role == "user" {
				userMsgs++
			} else if c.Role == "model" {
				modelMsgs++
			}
			contentChars := 0
			for _, p := range c.Parts {
				if p.Text != "" {
					l := len(p.Text)
					totalTextChars += l
					contentChars += l
				}
			}
			if contentChars > maxContentChars {
				maxContentChars = contentChars
			}
		}
		sysParts := 0
		sysChars := 0
		if req.SystemInstruction != nil {
			sysParts = len(req.SystemInstruction.Parts)
			for _, p := range req.SystemInstruction.Parts {
				if p.Text != "" {
					sysChars += len(p.Text)
				}
			}
		}
		fnDecls := 0
		for _, t := range req.Tools {
			fnDecls += len(t.FunctionDeclarations)
		}
		maxTok := 0
		if req.GenerationConfig != nil {
			maxTok = req.GenerationConfig.MaxOutputTokens
		}
		logger.Get().Debug().
			Str("model", model).
			Int("contents", len(req.Contents)).
			Int("user_messages", userMsgs).
			Int("model_messages", modelMsgs).
			Int("total_text_chars", totalTextChars).
			Int("max_content_chars", maxContentChars).
			Int("system_parts", sysParts).
			Int("system_chars", sysChars).
			Int("tools", len(req.Tools)).
			Int("function_declarations", fnDecls).
			Int("max_output_tokens", maxTok).
			Msg("Upstream request summary (on error)")
		// We already wrote SSE headers; emit an SSE-formatted error payload instead of resetting headers.
		errPayload := map[string]interface{}{
			"error": map[string]interface{}{
				"message": err.Error(),
			},
		}
		if b, mErr := json.Marshal(errPayload); mErr == nil {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", string(b))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
		return
	}

	// Stream loop: transform data lines and forward to client
	firstWrite := true
	// Send SSE keepalives until first upstream byte to avoid idle timeouts
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
streamLoop:
	for {
		select {
		case <-r.Context().Done():
			logger.Get().Info().Msg("Client canceled SSE stream")
			return

		case line, ok := <-lines:
			if !ok {
				logger.Get().Info().Msg("Upstream stream ended")
				break streamLoop
			}
			if firstWrite {
				logger.Get().Info().
					Dur("time_to_first_write", time.Since(startTime)).
					Msg("First SSE data written to client (direct stream)")
				firstWrite = false
			}

			// Transform CloudCode SSE line into standard Gemini format
			transformed := TransformSSELine(line)

			// CloudCode-style streams can send a literal [DONE] token.
			// Standard Gemini streaming responses do not include this marker, and
			// some clients will attempt to parse it as JSON.
			trimmed := strings.TrimSpace(transformed)
			if strings.HasPrefix(trimmed, "data:") {
				data := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
				if data == "[DONE]" || data == "\"[DONE]\"" {
					logger.Get().Info().Msg("Upstream sent [DONE], ending stream")
					break streamLoop
				}
			}

			// Write transformed line and a newline; upstream blank lines will pass through too
			if _, err := fmt.Fprintf(w, "%s\n", transformed); err != nil {
				logger.Get().Error().Err(err).Msg("Error writing SSE line to client")
				return
			}

			// Flush per line if supported
			if flusher != nil {
				flusher.Flush()
			}

		case <-ticker.C:
			if firstWrite {
				// SSE comment keepalive to keep connection open
				if _, err := io.WriteString(w, ":\n\n"); err != nil {
					logger.Get().Error().Err(err).Msg("Error writing keepalive")
					return
				}
				if flusher != nil {
					flusher.Flush()
				}
				logger.Get().Debug().Msg("Wrote SSE keepalive before first upstream byte")
			}
		}
	}

	logger.Get().Info().
		Str("model", model).
		Dur("total_duration", time.Since(startTime)).
		Dur("api_call_duration", time.Since(apiCallStart)).
		Msg("streamGenerateContent completed")
}
