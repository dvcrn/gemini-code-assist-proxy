package server

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/dvcrn/gemini-code-assist-proxy/internal/env"
	"github.com/dvcrn/gemini-code-assist-proxy/internal/logger"
)

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

// normalizeModelName converts any model name containing "pro", "flash", or "lite" to
// our normalized CloudCode models
func normalizeModelName(model string) string {
	lowerModel := strings.ToLower(model)
	if strings.Contains(lowerModel, "lite") {
		return "gemini-2.5-flash-lite"
	} else if strings.Contains(lowerModel, "pro") {
		return "gemini-2.5-pro"
	} else if strings.Contains(lowerModel, "flash") {
		return "gemini-2.5-flash"
	}
	return model
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

	// Copy top-level fields first (except "response")
	for k, v := range cloudCodeResp {
		if k != "response" {
			geminiResp[k] = v
		}
	}

	// Then, copy all fields from the nested response object.
	// This ensures the nested response's fields (like 'candidates') take precedence.
	for k, v := range response {
		geminiResp[k] = v
	}

	return geminiResp
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
		if env.GetOrDefault("DEBUG_SSE", "false") == "true" {
			logger.Get().Debug().Err(err).Msg("Error parsing SSE JSON")
		}
		return line // Return unchanged if we can't parse
	}

	// Unwrap the CloudCode response
	geminiResp := unwrapCloudCodeResponse(cloudCodeResp)

	// Convert back to JSON
	transformedJSON, err := json.Marshal(geminiResp)
	if err != nil {
		// Only log if debug mode is enabled
		if env.GetOrDefault("DEBUG_SSE", "false") == "true" {
			logger.Get().Debug().Err(err).Msg("Error marshaling transformed response")
		}
		return line
	}

	return "data: " + string(transformedJSON)
}
