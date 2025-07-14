package server

import (
	"bytes"
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
)

// OAuthCredentials represents the OAuth credentials from the JSON file
type OAuthCredentials struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiryDate   int64  `json:"expiry_date"`
	TokenType    string `json:"token_type"`
}

// TokenRefreshResponse represents the response from the token refresh endpoint
type TokenRefreshResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	Scope       string `json:"scope"`
	TokenType   string `json:"token_type"`
}

var oauthCreds *OAuthCredentials
var oauthCredsPath string // Store the path to the credentials file
var projectID string

const (
	codeAssistEndpoint   = "https://cloudcode-pa.googleapis.com"
	codeAssistAPIVersion = "v1internal"
	oauthClientID        = "681255809395-oo8ft2oprdrnp9e3aqf6av3hmdib135j.apps.googleusercontent.com"
	oauthClientSecret    = "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"
	oauthRedirectURI     = "http://localhost:45289"
)

// LoadOAuthCredentials loads OAuth credentials from various sources.
// It checks for credentials in the following order:
// 1. CLOUDCODE_OAUTH_CREDS_PATH environment variable (file path)
// 2. Default path: ~/.gemini/oauth_creds.json
// 3. CLOUDCODE_OAUTH_CREDS environment variable (raw JSON content)
func LoadOAuthCredentials() error {
	// 1. Check for file path in environment variable
	if credsPath := os.Getenv("CLOUDCODE_OAUTH_CREDS_PATH"); credsPath != "" {
		if err := loadCredsFromFile(credsPath); err == nil {
			log.Printf("Loaded OAuth credentials from path specified in CLOUDCODE_OAUTH_CREDS_PATH: %s", credsPath)
			return nil
		} else {
			log.Printf("Warning: CLOUDCODE_OAUTH_CREDS_PATH is set, but failed to load credentials from %s: %v", credsPath, err)
		}
	}

	// 2. Check default file locations
	homeDir, _ := os.UserHomeDir()
	defaultPaths := []string{
		filepath.Join(homeDir, ".gemini", "oauth_creds.json"),
	}

	for _, path := range defaultPaths {
		if err := loadCredsFromFile(path); err == nil {
			log.Printf("Loaded OAuth credentials from default location: %s", path)
			return nil
		}
	}

	// 3. Fallback to raw JSON from environment variable
	if credsJSON := os.Getenv("CLOUDCODE_OAUTH_CREDS"); credsJSON != "" {
		if err := loadCredsFromJSON(credsJSON); err == nil {
			log.Println("Loaded OAuth credentials from CLOUDCODE_OAUTH_CREDS environment variable")
			return nil
		} else {
			return fmt.Errorf("failed to parse CLOUDCODE_OAUTH_CREDS: %v", err)
		}
	}

	return fmt.Errorf("OAuth credentials not found. Please set CLOUDCODE_OAUTH_CREDS_PATH, place oauth_creds.json in a default location, or set CLOUDCODE_OAUTH_CREDS")
}

// loadCredsFromFile attempts to load and parse credentials from a file.
func loadCredsFromFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	// Store the path for later use (saving refreshed tokens)
	oauthCredsPath = path
	return parseAndSetCreds(data)
}

// loadCredsFromJSON attempts to load and parse credentials from a JSON string.
func loadCredsFromJSON(jsonStr string) error {
	return parseAndSetCreds([]byte(jsonStr))
}

// parseAndSetCreds parses JSON data, sets the global credentials, and checks for expiry.
func parseAndSetCreds(data []byte) error {
	creds := &OAuthCredentials{}
	if err := json.Unmarshal(data, &creds); err != nil {
		return err
	}

	oauthCreds = creds

	// Check if token is expired (with a 5-minute buffer)
	if creds.ExpiryDate > 0 {
		expiryTime := time.Unix(creds.ExpiryDate/1000, 0)
		if time.Now().After(expiryTime.Add(-5 * time.Minute)) {
			log.Println("OAuth token is expired or expiring soon, attempting to refresh...")
			if err := refreshAccessToken(); err != nil {
				log.Printf("Failed to refresh OAuth token: %v", err)
				// Continue with the expired token, the API call might still work or will fail with 401
			}
		} else {
			timeUntilExpiry := time.Until(expiryTime)
			log.Printf("OAuth token valid for %v", timeUntilExpiry.Round(time.Second))
		}
	}

	return nil
}

// refreshAccessToken uses the refresh token to get a new access token
func refreshAccessToken() error {
	if oauthCreds == nil || oauthCreds.RefreshToken == "" {
		return fmt.Errorf("no refresh token available")
	}

	form := url.Values{}
	form.Add("client_id", oauthClientID)
	form.Add("client_secret", oauthClientSecret)
	form.Add("refresh_token", oauthCreds.RefreshToken)
	form.Add("grant_type", "refresh_token")

	req, err := http.NewRequest("POST", "https://oauth2.googleapis.com/token", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	// Use a new client for the refresh request to avoid deadlocks on the global httpClient
	client := &http.Client{}
	resp, err := client.Do(req)
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

	var refreshResp TokenRefreshResponse
	if err := json.Unmarshal(body, &refreshResp); err != nil {
		return err
	}

	// Update credentials
	oauthCreds.AccessToken = refreshResp.AccessToken
	oauthCreds.ExpiryDate = time.Now().Add(time.Duration(refreshResp.ExpiresIn)*time.Second).Unix() * 1000
	log.Println("Successfully refreshed OAuth token.")

	// Save the updated credentials back to the file
	if oauthCredsPath != "" {
		updatedCredsJSON, err := json.MarshalIndent(oauthCreds, "", "  ")
		if err != nil {
			log.Printf("Warning: failed to marshal updated credentials: %v", err)
			return nil
		}
		if err := ioutil.WriteFile(oauthCredsPath, updatedCredsJSON, 0644); err != nil {
			log.Printf("Warning: failed to write updated credentials to %s: %v", oauthCredsPath, err)
		} else {
			log.Printf("Saved refreshed credentials to %s", oauthCredsPath)
		}
	}

	return nil
}

// DiscoverProjectID automatically discovers the GCP project ID using the Code Assist API.
func DiscoverProjectID() (string, error) {
	if projectID != "" {
		return projectID, nil
	}

	if oauthCreds == nil {
		return "", fmt.Errorf("OAuth credentials not loaded")
	}

	initialProjectID := "default"
	clientMetadata := map[string]interface{}{
		"ideType":     "IDE_UNSPECIFIED",
		"platform":    "PLATFORM_UNSPECIFIED",
		"pluginType":  "GEMINI",
		"duetProject": initialProjectID,
	}

	loadRequest := map[string]interface{}{
		"cloudaicompanionProject": initialProjectID,
		"metadata":                clientMetadata,
	}

	loadResponse, err := callEndpoint("loadCodeAssist", loadRequest)
	if err != nil {
		return "", fmt.Errorf("failed to call loadCodeAssist: %w", err)
	}

	if companionProject, ok := loadResponse["cloudaicompanionProject"].(string); ok && companionProject != "" {
		projectID = companionProject
		log.Printf("Discovered project ID: %s", projectID)
		return projectID, nil
	}

	// Onboarding flow
	var tierID string
	if allowedTiers, ok := loadResponse["allowedTiers"].([]interface{}); ok {
		for _, tier := range allowedTiers {
			if tierMap, ok := tier.(map[string]interface{}); ok {
				if isDefault, ok := tierMap["isDefault"].(bool); ok && isDefault {
					tierID = tierMap["id"].(string)
					break
				}
			}
		}
	}
	if tierID == "" {
		tierID = "free-tier"
	}

	onboardRequest := map[string]interface{}{
		"tierId":                  tierID,
		"cloudaicompanionProject": initialProjectID,
		"metadata":                clientMetadata,
	}

	lroResponse, err := callEndpoint("onboardUser", onboardRequest)
	if err != nil {
		return "", fmt.Errorf("failed to call onboardUser: %w", err)
	}

	// Polling for completion
	for {
		if done, ok := lroResponse["done"].(bool); ok && done {
			if response, ok := lroResponse["response"].(map[string]interface{}); ok {
				if companionProject, ok := response["cloudaicompanionProject"].(map[string]interface{}); ok {
					if id, ok := companionProject["id"].(string); ok && id != "" {
						projectID = id
						log.Printf("Discovered project ID after onboarding: %s", projectID)
						return projectID, nil
					}
				}
			}
			return "", fmt.Errorf("onboarding completed but no project ID found")
		}

		time.Sleep(2 * time.Second)
		lroResponse, err = callEndpoint("onboardUser", onboardRequest)
		if err != nil {
			return "", fmt.Errorf("failed to poll onboardUser: %w", err)
		}
	}
}

func callEndpoint(method string, body interface{}) (map[string]interface{}, error) {
	reqBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", fmt.Sprintf("%s/%s:%s", codeAssistEndpoint, codeAssistAPIVersion, method), bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+oauthCreds.AccessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API call failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	return result, nil
}
