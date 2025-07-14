package server

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"time"
)

// OAuthCredentials represents the OAuth credentials from the JSON file
type OAuthCredentials struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiryDate   int64  `json:"expiry_date"`
	TokenType    string `json:"token_type"`
}

var oauthCreds *OAuthCredentials

// LoadOAuthCredentials loads OAuth credentials from ~/.gemini/oauth_creds.json
func LoadOAuthCredentials() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %v", err)
	}

	credsPath := fmt.Sprintf("%s/.gemini/oauth_creds.json", homeDir)
	data, err := ioutil.ReadFile(credsPath)
	if err != nil {
		return fmt.Errorf("failed to read oauth_creds.json: %v", err)
	}

	creds := &OAuthCredentials{}
	if err := json.Unmarshal(data, creds); err != nil {
		return fmt.Errorf("failed to parse oauth_creds.json: %v", err)
	}

	oauthCreds = creds
	log.Printf("Loaded OAuth credentials from %s", credsPath)

	// Check if token is expired
	if creds.ExpiryDate > 0 {
		expiryTime := creds.ExpiryDate / 1000 // Convert from milliseconds to seconds
		currentTime := time.Now().Unix()
		if currentTime >= expiryTime {
			log.Printf("WARNING: OAuth token has expired (expired at %v)", time.Unix(expiryTime, 0))
			log.Println("Please refresh your OAuth credentials in ~/.gemini/oauth_creds.json")
		} else {
			timeUntilExpiry := time.Duration(expiryTime-currentTime) * time.Second
			log.Printf("OAuth token valid for %v", timeUntilExpiry)
		}
	}

	return nil
}
