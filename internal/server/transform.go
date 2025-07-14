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
)

// CloudCodeRequest represents the structure of the request expected by the Cloud Code API.
type CloudCodeRequest struct {
	Model   string                 `json:"model"`
	Project string                 `json:"project"`
	Request map[string]interface{} `json:"request"`
}

var geminiPathRegex = regexp.MustCompile(`v1(?:beta)?/models/([^/:]+):(.+)`)

// parseGeminiPath extracts the model and action from a Gemini API path
// Returns empty strings if the path doesn't match the expected format
func parseGeminiPath(path string) (model, action string) {
	matches := geminiPathRegex.FindStringSubmatch(path)
	if len(matches) < 3 {
		return "", ""
	}
	return matches[1], matches[2]
}

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

// buildCountTokensRequest creates a request body for the countTokens action
// CloudCode expects only { "request": {...} } structure for countTokens
func buildCountTokensRequest(requestData map[string]interface{}, model string) ([]byte, error) {
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

	return json.Marshal(countTokensReq)
}

// buildCloudCodeRequest creates a standard CloudCode request body
// Used for actions like streamGenerateContent and generateContent
func buildCloudCodeRequest(requestData map[string]interface{}, model, projectID string) ([]byte, error) {
	cloudCodeReq := CloudCodeRequest{
		Model:   model,
		Project: projectID,
		Request: requestData,
	}
	return json.Marshal(cloudCodeReq)
}

// processQueryParams processes query parameters, extracting and removing API key if present
// Returns the processed query string and whether an API key was found
func processQueryParams(originalQuery string) (processedQuery string, hasAPIKey bool) {
	if originalQuery == "" {
		return "", false
	}

	values, err := url.ParseQuery(originalQuery)
	if err != nil {
		// If we can't parse, return original
		return originalQuery, false
	}

	apiKey := values.Get("key")
	if apiKey != "" {
		values.Del("key")
		return values.Encode(), true
	}

	return originalQuery, false
}

// unwrapCloudCodeResponse extracts the standard Gemini response from CloudCode's wrapped format
// CloudCode wraps responses in a "response" field which needs to be unwrapped
func unwrapCloudCodeResponse(cloudCodeResp map[string]interface{}) map[string]interface{} {
	// If there's no "response" field, return as-is
	response, ok := cloudCodeResp["response"].(map[string]interface{})
	if !ok {
		return cloudCodeResp
	}

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

	return geminiResp
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

	// Extract model and action from the path
	model, action := parseGeminiPath(r.URL.Path)

	if model == "" || action == "" {
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
	log.Printf("Extracted Model: %s, Action: %s", model, action)

	// Normalize model name
	normalizedModel := normalizeModelName(model)
	if normalizedModel != model {
		log.Printf("Normalized model from %s to %s", model, normalizedModel)
		model = normalizedModel
	}

	// Get project ID from environment or use default
	projectID := os.Getenv("CLOUDCODE_GCP_PROJECT_ID")
	if projectID == "" {
		var err error
		projectID, err = DiscoverProjectID()
		if err != nil {
			log.Printf("Error discovering project ID: %v", err)
			return nil, err
		}
	} else {
		log.Printf("Using project ID from CLOUDCODE_GCP_PROJECT_ID environment variable: %s", projectID)
	}

	// Build the appropriate request body based on the action
	var newBody []byte
	if action == "countTokens" {
		newBody, err = buildCountTokensRequest(requestData, model)
		if err != nil {
			log.Printf("Error building countTokens request: %v", err)
			return nil, err
		}
	} else {
		newBody, err = buildCloudCodeRequest(requestData, model, projectID)
		if err != nil {
			log.Printf("Error building CloudCode request: %v", err)
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

	// Process query parameters and check for API key
	processedQuery, hasAPIKey := processQueryParams(r.URL.RawQuery)
	targetURL.RawQuery = processedQuery

	if hasAPIKey {
		log.Printf("API Key provided in query params, removed it for CloudCode request")
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
	if hasAPIKey {
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
		// Only log if debug mode is enabled
		if os.Getenv("DEBUG_SSE") == "true" {
			log.Printf("Error parsing SSE JSON: %v", err)
		}
		return line // Return unchanged if we can't parse
	}

	// Unwrap the CloudCode response
	geminiResp := unwrapCloudCodeResponse(cloudCodeResp)

	// Convert back to JSON
	transformedJSON, err := json.Marshal(geminiResp)
	if err != nil {
		// Only log if debug mode is enabled
		if os.Getenv("DEBUG_SSE") == "true" {
			log.Printf("Error marshaling transformed response: %v", err)
		}
		return line
	}

	return "data: " + string(transformedJSON)
}

// TransformJSONResponse transforms a CloudCode JSON response to standard Gemini format
func TransformJSONResponse(body []byte) []byte {
	var cloudCodeResp map[string]interface{}
	if err := json.Unmarshal(body, &cloudCodeResp); err != nil {
		log.Printf("Error parsing JSON response: %v", err)
		return body // Return unchanged if we can't parse
	}

	// Unwrap the CloudCode response
	geminiResp := unwrapCloudCodeResponse(cloudCodeResp)

	// Convert back to JSON
	transformedJSON, err := json.Marshal(geminiResp)
	if err != nil {
		log.Printf("Error marshaling transformed response: %v", err)
		return body
	}

	return transformedJSON
}
