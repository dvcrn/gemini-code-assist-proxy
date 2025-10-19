//go:build !js || !wasm

package http

import (
	"net"
	"net/http"
	"time"
)

// NewHTTPClient creates a new HTTP client for regular environments
func NewHTTPClient() HTTPClient {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
			TLSHandshakeTimeout: 10 * time.Second,
			DisableCompression:  true, // Important for SSE
			// Enable HTTP/2
			ForceAttemptHTTP2: true,
		},
	}
}
