package server

import (
	"net/http"
	"time"

	"github.com/dvcrn/gemini-code-assist-proxy/internal/logger"
)

// loggingMiddleware logs all incoming requests
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Log the request
		logger.Get().Info().
			Str("method", r.Method).
			Str("url", r.URL.String()).
			Str("remote_addr", r.RemoteAddr).
			Msg("Incoming request")

		// Call the next handler
		next.ServeHTTP(w, r)

		// Log the response
		logger.Get().Info().
			Str("method", r.Method).
			Str("url", r.URL.String()).
			Dur("duration", time.Since(start)).
			Msg("Finished request")
	})
}
