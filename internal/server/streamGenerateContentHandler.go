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

	logger.Get().Info().
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Str("query", r.URL.RawQuery).
		Str("model", model).
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
		s.handleStreamGenerateContent(w, r, model)

	case "generateContent":
		s.handleGenerateContent(w, r, model)

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

	var requestBody map[string]interface{}
	if err := json.Unmarshal(body, &requestBody); err != nil {
		logger.Get().Error().Err(err).Msg("Failed to parse request body")
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

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

	var requestBody map[string]interface{}
	if err := json.Unmarshal(body, &requestBody); err != nil {
		logger.Get().Error().Err(err).Msg("Failed to parse request body")
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

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

	// Detect flush capability and flush headers once
	flusher, canFlush := w.(http.Flusher)
	if canFlush {
		flusher.Flush()
	}

	// Start upstream streaming and pipe raw lines
	lines := make(chan string, 16)
	apiCallStart := time.Now()
	if err := s.geminiClient.StreamGenerateContent(genReq, lines); err != nil {
		logger.Get().Error().
			Err(err).
			Str("model", model).
			Dur("api_call_duration", time.Since(apiCallStart)).
			Msg("StreamGenerateContent failed")
		http.Error(w, fmt.Sprintf("Error calling StreamGenerateContent: %v", err), http.StatusInternalServerError)
		return
	}

	// Stream loop: transform data lines and forward to client
	firstWrite := true
	for line := range lines {
		if firstWrite {
			logger.Get().Info().
				Dur("time_to_first_write", time.Since(startTime)).
				Msg("First SSE data written to client (direct stream)")
			firstWrite = false
		}

		// Transform CloudCode SSE line into standard Gemini format
		transformed := TransformSSELine(line)
		isData := strings.HasPrefix(transformed, "data: ")

		// Write line
		if _, err := fmt.Fprintf(w, "%s\n", transformed); err != nil {
			logger.Get().Error().Err(err).Msg("Error writing SSE line to client")
			return
		}

		// For environments without flush support, ensure data events are separated by a blank line
		if !canFlush && isData {
			if _, err := fmt.Fprint(w, "\n"); err != nil {
				logger.Get().Error().Err(err).Msg("Error writing SSE separator to client")
				return
			}
		}

		// Flush only when available and meaningful (data or empty line)
		if canFlush && (isData || transformed == "") {
			flusher.Flush()
		}
	}

	logger.Get().Info().
		Str("model", model).
		Dur("total_duration", time.Since(startTime)).
		Dur("api_call_duration", time.Since(apiCallStart)).
		Msg("streamGenerateContent completed")
}
