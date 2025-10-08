package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/dvcrn/gemini-code-assist-proxy/internal/credentials"
	"github.com/dvcrn/gemini-code-assist-proxy/internal/env"
	"github.com/dvcrn/gemini-code-assist-proxy/internal/gemini"
	serverhttp "github.com/dvcrn/gemini-code-assist-proxy/internal/http"
	"github.com/dvcrn/gemini-code-assist-proxy/internal/logger"
)

// Server represents the proxy server with its dependencies
type Server struct {
	httpClient   serverhttp.HTTPClient
	provider     credentials.CredentialsProvider
	oauthCreds   *credentials.OAuthCredentials
	projectID    string
	mux          *http.ServeMux
	geminiClient *gemini.Client
}

// NewServer creates a new server instance with the given credentials provider
func NewServer(provider credentials.CredentialsProvider, projectID string) *Server {
	s := &Server{
		httpClient:   serverhttp.NewHTTPClient(),
		provider:     provider,
		projectID:    projectID,
		mux:          http.NewServeMux(),
		geminiClient: gemini.NewClient(provider),
	}
	s.setupRoutes()

	return s
}

// Start launches the proxy server with the configured provider
func (s *Server) Start(addr string) error {
	// Load OAuth credentials on startup
	if err := s.LoadCredentials(false); err != nil {
		logger.Get().Error().Err(err).Msg("Failed to load OAuth credentials")
		logger.Get().Warn().Msg("The proxy will run but authentication will fail without valid credentials")
	}

	// Start periodic token refresh
	s.startTokenRefreshLoop()

	logger.Get().Info().Msgf("Starting proxy server on %s", addr)
	return http.ListenAndServe(addr, s.mux)
}

// LoadCredentials loads OAuth credentials using the configured provider
func (s *Server) LoadCredentials(isPeriodicRefresh bool) error {
	creds, err := s.provider.GetCredentials()
	if err != nil {
		return err
	}

	s.oauthCreds = creds

	// Check if token is expired (with a 5-minute buffer)
	if creds.ExpiryDate > 0 {
		expiryTime := time.Unix(creds.ExpiryDate/1000, 0)
		if time.Now().After(expiryTime.Add(-5 * time.Minute)) {
			logger.Get().Info().Msg("OAuth token is expired or expiring soon, attempting to refresh...")
			if err := s.provider.RefreshToken(); err != nil {
				logger.Get().Error().Err(err).Msg("Failed to refresh OAuth token")
				// Continue with the expired token, the API call might still work or will fail with 401
			} else {
				// Reload credentials after refresh
				creds, err = s.provider.GetCredentials()
				if err != nil {
					return err
				}
				s.oauthCreds = creds
			}
		} else {
			if !isPeriodicRefresh {
				timeUntilExpiry := time.Until(expiryTime)
				logger.Get().Info().Dur("valid_for", timeUntilExpiry.Round(time.Second)).Msg("OAuth token valid")
			}
		}
	}

	if !isPeriodicRefresh {
		logger.Get().Info().Str("provider", s.provider.Name()).Msg("Loaded OAuth credentials")
	}
	return nil
}

// startTokenRefreshLoop starts a goroutine to periodically refresh the OAuth token.
func (s *Server) startTokenRefreshLoop() {
	// Get refresh interval from environment, default to 5 minutes
	refreshIntervalStr := env.GetOrDefault("TOKEN_REFRESH_INTERVAL", "5m")
	refreshInterval, err := time.ParseDuration(refreshIntervalStr)
	if err != nil {
		logger.Get().Warn().Err(err).Str("value", refreshIntervalStr).Msg("Invalid token refresh interval, defaulting to 5 minutes")
		refreshInterval = 5 * time.Minute
	}

	logger.Get().Info().Dur("refresh_interval", refreshInterval).Msg("Starting periodic token refresh")

	// Run the refresh loop in a separate goroutine
	go func() {
		// Create a ticker that fires at the specified interval
		ticker := time.NewTicker(refreshInterval)
		defer ticker.Stop()

		for range ticker.C {
			logger.Get().Debug().Msg("Running periodic token refresh check...")
			if err := s.LoadCredentials(true); err != nil {
				logger.Get().Error().Err(err).Msg("Error during periodic token refresh")
			}
		}
	}()
}

// setupRoutes configures all HTTP routes
func (s *Server) setupRoutes() {
	s.mux.HandleFunc("/admin/credentials", s.adminMiddleware(s.credentialsHandler))
	s.mux.HandleFunc("/admin/credentials/status", s.adminMiddleware(s.credentialsStatusHandler))
	s.mux.HandleFunc("/v1beta/models/", s.adminMiddleware(s.streamGenerateContentHandler))
	s.mux.HandleFunc("/v1/models/", s.modelsHandler)
	s.mux.HandleFunc("/v1/chat/completions", s.adminMiddleware(s.openAIChatCompletionsHandler))
}

// ServeHTTP implements http.Handler interface
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// credentialsHandler handles POST /admin/credentials for setting OAuth credentials
func (s *Server) credentialsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Parse request body - using the exact same format as oauth_creds.json
	var creds credentials.OAuthCredentials
	if err := json.NewDecoder(r.Body).Decode(&creds); err != nil {
		logger.Get().Error().Err(err).Msg("Failed to decode credentials request")
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Save credentials
	if err := s.provider.SaveCredentials(&creds); err != nil {
		logger.Get().Error().Err(err).Msg("Failed to save credentials")
		http.Error(w, "Failed to save credentials", http.StatusInternalServerError)
		return
	}

	// Update server's cached credentials
	s.oauthCreds = &creds

	// Return success response
	w.Header().Set("Content-Type", "application/json")
	response := map[string]interface{}{
		"success": true,
		"message": "Credentials saved successfully",
	}
	json.NewEncoder(w).Encode(response)
}

// credentialsStatusHandler handles GET /admin/credentials/status
func (s *Server) credentialsStatusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Try to get current credentials
	creds, err := s.provider.GetCredentials()

	response := map[string]interface{}{
		"type":           "oauth",
		"hasCredentials": err == nil && creds != nil,
		"provider":       s.provider.Name(),
	}

	if err == nil && creds != nil {
		// Check expiry
		isExpired := false
		var expiresAt time.Time
		if creds.ExpiryDate > 0 {
			expiresAt = time.Unix(creds.ExpiryDate/1000, 0)
			isExpired = time.Now().After(expiresAt)
		}

		response["is_expired"] = isExpired
		if creds.ExpiryDate > 0 {
			response["expiry_date"] = creds.ExpiryDate
			response["expiry_date_formatted"] = expiresAt.Format(time.RFC3339)
		}
		response["has_refresh_token"] = creds.RefreshToken != ""
	} else if err != nil {
		response["error"] = err.Error()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
