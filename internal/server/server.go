package server

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dvcrn/gemini-code-assist-proxy/internal/credentials"
	"github.com/dvcrn/gemini-code-assist-proxy/internal/env"
	"github.com/dvcrn/gemini-code-assist-proxy/internal/logger"
)

// Server represents the proxy server with its dependencies
type Server struct {
	httpClient HTTPClient
	provider   credentials.CredentialsProvider
	oauthCreds *credentials.OAuthCredentials
	projectID  string
	mux        *http.ServeMux
}

// NewServer creates a new server instance with the given credentials provider
func NewServer(provider credentials.CredentialsProvider) *Server {
	s := &Server{
		httpClient: NewHTTPClient(),
		provider:   provider,
		mux:        http.NewServeMux(),
	}
	s.setupRoutes()

	return s
}

// sseMessage represents a single SSE message to be processed
type sseMessage struct {
	line       string
	isDataLine bool
}

// streamSSEResponseDirect handles SSE streaming for Workers environment without flushing
func streamSSEResponseDirect(body io.Reader, w http.ResponseWriter, debugSSE bool) {
	startTime := time.Now()
	eventCount := 0
	firstDataWritten := false

	logger.Get().Info().
		Time("stream_start", startTime).
		Msg("Starting direct SSE stream processing (Workers mode)")

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	firstLine := true
	for scanner.Scan() {
		line := scanner.Text()

		if debugSSE {
			logger.Get().Debug().Str("raw_line", line).Msg("Raw SSE line received from upstream")
		}

		if firstLine {
			logger.Get().Info().
				Dur("time_to_first_data", time.Since(startTime)).
				Msg("First SSE line received from upstream")
			firstLine = false
		}

		// Transform data lines
		if strings.HasPrefix(line, "data: ") {
			transformed := TransformSSELine(line)
			if transformed != "" {
				if debugSSE {
					logger.Get().Debug().Str("transformed_line", transformed).Msg("Writing transformed data to client")
				}
				// Write transformed line immediately
				if _, err := fmt.Fprintf(w, "%s\n\n", transformed); err != nil {
					logger.Get().Error().Err(err).Msg("Error writing to client")
					return
				}

				eventCount++
				if !firstDataWritten {
					logger.Get().Info().
						Dur("time_to_first_write", time.Since(startTime)).
						Msg("First SSE data written to client (Workers direct mode)")
					firstDataWritten = true
				}
				if debugSSE {
					logger.Get().Debug().
						Int("event_num", eventCount).
						Dur("elapsed", time.Since(startTime)).
						Msg("SSE event written directly to client")
				}
			}
		} else if line != "" {
			if debugSSE {
				logger.Get().Debug().Str("passthrough_line", line).Msg("Writing non-data line to client")
			}
			// Write non-data lines as-is, but only if they are not empty
			if _, err := fmt.Fprintf(w, "%s\n", line); err != nil {
				logger.Get().Error().Err(err).Msg("Error writing to client")
				return
			}
		}
	}

	if err := scanner.Err(); err != nil {
		logger.Get().Error().Err(err).Msg("Error reading stream")
	}

	if debugSSE {
		logger.Get().Debug().
			Int("total_events", eventCount).
			Dur("total_duration", time.Since(startTime)).
			Msg("Direct SSE streaming completed")
	}
}

// streamSSEResponse handles SSE streaming with a goroutine pipeline for better performance
func streamSSEResponse(body io.Reader, w http.ResponseWriter, flusher http.Flusher, canFlush bool, debugSSE bool) {
	// Get buffer size from environment, default to 1 for minimal buffering
	bufferSize := 1
	if envSize, ok := env.Get("SSE_BUFFER_SIZE"); ok {
		if size, err := strconv.Atoi(envSize); err == nil && size > 0 {
			bufferSize = size
		}
	}

	startTime := time.Now()
	eventCount := 0
	bufferedEventCount := 0
	flushedEventCount := 0

	// Log streaming setup
	if debugSSE {
		logger.Get().Debug().
			Bool("can_flush", canFlush).
			Int("buffer_size", bufferSize).
			Time("stream_start", startTime).
			Msg("Starting SSE stream processing")
	}

	// Create channels for the pipeline
	rawLines := make(chan string, bufferSize)
	transformedLines := make(chan sseMessage, bufferSize)
	done := make(chan struct{})

	// Goroutine 1: Read lines from response body
	go func() {
		defer close(rawLines)
		scanner := bufio.NewScanner(body)
		// Use a larger buffer for the scanner
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)

		firstLine := true
		for scanner.Scan() {
			line := scanner.Text()
			receiveTime := time.Now()
			if firstLine && debugSSE {
				logger.Get().Debug().
					Dur("time_to_first_data", time.Since(startTime)).
					Msg("First SSE line received from upstream")
				firstLine = false
			}
			select {
			case rawLines <- line:
				if debugSSE && strings.HasPrefix(line, "data: ") {
					logger.Get().Debug().
						Dur("receive_latency", time.Since(receiveTime)).
						Msg("SSE data line queued for processing")
				}
			case <-done:
				return
			}
		}

		if err := scanner.Err(); err != nil {
			logger.Get().Error().Err(err).Msg("Error reading stream")
		}
	}()

	// Goroutine 2: Transform lines
	go func() {
		defer close(transformedLines)
		for line := range rawLines {
			msg := sseMessage{
				line:       line,
				isDataLine: strings.HasPrefix(line, "data: "),
			}

			// Only transform data lines
			if msg.isDataLine {
				if transformed := TransformSSELine(line); transformed != "" {
					msg.line = transformed
				} else {
					continue // Skip empty transformations
				}
			}

			select {
			case transformedLines <- msg:
			case <-done:
				return
			}
		}
	}()

	// Main goroutine: Write to client
	defer close(done)

	firstDataWritten := false
	firstFlushTime := time.Time{}
	// For Workers environment without flush support, we need to ensure each write
	// is followed by a newline to trigger streaming
	for msg := range transformedLines {
		writeStart := time.Now()

		// Write the line
		if _, err := fmt.Fprintf(w, "%s\n", msg.line); err != nil {
			logger.Get().Error().Err(err).Msg("Error writing to client")
			return
		}

		// For Workers (no flush), add an extra newline after data lines to ensure streaming
		if !canFlush && msg.isDataLine {
			if _, err := fmt.Fprintf(w, "\n"); err != nil {
				logger.Get().Error().Err(err).Msg("Error writing separator to client")
				return
			}
		}

		// Log SSE events in debug mode
		if msg.isDataLine {
			eventCount++
			if !firstDataWritten {
				logger.Get().Info().
					Dur("time_to_first_write", time.Since(startTime)).
					Msg("First SSE data written to client")
				firstDataWritten = true
			}
			if debugSSE {
				logger.Get().Debug().
					Int("event_num", eventCount).
					Dur("elapsed", time.Since(startTime)).
					Dur("write_duration", time.Since(writeStart)).
					Bool("workers_mode", !canFlush).
					Msg("SSE event written to client")
			}
		}

		// Flush after data lines or empty lines (if flushing is available)
		if (msg.isDataLine || msg.line == "") && canFlush {
			flushStart := time.Now()
			flusher.Flush()
			flushedEventCount++
			if firstFlushTime.IsZero() {
				firstFlushTime = time.Now()
				logger.Get().Info().
					Dur("time_to_first_flush", firstFlushTime.Sub(startTime)).
					Msg("First SSE flush to client")
			}
			if debugSSE {
				logger.Get().Debug().
					Dur("flush_duration", time.Since(flushStart)).
					Int("flush_count", flushedEventCount).
					Msg("Flushed SSE data")
			}
		} else if !canFlush {
			// In Workers, each write should stream automatically
			bufferedEventCount++
			if debugSSE && bufferedEventCount == 1 {
				logger.Get().Info().
					Msg("Workers environment detected - streaming without explicit flush")
			}
		}
	}

	if debugSSE {
		logger.Get().Debug().
			Int("total_events", eventCount).
			Int("flushed_events", flushedEventCount).
			Int("buffered_events", bufferedEventCount).
			Dur("total_duration", time.Since(startTime)).
			Msg("SSE streaming completed")
	}
}

// HandleProxyRequest handles incoming proxy requests for the server instance
func (s *Server) HandleProxyRequest(w http.ResponseWriter, r *http.Request) {
	// Start timing the entire request
	requestStartTime := time.Now()
	logger.Get().Info().
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Str("query", r.URL.RawQuery).
		Time("start_time", requestStartTime).
		Msg("Incoming request")

	// Read the request body once to enable retry after OAuth refresh
	bodyReadStart := time.Now()
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Error reading request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	r.Body.Close()
	bodyReadDuration := time.Since(bodyReadStart)
	logger.Get().Debug().
		Dur("body_read_duration", bodyReadDuration).
		Int("body_size", len(body)).
		Msg("Request body read complete")

	// Transform the request
	transformStart := time.Now()
	proxyReq, err := s.TransformRequest(r, body)
	if err != nil {
		http.Error(w, "Error transforming request: "+err.Error(), http.StatusInternalServerError)
		return
	}
	transformDuration := time.Since(transformStart)
	logger.Get().Debug().
		Dur("transform_duration", transformDuration).
		Msg("Request transformation complete")

	// Log when we're about to send the request
	apiCallStart := time.Now()

	// Log request details
	logger.Get().Debug().
		Time("api_call_start", apiCallStart).
		Dur("time_before_api_call", time.Since(requestStartTime)).
		Str("url", proxyReq.URL.String()).
		Str("method", proxyReq.Method).
		Int64("content_length", proxyReq.ContentLength).
		Msg("Sending request to CloudCode")

	// Log request headers
	for name, values := range proxyReq.Header {
		for _, value := range values {
			// Redact authorization header
			if strings.ToLower(name) == "authorization" {
				logger.Get().Debug().Str("header", name).Str("value", "REDACTED").Msg("Request header")
			} else {
				logger.Get().Debug().Str("header", name).Str("value", value).Msg("Request header")
			}
		}
	}

	resp, err := s.httpClient.Do(proxyReq)
	if err != nil {
		http.Error(w, "Error forwarding request: "+err.Error(), http.StatusBadGateway)
		return
	}

	// Check for 401 Unauthorized and attempt a token refresh
	if resp.StatusCode == http.StatusUnauthorized {
		logger.Get().Info().Msg("Received 401 Unauthorized, attempting to refresh token...")
		resp.Body.Close() // Close the first response body

		// Time the OAuth refresh process
		oauthRefreshStart := time.Now()
		if err := s.provider.RefreshToken(); err != nil {
			http.Error(w, "Failed to refresh token: "+err.Error(), http.StatusUnauthorized)
			return
		}

		// Reload credentials after refresh
		creds, err := s.provider.GetCredentials()
		if err != nil {
			http.Error(w, "Failed to reload credentials after refresh: "+err.Error(), http.StatusInternalServerError)
			return
		}
		s.oauthCreds = creds
		oauthRefreshDuration := time.Since(oauthRefreshStart)
		logger.Get().Debug().
			Dur("oauth_refresh_duration", oauthRefreshDuration).
			Msg("OAuth token refresh complete")

		// Re-create the request with the new token using the saved body
		retransformStart := time.Now()
		newProxyReq, err := s.TransformRequest(r, body)
		if err != nil {
			http.Error(w, "Error re-transforming request after refresh: "+err.Error(), http.StatusInternalServerError)
			return
		}
		retransformDuration := time.Since(retransformStart)
		logger.Get().Debug().
			Dur("retransform_duration", retransformDuration).
			Msg("Request re-transformation complete")

		logger.Get().Info().Msg("Retrying request with new token...")
		retryStart := time.Now()
		resp, err = s.httpClient.Do(newProxyReq)
		if err != nil {
			http.Error(w, "Error forwarding request after refresh: "+err.Error(), http.StatusBadGateway)
			return
		}
		// Update API call duration to include retry
		apiCallDuration := time.Since(apiCallStart) + time.Since(retryStart)
		logger.Get().Debug().
			Dur("api_call_duration_with_retry", apiCallDuration).
			Msg("API call complete (with retry)")
	} else {
		// Log API call duration for successful first attempt
		apiCallDuration := time.Since(apiCallStart)
		logger.Get().Debug().
			Dur("api_call_duration", apiCallDuration).
			Msg("API call complete")
	}
	defer resp.Body.Close()

	// Log the response with timing
	logger.Get().Info().
		Str("status", resp.Status).
		Dur("time_to_first_byte", time.Since(requestStartTime)).
		Dur("api_call_duration", time.Since(apiCallStart)).
		Int64("content_length", resp.ContentLength).
		Msg("Upstream response received")

	// Log response headers
	for name, values := range resp.Header {
		for _, value := range values {
			logger.Get().Debug().Str("header", name).Str("value", value).Msg("Response header")
		}
	}

	// Copy headers from the upstream response to the original response writer
	hasContentEncoding := resp.Header.Get("Content-Encoding") != ""
	for h, val := range resp.Header {
		// Skip transfer-encoding as we handle it ourselves
		if h == "Transfer-Encoding" {
			continue
		}
		// Skip Content-Length if response is compressed (to avoid FixedLengthStream errors)
		if h == "Content-Length" && hasContentEncoding {
			logger.Get().Debug().
				Str("content_encoding", resp.Header.Get("Content-Encoding")).
				Str("content_length", resp.Header.Get("Content-Length")).
				Msg("Skipping Content-Length header for compressed response")
			continue
		}
		w.Header()[h] = val
	}

	// Check if this is a streaming response - use mime.ParseMediaType for robust detection
	contentType := resp.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		// If parsing fails, fall back to simple check
		mediaType = contentType
	}
	isStreaming := mediaType == "text/event-stream" && resp.StatusCode == http.StatusOK

	// Log the Content-Type detection
	logger.Get().Debug().
		Str("raw_content_type", contentType).
		Str("parsed_media_type", mediaType).
		Bool("is_streaming", isStreaming).
		Msg("Content-Type detection for streaming")

	if isStreaming {
		// Handle SSE streaming response
		logger.Get().Info().
			Str("content_type", contentType).
			Str("path", r.URL.Path).
			Msg("Starting SSE streaming response handling")

		// Check for debug mode from environment or query parameter
		debugSSE := env.GetOrDefault("DEBUG_SSE", "false") == "true"
		if r.URL.Query().Get("debug_sse") == "true" {
			debugSSE = true
		}

		if debugSSE {
			logger.Get().Debug().
				Bool("debug_sse_enabled", debugSSE).
				Str("debug_source", "environment").
				Msg("SSE debug mode enabled")
		}

		// Set headers for SSE
		w.Header().Del("Content-Length") // Always remove Content-Length for streaming
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(resp.StatusCode)

		// Flush headers immediately to start streaming
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
			logger.Get().Debug().Msg("Flushed SSE headers immediately")
		}

		// Check if flushing is available (graceful fallback if not)
		flusher, canFlush := w.(http.Flusher)
		if !canFlush {
			logger.Get().Info().Msg("Workers environment detected - using direct streaming mode")
			// For Workers, use a simpler direct streaming approach
			streamSSEResponseDirect(resp.Body, w, debugSSE)
		} else {
			// Use goroutine pipeline for better streaming performance
			streamSSEResponse(resp.Body, w, flusher, canFlush, debugSSE)
		}
	} else {
		// Handle non-streaming response
		logger.Get().Info().
			Str("content_type", contentType).
			Str("path", r.URL.Path).
			Int("status_code", resp.StatusCode).
			Msg("Handling non-streaming response (buffering entire body)")

		w.WriteHeader(resp.StatusCode)

		// Read the entire response
		responseReadStart := time.Now()
		respBody, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			logger.Get().Error().Err(err).Msg("Error reading response body")
			return
		}
		responseReadDuration := time.Since(responseReadStart)
		logger.Get().Debug().
			Dur("response_read_duration", responseReadDuration).
			Int("response_size", len(respBody)).
			Msg("Response body read complete")

		// Log response preview
		preview := string(respBody)
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		logger.Get().Debug().Str("preview", preview).Msg("Response preview")

		// For non-streaming JSON responses, we might need to transform them too
		responseProcessStart := time.Now()
		if resp.StatusCode == http.StatusOK && strings.Contains(contentType, "application/json") {
			transformedBody := TransformJSONResponse(respBody)
			if _, err := w.Write(transformedBody); err != nil {
				logger.Get().Error().Err(err).Msg("Error writing response body")
			}
		} else {
			// Write response as-is for errors and other content types
			if _, err := w.Write(respBody); err != nil {
				logger.Get().Error().Err(err).Msg("Error writing response body")
			}
		}
		responseProcessDuration := time.Since(responseProcessStart)
		logger.Get().Debug().
			Dur("response_process_duration", responseProcessDuration).
			Msg("Response processing complete")
	}

	// Log total request duration
	totalDuration := time.Since(requestStartTime)
	logger.Get().Info().
		Dur("total_duration", totalDuration).
		Str("path", r.URL.Path).
		Msg("Request completed")
}

// Start launches the proxy server with the configured provider
func (s *Server) Start(addr string) error {
	// Load OAuth credentials on startup
	if err := s.LoadCredentials(false); err != nil {
		logger.Get().Error().Err(err).Msg("Failed to load OAuth credentials")
		logger.Get().Warn().Msg("The proxy will run but authentication will fail without valid credentials")
	}

	// Start periodic token refresh
	s.startTokenRefreshLoop()

	logger.Get().Info().Msgf("Starting proxy server on %s", addr)
	return http.ListenAndServe(addr, s.mux)
}

// LoadCredentials loads OAuth credentials using the configured provider
func (s *Server) LoadCredentials(isPeriodicRefresh bool) error {
	creds, err := s.provider.GetCredentials()
	if err != nil {
		return err
	}

	s.oauthCreds = creds

	// Check if token is expired (with a 5-minute buffer)
	if creds.ExpiryDate > 0 {
		expiryTime := time.Unix(creds.ExpiryDate/1000, 0)
		if time.Now().After(expiryTime.Add(-5 * time.Minute)) {
			logger.Get().Info().Msg("OAuth token is expired or expiring soon, attempting to refresh...")
			if err := s.provider.RefreshToken(); err != nil {
				logger.Get().Error().Err(err).Msg("Failed to refresh OAuth token")
				// Continue with the expired token, the API call might still work or will fail with 401
			} else {
				// Reload credentials after refresh
				creds, err = s.provider.GetCredentials()
				if err != nil {
					return err
				}
				s.oauthCreds = creds
			}
		} else {
			if !isPeriodicRefresh {
				timeUntilExpiry := time.Until(expiryTime)
				logger.Get().Info().Dur("valid_for", timeUntilExpiry.Round(time.Second)).Msg("OAuth token valid")
			}
		}
	}

	if !isPeriodicRefresh {
		logger.Get().Info().Str("provider", s.provider.Name()).Msg("Loaded OAuth credentials")
	}
	return nil
}

// startTokenRefreshLoop starts a goroutine to periodically refresh the OAuth token.
func (s *Server) startTokenRefreshLoop() {
	// Get refresh interval from environment, default to 5 minutes
	refreshIntervalStr := env.GetOrDefault("TOKEN_REFRESH_INTERVAL", "5m")
	refreshInterval, err := time.ParseDuration(refreshIntervalStr)
	if err != nil {
		logger.Get().Warn().Err(err).Str("value", refreshIntervalStr).Msg("Invalid token refresh interval, defaulting to 5 minutes")
		refreshInterval = 5 * time.Minute
	}

	logger.Get().Info().Dur("refresh_interval", refreshInterval).Msg("Starting periodic token refresh")

	// Run the refresh loop in a separate goroutine
	go func() {
		// Create a ticker that fires at the specified interval
		ticker := time.NewTicker(refreshInterval)
		defer ticker.Stop()

		for range ticker.C {
			logger.Get().Debug().Msg("Running periodic token refresh check...")
			if err := s.LoadCredentials(true); err != nil {
				logger.Get().Error().Err(err).Msg("Error during periodic token refresh")
			}
		}
	}()
}

// DiscoverProjectID automatically discovers the GCP project ID using the Code Assist API.
func (s *Server) DiscoverProjectID() (string, error) {
	discoveryStartTime := time.Now()
	logger.Get().Debug().Msg("Starting project ID discovery")
	defer func() {
		discoveryDuration := time.Since(discoveryStartTime)
		logger.Get().Debug().
			Dur("total_discovery_duration", discoveryDuration).
			Msg("Project ID discovery completed")
	}()

	if s.projectID != "" {
		logger.Get().Debug().Str("cached_project_id", s.projectID).Msg("Using cached project ID")
		return s.projectID, nil
	}

	if s.oauthCreds == nil {
		return "", fmt.Errorf("OAuth credentials not loaded")
	}

	initialProjectID := "default"
	clientMetadata := map[string]interface{}{
		"ideType":     "IDE_UNSPECIFIED",
		"platform":    "PLATFORM_UNSPECIFIED",
		"pluginType":  "GEMINI",
		"duetProject": initialProjectID,
	}

	loadRequest := map[string]interface{}{
		"cloudaicompanionProject": initialProjectID,
		"metadata":                clientMetadata,
	}

	// Call loadCodeAssist endpoint
	loadCallStart := time.Now()
	loadResponse, err := s.callEndpoint("loadCodeAssist", loadRequest)
	if err != nil {
		return "", fmt.Errorf("failed to call loadCodeAssist: %w", err)
	}
	loadCallDuration := time.Since(loadCallStart)
	logger.Get().Debug().
		Dur("load_code_assist_duration", loadCallDuration).
		Msg("loadCodeAssist call complete")

	if companionProject, ok := loadResponse["cloudaicompanionProject"].(string); ok && companionProject != "" {
		s.projectID = companionProject
		logger.Get().Info().
			Str("project_id", s.projectID).
			Dur("quick_discovery_duration", time.Since(discoveryStartTime)).
			Msg("Discovered project ID (quick path)")
		return s.projectID, nil
	}

	// Onboarding flow
	logger.Get().Debug().Msg("Starting onboarding flow")
	onboardingStart := time.Now()

	var tierID string
	if allowedTiers, ok := loadResponse["allowedTiers"].([]interface{}); ok {
		for _, tier := range allowedTiers {
			if tierMap, ok := tier.(map[string]interface{}); ok {
				if isDefault, ok := tierMap["isDefault"].(bool); ok && isDefault {
					tierID = tierMap["id"].(string)
					break
				}
			}
		}
	}
	if tierID == "" {
		tierID = "free-tier"
	}
	logger.Get().Debug().Str("tier_id", tierID).Msg("Selected tier for onboarding")

	onboardRequest := map[string]interface{}{
		"tierId":                  tierID,
		"cloudaicompanionProject": initialProjectID,
		"metadata":                clientMetadata,
	}

	// Initial onboarding call
	onboardCallStart := time.Now()
	lroResponse, err := s.callEndpoint("onboardUser", onboardRequest)
	if err != nil {
		return "", fmt.Errorf("failed to call onboardUser: %w", err)
	}
	onboardCallDuration := time.Since(onboardCallStart)
	logger.Get().Debug().
		Dur("onboard_user_duration", onboardCallDuration).
		Msg("onboardUser call complete")

	// Polling for completion
	pollCount := 0
	pollStart := time.Now()
	for {
		if done, ok := lroResponse["done"].(bool); ok && done {
			if response, ok := lroResponse["response"].(map[string]interface{}); ok {
				if companionProject, ok := response["cloudaicompanionProject"].(map[string]interface{}); ok {
					if id, ok := companionProject["id"].(string); ok && id != "" {
						s.projectID = id
						onboardingDuration := time.Since(onboardingStart)
						logger.Get().Info().
							Str("project_id", s.projectID).
							Dur("onboarding_duration", onboardingDuration).
							Int("poll_count", pollCount).
							Dur("polling_duration", time.Since(pollStart)).
							Msg("Discovered project ID after onboarding")
						return s.projectID, nil
					}
				}
			}
			return "", fmt.Errorf("onboarding completed but no project ID found")
		}

		pollCount++
		logger.Get().Debug().
			Int("poll_count", pollCount).
			Dur("elapsed", time.Since(pollStart)).
			Msg("Polling onboardUser status")

		time.Sleep(2 * time.Second)

		pollCallStart := time.Now()
		lroResponse, err = s.callEndpoint("onboardUser", onboardRequest)
		if err != nil {
			return "", fmt.Errorf("failed to poll onboardUser: %w", err)
		}
		pollCallDuration := time.Since(pollCallStart)
		logger.Get().Debug().
			Dur("poll_call_duration", pollCallDuration).
			Msg("Polling call complete")
	}
}

func (s *Server) callEndpoint(method string, body interface{}) (map[string]interface{}, error) {
	callStart := time.Now()
	defer func() {
		callDuration := time.Since(callStart)
		logger.Get().Debug().
			Str("method", method).
			Dur("endpoint_call_duration", callDuration).
			Msg("Code Assist API call complete")
	}()

	reqBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", fmt.Sprintf("%s/%s:%s", credentials.CodeAssistEndpoint, credentials.CodeAssistAPIVersion, method), bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+s.oauthCreds.AccessToken)
	req.Header.Set("Content-Type", "application/json")

	httpStart := time.Now()
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	httpDuration := time.Since(httpStart)

	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	logger.Get().Debug().
		Str("method", method).
		Dur("http_duration", httpDuration).
		Int("status_code", resp.StatusCode).
		Int("response_size", len(respBody)).
		Msg("HTTP request complete")

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API call failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	return result, nil
}

// setupRoutes configures all HTTP routes
func (s *Server) setupRoutes() {
	s.mux.HandleFunc("/admin/credentials", s.adminMiddleware(s.credentialsHandler))
	s.mux.HandleFunc("/admin/credentials/status", s.adminMiddleware(s.credentialsStatusHandler))
	s.mux.HandleFunc("/", s.adminMiddleware(s.HandleProxyRequest))
}

// ServeHTTP implements http.Handler interface
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// credentialsHandler handles POST /admin/credentials for setting OAuth credentials
func (s *Server) credentialsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Parse request body - using the exact same format as oauth_creds.json
	var creds credentials.OAuthCredentials
	if err := json.NewDecoder(r.Body).Decode(&creds); err != nil {
		logger.Get().Error().Err(err).Msg("Failed to decode credentials request")
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Save credentials
	if err := s.provider.SaveCredentials(&creds); err != nil {
		logger.Get().Error().Err(err).Msg("Failed to save credentials")
		http.Error(w, "Failed to save credentials", http.StatusInternalServerError)
		return
	}

	// Update server's cached credentials
	s.oauthCreds = &creds

	// Return success response
	w.Header().Set("Content-Type", "application/json")
	response := map[string]interface{}{
		"success": true,
		"message": "Credentials saved successfully",
	}
	json.NewEncoder(w).Encode(response)
}

// credentialsStatusHandler handles GET /admin/credentials/status
func (s *Server) credentialsStatusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Try to get current credentials
	creds, err := s.provider.GetCredentials()

	response := map[string]interface{}{
		"type":           "oauth",
		"hasCredentials": err == nil && creds != nil,
		"provider":       s.provider.Name(),
	}

	if err == nil && creds != nil {
		// Check expiry
		isExpired := false
		var expiresAt time.Time
		if creds.ExpiryDate > 0 {
			expiresAt = time.Unix(creds.ExpiryDate/1000, 0)
			isExpired = time.Now().After(expiresAt)
		}

		response["is_expired"] = isExpired
		if creds.ExpiryDate > 0 {
			response["expiry_date"] = creds.ExpiryDate
			response["expiry_date_formatted"] = expiresAt.Format(time.RFC3339)
		}
		response["has_refresh_token"] = creds.RefreshToken != ""
	} else if err != nil {
		response["error"] = err.Error()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
