package main

import (
	"log"

	"github.com/dvcrn/gemini-cli-proxy/internal/credentials"
	"github.com/dvcrn/gemini-cli-proxy/internal/env"
	"github.com/dvcrn/gemini-cli-proxy/internal/server"
)

func main() {
	port := env.GetOrDefault("PORT", "9877")

	// Create file provider
	provider, err := credentials.NewFileProvider()
	if err != nil {
		log.Fatalf("Failed to create credentials provider: %v", err)
	}

	// Create server with provider
	srv := server.NewServer(provider)

	// Start server
	if err := srv.Start(":" + port); err != nil {
		log.Fatal(err)
	}
}
