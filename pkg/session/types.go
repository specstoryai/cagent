package session

import (
	"time"

	"github.com/google/uuid"

	"github.com/docker/cagent/pkg/chat"
)

// TUISessionMetadata represents the metadata for a TUI session stored as the first line of JSONL
type TUISessionMetadata struct {
	Type         string    `json:"type"` // Always "session_metadata"
	SessionID    string    `json:"sessionId"`
	ProjectPath  string    `json:"projectPath"`
	Title        string    `json:"title,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
	GitBranch    string    `json:"gitBranch,omitempty"`
	MessageCount int       `json:"messageCount"`
	TotalCost    float64   `json:"totalCost"`
	AgentName    string    `json:"agentName,omitempty"`
}

// TUISessionEntry represents a message entry in the JSONL file
type TUISessionEntry struct {
	Type       string       `json:"type"` // "user", "assistant", "tool_call", "tool_result"
	SessionID  string       `json:"sessionId"`
	ParentUUID string       `json:"parentUuid,omitempty"`
	UUID       string       `json:"uuid"`
	Timestamp  time.Time    `json:"timestamp"`
	CWD        string       `json:"cwd,omitempty"`
	GitBranch  string       `json:"gitBranch,omitempty"`
	Message    *chat.Message `json:"message,omitempty"`

	// For assistant messages
	Model        string  `json:"model,omitempty"`
	Cost         float64 `json:"cost,omitempty"`
	InputTokens  int     `json:"inputTokens,omitempty"`
	OutputTokens int     `json:"outputTokens,omitempty"`
	Duration     int64   `json:"duration,omitempty"` // milliseconds

	// Agent info
	AgentName     string `json:"agentName,omitempty"`
	AgentFilename string `json:"agentFilename,omitempty"`
}

// TUISession represents a TUI session with all its metadata and messages
type TUISession struct {
	Metadata TUISessionMetadata
	Entries  []TUISessionEntry
}

// ToSession converts a TUISession to a regular Session for use with the runtime
func (ts *TUISession) ToSession() *Session {
	sess := &Session{
		ID:           ts.Metadata.SessionID,
		Title:        ts.Metadata.Title,
		CreatedAt:    ts.Metadata.CreatedAt,
		WorkingDir:   ts.Metadata.ProjectPath,
		Messages:     make([]Item, 0, len(ts.Entries)),
		InputTokens:  0,
		OutputTokens: 0,
		Cost:         ts.Metadata.TotalCost,
	}

	// Convert entries to messages
	for _, entry := range ts.Entries {
		if entry.Message != nil {
			msg := &Message{
				AgentFilename: entry.AgentFilename,
				AgentName:     entry.AgentName,
				Message:       *entry.Message,
			}
			sess.Messages = append(sess.Messages, NewMessageItem(msg))

			// Accumulate tokens
			sess.InputTokens += entry.InputTokens
			sess.OutputTokens += entry.OutputTokens
		}
	}

	return sess
}

// FromSession creates a TUISession from a regular Session
func FromSession(sess *Session, projectPath string) *TUISession {
	gitBranch := GetGitBranch(projectPath)

	tuiSess := &TUISession{
		Metadata: TUISessionMetadata{
			Type:         "session_metadata",
			SessionID:    sess.ID,
			ProjectPath:  projectPath,
			Title:        sess.Title,
			CreatedAt:    sess.CreatedAt,
			UpdatedAt:    time.Now(),
			GitBranch:    gitBranch,
			MessageCount: len(sess.Messages),
			TotalCost:    sess.Cost,
		},
		Entries: make([]TUISessionEntry, 0, len(sess.Messages)),
	}

	// Keep track of UUIDs for parent linking
	var lastUUID string

	// Convert messages to entries
	for _, item := range sess.Messages {
		if item.IsMessage() {
			msg := item.Message
			msgUUID := uuid.New().String()

			// Parse timestamp from message
			var msgTimestamp time.Time
			if msg.Message.CreatedAt != "" {
				parsed, err := time.Parse(time.RFC3339, msg.Message.CreatedAt)
				if err == nil {
					msgTimestamp = parsed
				} else {
					msgTimestamp = time.Now()
				}
			} else {
				msgTimestamp = time.Now()
			}

			entry := TUISessionEntry{
				Type:          string(msg.Message.Role),
				SessionID:     sess.ID,
				UUID:          msgUUID,
				Timestamp:     msgTimestamp,
				CWD:           sess.WorkingDir,
				GitBranch:     gitBranch,
				Message:       &msg.Message,
				AgentName:     msg.AgentName,
				AgentFilename: msg.AgentFilename,
			}

			// Set parent UUID (link to previous message)
			if lastUUID != "" {
				entry.ParentUUID = lastUUID
			}

			tuiSess.Entries = append(tuiSess.Entries, entry)
			lastUUID = msgUUID
		}
	}

	return tuiSess
}
