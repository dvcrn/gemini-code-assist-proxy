package server

import (
	"bufio"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// Global HTTP client instance
var httpClient HTTPClient

// InitHTTPClient initializes the global HTTP client
func InitHTTPClient() {
	httpClient = NewHTTPClient()
}

// sseMessage represents a single SSE message to be processed
type sseMessage struct {
	line       string
	isDataLine bool
}

// streamSSEResponse handles SSE streaming with a goroutine pipeline for better performance
func streamSSEResponse(body io.Reader, w http.ResponseWriter, flusher http.Flusher) {
	// Get buffer size from environment, default to 3
	bufferSize := 3
	if envSize := os.Getenv("SSE_BUFFER_SIZE"); envSize != "" {
		if size, err := strconv.Atoi(envSize); err == nil && size > 0 {
			bufferSize = size
		}
	}

	debugSSE := os.Getenv("DEBUG_SSE") == "true"
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

		// Flush after data lines or empty lines
		if msg.isDataLine || msg.line == "" {
			flusher.Flush()
		}
	}

	if debugSSE {
		log.Printf("SSE streaming completed: %d events in %v", eventCount, time.Since(startTime))
	}
}

// HandleProxyRequest is the main handler for all incoming proxy requests.
func HandleProxyRequest(w http.ResponseWriter, r *http.Request) {
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

	proxyReq, err := TransformRequest(r, body)
	if err != nil {
		http.Error(w, "Error transforming request: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Log when we're about to send the request
	startTime := time.Now()
	log.Printf("Sending request to CloudCode at %s", startTime.Format("15:04:05.000"))

	resp, err := httpClient.Do(proxyReq)
	if err != nil {
		http.Error(w, "Error forwarding request: "+err.Error(), http.StatusBadGateway)
		return
	}

	// Check for 401 Unauthorized and attempt a token refresh
	if resp.StatusCode == http.StatusUnauthorized {
		log.Println("Received 401 Unauthorized, attempting to refresh token...")
		resp.Body.Close() // Close the first response body

		if err := refreshAccessToken(); err != nil {
			http.Error(w, "Failed to refresh token: "+err.Error(), http.StatusUnauthorized)
			return
		}

		// Re-create the request with the new token using the saved body
		newProxyReq, err := TransformRequest(r, body)
		if err != nil {
			http.Error(w, "Error re-transforming request after refresh: "+err.Error(), http.StatusInternalServerError)
			return
		}

		log.Println("Retrying request with new token...")
		resp, err = httpClient.Do(newProxyReq)
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

		// Create a flusher for real-time streaming
		flusher, ok := w.(http.Flusher)
		if !ok {
			log.Println("ResponseWriter does not support flushing")
			http.Error(w, "Streaming not supported", http.StatusInternalServerError)
			return
		}

		// Use goroutine pipeline for better streaming performance
		streamSSEResponse(resp.Body, w, flusher)
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

// Start launches the proxy server.
func Start(addr string) {
	// Initialize HTTP client
	InitHTTPClient()

	// Load OAuth credentials on startup
	if err := LoadOAuthCredentials(); err != nil {
		log.Printf("Failed to load OAuth credentials: %v", err)
		log.Println("The proxy will run but authentication will fail without valid credentials")
	}

	http.HandleFunc("/", HandleProxyRequest)
	log.Printf("Starting proxy server on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal(err)
	}
}
