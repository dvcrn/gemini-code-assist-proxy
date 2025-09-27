//go:build js && wasm

package main

import (
	"fmt"

	"github.com/dvcrn/gemini-code-assist-proxy/internal/credentials"
	"github.com/dvcrn/gemini-code-assist-proxy/internal/gemini"
	"github.com/dvcrn/gemini-code-assist-proxy/internal/logger"
	"github.com/dvcrn/gemini-code-assist-proxy/internal/server"
	"github.com/syumai/workers"
)

var srv *server.Server

func init() {
	// Create Cloudflare KV provider for Workers environment
	provider, err := credentials.NewCloudflareKVProvider()
	if err != nil {
		logger.Get().Error().Err(err).Msg("Failed to create credentials provider")
		// Continue anyway, authentication will fail
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
	srv = server.NewServer(provider)

	// Load OAuth credentials on startup
	if err := srv.LoadCredentials(false); err != nil {
		logger.Get().Error().Err(err).Msg("Failed to load OAuth credentials")
		logger.Get().Warn().Msg("The proxy will run but authentication will fail without valid credentials")
	}
}

func main() {
	// Serve using workers - it handles all the HTTP server setup
	workers.Serve(srv)
}
