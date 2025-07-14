package server

import (
	"encoding/json"
	"fmt"
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

// LoadOAuthCredentials loads OAuth credentials from the CLOUDCODE_OAUTH_CREDS environment variable
func LoadOAuthCredentials() error {
	credsJSON := os.Getenv("CLOUDCODE_OAUTH_CREDS")
	if credsJSON == "" {
		return fmt.Errorf("CLOUDCODE_OAUTH_CREDS environment variable not set or empty")
	}

	data := []byte(credsJSON)
	creds := &OAuthCredentials{}
	if err := json.Unmarshal(data, creds); err != nil {
		return fmt.Errorf("failed to parse CLOUDCODE_OAUTH_CREDS: %v", err)
	}

	oauthCreds = creds
	log.Println("Loaded OAuth credentials from CLOUDCODE_OAUTH_CREDS environment variable")

	// Check if token is expired
	if creds.ExpiryDate > 0 {
		expiryTime := creds.ExpiryDate / 1000 // Convert from milliseconds to seconds
		currentTime := time.Now().Unix()
		if currentTime >= expiryTime {
			log.Printf("WARNING: OAuth token has expired (expired at %v)", time.Unix(expiryTime, 0))
			log.Println("Please refresh your OAuth credentials in the CLOUDCODE_OAUTH_CREDS environment variable")
		} else {
			timeUntilExpiry := time.Duration(expiryTime-currentTime) * time.Second
			log.Printf("OAuth token valid for %v", timeUntilExpiry)
		}
	}

	return nil
}
