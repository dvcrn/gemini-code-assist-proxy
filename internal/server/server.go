package server

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dvcrn/gemini-cli-proxy/internal/credentials"
	"github.com/dvcrn/gemini-cli-proxy/internal/env"
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

// streamSSEResponse handles SSE streaming with a goroutine pipeline for better performance
func streamSSEResponse(body io.Reader, w http.ResponseWriter, flusher http.Flusher, canFlush bool) {
	// Get buffer size from environment, default to 3
	bufferSize := 3
	if envSize, ok := env.Get("SSE_BUFFER_SIZE"); ok {
		if size, err := strconv.Atoi(envSize); err == nil && size > 0 {
			bufferSize = size
		}
	}

	debugSSE := env.GetOrDefault("DEBUG_SSE", "false") == "true"
	startTime := time.Now()
	eventCount := 0

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
			if firstLine && debugSSE {
				log.Printf("First SSE line received after %v", time.Since(startTime))
				firstLine = false
			}
			select {
			case rawLines <- line:
			case <-done:
				return
			}
		}

		if err := scanner.Err(); err != nil {
			log.Printf("Error reading stream: %v", err)
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

	for msg := range transformedLines {
		if _, err := fmt.Fprintf(w, "%s\n", msg.line); err != nil {
			log.Printf("Error writing to client: %v", err)
			return
		}

		// Log SSE events in debug mode
		if debugSSE && msg.isDataLine {
			eventCount++
			log.Printf("SSE event #%d sent to client after %v", eventCount, time.Since(startTime))
		}

		// Flush after data lines or empty lines (if flushing is available)
		if (msg.isDataLine || msg.line == "") && canFlush {
			flusher.Flush()
		}
	}

	if debugSSE {
		log.Printf("SSE streaming completed: %d events in %v", eventCount, time.Since(startTime))
	}
}

// HandleProxyRequest handles incoming proxy requests for the server instance
func (s *Server) HandleProxyRequest(w http.ResponseWriter, r *http.Request) {
	log.Printf("Incoming request: %s %s%s", r.Method, r.URL.Path, func() string {
		if r.URL.RawQuery != "" {
			return "?" + r.URL.RawQuery
		}
		return ""
	}())

	// Read the request body once to enable retry after OAuth refresh
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Error reading request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	r.Body.Close()

	proxyReq, err := s.TransformRequest(r, body)
	if err != nil {
		http.Error(w, "Error transforming request: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Log when we're about to send the request
	startTime := time.Now()
	log.Printf("Sending request to CloudCode at %s", startTime.Format("15:04:05.000"))

	resp, err := s.httpClient.Do(proxyReq)
	if err != nil {
		http.Error(w, "Error forwarding request: "+err.Error(), http.StatusBadGateway)
		return
	}

	// Check for 401 Unauthorized and attempt a token refresh
	if resp.StatusCode == http.StatusUnauthorized {
		log.Println("Received 401 Unauthorized, attempting to refresh token...")
		resp.Body.Close() // Close the first response body

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

		// Re-create the request with the new token using the saved body
		newProxyReq, err := s.TransformRequest(r, body)
		if err != nil {
			http.Error(w, "Error re-transforming request after refresh: "+err.Error(), http.StatusInternalServerError)
			return
		}

		log.Println("Retrying request with new token...")
		resp, err = s.httpClient.Do(newProxyReq)
		if err != nil {
			http.Error(w, "Error forwarding request after refresh: "+err.Error(), http.StatusBadGateway)
			return
		}
	}
	defer resp.Body.Close()

	responseTime := time.Since(startTime)
	log.Printf("Upstream response status: %s (took %v)", resp.Status, responseTime)

	// Copy headers from the upstream response to the original response writer
	for h, val := range resp.Header {
		// Skip transfer-encoding as we handle it ourselves
		if h == "Transfer-Encoding" {
			continue
		}
		w.Header()[h] = val
	}

	// Check if this is a streaming response
	contentType := resp.Header.Get("Content-Type")
	isStreaming := contentType == "text/event-stream" && resp.StatusCode == http.StatusOK

	if isStreaming {
		// Handle SSE streaming response
		log.Println("Handling streaming response")

		// Set headers for SSE
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(resp.StatusCode)

		// Check if flushing is available (graceful fallback if not)
		flusher, canFlush := w.(http.Flusher)
		if !canFlush {
			log.Println("ResponseWriter does not support flushing - streaming may be buffered")
		}

		// Use goroutine pipeline for better streaming performance
		streamSSEResponse(resp.Body, w, flusher, canFlush)
	} else {
		// Handle non-streaming response
		w.WriteHeader(resp.StatusCode)

		// Read the entire response
		respBody, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Printf("Error reading response body: %v", err)
			return
		}

		// Log response preview
		preview := string(respBody)
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		log.Printf("Response preview: %s", preview)

		// For non-streaming JSON responses, we might need to transform them too
		if resp.StatusCode == http.StatusOK && strings.Contains(contentType, "application/json") {
			transformedBody := TransformJSONResponse(respBody)
			if _, err := w.Write(transformedBody); err != nil {
				log.Printf("Error writing response body: %v", err)
			}
		} else {
			// Write response as-is for errors and other content types
			if _, err := w.Write(respBody); err != nil {
				log.Printf("Error writing response body: %v", err)
			}
		}
	}
}

// Start launches the proxy server with the configured provider
func (s *Server) Start(addr string) error {
	// Load OAuth credentials on startup
	if err := s.LoadCredentials(); err != nil {
		log.Printf("Failed to load OAuth credentials: %v", err)
		log.Println("The proxy will run but authentication will fail without valid credentials")
	}

	log.Printf("Starting proxy server on %s", addr)
	return http.ListenAndServe(addr, s.mux)
}

// LoadCredentials loads OAuth credentials using the configured provider
func (s *Server) LoadCredentials() error {
	creds, err := s.provider.GetCredentials()
	if err != nil {
		return err
	}

	s.oauthCreds = creds

	// Check if token is expired (with a 5-minute buffer)
	if creds.ExpiryDate > 0 {
		expiryTime := time.Unix(creds.ExpiryDate/1000, 0)
		if time.Now().After(expiryTime.Add(-5 * time.Minute)) {
			log.Println("OAuth token is expired or expiring soon, attempting to refresh...")
			if err := s.provider.RefreshToken(); err != nil {
				log.Printf("Failed to refresh OAuth token: %v", err)
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
			timeUntilExpiry := time.Until(expiryTime)
			log.Printf("OAuth token valid for %v", timeUntilExpiry.Round(time.Second))
		}
	}

	log.Printf("Loaded OAuth credentials from %s", s.provider.Name())
	return nil
}

// DiscoverProjectID automatically discovers the GCP project ID using the Code Assist API.
func (s *Server) DiscoverProjectID() (string, error) {
	if s.projectID != "" {
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

	loadResponse, err := s.callEndpoint("loadCodeAssist", loadRequest)
	if err != nil {
		return "", fmt.Errorf("failed to call loadCodeAssist: %w", err)
	}

	if companionProject, ok := loadResponse["cloudaicompanionProject"].(string); ok && companionProject != "" {
		s.projectID = companionProject
		log.Printf("Discovered project ID: %s", s.projectID)
		return s.projectID, nil
	}

	// Onboarding flow
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

	onboardRequest := map[string]interface{}{
		"tierId":                  tierID,
		"cloudaicompanionProject": initialProjectID,
		"metadata":                clientMetadata,
	}

	lroResponse, err := s.callEndpoint("onboardUser", onboardRequest)
	if err != nil {
		return "", fmt.Errorf("failed to call onboardUser: %w", err)
	}

	// Polling for completion
	for {
		if done, ok := lroResponse["done"].(bool); ok && done {
			if response, ok := lroResponse["response"].(map[string]interface{}); ok {
				if companionProject, ok := response["cloudaicompanionProject"].(map[string]interface{}); ok {
					if id, ok := companionProject["id"].(string); ok && id != "" {
						s.projectID = id
						log.Printf("Discovered project ID after onboarding: %s", s.projectID)
						return s.projectID, nil
					}
				}
			}
			return "", fmt.Errorf("onboarding completed but no project ID found")
		}

		time.Sleep(2 * time.Second)
		lroResponse, err = s.callEndpoint("onboardUser", onboardRequest)
		if err != nil {
			return "", fmt.Errorf("failed to poll onboardUser: %w", err)
		}
	}
}

func (s *Server) callEndpoint(method string, body interface{}) (map[string]interface{}, error) {
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

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

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
		log.Printf("Failed to decode credentials request: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Save credentials
	if err := s.provider.SaveCredentials(&creds); err != nil {
		log.Printf("Failed to save credentials: %v", err)
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
