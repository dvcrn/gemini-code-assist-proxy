package main

import (
	"github.com/dvcrn/gemini-code-assist-proxy/internal/credentials"
	"github.com/dvcrn/gemini-code-assist-proxy/internal/env"
	"github.com/dvcrn/gemini-code-assist-proxy/internal/logger"
	"github.com/dvcrn/gemini-code-assist-proxy/internal/server"
)

func main() {
	port := env.GetOrDefault("PORT", "9877")

	// Create file provider
	provider, err := credentials.NewFileProvider()
	if err != nil {
		logger.Get().Fatal().Err(err).Msg("Failed to create credentials provider")
	}

	// Create server with provider
	srv := server.NewServer(provider)

	// Start server
	if err := srv.Start(":" + port); err != nil {
		logger.Get().Fatal().Err(err).Msg("Failed to start server")
	}
}
