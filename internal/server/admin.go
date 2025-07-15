package server

import (
	"net/http"
	"strings"

	"github.com/dvcrn/gemini-code-assist-proxy/internal/env"
	"github.com/dvcrn/gemini-code-assist-proxy/internal/logger"
)

// adminMiddleware checks for valid admin API key from either
// 'Authorization: Bearer <key>', 'X-Goog-Api-Key: <key>' headers, or 'key' query parameter.
func (s *Server) adminMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		adminKey, ok := env.Get("ADMIN_API_KEY")
		if !ok || adminKey == "" {
			logger.Get().Error().Msg("ADMIN_API_KEY environment variable not set")
			http.Error(w, "Admin API not configured", http.StatusInternalServerError)
			return
		}

		var providedToken string
		authHeader := r.Header.Get("Authorization")
		googApiKey := r.Header.Get("X-Goog-Api-Key")
		keyParam := r.URL.Query().Get("key")

		if authHeader != "" {
			// Expect "Bearer <token>" format, case-insensitive
			parts := strings.Split(authHeader, " ")
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				logger.Get().Warn().Msgf("Invalid Authorization header format for admin endpoint: %s %s from %s",
					r.Method, r.RequestURI, r.RemoteAddr)
				http.Error(w, "Invalid Authorization header format", http.StatusUnauthorized)
				return
			}
			providedToken = parts[1]
		} else if googApiKey != "" {
			// Use X-Goog-Api-Key header directly
			providedToken = googApiKey
		} else if keyParam != "" {
			// Use the key from query parameter directly
			providedToken = keyParam
		} else {
			logger.Get().Warn().Msgf("Missing required Authorization header, X-Goog-Api-Key header, or key query parameter for admin endpoint: %s %s from %s",
				r.Method, r.RequestURI, r.RemoteAddr)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Verify admin key
		if providedToken != adminKey {
			logger.Get().Warn().Msgf("Invalid admin API key provided: %s %s from %s",
				r.Method, r.RequestURI, r.RemoteAddr)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Admin authorized
		logger.Get().Info().Msgf("Admin request authorized: %s %s from %s",
			r.Method, r.RequestURI, r.RemoteAddr)

		next(w, r)
	}
}
