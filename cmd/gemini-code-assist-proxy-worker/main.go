//go:build js && wasm

package main

import (
	"fmt"

	"github.com/dvcrn/gemini-code-assist-proxy/internal/credentials"
	"github.com/dvcrn/gemini-code-assist-proxy/internal/env"
	"github.com/dvcrn/gemini-code-assist-proxy/internal/gemini"
	"github.com/dvcrn/gemini-code-assist-proxy/internal/logger"
	"github.com/dvcrn/gemini-code-assist-proxy/internal/project"
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
	loadAssistResponse, err := geminiClient.LoadCodeAssist()
	if err != nil {
		logger.Get().Warn().Err(err).Msg("Startup authentication check failed.")
	} else {
		tier := fmt.Sprintf("%s (%s)", loadAssistResponse.CurrentTier.Name, loadAssistResponse.CurrentTier.ID)
		logger.Get().Info().
			Str("tier", tier).
			Str("project_id", loadAssistResponse.CloudAICompanionProject).
			Bool("gcp_managed", loadAssistResponse.GCPManaged).
			Msg("Startup authentication check successful.")
	}

	// Discover project ID (env override, LoadCodeAssist response, or onboarding flow)
	var projectID string
	envProjectID, _ := env.Get("CLOUDCODE_GCP_PROJECT_ID")
	if loadAssistResponse != nil {
		if discoveredProjectID, err := project.Discover(provider, envProjectID, loadAssistResponse); err != nil {
			logger.Get().Warn().Err(err).Msg("Project discovery failed; continuing without explicit project ID")
		} else {
			projectID = discoveredProjectID
		}
	} else if envProjectID != "" {
		// Fallback to explicit env override when LoadCodeAssist failed
		projectID = envProjectID
	}

	if projectID == "" {
		logger.Get().Warn().Msg("No project ID discovered; CloudCode may rely on its own defaults")
	} else {
		logger.Get().Info().Str("project_id", projectID).Msg("Using project ID for CloudCode requests")
	}

	// Create server with provider and project ID
	srv = server.NewServer(provider, projectID)

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
