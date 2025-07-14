package server

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
)

// HandleProxyRequest is the main handler for all incoming proxy requests.
func HandleProxyRequest(w http.ResponseWriter, r *http.Request) {
	log.Printf("Incoming request: %s %s%s", r.Method, r.URL.Path, func() string {
		if r.URL.RawQuery != "" {
			return "?" + r.URL.RawQuery
		}
		return ""
	}())

	proxyReq, err := TransformRequest(r)
	if err != nil {
		http.Error(w, "Error transforming request: "+err.Error(), http.StatusInternalServerError)
		return
	}

	client := &http.Client{}
	resp, err := client.Do(proxyReq)
	if err != nil {
		http.Error(w, "Error forwarding request: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	log.Printf("Upstream response status: %s", resp.Status)

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

		// Stream the response
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()

			// Transform CloudCode SSE response to standard Gemini format
			if strings.HasPrefix(line, "data: ") {
				transformedLine := TransformSSELine(line)
				if transformedLine != "" {
					fmt.Fprintf(w, "%s\n", transformedLine)
					flusher.Flush()
				}
			} else {
				// Pass through empty lines and other SSE fields
				fmt.Fprintf(w, "%s\n", line)
				if line == "" {
					flusher.Flush()
				}
			}
		}

		if err := scanner.Err(); err != nil {
			log.Printf("Error reading stream: %v", err)
		}
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
