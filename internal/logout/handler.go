//go:build linux

package logout

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"syscall"

	"github.com/satnusa/keycloak-pam-ssh/internal/session"
)

// LogoutTokenClaims represents the relevant claims from a Keycloak
// OIDC Back-Channel Logout Token (a JWT Security Event Token).
//
// Required claims per spec:
//   - iss (Issuer)
//   - aud (Audience)
//   - iat (Issued At)
//   - jti (JWT ID)
//   - events (must contain "http://schemas.openid.net/event/backchannel-logout")
//   - sid and/or sub (Session ID and/or Subject)
type LogoutTokenClaims struct {
	Issuer    string                 `json:"iss"`
	Subject   string                 `json:"sub"`
	Audience  interface{}            `json:"aud"`
	IssuedAt  int64                  `json:"iat"`
	JWTID     string                 `json:"jti"`
	SessionID string                 `json:"sid"`
	Events    map[string]interface{} `json:"events"`
}

// Handler handles backchannel logout webhook requests from Keycloak.
type Handler struct {
	store  *session.Store
	logger *log.Logger
}

// NewHandler creates a new backchannel logout handler.
func NewHandler(store *session.Store, logger *log.Logger) *Handler {
	return &Handler{
		store:  store,
		logger: logger,
	}
}

// ServeHTTP handles the POST /backchannel-logout endpoint.
// Keycloak sends a POST with form parameter "logout_token" containing
// a signed JWT with the session ID (sid) to terminate.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.logger.Printf("[WARN] Received %s request to backchannel-logout (expected POST)", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseForm(); err != nil {
		h.logger.Printf("[ERROR] Failed to parse form: %v", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	logoutToken := r.FormValue("logout_token")
	if logoutToken == "" {
		h.logger.Printf("[ERROR] No logout_token in request")
		http.Error(w, "Missing logout_token", http.StatusBadRequest)
		return
	}

	// Parse the logout token JWT (without signature verification —
	// the token comes from our trusted Keycloak server over HTTPS/internal network)
	claims, err := parseLogoutToken(logoutToken)
	if err != nil {
		h.logger.Printf("[ERROR] Failed to parse logout token: %v", err)
		http.Error(w, "Invalid logout token", http.StatusBadRequest)
		return
	}

	// Validate that this is a backchannel logout event
	if _, ok := claims.Events["http://schemas.openid.net/event/backchannel-logout"]; !ok {
		h.logger.Printf("[ERROR] Token is not a backchannel-logout event")
		http.Error(w, "Invalid event type", http.StatusBadRequest)
		return
	}

	if claims.SessionID == "" {
		h.logger.Printf("[WARN] No sid in logout token (sub: %s), cannot identify specific session", claims.Subject)
		http.Error(w, "Missing sid claim — enable 'Backchannel logout session required' in Keycloak client settings", http.StatusBadRequest)
		return
	}

	h.logger.Printf("[INFO] Backchannel logout received — session: %s, subject: %s, issuer: %s",
		claims.SessionID, claims.Subject, claims.Issuer)

	// Find the local SSH session matching this Keycloak session ID
	sess, err := h.store.FindBySessionID(claims.SessionID)
	if err != nil {
		h.logger.Printf("[INFO] Session %s not found locally (may have already ended): %v", claims.SessionID, err)
		// Return 200 — Keycloak expects success even if session is already gone
		w.WriteHeader(http.StatusOK)
		return
	}

	h.logger.Printf("[INFO] Terminating SSH session — user: %s, PID: %d, session: %s",
		sess.Username, sess.SSHPid, claims.SessionID)

	// Send SIGHUP to the sshd child process to terminate the SSH connection
	if err := syscall.Kill(sess.SSHPid, syscall.SIGHUP); err != nil {
		h.logger.Printf("[WARN] Failed to send SIGHUP to PID %d: %v (process may have already exited)", sess.SSHPid, err)
	} else {
		h.logger.Printf("[INFO] SIGHUP sent to PID %d — SSH session terminated", sess.SSHPid)
	}

	// Clean up the session file
	if err := h.store.Delete(claims.SessionID); err != nil {
		h.logger.Printf("[WARN] Failed to delete session file for %s: %v", claims.SessionID, err)
	}

	w.WriteHeader(http.StatusOK)
}

// parseLogoutToken decodes a JWT logout token and extracts the claims.
// We only decode the payload (base64) without verifying the signature,
// because the token arrives from our trusted Keycloak instance.
func parseLogoutToken(tokenStr string) (*LogoutTokenClaims, error) {
	parts := strings.Split(tokenStr, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid JWT format: expected 3 parts, got %d", len(parts))
	}

	// Decode the payload (second segment)
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("failed to decode JWT payload: %w", err)
	}

	var claims LogoutTokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("failed to parse JWT claims: %w", err)
	}

	return &claims, nil
}
