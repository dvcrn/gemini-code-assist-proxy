package http

import "net/http"

// HTTPClient interface abstracts HTTP client operations
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}
