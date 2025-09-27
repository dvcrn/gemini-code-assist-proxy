package project

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/dvcrn/gemini-code-assist-proxy/internal/credentials"
	"github.com/dvcrn/gemini-code-assist-proxy/internal/gemini"
	"github.com/dvcrn/gemini-code-assist-proxy/internal/logger"
)

// Discover determines the GCP Project ID to use for the proxy.
func Discover(provider credentials.CredentialsProvider, envProjectID string, loadAssist *gemini.LoadCodeAssistResponse) (string, error) {
	// 1. Check for environment variable override
	if envProjectID != "" {
		logger.Get().Info().Str("project_id", envProjectID).Msg("Using project ID from CLOUDCODE_GCP_PROJECT_ID environment variable")
		return envProjectID, nil
	}

	// 2. Check the pre-fetched loadAssist response
	if loadAssist == nil {
		return "", fmt.Errorf("loadAssist response is nil")
	}

	// 3. If not GCP Managed, use the CloudAICompanionProject
	if !loadAssist.GCPManaged {
		projectID := loadAssist.CloudAICompanionProject
		logger.Get().Info().Str("project_id", projectID).Msg("Using project ID from loadCodeAssist (gcpManaged=false)")
		return projectID, nil
	}

	// 4. If GCP Managed, run the full discovery/onboarding flow
	logger.Get().Info().Msg("gcpManaged=true, starting full project discovery and onboarding flow")
	return runOnboardingFlow(provider, loadAssist)
}

func runOnboardingFlow(provider credentials.CredentialsProvider, loadResponse *gemini.LoadCodeAssistResponse) (string, error) {
	discoveryStartTime := time.Now()

	creds, err := provider.GetCredentials()
	if err != nil {
		return "", fmt.Errorf("OAuth credentials not loaded: %w", err)
	}

	if companionProject := loadResponse.CloudAICompanionProject; companionProject != "" {
		logger.Get().Info().
			Str("project_id", companionProject).
			Dur("quick_discovery_duration", time.Since(discoveryStartTime)).
			Msg("Discovered project ID (quick path)")
		return companionProject, nil
	}

	// Onboarding flow
	logger.Get().Debug().Msg("Starting onboarding flow")
	onboardingStart := time.Now()

	var tierID string
	if loadResponse.AllowedTiers != nil {
		for _, tier := range loadResponse.AllowedTiers {
			if tier.IsDefault {
				tierID = tier.ID
				break
			}
		}
	}
	if tierID == "" {
		tierID = "free-tier"
	}
	logger.Get().Debug().Str("tier_id", tierID).Msg("Selected tier for onboarding")

	initialProjectID := "default"
	clientMetadata := map[string]interface{}{
		"ideType":     "IDE_UNSPECIFIED",
		"platform":    "PLATFORM_UNSPECIFIED",
		"pluginType":  "GEMINI",
		"duetProject": initialProjectID,
	}

	onboardRequest := map[string]interface{}{
		"tierId":                  tierID,
		"cloudaicompanionProject": initialProjectID,
		"metadata":                clientMetadata,
	}

	// Initial onboarding call
	onboardCallStart := time.Now()
	lroResponse, err := callEndpoint(creds.AccessToken, "onboardUser", onboardRequest)
	if err != nil {
		return "", fmt.Errorf("failed to call onboardUser: %w", err)
	}
	onboardCallDuration := time.Since(onboardCallStart)
	logger.Get().Debug().
		Dur("onboard_user_duration", onboardCallDuration).
		Msg("onboardUser call complete")

	// Polling for completion
	pollCount := 0
	pollStart := time.Now()
	for {
		if done, ok := lroResponse["done"].(bool); ok && done {
			if response, ok := lroResponse["response"].(map[string]interface{}); ok {
				if companionProject, ok := response["cloudaicompanionProject"].(map[string]interface{}); ok {
					if id, ok := companionProject["id"].(string); ok && id != "" {
						onboardingDuration := time.Since(onboardingStart)
						logger.Get().Info().
							Str("project_id", id).
							Dur("onboarding_duration", onboardingDuration).
							Int("poll_count", pollCount).
							Dur("polling_duration", time.Since(pollStart)).
							Msg("Discovered project ID after onboarding")
						return id, nil
					}
				}
			}
			return "", fmt.Errorf("onboarding completed but no project ID found")
		}

		pollCount++
		logger.Get().Debug().
			Int("poll_count", pollCount).
			Dur("elapsed", time.Since(pollStart)).
			Msg("Polling onboardUser status")

		time.Sleep(2 * time.Second)

		pollCallStart := time.Now()
		lroResponse, err = callEndpoint(creds.AccessToken, "onboardUser", onboardRequest)
		if err != nil {
			return "", fmt.Errorf("failed to poll onboardUser: %w", err)
		}
		pollCallDuration := time.Since(pollCallStart)
		logger.Get().Debug().
			Dur("poll_call_duration", pollCallDuration).
			Msg("Polling call complete")
	}
}

func callEndpoint(accessToken, method string, body interface{}) (map[string]interface{}, error) {
	callStart := time.Now()
	defer func() {
		callDuration := time.Since(callStart)
		logger.Get().Debug().
			Str("method", method).
			Dur("endpoint_call_duration", callDuration).
			Msg("Code Assist API call complete")
	}()

	reqBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", fmt.Sprintf("%s/%s:%s", credentials.CodeAssistEndpoint, credentials.CodeAssistAPIVersion, method), bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	httpClient := &http.Client{}
	httpStart := time.Now()
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	httpDuration := time.Since(httpStart)

	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	logger.Get().Debug().
		Str("method", method).
		Dur("http_duration", httpDuration).
		Int("status_code", resp.StatusCode).
		Int("response_size", len(respBody)).
		Msg("HTTP request complete")

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API call failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	return result, nil
}
