package credentials

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dvcrn/gemini-cli-proxy/internal/env"
)

// FileProvider implements CredentialsProvider using file-based storage
type FileProvider struct {
	filePath   string
	httpClient *http.Client
}

// NewFileProvider creates a new file-based credentials provider
func NewFileProvider() (*FileProvider, error) {
	provider := &FileProvider{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}

	// Determine the file path
	if err := provider.determineFilePath(); err != nil {
		return nil, err
	}

	return provider, nil
}

// determineFilePath sets the file path based on environment variables or defaults
func (f *FileProvider) determineFilePath() error {
	// 1. Check for file path in environment variable
	if credsPath, ok := env.Get("CLOUDCODE_OAUTH_CREDS_PATH"); ok {
		f.filePath = credsPath
		return nil
	}

	// 2. Use default path
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}
	f.filePath = filepath.Join(homeDir, ".gemini", "oauth_creds.json")
	return nil
}

// GetCredentials retrieves credentials from file or environment
func (f *FileProvider) GetCredentials() (*OAuthCredentials, error) {
	// Try to load from file first
	if f.filePath != "" {
		data, err := os.ReadFile(f.filePath)
		if err == nil {
			creds := &OAuthCredentials{}
			if err := json.Unmarshal(data, creds); err != nil {
				return nil, fmt.Errorf("failed to parse credentials from file: %w", err)
			}
			return creds, nil
		}
		// If file doesn't exist, continue to check environment variable
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to read credentials file: %w", err)
		}
	}

	// Fallback to raw JSON from environment variable
	if credsJSON, ok := env.Get("CLOUDCODE_OAUTH_CREDS"); ok {
		creds := &OAuthCredentials{}
		if err := json.Unmarshal([]byte(credsJSON), creds); err != nil {
			return nil, fmt.Errorf("failed to parse CLOUDCODE_OAUTH_CREDS: %w", err)
		}
		// When using environment variable, disable file writing
		f.filePath = ""
		return creds, nil
	}

	return nil, fmt.Errorf("OAuth credentials not found. Please set CLOUDCODE_OAUTH_CREDS_PATH, place oauth_creds.json in ~/.gemini/, or set CLOUDCODE_OAUTH_CREDS")
}

// SaveCredentials saves credentials to file if file path is set
func (f *FileProvider) SaveCredentials(creds *OAuthCredentials) error {
	if f.filePath == "" {
		// When using environment variable, we can't save
		log.Println("Cannot save credentials when using CLOUDCODE_OAUTH_CREDS environment variable")
		return nil
	}

	// Ensure directory exists
	dir := filepath.Dir(f.filePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Marshal credentials
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal credentials: %w", err)
	}

	// Write to file
	if err := ioutil.WriteFile(f.filePath, data, 0o600); err != nil {
		return fmt.Errorf("failed to write credentials to %s: %w", f.filePath, err)
	}

	log.Printf("Saved credentials to %s", f.filePath)
	return nil
}

// RefreshToken refreshes the OAuth token using the refresh token
func (f *FileProvider) RefreshToken() error {
	creds, err := f.GetCredentials()
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

	req, err := http.NewRequest("POST", "https://oauth2.googleapis.com/token", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	// Execute refresh request
	resp, err := f.httpClient.Do(req)
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
	if err := f.SaveCredentials(creds); err != nil {
		log.Printf("Warning: failed to save refreshed credentials: %v", err)
		// Don't fail the refresh if save fails
	}

	log.Println("Successfully refreshed OAuth token")
	return nil
}

// Name returns the provider name
func (f *FileProvider) Name() string {
	if f.filePath != "" {
		return fmt.Sprintf("FileProvider(%s)", f.filePath)
	}
	return "FileProvider(env)"
}
