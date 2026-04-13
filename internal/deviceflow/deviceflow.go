package deviceflow

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// DeviceAuthResponse represents the response from the Device Authorization endpoint.
type DeviceAuthResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// TokenResponse represents a successful token response from Keycloak.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	SessionState string `json:"session_state"`
	Scope        string `json:"scope"`
}

// TokenError represents an error response from the token endpoint.
type TokenError struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// StartDeviceAuth initiates the OAuth 2.0 Device Authorization Grant (RFC 8628)
// by calling the Keycloak device authorization endpoint.
func StartDeviceAuth(deviceAuthEndpoint, clientID, clientSecret, scopes string) (*DeviceAuthResponse, error) {
	data := url.Values{
		"client_id": {clientID},
		"scope":     {scopes},
	}
	if clientSecret != "" {
		data.Set("client_secret", clientSecret)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.PostForm(deviceAuthEndpoint, data)
	if err != nil {
		return nil, fmt.Errorf("device auth request failed (network error or timeout): %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read device auth response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("device auth failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var authResp DeviceAuthResponse
	if err := json.Unmarshal(body, &authResp); err != nil {
		return nil, fmt.Errorf("failed to parse device auth response: %w", err)
	}

	return &authResp, nil
}

// PollToken polls the Keycloak token endpoint using the device_code until the user
// completes browser authentication or the request times out.
//
// It handles the standard OAuth 2.0 device flow polling states:
//   - authorization_pending: user hasn't authenticated yet, keep polling
//   - slow_down: increase polling interval
//   - expired_token: device code expired
//   - access_denied: user denied access
func PollToken(tokenEndpoint, clientID, clientSecret, deviceCode string, interval, timeout int) (*TokenResponse, error) {
	if interval <= 0 {
		interval = 5
	}
	if timeout <= 0 {
		timeout = 300
	}

	deadline := time.Now().Add(time.Duration(timeout) * time.Second)
	pollInterval := time.Duration(interval) * time.Second
	client := &http.Client{Timeout: 10 * time.Second}

	for {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("authentication timed out after %d seconds", timeout)
		}

		data := url.Values{
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			"device_code": {deviceCode},
			"client_id":   {clientID},
		}
		if clientSecret != "" {
			data.Set("client_secret", clientSecret)
		}

		resp, err := client.PostForm(tokenEndpoint, data)
		if err != nil {
			// Network error — retry after interval
			time.Sleep(pollInterval)
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			time.Sleep(pollInterval)
			continue
		}

		// Success — user has authenticated
		if resp.StatusCode == http.StatusOK {
			var tokenResp TokenResponse
			if err := json.Unmarshal(body, &tokenResp); err != nil {
				return nil, fmt.Errorf("failed to parse token response: %w", err)
			}
			return &tokenResp, nil
		}

		// Handle error response
		var tokenErr TokenError
		if err := json.Unmarshal(body, &tokenErr); err == nil {
			switch tokenErr.Error {
			case "authorization_pending":
				// User hasn't authenticated yet — keep polling
			case "slow_down":
				// Server asks us to slow down — increase interval by 5 seconds
				pollInterval += 5 * time.Second
			case "expired_token":
				return nil, fmt.Errorf("device code expired — please try again")
			case "access_denied":
				return nil, fmt.Errorf("access denied by user")
			default:
				return nil, fmt.Errorf("token error: %s — %s", tokenErr.Error, tokenErr.ErrorDescription)
			}
		}

		time.Sleep(pollInterval)
	}
}
