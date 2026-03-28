// Package session manages ephemeral session identity, claim tracking,
// and stale-session detection for the st CLI.
package session

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Session represents an active CLI session (one Claude Code run).
type Session struct {
	ID           string
	StartedAt    time.Time
	AgentID      string
	Sprint       string
	LastActive   time.Time
	ClaimedItems []string
}

// Manager provides session lifecycle operations.
type Manager struct {
	dir string // .as/sessions/
	ttl time.Duration
}

// NewManager creates a session manager.
// dir is the .as/sessions/ directory path.
// ttl is the stale claim threshold.
func NewManager(dir string, ttl time.Duration) *Manager {
	return &Manager{dir: dir, ttl: ttl}
}

// Load reads a session file. Returns nil, nil if not found.
func (m *Manager) Load(sessionID string) (*Session, error) {
	path := m.path(sessionID)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	s := &Session{}
	scanner := bufio.NewScanner(f)
	var inClaimed bool

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if trimmed == "claimed_items:" {
			inClaimed = true
			continue
		}

		if strings.HasPrefix(trimmed, "- ") && inClaimed {
			item := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
			item = strings.Trim(item, `"'`)
			if item != "" && item != "[]" {
				s.ClaimedItems = append(s.ClaimedItems, item)
			}
			continue
		}

		if inClaimed && !strings.HasPrefix(trimmed, "- ") {
			inClaimed = false
		}

		if idx := strings.Index(trimmed, ":"); idx >= 0 {
			key := strings.TrimSpace(trimmed[:idx])
			val := strings.TrimSpace(trimmed[idx+1:])
			val = strings.Trim(val, `"'`)

			switch key {
			case "id":
				s.ID = val
			case "started_at":
				s.StartedAt = parseTime(val)
			case "agent_id":
				s.AgentID = val
			case "sprint":
				s.Sprint = val
			case "last_active":
				s.LastActive = parseTime(val)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return s, nil
}

// Save writes a session file.
func (m *Manager) Save(s *Session) error {
	if err := os.MkdirAll(m.dir, 0755); err != nil {
		return err
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("id: %s\n", s.ID))
	b.WriteString(fmt.Sprintf("started_at: %s\n", s.StartedAt.Format(time.RFC3339)))
	if s.AgentID != "" {
		b.WriteString(fmt.Sprintf("agent_id: %s\n", s.AgentID))
	}
	if s.Sprint != "" {
		b.WriteString(fmt.Sprintf("sprint: %s\n", s.Sprint))
	}
	b.WriteString(fmt.Sprintf("last_active: %s\n", s.LastActive.Format(time.RFC3339)))
	b.WriteString("claimed_items:\n")
	if len(s.ClaimedItems) == 0 {
		b.WriteString("  - []\n")
	} else {
		for _, id := range s.ClaimedItems {
			b.WriteString(fmt.Sprintf("  - %s\n", id))
		}
	}

	return os.WriteFile(m.path(s.ID), []byte(b.String()), 0644)
}

// EnsureSession loads or creates a session for the given ID.
// If the session doesn't exist, a new one is created with the current time.
func (m *Manager) EnsureSession(sessionID, agentID string) (*Session, error) {
	s, err := m.Load(sessionID)
	if err != nil {
		return nil, err
	}
	if s != nil {
		return s, nil
	}

	now := time.Now()
	s = &Session{
		ID:        sessionID,
		StartedAt: now,
		AgentID:   agentID,
		LastActive: now,
	}
	if err := m.Save(s); err != nil {
		return nil, err
	}
	return s, nil
}

// Touch updates last_active on a session (heartbeat).
func (m *Manager) Touch(sessionID string) error {
	s, err := m.Load(sessionID)
	if err != nil {
		return err
	}
	if s == nil {
		return nil // session doesn't exist yet — will be created on first mutating command
	}
	s.LastActive = time.Now()
	return m.Save(s)
}

// AddClaim records that a session has claimed an item.
func (m *Manager) AddClaim(sessionID, itemID string) error {
	s, err := m.Load(sessionID)
	if err != nil {
		return err
	}
	if s == nil {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	for _, id := range s.ClaimedItems {
		if id == itemID {
			return nil // already claimed
		}
	}
	s.ClaimedItems = append(s.ClaimedItems, itemID)
	s.LastActive = time.Now()
	return m.Save(s)
}

// RemoveClaim removes a claim from a session.
func (m *Manager) RemoveClaim(sessionID, itemID string) error {
	s, err := m.Load(sessionID)
	if err != nil {
		return err
	}
	if s == nil {
		return nil // session gone, nothing to remove
	}

	var filtered []string
	for _, id := range s.ClaimedItems {
		if id != itemID {
			filtered = append(filtered, id)
		}
	}
	s.ClaimedItems = filtered
	s.LastActive = time.Now()
	return m.Save(s)
}

// IsStale returns true if the session's last_active is older than the configured TTL.
func (m *Manager) IsStale(s *Session) bool {
	if m.ttl <= 0 {
		return false
	}
	return time.Since(s.LastActive) > m.ttl
}

// ListSessions returns all session files in the directory.
func (m *Manager) ListSessions() ([]*Session, error) {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var sessions []*Session
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".yaml")
		s, err := m.Load(id)
		if err != nil {
			continue
		}
		if s != nil {
			sessions = append(sessions, s)
		}
	}
	return sessions, nil
}

// StaleSessions returns sessions that are past the TTL.
func (m *Manager) StaleSessions() ([]*Session, error) {
	all, err := m.ListSessions()
	if err != nil {
		return nil, err
	}
	var stale []*Session
	for _, s := range all {
		if m.IsStale(s) {
			stale = append(stale, s)
		}
	}
	return stale, nil
}

func (m *Manager) path(sessionID string) string {
	return filepath.Join(m.dir, sessionID+".yaml")
}

func parseTime(s string) time.Time {
	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05-07:00",
		"2006-01-02T15:04:05Z",
		"2006-01-02",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
