package main

import (
	"log"
	"os"

	"github.com/dvcrn/gemini-cli-proxy/internal/credentials"
	"github.com/dvcrn/gemini-cli-proxy/internal/server"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "9877"
	}

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
