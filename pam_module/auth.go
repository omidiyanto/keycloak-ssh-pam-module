//go:build linux

package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/satnusa/keycloak-pam-ssh/internal/config"
	"github.com/satnusa/keycloak-pam-ssh/internal/deviceflow"
	"github.com/satnusa/keycloak-pam-ssh/internal/session"
)

// AuthResult represents the outcome of the authentication attempt.
type AuthResult int

const (
	// AuthError indicates authentication failure.
	AuthError AuthResult = iota
	// AuthSuccess indicates authentication success.
	AuthSuccess
)

// PAMConv provides callback functions for communicating with the SSH client
// via the PAM conversation mechanism. This abstraction keeps auth.go free
// from CGo dependencies, making it testable with pure Go.
type PAMConv struct {
	SendInfo  func(msg string)
	SendError func(msg string)
	Prompt    func(prompt string) string
}

// performDeviceFlowAuth orchestrates the full Keycloak Device Authorization Flow.
//
// Flow:
//  1. Load config
//  2. Call Keycloak Device Authorization endpoint → get URL + device_code
//  3. Display URL to user via PAM conversation (SSH keyboard-interactive)
//  4. Block until user presses Enter
//  5. Poll Keycloak token endpoint until user completes browser login
//  6. Save session for backchannel logout tracking
//  7. Return AuthSuccess or AuthError
func performDeviceFlowAuth(username, configPath string, conv *PAMConv) AuthResult {
	// Step 1: Load configuration
	cfg, err := config.Load(configPath)
	if err != nil {
		pamLogError("failed to load config: %v", err)
		conv.SendError("  Internal error: failed to load configuration")
		return AuthError
	}

	// Step 2: Initiate Device Authorization
	pamLog("initiating device auth at: %s", cfg.DeviceAuthEndpoint())

	authResp, err := deviceflow.StartDeviceAuth(
		cfg.DeviceAuthEndpoint(),
		cfg.Keycloak.ClientID,
		cfg.Keycloak.ClientSecret,
		cfg.Auth.Scopes,
	)
	if err != nil {
		pamLogError("device auth request failed: %v", err)
		conv.SendError("  Failed to initiate Keycloak authentication")
		return AuthError
	}

	pamLog("device auth initiated: user_code=%s, expires_in=%ds", authResp.UserCode, authResp.ExpiresIn)

	// Step 3: Determine the best URL to show the user
	verificationURL := authResp.VerificationURIComplete
	if verificationURL == "" {
		verificationURL = authResp.VerificationURI
	}

	// Step 4: Display authentication prompt to user
	displayAuthBanner(conv, verificationURL, authResp.UserCode)

	// Step 5: Block waiting for user to press Enter
	conv.Prompt("  Press ENTER after completing browser login: ")

	// Step 6: Poll for token
	pollInterval := authResp.Interval
	if pollInterval <= 0 {
		pollInterval = cfg.Auth.PollIntervalSeconds
	}

	conv.SendInfo("")
	conv.SendInfo("  ⏳ Verifying authentication with Keycloak...")

	tokenResp, err := deviceflow.PollToken(
		cfg.TokenEndpoint(),
		cfg.Keycloak.ClientID,
		cfg.Keycloak.ClientSecret,
		authResp.DeviceCode,
		pollInterval,
		cfg.Auth.PollTimeoutSeconds,
	)
	if err != nil {
		pamLogError("token polling failed for user %s: %v", username, err)
		conv.SendError(fmt.Sprintf("  ❌ Authentication failed: %v", err))
		return AuthError
	}

	pamLog("authentication successful — user: %s, session_state: %s", username, tokenResp.SessionState)
	conv.SendInfo("  ✅ Authentication successful!")

	// Step 7: Save session for backchannel logout tracking
	if err := saveAuthSession(cfg, username, tokenResp); err != nil {
		// Log but don't fail authentication — session tracking is non-critical
		pamLogError("failed to save session: %v", err)
	}

	return AuthSuccess
}

// displayAuthBanner renders the authentication banner shown to the SSH user.
func displayAuthBanner(conv *PAMConv, verificationURL, userCode string) {
	conv.SendInfo("")
	conv.SendInfo("  ╔══════════════════════════════════════════════════════╗")
	conv.SendInfo("  ║         🔐 Keycloak SSH Authentication              ║")
	conv.SendInfo("  ╚══════════════════════════════════════════════════════╝")
	conv.SendInfo("")
	conv.SendInfo("  Complete your login in the browser:")
	conv.SendInfo(fmt.Sprintf("  👉 %s", verificationURL))
	conv.SendInfo("")

	// If the URL doesn't already contain the user_code (verification_uri_complete),
	// show the code separately so the user can enter it manually.
	if userCode != "" && !strings.Contains(verificationURL, "user_code") {
		conv.SendInfo(fmt.Sprintf("  Your code: %s", userCode))
		conv.SendInfo("")
	}
}

// saveAuthSession saves the authenticated session metadata to disk so the
// backchannel logout monitor daemon can find and kill it later.
func saveAuthSession(cfg *config.Config, username string, tokenResp *deviceflow.TokenResponse) error {
	store, err := session.NewStore(cfg.Session.StorageDir)
	if err != nil {
		return fmt.Errorf("failed to create session store: %w", err)
	}

	// The PID of the sshd child process handling this SSH connection.
	// Killing this PID will terminate the entire SSH session.
	sshPid := getSSHPid()

	sessionID := tokenResp.SessionState
	if sessionID == "" {
		// Fallback if Keycloak doesn't provide session_state
		sessionID = fmt.Sprintf("local-%d-%d", sshPid, time.Now().UnixNano())
	}

	// Calculate session expiry — use token lifetime as baseline,
	// but add a generous buffer since the refresh token typically lives longer.
	expiresIn := tokenResp.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 3600 // default 1 hour if not specified
	}

	sess := &session.Session{
		SessionID: sessionID,
		Username:  username,
		SSHPid:    sshPid,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour), // Sessions valid for 24h max
	}

	if err := store.Save(sess); err != nil {
		return fmt.Errorf("failed to save session: %w", err)
	}

	pamLog("session saved — id: %s, user: %s, pid: %d", sessionID, username, sshPid)
	return nil
}

// cleanupSession removes any session file associated with the current SSH process.
// Called during pam_sm_close_session when the user logs out normally.
func cleanupSession(username, configPath string) {
	cfg, err := config.Load(configPath)
	if err != nil {
		pamLog("session cleanup: failed to load config: %v", err)
		return
	}

	store, err := session.NewStore(cfg.Session.StorageDir)
	if err != nil {
		pamLog("session cleanup: failed to open session store: %v", err)
		return
	}

	pid := os.Getpid()
	sess, err := store.FindByPID(pid)
	if err != nil {
		pamLog("session cleanup: no session found for PID %d (user: %s)", pid, username)
		return
	}

	if sess.Username == username {
		if err := store.Delete(sess.SessionID); err != nil {
			pamLog("session cleanup: failed to delete session %s: %v", sess.SessionID, err)
		} else {
			pamLog("session cleaned up — id: %s, user: %s, pid: %d", sess.SessionID, username, pid)
		}
	}
}
