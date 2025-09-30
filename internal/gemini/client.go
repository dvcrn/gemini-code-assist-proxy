package gemini

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/dvcrn/gemini-code-assist-proxy/internal/credentials"
	"github.com/dvcrn/gemini-code-assist-proxy/internal/server"
)

// Client is a client for the Gemini API.
type Client struct {
	httpClient server.HTTPClient
	provider   credentials.CredentialsProvider
}

// NewClient creates a new Gemini API client.
func NewClient(provider credentials.CredentialsProvider) *Client {
	return &Client{
		httpClient: server.NewHTTPClient(),
		provider:   provider,
	}
}

// LoadCodeAssist performs a request to the Gemini API to check if the credentials are valid.
func (c *Client) LoadCodeAssist() (*LoadCodeAssistResponse, error) {
	creds, err := c.provider.GetCredentials()
	if err != nil {
		return nil, fmt.Errorf("unable to get credentials: %w", err)
	}

	if creds.AccessToken == "" {
		return nil, fmt.Errorf("access token is empty")
	}

	requestBody := LoadCodeAssistRequest{
		Metadata: Metadata{
			IdeType:    "IDE_UNSPECIFIED",
			Platform:   "PLATFORM_UNSPECIFIED",
			PluginType: "GEMINI",
		},
	}

	bodyBytes, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("could not marshal request body: %w", err)
	}

	req, err := http.NewRequest("POST", fmt.Sprintf("%s/v1internal:loadCodeAssist", credentials.CodeAssistEndpoint), bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("could not create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+creds.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "google-api-nodejs-client/9.15.1")
	req.Header.Set("x-goog-api-client", "gl-node/23.5.0")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request execution error: %w", err)
	}

	// Check for 401 Unauthorized and attempt a token refresh
	if resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close() // Close the first response body

		if err := c.provider.RefreshToken(); err != nil {
			return nil, fmt.Errorf("failed to refresh token: %w", err)
		}

		// Reload credentials after refresh
		refreshedCreds, err := c.provider.GetCredentials()
		if err != nil {
			return nil, fmt.Errorf("failed to reload credentials after refresh: %w", err)
		}

		// Re-create the request with the new token
		req.Header.Set("Authorization", "Bearer "+refreshedCreds.AccessToken)

		// Retry the request
		resp, err = c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("request execution error after refresh: %w", err)
		}
	}
	defer resp.Body.Close()

	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("could not read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("auth check failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var result LoadCodeAssistResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("could not unmarshal response body: %w", err)
	}

	return &result, nil
}
