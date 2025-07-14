//go:build js && wasm

package main

import (
	"log"
	"net/http"

	"github.com/dvcrn/gemini-cli-proxy/internal/credentials"
	"github.com/dvcrn/gemini-cli-proxy/internal/server"
	"github.com/syumai/workers"
)

var srv *server.Server

func init() {
	// Create Cloudflare KV provider for Workers environment
	provider, err := credentials.NewCloudflareKVProvider()
	if err != nil {
		log.Printf("Failed to create credentials provider: %v", err)
		// Continue anyway, authentication will fail
	}

	// Create server with provider
	srv = server.NewServer(provider)

	// Load OAuth credentials on startup
	if err := srv.LoadCredentials(); err != nil {
		log.Printf("Failed to load OAuth credentials: %v", err)
		log.Println("The proxy will run but authentication will fail without valid credentials")
	}
}

func main() {
	// Serve using workers - it handles all the HTTP server setup
	workers.Serve(http.HandlerFunc(srv.HandleProxyRequest))
}
