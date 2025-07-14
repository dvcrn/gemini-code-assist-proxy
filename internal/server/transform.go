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
	"regexp"
	"strings"
	"time"
)

// CloudCodeRequest represents the structure of the request expected by the Cloud Code API.
type CloudCodeRequest struct {
	Model   string                 `json:"model"`
	Project string                 `json:"project"`
	Request map[string]interface{} `json:"request"`
}

// OAuthCredentials represents the OAuth credentials from the JSON file
type OAuthCredentials struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiryDate   int64  `json:"expiry_date"`
	TokenType    string `json:"token_type"`
}

var oauthCreds *OAuthCredentials

// normalizeModelName converts any model name containing "pro" or "flash" to the
// only two models supported by CloudCode: gemini-2.5-pro and gemini-2.5-flash
func normalizeModelName(model string) string {
	lowerModel := strings.ToLower(model)
	if strings.Contains(lowerModel, "pro") {
		return "gemini-2.5-pro"
	} else if strings.Contains(lowerModel, "flash") {
		return "gemini-2.5-flash"
	}
	// Return as-is if neither pro nor flash is found
	return model
}

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

// TransformRequest rewrites the incoming standard Gemini request to the Cloud Code format.
func TransformRequest(r *http.Request) (*http.Request, error) {
	log.Println("--- Start Request Transformation ---")
	defer log.Println("--- End Request Transformation ---")

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		log.Printf("Error reading request body: %v", err)
		return nil, err
	}
	// Log truncated request body for debugging
	bodyPreview := string(body)
	if len(bodyPreview) > 200 {
		bodyPreview = bodyPreview[:200] + "..."
	}
	log.Printf("Original request body: %s", bodyPreview)

	// Parse the request body as a generic map to handle all fields
	var requestData map[string]interface{}
	if err := json.Unmarshal(body, &requestData); err != nil {
		log.Printf("Error unmarshaling JSON: %v", err)
		return nil, err
	}

	// Regex to extract model and action from the path
	// Handle paths like /v1/... or /v1beta/...
	re := regexp.MustCompile(`v1(?:beta)?/models/([^/:]+):(.+)`)
	matches := re.FindStringSubmatch(r.URL.Path)

	if len(matches) < 3 {
		log.Printf("Path '%s' did not match expected format.", r.URL.Path)
		// If the path doesn't match, we can't transform it.
		// We'll just forward it as is.
		proxyReq, err := http.NewRequest(r.Method, r.URL.String(), bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		proxyReq.Header = r.Header
		return proxyReq, nil
	}

	model := matches[1]
	action := matches[2]
	log.Printf("Extracted Model: %s, Action: %s", model, action)

	// Normalize model name
	normalizedModel := normalizeModelName(model)
	if normalizedModel != model {
		log.Printf("Normalized model from %s to %s", model, normalizedModel)
		model = normalizedModel
	}

	// Get project ID from environment or use default
	projectID := os.Getenv("CLOUDCODE_PROJECT_ID")
	if projectID == "" {
		log.Fatal("no project id set")
	}

	// Handle different request structures based on the action
	var newBody []byte

	if action == "countTokens" {
		// For countTokens, CloudCode expects only { "request": {...} } structure
		// Extract the generateContentRequest wrapper if present
		innerRequest := requestData
		if genContentReq, ok := requestData["generateContentRequest"].(map[string]interface{}); ok {
			innerRequest = genContentReq
		}

		// Add the model to the inner request
		innerRequest["model"] = "models/" + model

		// Create the countTokens request structure
		countTokensReq := map[string]interface{}{
			"request": innerRequest,
		}

		newBody, err = json.Marshal(countTokensReq)
		if err != nil {
			log.Printf("Error marshaling countTokens request: %v", err)
			return nil, err
		}
	} else {
		// For other actions (streamGenerateContent, generateContent), use the standard CloudCode structure
		cloudCodeReq := CloudCodeRequest{
			Model:   model,
			Project: projectID,
			Request: requestData,
		}

		newBody, err = json.Marshal(cloudCodeReq)
		if err != nil {
			log.Printf("Error marshaling request body: %v", err)
			return nil, err
		}
	}
	// Log truncated transformed body
	transformedPreview := string(newBody)
	if len(transformedPreview) > 200 {
		transformedPreview = transformedPreview[:200] + "..."
	}
	log.Printf("Transformed request body: %s", transformedPreview)

	// Construct the new URL for the Cloud Code API.
	newPath := "/v1internal:" + action

	// Preserve query parameters
	targetURL, err := url.Parse("https://cloudcode-pa.googleapis.com" + newPath)
	if err != nil {
		log.Printf("Error parsing target URL: %v", err)
		return nil, err
	}

	// Copy query parameters from original request (will be modified below if API key present)
	targetURL.RawQuery = r.URL.RawQuery

	// Handle authorization transformation first
	// Standard Gemini uses API key in query params, CloudCode uses Bearer token
	queryValues := targetURL.Query()
	apiKey := queryValues.Get("key")
	if apiKey != "" {
		log.Printf("API Key provided in query params, removing it and using OAuth token")
		// Remove the key from query params since CloudCode doesn't use it
		queryValues.Del("key")
		targetURL.RawQuery = queryValues.Encode()
	}

	log.Printf("Target URL: %s", targetURL.String())

	// Create the proxy request with the updated URL
	proxyReq, err := http.NewRequest(r.Method, targetURL.String(), bytes.NewReader(newBody))
	if err != nil {
		log.Printf("Error creating new request: %v", err)
		return nil, err
	}

	// Copy headers from original request
	proxyReq.Header = make(http.Header)
	for h, val := range r.Header {
		// Skip certain headers that need special handling
		if h == "Authorization" || h == "Host" || h == "Content-Length" {
			continue
		}
		proxyReq.Header[h] = val
	}

	// Set authorization based on whether API key was provided
	if apiKey != "" {
		// Use OAuth token from loaded credentials
		if oauthCreds != nil && oauthCreds.AccessToken != "" {
			proxyReq.Header.Set("Authorization", "Bearer "+oauthCreds.AccessToken)
		} else {
			log.Printf("Warning: No OAuth credentials loaded, authentication will likely fail")
		}
	} else if authHeader := r.Header.Get("Authorization"); authHeader != "" {
		// Pass through existing Authorization header if present
		proxyReq.Header.Set("Authorization", authHeader)
	}

	// Set required headers for CloudCode
	proxyReq.Header.Set("Host", targetURL.Host)
	proxyReq.Header.Set("Content-Length", fmt.Sprintf("%d", len(newBody)))
	proxyReq.Header.Set("Content-Type", "application/json")

	// Set x-goog-api-client if not present
	if proxyReq.Header.Get("x-goog-api-client") == "" {
		proxyReq.Header.Set("x-goog-api-client", "gemini-proxy/1.0")
	}

	return proxyReq, nil
}

// TransformSSELine transforms a CloudCode SSE data line to standard Gemini format
func TransformSSELine(line string) string {
	if !strings.HasPrefix(line, "data: ") {
		return line
	}

	jsonData := strings.TrimPrefix(line, "data: ")

	// Parse the JSON
	var cloudCodeResp map[string]interface{}
	if err := json.Unmarshal([]byte(jsonData), &cloudCodeResp); err != nil {
		log.Printf("Error parsing SSE JSON: %v", err)
		return line // Return unchanged if we can't parse
	}

	// CloudCode wraps the response in a "response" field, extract it
	if response, ok := cloudCodeResp["response"].(map[string]interface{}); ok {
		// Build the standard Gemini response by merging fields
		geminiResp := make(map[string]interface{})

		// Copy all fields from the response object
		for k, v := range response {
			geminiResp[k] = v
		}

		// Copy other top-level fields (except "response")
		for k, v := range cloudCodeResp {
			if k != "response" {
				geminiResp[k] = v
			}
		}

		// Convert back to JSON
		transformedJSON, err := json.Marshal(geminiResp)
		if err != nil {
			log.Printf("Error marshaling transformed response: %v", err)
			return line
		}

		return "data: " + string(transformedJSON)
	}

	// If no "response" field, return as-is
	return line
}

// TransformJSONResponse transforms a CloudCode JSON response to standard Gemini format
func TransformJSONResponse(body []byte) []byte {
	var cloudCodeResp map[string]interface{}
	if err := json.Unmarshal(body, &cloudCodeResp); err != nil {
		log.Printf("Error parsing JSON response: %v", err)
		return body // Return unchanged if we can't parse
	}

	// CloudCode wraps the response in a "response" field, extract it
	if response, ok := cloudCodeResp["response"].(map[string]interface{}); ok {
		// Build the standard Gemini response
		geminiResp := make(map[string]interface{})

		// Copy all fields from the response object
		for k, v := range response {
			geminiResp[k] = v
		}

		// Copy other top-level fields (except "response")
		for k, v := range cloudCodeResp {
			if k != "response" {
				geminiResp[k] = v
			}
		}

		// Convert back to JSON
		transformedJSON, err := json.Marshal(geminiResp)
		if err != nil {
			log.Printf("Error marshaling transformed response: %v", err)
			return body
		}

		return transformedJSON
	}

	// If no "response" field, return as-is
	return body
}
