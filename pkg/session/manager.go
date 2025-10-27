package session

import (
	"fmt"
	"log/slog"
	"os"
	"sort"
	"time"

	"github.com/google/uuid"
)

// TUISessionManager manages TUI session persistence
type TUISessionManager struct {
	projectPath   string
	currentSess   *TUISession
	jsonlWriter   *JSONLWriter
	autoSave      bool
	metadataCache map[string]*TUISessionMetadata // Cache for session metadata
}

// NewTUISessionManager creates a new TUI session manager
func NewTUISessionManager(projectPath string) (*TUISessionManager, error) {
	if projectPath == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("failed to get current directory: %w", err)
		}
		projectPath = cwd
	}

	return &TUISessionManager{
		projectPath:   projectPath,
		autoSave:      true,
		metadataCache: make(map[string]*TUISessionMetadata),
	}, nil
}

// CreateNewSession creates a new TUI session
func (m *TUISessionManager) CreateNewSession(agentName string) (*Session, error) {
	sessionID := uuid.New().String()
	gitBranch := GetGitBranch(m.projectPath)
	now := time.Now()

	// Create TUI session with metadata
	tuiSess := &TUISession{
		Metadata: TUISessionMetadata{
			Type:         "session_metadata",
			SessionID:    sessionID,
			ProjectPath:  m.projectPath,
			CreatedAt:    now,
			UpdatedAt:    now,
			GitBranch:    gitBranch,
			MessageCount: 0,
			TotalCost:    0,
			AgentName:    agentName,
		},
		Entries: []TUISessionEntry{},
	}

	// Ensure directory exists
	if err := EnsureSessionDir(m.projectPath); err != nil {
		return nil, fmt.Errorf("failed to create session directory: %w", err)
	}

	// Write initial metadata
	path := GetSessionJSONLPath(m.projectPath, sessionID)
	if err := WriteMetadata(path, &tuiSess.Metadata); err != nil {
		return nil, fmt.Errorf("failed to write metadata: %w", err)
	}

	// Set as current session
	m.currentSess = tuiSess
	m.jsonlWriter = NewJSONLWriter(path)

	slog.Debug("Created new TUI session", "session_id", sessionID, "project", m.projectPath)

	// Return as regular session for use with runtime
	return tuiSess.ToSession(), nil
}

// SaveMessage saves a message to the current session
func (m *TUISessionManager) SaveMessage(msg *Message) error {
	if m.currentSess == nil {
		return fmt.Errorf("no active session")
	}

	if !m.autoSave {
		return nil
	}

	// Create entry
	entry := TUISessionEntry{
		Type:          string(msg.Message.Role),
		SessionID:     m.currentSess.Metadata.SessionID,
		UUID:          uuid.New().String(),
		Timestamp:     time.Now(),
		CWD:           m.projectPath,
		GitBranch:     m.currentSess.Metadata.GitBranch,
		Message:       &msg.Message,
		AgentName:     msg.AgentName,
		AgentFilename: msg.AgentFilename,
	}

	// Set parent UUID (last entry)
	if len(m.currentSess.Entries) > 0 {
		entry.ParentUUID = m.currentSess.Entries[len(m.currentSess.Entries)-1].UUID
	}

	// Append to JSONL file
	path := GetSessionJSONLPath(m.projectPath, m.currentSess.Metadata.SessionID)
	if err := AppendEntry(path, &entry); err != nil {
		return fmt.Errorf("failed to append entry: %w", err)
	}

	// Update in-memory session
	m.currentSess.Entries = append(m.currentSess.Entries, entry)
	m.currentSess.Metadata.MessageCount++
	m.currentSess.Metadata.UpdatedAt = time.Now()

	// Update metadata line (batched - only update every 5 messages to reduce I/O)
	if m.currentSess.Metadata.MessageCount%5 == 0 {
		if err := WriteMetadata(path, &m.currentSess.Metadata); err != nil {
			slog.Warn("Failed to update session metadata", "error", err)
		}
	}

	return nil
}

// SaveSession saves the current session state including all messages
func (m *TUISessionManager) SaveSession(sess *Session) error {
	if m.currentSess == nil {
		return fmt.Errorf("no active session")
	}

	// Convert runtime Session to TUISession
	tuiSess := FromSession(sess, m.projectPath)

	// Preserve the session ID from current session
	tuiSess.Metadata.SessionID = m.currentSess.Metadata.SessionID
	tuiSess.Metadata.CreatedAt = m.currentSess.Metadata.CreatedAt
	tuiSess.Metadata.UpdatedAt = time.Now()

	// Update in-memory session
	m.currentSess = tuiSess

	// Write entire session to JSONL file (overwrites)
	path := GetSessionJSONLPath(m.projectPath, m.currentSess.Metadata.SessionID)
	return WriteSession(path, tuiSess)
}

// LoadSession loads a session by ID
func (m *TUISessionManager) LoadSession(sessionID string) (*Session, error) {
	path := GetSessionJSONLPath(m.projectPath, sessionID)

	// Read JSONL file
	tuiSess, err := ReadJSONL(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read session: %w", err)
	}

	// Set as current session
	m.currentSess = tuiSess
	m.jsonlWriter = NewJSONLWriter(path)

	slog.Debug("Loaded TUI session", "session_id", sessionID, "messages", len(tuiSess.Entries))

	// Return as regular session
	return tuiSess.ToSession(), nil
}

// ListSessions returns all sessions for the current project, sorted by update time (most recent first)
func (m *TUISessionManager) ListSessions() ([]*TUISessionMetadata, error) {
	sessionIDs, err := ListSessionFiles(m.projectPath)
	if err != nil {
		return nil, err
	}

	var sessions []*TUISessionMetadata
	for _, sessionID := range sessionIDs {
		// Check cache first
		if cached, ok := m.metadataCache[sessionID]; ok {
			sessions = append(sessions, cached)
			continue
		}

		// Read metadata
		path := GetSessionJSONLPath(m.projectPath, sessionID)
		metadata, err := ReadSessionMetadata(path)
		if err != nil {
			slog.Warn("Failed to read session metadata, skipping", "session_id", sessionID, "error", err)
			continue
		}

		// Cache it
		m.metadataCache[sessionID] = metadata
		sessions = append(sessions, metadata)
	}

	// Sort by UpdatedAt (most recent first)
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})

	return sessions, nil
}

// GetMostRecentSession returns the most recently updated session
func (m *TUISessionManager) GetMostRecentSession() (*Session, error) {
	sessions, err := m.ListSessions()
	if err != nil {
		return nil, err
	}

	if len(sessions) == 0 {
		return nil, fmt.Errorf("no sessions found")
	}

	// First session is most recent (sorted)
	return m.LoadSession(sessions[0].SessionID)
}

// DeleteSession deletes a session by ID
func (m *TUISessionManager) DeleteSession(sessionID string) error {
	path := GetSessionJSONLPath(m.projectPath, sessionID)

	// Remove from cache
	delete(m.metadataCache, sessionID)

	// Delete file
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete session: %w", err)
	}

	slog.Debug("Deleted TUI session", "session_id", sessionID)
	return nil
}

// UpdateSessionTitle updates the title of the current session
func (m *TUISessionManager) UpdateSessionTitle(title string) error {
	if m.currentSess == nil {
		return fmt.Errorf("no active session")
	}

	m.currentSess.Metadata.Title = title
	m.currentSess.Metadata.UpdatedAt = time.Now()

	path := GetSessionJSONLPath(m.projectPath, m.currentSess.Metadata.SessionID)
	return WriteMetadata(path, &m.currentSess.Metadata)
}

// GetCurrentSessionID returns the ID of the current session
func (m *TUISessionManager) GetCurrentSessionID() string {
	if m.currentSess == nil {
		return ""
	}
	return m.currentSess.Metadata.SessionID
}

// Close closes the session manager and any open file handles
func (m *TUISessionManager) Close() error {
	if m.jsonlWriter != nil {
		if err := m.jsonlWriter.Close(); err != nil {
			return err
		}
	}

	// Final metadata update on close
	if m.currentSess != nil {
		path := GetSessionJSONLPath(m.projectPath, m.currentSess.Metadata.SessionID)
		m.currentSess.Metadata.UpdatedAt = time.Now()
		if err := WriteMetadata(path, &m.currentSess.Metadata); err != nil {
			slog.Warn("Failed to update session metadata on close", "error", err)
		}
	}

	return nil
}

// SetAutoSave enables or disables auto-saving
func (m *TUISessionManager) SetAutoSave(enabled bool) {
	m.autoSave = enabled
}

// ExportSession exports a session to a specific path (useful for sharing)
func (m *TUISessionManager) ExportSession(sessionID, destPath string) error {
	srcPath := GetSessionJSONLPath(m.projectPath, sessionID)

	// Read session
	tuiSess, err := ReadJSONL(srcPath)
	if err != nil {
		return fmt.Errorf("failed to read session: %w", err)
	}

	// Write to destination
	return WriteSession(destPath, tuiSess)
}
