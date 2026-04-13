package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Session represents an active SSH session authenticated via Keycloak.
type Session struct {
	SessionID string    `json:"session_id"`
	Username  string    `json:"username"`
	SSHPid    int       `json:"ssh_pid"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Store manages session files on disk.
// Each session is stored as a JSON file named {session_id}.json in the storage directory.
type Store struct {
	dir string
}

// NewStore creates a new session store backed by the given directory.
// The directory and any parents are created automatically with mode 0700.
func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create session directory %s: %w", dir, err)
	}
	return &Store{dir: dir}, nil
}

// sessionPath returns the filesystem path for a given session ID.
func (s *Store) sessionPath(sessionID string) string {
	// Sanitize session ID to prevent path traversal
	safe := filepath.Base(sessionID)
	return filepath.Join(s.dir, safe+".json")
}

// Save writes a session to disk as a JSON file.
func (s *Store) Save(sess *Session) error {
	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal session: %w", err)
	}

	path := s.sessionPath(sess.SessionID)
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write session file %s: %w", path, err)
	}

	return nil
}

// Load reads a session from disk by its session ID.
func (s *Store) Load(sessionID string) (*Session, error) {
	path := s.sessionPath(sessionID)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read session file %s: %w", path, err)
	}

	var sess Session
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, fmt.Errorf("failed to parse session file %s: %w", path, err)
	}

	return &sess, nil
}

// Delete removes a session file from disk.
func (s *Store) Delete(sessionID string) error {
	path := s.sessionPath(sessionID)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete session file %s: %w", path, err)
	}
	return nil
}

// FindBySessionID looks up a session by its Keycloak session ID.
func (s *Store) FindBySessionID(sessionID string) (*Session, error) {
	return s.Load(sessionID)
}

// FindByPID searches all session files for one matching the given SSH PID.
// Returns the session and its ID, or an error if not found.
func (s *Store) FindByPID(pid int) (*Session, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read session directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		sessionID := entry.Name()[:len(entry.Name())-5] // strip ".json"
		sess, err := s.Load(sessionID)
		if err != nil {
			continue
		}
		if sess.SSHPid == pid {
			return sess, nil
		}
	}

	return nil, fmt.Errorf("no session found for PID %d", pid)
}

// ListAll returns all active sessions from the store.
func (s *Store) ListAll() ([]*Session, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read session directory: %w", err)
	}

	var sessions []*Session
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		sessionID := entry.Name()[:len(entry.Name())-5]
		sess, err := s.Load(sessionID)
		if err != nil {
			continue
		}
		sessions = append(sessions, sess)
	}

	return sessions, nil
}

// CleanExpired removes all session files that have passed their expiration time.
// Also removes session files whose SSH PID is no longer running.
// Returns the number of sessions cleaned up.
func (s *Store) CleanExpired() (int, error) {
	sessions, err := s.ListAll()
	if err != nil {
		return 0, err
	}

	cleaned := 0
	now := time.Now()
	for _, sess := range sessions {
		shouldClean := false

		// Check if session expired
		if !sess.ExpiresAt.IsZero() && now.After(sess.ExpiresAt) {
			shouldClean = true
		}

		// Check if PID is still alive (send signal 0 to check existence)
		if !shouldClean && sess.SSHPid > 0 {
			process, err := os.FindProcess(sess.SSHPid)
			if err != nil {
				shouldClean = true
			} else {
				// On Linux, FindProcess always succeeds. Check /proc instead.
				if _, err := os.Stat(fmt.Sprintf("/proc/%d", sess.SSHPid)); os.IsNotExist(err) {
					shouldClean = true
				}
				_ = process
			}
		}

		if shouldClean {
			if err := s.Delete(sess.SessionID); err == nil {
				cleaned++
			}
		}
	}

	return cleaned, nil
}
