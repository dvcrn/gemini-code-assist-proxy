package gemini

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/dvcrn/gemini-code-assist-proxy/internal/credentials"
	serverhttp "github.com/dvcrn/gemini-code-assist-proxy/internal/http"
)

// Client is a client for the Gemini API.
type Client struct {
	httpClient serverhttp.HTTPClient
	provider   credentials.CredentialsProvider
}

// NewClient creates a new Gemini API client.
func NewClient(provider credentials.CredentialsProvider) *Client {
	return &Client{
		httpClient: serverhttp.NewHTTPClient(),
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

// GenerateContent performs a request to the Gemini API to generate content.
func (c *Client) GenerateContent(req *GenerateContentRequest) (*GenerateContentResponse, error) {
	creds, err := c.provider.GetCredentials()
	if err != nil {
		return nil, fmt.Errorf("unable to get credentials: %w", err)
	}

	if creds.AccessToken == "" {
		return nil, fmt.Errorf("access token is empty")
	}

	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("could not marshal request body: %w", err)
	}

	httpReq, err := http.NewRequest("POST", fmt.Sprintf("%s/v1internal:generateContent", credentials.CodeAssistEndpoint), bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("could not create request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+creds.AccessToken)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", "GeminiCLI/v23.5.0 (darwin; arm64) google-api-nodejs-client/9.15.1")
	httpReq.Header.Set("x-goog-api-client", "gl-node/23.5.0")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(httpReq)
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
		httpReq.Header.Set("Authorization", "Bearer "+refreshedCreds.AccessToken)

		// Retry the request
		resp, err = c.httpClient.Do(httpReq)
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
		return nil, fmt.Errorf("generateContent failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var result GenerateContentResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("could not unmarshal response body: %w", err)
	}

	return &result, nil
}

// StreamGenerateContent performs a streaming request and sends each raw SSE line to the provided channel.
// It does not transform or interpret SSE content; lines are forwarded as-is.
// The caller owns the lifecycle of the 'out' channel; this function will not close it.
func (c *Client) StreamGenerateContent(req *GenerateContentRequest, out chan<- string) error {
	creds, err := c.provider.GetCredentials()
	if err != nil {
		return fmt.Errorf("unable to get credentials: %w", err)
	}

	if creds.AccessToken == "" {
		return fmt.Errorf("access token is empty")
	}

	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("could not marshal request body: %w", err)
	}

	httpReq, err := http.NewRequest("POST", fmt.Sprintf("%s/v1internal:streamGenerateContent", credentials.CodeAssistEndpoint), bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("could not create request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+creds.AccessToken)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", "GeminiCLI/v23.5.0 (darwin; arm64) google-api-nodejs-client/9.15.1")
	httpReq.Header.Set("x-goog-api-client", "gl-node/23.5.0")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("request execution error: %w", err)
	}

	// Check for 401 Unauthorized and attempt a token refresh
	if resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close() // Close the first response body

		if err := c.provider.RefreshToken(); err != nil {
			return fmt.Errorf("failed to refresh token: %w", err)
		}

		// Reload credentials after refresh
		refreshedCreds, err := c.provider.GetCredentials()
		if err != nil {
			return fmt.Errorf("failed to reload credentials after refresh: %w", err)
		}

		// Re-create the request with the new token
		httpReq.Header.Set("Authorization", "Bearer "+refreshedCreds.AccessToken)

		// Retry the request
		resp, err = c.httpClient.Do(httpReq)
		if err != nil {
			return fmt.Errorf("request execution error after refresh: %w", err)
		}
	}

	// Non-OK status: read body and return error
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		respBody, readErr := ioutil.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("streamGenerateContent failed with status %d and read error: %v", resp.StatusCode, readErr)
		}
		return fmt.Errorf("streamGenerateContent failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	// Start a goroutine to stream lines to the provided channel.
	go func() {
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		// Increase the scanner buffer for large SSE events
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)

		for scanner.Scan() {
			out <- scanner.Text()
		}
		// scanner.Err() is intentionally ignored to keep this minimal per request
	}()

	return nil
}
