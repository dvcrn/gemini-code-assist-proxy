//go:build js && wasm

package credentials

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dvcrn/gemini-code-assist-proxy/internal/logger"

	"github.com/syumai/workers/cloudflare/fetch"
	"github.com/syumai/workers/cloudflare/kv"
)

// CloudflareKVProvider implements CredentialsProvider using Cloudflare KV storage
type CloudflareKVProvider struct {
	kvStore    *kv.Namespace
	httpClient *fetch.Client
}

// NewCloudflareKVProvider creates a new Cloudflare KV-based credentials provider
func NewCloudflareKVProvider() (*CloudflareKVProvider, error) {
	// In Cloudflare Workers, KV namespaces are accessed via bindings
	// The binding name is configured in wrangler.toml
	kvStore, err := kv.NewNamespace("gemini_code_assist_proxy_kv")
	if err != nil {
		return nil, fmt.Errorf("failed to initialize KV namespace: %w", err)
	}

	return &CloudflareKVProvider{
		kvStore:    kvStore,
		httpClient: fetch.NewClient(),
	}, nil
}

// GetCredentials retrieves credentials from Cloudflare KV
func (c *CloudflareKVProvider) GetCredentials() (*OAuthCredentials, error) {
	// Get credentials JSON from KV
	credsJSON, err := c.kvStore.GetString("gemini_cli_oauth_credentials", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get credentials from KV: %w", err)
	}

	if credsJSON == "" {
		return nil, fmt.Errorf("no credentials found in KV storage")
	}

	// Parse JSON
	var creds OAuthCredentials
	if err := json.Unmarshal([]byte(credsJSON), &creds); err != nil {
		return nil, fmt.Errorf("failed to parse credentials JSON: %w", err)
	}

	return &creds, nil
}

// SaveCredentials saves credentials to Cloudflare KV
func (c *CloudflareKVProvider) SaveCredentials(creds *OAuthCredentials) error {
	// Marshal to JSON
	credsJSON, err := json.Marshal(creds)
	if err != nil {
		return fmt.Errorf("failed to marshal credentials: %w", err)
	}

	// Store in KV
	if err := c.kvStore.PutString("gemini_cli_oauth_credentials", string(credsJSON), nil); err != nil {
		return fmt.Errorf("failed to store credentials in KV: %w", err)
	}

	logger.Get().Info().Msg("Saved credentials to Cloudflare KV")
	return nil
}

// RefreshToken refreshes the OAuth token using the refresh token
func (c *CloudflareKVProvider) RefreshToken() error {
	creds, err := c.GetCredentials()
	if err != nil {
		return fmt.Errorf("failed to get credentials for refresh: %w", err)
	}

	if creds.RefreshToken == "" {
		return fmt.Errorf("no refresh token available")
	}

	// Prepare refresh request
	form := url.Values{}
	form.Add("client_id", OAuthClientID)
	form.Add("client_secret", OAuthClientSecret)
	form.Add("refresh_token", creds.RefreshToken)
	form.Add("grant_type", "refresh_token")

	// Create fetch request for Workers
	fetchReq, err := fetch.NewRequest(context.Background(), "POST", "https://oauth2.googleapis.com/token", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	fetchReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// Execute refresh request
	resp, err := c.httpClient.Do(fetchReq, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("token refresh failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var refreshResp TokenRefreshResponse
	if err := json.Unmarshal(body, &refreshResp); err != nil {
		return err
	}

	// Update credentials
	creds.AccessToken = refreshResp.AccessToken
	creds.ExpiryDate = time.Now().Add(time.Duration(refreshResp.ExpiresIn)*time.Second).Unix() * 1000
	creds.TokenType = refreshResp.TokenType

	// Update scope if provided in refresh response
	if refreshResp.Scope != "" {
		creds.Scope = refreshResp.Scope
	}

	// Save updated credentials
	if err := c.SaveCredentials(creds); err != nil {
		logger.Get().Warn().Err(err).Msg("failed to save refreshed credentials")
		// Don't fail the refresh if save fails
	}

	logger.Get().Info().Msg("Successfully refreshed OAuth token")
	return nil
}

// Name returns the provider name
func (c *CloudflareKVProvider) Name() string {
	return "CloudflareKVProvider"
}
