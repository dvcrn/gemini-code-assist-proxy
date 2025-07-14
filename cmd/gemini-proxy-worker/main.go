//go:build js && wasm

package main

import (
	"log"
	"net/http"

	"github.com/dvcrn/gemini-cli-proxy/internal/server"
	"github.com/syumai/workers"
)

func init() {
	// Initialize HTTP client - this will be called before main
	server.InitHTTPClient()

	// Load OAuth credentials on startup
	if err := server.LoadOAuthCredentials(); err != nil {
		log.Printf("Failed to load OAuth credentials: %v", err)
		log.Println("The proxy will run but authentication will fail without valid credentials")
	}
}

func main() {
	// Serve using workers - it handles all the HTTP server setup
	workers.Serve(http.HandlerFunc(server.HandleProxyRequest))
}
