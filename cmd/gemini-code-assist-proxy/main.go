package main

import (
	"fmt"

	"github.com/dvcrn/gemini-code-assist-proxy/internal/credentials"
	"github.com/dvcrn/gemini-code-assist-proxy/internal/env"
	"github.com/dvcrn/gemini-code-assist-proxy/internal/gemini"
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

	// Perform startup auth check
	logger.Get().Info().Msg("Performing startup authentication check...")
	geminiClient := gemini.NewClient(provider)
	if response, err := geminiClient.LoadCodeAssist(); err != nil {
		logger.Get().Warn().Err(err).Msg("Startup authentication check failed.")
	} else {
		tier := fmt.Sprintf("%s (%s)", response.CurrentTier.Name, response.CurrentTier.ID)
		logger.Get().Info().
			Str("tier", tier).
			Str("project_id", response.CloudAICompanionProject).
			Bool("gcp_managed", response.GCPManaged).
			Msg("Startup authentication check successful.")
	}

	// Create server with provider
	srv := server.NewServer(provider)

	// Start server
	if err := srv.Start(":" + port); err != nil {
		logger.Get().Fatal().Err(err).Msg("Failed to start server")
	}
}
