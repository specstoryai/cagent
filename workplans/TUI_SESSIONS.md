# TUI Session Persistence and Resume - Design Document

## Overview

This document outlines the design for implementing session persistence and resume functionality in the cagent TUI, modeled after Claude Code CLI's architecture.

## Current State

### cagent TUI (Current)
- Sessions exist **in-memory only** during TUI lifetime
- No persistence between runs
- Command palette (Ctrl+P) offers: New, Compact, Copy
- Lost context when exiting

### cagent API Server (Current)
- Sessions stored in SQLite (`session.db`)
- Full REST API for session management
- Sessions include: ID, title, messages, tokens, cost, timestamps

## Claude Code CLI Architecture Study

### Storage Location
```
~/.claude/
├── __store.db                    # SQLite database for all messages
├── history.jsonl                 # Global command history log
├── projects/                     # Per-project session storage
│   └── -Users-jakelevirne-dev-project/
│       ├── {uuid}.jsonl          # Session files
│       └── {uuid}.jsonl
├── session-env/                  # Session environment state
├── file-history/                 # File change tracking per session
└── settings.json                 # User preferences
```

### Key Design Patterns

#### 1. Dual Storage System
**SQLite Database (`__store.db`)**
- Structured queries (search by session_id, project, date)
- Cost/usage tracking
- Conversation summaries for quick display
- Efficient indexing

**JSONL Files (`projects/{encoded-path}/{uuid}.jsonl`)**
- Raw conversation data (one JSON object per line)
- Human-readable and portable
- Easy to backup/share
- Direct replay capability

#### 2. Session Tree Structure
```javascript
{
  uuid: "message-uuid",              // This message's ID
  parentUuid: "parent-message-uuid", // Links to parent (conversation tree)
  sessionId: "session-uuid",         // Which session this belongs to
  timestamp: "2025-10-24T17:15:39.798Z",
  type: "user" | "assistant",
  message: { /* actual message content */ }
}
```

**Benefits:**
- Non-linear conversations (branching)
- Multiple children per parent message
- Easy to find conversation leaf nodes
- Natural undo/redo through tree traversal

#### 3. Per-Project Organization
- Project paths encoded in directory names: `/Users/foo/bar` → `-Users-foo-bar`
- All sessions for a project in one directory
- Easy to list sessions for current working directory
- Git branch tracking per message

#### 4. Rich Metadata Tracking
Every message records:
- `cwd`: Working directory at time of message
- `version`: cagent version
- `gitBranch`: Git branch (if in repo)
- `timestamp`: ISO 8601 timestamp
- `cost_usd`: API cost (assistant messages)
- `duration_ms`: Response time
- `model`: Model used

### Session Data Structures

#### SQLite Schema (Simplified for cagent)

**sessions table:**
```sql
CREATE TABLE sessions (
  id TEXT PRIMARY KEY,              -- UUID
  project_path TEXT NOT NULL,       -- /Users/jakelevirne/dev/cagent
  title TEXT,                       -- AI-generated summary
  created_at TEXT NOT NULL,         -- ISO timestamp
  updated_at TEXT NOT NULL,         -- ISO timestamp
  git_branch TEXT,                  -- Current branch at creation
  total_cost REAL DEFAULT 0,        -- Total API cost
  message_count INTEGER DEFAULT 0,  -- Number of messages
  working_dir TEXT                  -- Working directory
);

CREATE INDEX idx_sessions_project ON sessions(project_path);
CREATE INDEX idx_sessions_updated ON sessions(updated_at DESC);
```

**messages table:**
```sql
CREATE TABLE messages (
  uuid TEXT PRIMARY KEY,
  session_id TEXT NOT NULL,
  parent_uuid TEXT,                 -- NULL for first message
  timestamp TEXT NOT NULL,
  role TEXT NOT NULL,               -- 'user', 'assistant', 'system', 'tool'
  content TEXT NOT NULL,            -- JSON string of message content
  model TEXT,                       -- Model used (assistant messages)
  input_tokens INTEGER,
  output_tokens INTEGER,
  cost REAL,
  duration_ms INTEGER,

  FOREIGN KEY(session_id) REFERENCES sessions(id) ON DELETE CASCADE,
  FOREIGN KEY(parent_uuid) REFERENCES messages(uuid)
);

CREATE INDEX idx_messages_session ON messages(session_id);
CREATE INDEX idx_messages_timestamp ON messages(timestamp);
```

#### JSONL File Format

Each line is a complete JSON object (newline-delimited JSON):

```jsonl
{"type":"user","sessionId":"7762b05e-...","parentUuid":null,"uuid":"78dafb20-...","timestamp":"2025-10-24T17:15:39.798Z","cwd":"/Users/jake/dev/cagent","gitBranch":"main","message":{"role":"user","content":"help me refactor this code"}}
{"type":"assistant","sessionId":"7762b05e-...","parentUuid":"78dafb20-...","uuid":"15aaa2c9-...","timestamp":"2025-10-24T17:15:43.235Z","model":"anthropic/claude-sonnet-4-0","cost":0.0023,"inputTokens":1234,"outputTokens":567,"message":{"role":"assistant","content":"I'll help you refactor..."}}
{"type":"summary","sessionId":"7762b05e-...","leafUuid":"15aaa2c9-...","summary":"Code refactoring assistance for main.go","timestamp":"2025-10-24T17:16:00.000Z"}
```

**Benefits of JSONL:**
- Append-only (just add new lines)
- Can stream parse without loading entire file
- Grep-able for debugging
- Easy to merge/split sessions
- Each line is independently valid JSON

## Proposed Implementation for cagent TUI

### Directory Structure

```
~/.cagent/
├── sessions.db                   # SQLite database (reuse existing schema mostly)
├── sessions/                     # JSONL session files
│   └── {encoded-project-path}/
│       ├── {session-uuid}.jsonl
│       └── {session-uuid}.jsonl
└── settings.json                 # TUI preferences (optional, future)
```

### Storage Strategy

#### Why Dual Storage?

1. **SQLite for queries:**
   - "Show me all sessions from last week"
   - "What sessions exist for this project?"
   - "What was the total cost across all sessions?"
   - Fast session listing and filtering

2. **JSONL for replay:**
   - Complete conversation history
   - Human-readable backup format
   - Can be version controlled
   - Easy to debug or manually edit
   - Portable between machines

### User Workflows

#### Workflow 1: Resume Most Recent Session
```bash
# Start cagent in resume mode
cagent run config.yaml --resume

# Or use alias for last session in this project
cagent run config.yaml --continue
```

**UI Flow:**
1. Check if sessions exist for current project path
2. If `--continue`: auto-resume most recent session
3. If `--resume`: show session selection dialog
4. Load session from JSONL + SQLite
5. Display conversation history in TUI
6. Allow continuation

#### Workflow 2: Session Selection Dialog

**Trigger:** `Ctrl+P` → "Resume Session" or `--resume` flag

**Dialog UI (using existing dialog system):**
```
┌─ Resume Session ──────────────────────────────────────┐
│ Type to search sessions...                            │
│ ────────────────────────────────────────────────────  │
│                                                        │
│ > [Today, 5:23 PM] Updated TUI placeholder text       │
│   3 messages • $0.002 • main branch                   │
│                                                        │
│   [Today, 2:15 PM] Fixed bug in session handling      │
│   12 messages • $0.015 • feature/sessions             │
│                                                        │
│   [Yesterday] Added new provider support              │
│   25 messages • $0.034 • main branch                  │
│                                                        │
│   [Oct 23] Refactored runtime module                  │
│   45 messages • $0.089 • refactor/runtime             │
│                                                        │
│ ↑↓: Navigate  Enter: Resume  Esc: Cancel              │
└────────────────────────────────────────────────────────┘
```

**Features:**
- Fuzzy search by summary text
- Group by project (if multiple projects in history)
- Show metadata: time, message count, cost, branch
- Sort by most recent first
- Keyboard navigation

#### Workflow 3: Auto-save During Session

**Behavior:**
- Every message automatically persisted to both SQLite and JSONL
- No explicit "save" action needed
- Write-ahead logging in SQLite prevents data loss
- Append-only JSONL writes are atomic

**Implementation Points:**
1. After user sends message → append to JSONL, insert into DB
2. After assistant responds → append to JSONL, insert into DB, update session cost/tokens
3. On graceful exit → update session `updated_at` timestamp
4. On crash → session remains valid with all completed messages

### Command Palette Integration

Add to existing command palette (`pkg/tui/tui.go:292`):

```go
{
    Name: "Session",
    Commands: []dialog.Command{
        {
            ID:          "session.new",
            Label:       "New Session",
            Description: "Start a new conversation",
            Execute: func() tea.Cmd { /* existing */ },
        },
        {
            ID:          "session.resume",
            Label:       "Resume Session",
            Description: "Continue a previous conversation",
            Execute: func() tea.Cmd {
                // Open session selection dialog
                return openSessionSelectorDialog()
            },
        },
        {
            ID:          "session.save",
            Label:       "Save Session",
            Description: "Explicitly save current session",
            Execute: func() tea.Cmd {
                // Force flush to disk (usually auto-saved)
                return saveCurrentSession()
            },
        },
        {
            ID:          "session.rename",
            Label:       "Rename Session",
            Description: "Edit session title",
            Execute: func() tea.Cmd {
                return openRenameSessionDialog()
            },
        },
        {
            ID:          "session.delete",
            Label:       "Delete Session",
            Description: "Remove this session permanently",
            Execute: func() tea.Cmd {
                return confirmDeleteSession()
            },
        },
        // ... existing compact, copy commands
    },
}
```

### Session Lifecycle

#### Creating a New Session

```go
// pkg/session/manager.go (new file)
func CreateSession(projectPath string, agentName string) (*Session, error) {
    sessionID := uuid.New().String()
    gitBranch := getGitBranch(projectPath) // helper function

    sess := &Session{
        ID:          sessionID,
        ProjectPath: projectPath,
        CreatedAt:   time.Now(),
        UpdatedAt:   time.Now(),
        GitBranch:   gitBranch,
        WorkingDir:  projectPath,
        Messages:    []Item{},
    }

    // 1. Create JSONL file
    jsonlPath := getSessionJSONLPath(projectPath, sessionID)
    if err := ensureDirectoryExists(filepath.Dir(jsonlPath)); err != nil {
        return nil, err
    }

    // 2. Insert into SQLite
    if err := sessionStore.AddSession(ctx, sess); err != nil {
        return nil, err
    }

    return sess, nil
}
```

#### Appending Messages

```go
func (s *Session) AppendMessage(msg *Message) error {
    // 1. Add to in-memory session
    s.Messages = append(s.Messages, NewMessageItem(msg))

    // 2. Append to JSONL file (atomic write)
    jsonlPath := getSessionJSONLPath(s.ProjectPath, s.ID)
    entry := SessionEntry{
        Type:       msg.Role,
        SessionID:  s.ID,
        ParentUUID: s.getLastMessageUUID(),
        UUID:       msg.UUID,
        Timestamp:  msg.CreatedAt,
        CWD:        s.WorkingDir,
        GitBranch:  s.GitBranch,
        Message:    msg,
    }

    if err := appendJSONL(jsonlPath, entry); err != nil {
        return fmt.Errorf("failed to append to JSONL: %w", err)
    }

    // 3. Insert into SQLite
    if err := sessionStore.AddMessage(ctx, s.ID, msg); err != nil {
        return fmt.Errorf("failed to insert into DB: %w", err)
    }

    // 4. Update session metadata
    s.UpdatedAt = time.Now()
    s.MessageCount++
    if msg.Cost > 0 {
        s.TotalCost += msg.Cost
    }

    return sessionStore.UpdateSession(ctx, s)
}
```

#### Loading a Session

```go
func LoadSession(sessionID string) (*Session, error) {
    // 1. Load metadata from SQLite
    sess, err := sessionStore.GetSession(ctx, sessionID)
    if err != nil {
        return nil, err
    }

    // 2. Load full conversation from JSONL
    jsonlPath := getSessionJSONLPath(sess.ProjectPath, sessionID)
    entries, err := readJSONL(jsonlPath)
    if err != nil {
        return nil, err
    }

    // 3. Reconstruct message tree
    messages := reconstructMessageTree(entries)
    sess.Messages = messages

    return sess, nil
}
```

#### Generating Session Summary

```go
// Triggered after N messages or on session close
func (s *Session) GenerateSummary(model provider.Provider) error {
    // Use existing summarization logic from pkg/runtime/runtime.go:1011
    // Similar to generateSessionTitle but for summary

    summary := s.generateSummaryViaLLM(model)
    s.Title = summary

    // Update both storages
    sessionStore.UpdateSession(ctx, s)

    // Append summary entry to JSONL
    summaryEntry := SessionEntry{
        Type:      "summary",
        SessionID: s.ID,
        LeafUUID:  s.getLastMessageUUID(),
        Summary:   summary,
        Timestamp: time.Now(),
    }
    appendJSONL(getSessionJSONLPath(s.ProjectPath, s.ID), summaryEntry)

    return nil
}
```

### File Path Helpers

```go
// pkg/session/paths.go (new file)
func getBaseSessionDir() string {
    home, _ := os.UserHomeDir()
    return filepath.Join(home, ".cagent")
}

func getSessionsDBPath() string {
    return filepath.Join(getBaseSessionDir(), "sessions.db")
}

func encodeProjectPath(projectPath string) string {
    // /Users/jake/dev/cagent → -Users-jake-dev-cagent
    return "-" + strings.ReplaceAll(strings.TrimPrefix(projectPath, "/"), "/", "-")
}

func getSessionJSONLPath(projectPath, sessionID string) string {
    encoded := encodeProjectPath(projectPath)
    return filepath.Join(
        getBaseSessionDir(),
        "sessions",
        encoded,
        sessionID+".jsonl",
    )
}

func getProjectSessions(projectPath string) ([]string, error) {
    encoded := encodeProjectPath(projectPath)
    dir := filepath.Join(getBaseSessionDir(), "sessions", encoded)

    entries, err := os.ReadDir(dir)
    if err != nil {
        if os.IsNotExist(err) {
            return []string{}, nil
        }
        return nil, err
    }

    var sessionIDs []string
    for _, entry := range entries {
        if filepath.Ext(entry.Name()) == ".jsonl" {
            sessionIDs = append(sessionIDs, strings.TrimSuffix(entry.Name(), ".jsonl"))
        }
    }
    return sessionIDs, nil
}
```

### TUI Integration Points

#### 1. App Initialization (`pkg/tui/tui.go`)

```go
// New function
func New(a *app.App, opts ...TUIOption) tea.Model {
    cfg := &tuiConfig{
        enablePersistence: true,  // default on
        sessionDBPath:     "",    // use default
    }

    for _, opt := range opts {
        opt(cfg)
    }

    var sessionMgr *session.Manager
    if cfg.enablePersistence {
        sm, err := session.NewManager(cfg.sessionDBPath)
        if err != nil {
            slog.Warn("Failed to initialize session manager, persistence disabled", "error", err)
        } else {
            sessionMgr = sm
        }
    }

    t := &appModel{
        chatPage:       chatpage.New(a),
        keyMap:         DefaultKeyMap(),
        dialog:         dialog.New(),
        application:    a,
        sessionManager: sessionMgr,
    }

    t.statusBar = statusbar.New(t)
    return t
}
```

#### 2. Session Manager (`pkg/session/manager.go` - new)

```go
type Manager struct {
    store        Store
    currentSess  *Session
    projectPath  string
    autoSave     bool
    jsonlWriter  *JSONLWriter
}

func NewManager(dbPath string) (*Manager, error) {
    if dbPath == "" {
        dbPath = getSessionsDBPath()
    }

    store, err := NewSQLiteSessionStore(dbPath)
    if err != nil {
        return nil, err
    }

    cwd, _ := os.Getwd()

    return &Manager{
        store:       store,
        projectPath: cwd,
        autoSave:    true,
    }, nil
}

func (m *Manager) CreateNewSession(agentName string) (*Session, error) {
    sess, err := CreateSession(m.projectPath, agentName)
    if err != nil {
        return nil, err
    }

    m.currentSess = sess
    m.jsonlWriter = NewJSONLWriter(getSessionJSONLPath(m.projectPath, sess.ID))
    return sess, nil
}

func (m *Manager) ResumeSession(sessionID string) (*Session, error) {
    sess, err := LoadSession(sessionID)
    if err != nil {
        return nil, err
    }

    m.currentSess = sess
    m.jsonlWriter = NewJSONLWriter(getSessionJSONLPath(sess.ProjectPath, sess.ID))
    return sess, nil
}

func (m *Manager) AppendMessage(msg *Message) error {
    if m.currentSess == nil {
        return fmt.Errorf("no active session")
    }
    return m.currentSess.AppendMessage(msg)
}

func (m *Manager) ListSessions() ([]*SessionMetadata, error) {
    // Query SQLite for sessions in current project
    return m.store.GetSessionsByProject(ctx, m.projectPath)
}
```

#### 3. JSONL Writer (`pkg/session/jsonl.go` - new)

```go
type JSONLWriter struct {
    file   *os.File
    mu     sync.Mutex
    path   string
}

func NewJSONLWriter(path string) *JSONLWriter {
    return &JSONLWriter{path: path}
}

func (w *JSONLWriter) Open() error {
    w.mu.Lock()
    defer w.mu.Unlock()

    if err := os.MkdirAll(filepath.Dir(w.path), 0755); err != nil {
        return err
    }

    f, err := os.OpenFile(w.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
    if err != nil {
        return err
    }

    w.file = f
    return nil
}

func (w *JSONLWriter) Append(entry interface{}) error {
    w.mu.Lock()
    defer w.mu.Unlock()

    if w.file == nil {
        if err := w.Open(); err != nil {
            return err
        }
    }

    data, err := json.Marshal(entry)
    if err != nil {
        return err
    }

    data = append(data, '\n')
    _, err = w.file.Write(data)
    if err != nil {
        return err
    }

    // Flush to disk immediately for durability
    return w.file.Sync()
}

func (w *JSONLWriter) Close() error {
    w.mu.Lock()
    defer w.mu.Unlock()

    if w.file != nil {
        return w.file.Close()
    }
    return nil
}

func ReadJSONL(path string) ([]SessionEntry, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, err
    }

    lines := strings.Split(string(data), "\n")
    entries := make([]SessionEntry, 0, len(lines))

    for _, line := range lines {
        line = strings.TrimSpace(line)
        if line == "" {
            continue
        }

        var entry SessionEntry
        if err := json.Unmarshal([]byte(line), &entry); err != nil {
            slog.Warn("Failed to parse JSONL line", "error", err, "line", line)
            continue
        }

        entries = append(entries, entry)
    }

    return entries, nil
}
```

### CLI Flags

Add new flags to `cmd/root/run.go`:

```go
var (
    resumeSession   string  // --resume=<session-id>
    continueSession bool    // --continue (resume most recent)
    noSession       bool    // --no-session (disable persistence)
)

cmd.PersistentFlags().StringVar(&resumeSession, "resume", "", "Resume a specific session by ID")
cmd.PersistentFlags().BoolVar(&continueSession, "continue", false, "Continue most recent session")
cmd.PersistentFlags().BoolVar(&noSession, "no-session", false, "Disable session persistence")
```

### Session Selection Dialog

New dialog component (`pkg/tui/dialog/session_selector.go`):

```go
type sessionSelectorDialog struct {
    width, height int
    sessions      []*SessionMetadata
    filtered      []*SessionMetadata
    selected      int
    textInput     textinput.Model
    keyMap        sessionSelectorKeyMap
}

type SessionMetadata struct {
    ID           string
    Title        string
    ProjectPath  string
    UpdatedAt    time.Time
    MessageCount int
    TotalCost    float64
    GitBranch    string
}

func NewSessionSelectorDialog(sessions []*SessionMetadata) Dialog {
    ti := textinput.New()
    ti.Placeholder = "Search sessions..."
    ti.Focus()

    return &sessionSelectorDialog{
        sessions:  sessions,
        filtered:  sessions,
        selected:  0,
        textInput: ti,
        keyMap:    defaultSessionSelectorKeyMap(),
    }
}

func (d *sessionSelectorDialog) View() string {
    // Render list of sessions with metadata
    // Similar to command palette but with richer display

    var b strings.Builder
    b.WriteString(styles.DialogTitle.Render("Resume Session"))
    b.WriteString("\n\n")
    b.WriteString(d.textInput.View())
    b.WriteString("\n\n")

    for i, sess := range d.filtered {
        style := styles.SessionEntry
        if i == d.selected {
            style = styles.SessionEntrySelected
        }

        // Format: "[Today, 5:23 PM] Updated TUI placeholder text"
        timeStr := formatRelativeTime(sess.UpdatedAt)
        line1 := fmt.Sprintf("[%s] %s", timeStr, sess.Title)

        // Format: "3 messages • $0.002 • main branch"
        line2 := fmt.Sprintf("%d messages • $%.4f • %s",
            sess.MessageCount, sess.TotalCost, sess.GitBranch)

        b.WriteString(style.Render(line1))
        b.WriteString("\n")
        b.WriteString(styles.SessionMetadata.Render(line2))
        b.WriteString("\n\n")
    }

    return styles.DialogBox.Render(b.String())
}
```

## Migration Strategy

### Phase 1: Core Infrastructure (Week 1)
- [ ] Create `pkg/session/manager.go`
- [ ] Create `pkg/session/jsonl.go` with JSONL read/write
- [ ] Create `pkg/session/paths.go` with path helpers
- [ ] Extend SQLite schema with new fields (git_branch, project_path)
- [ ] Add CLI flags: `--resume`, `--continue`, `--no-session`

### Phase 2: Session Persistence (Week 2)
- [ ] Integrate session manager into TUI initialization
- [ ] Auto-save messages to JSONL on send
- [ ] Auto-save to SQLite on send
- [ ] Test crash recovery and data integrity

### Phase 3: Session Loading (Week 3)
- [ ] Implement session listing queries
- [ ] Create session selector dialog UI
- [ ] Implement session resume logic
- [ ] Test loading sessions with various message counts

### Phase 4: Polish and Features (Week 4)
- [ ] Add session rename functionality
- [ ] Add session delete with confirmation
- [ ] Implement fuzzy search in session selector
- [ ] Add session export/import
- [ ] Write documentation

## Testing Strategy

### Unit Tests
```go
// pkg/session/jsonl_test.go
func TestJSONLRoundTrip(t *testing.T) {
    // Write entries, read back, verify integrity
}

func TestJSONLConcurrentWrites(t *testing.T) {
    // Ensure thread-safe appends
}

// pkg/session/manager_test.go
func TestSessionCreation(t *testing.T) {
    // Create session, verify DB + JSONL
}

func TestSessionResume(t *testing.T) {
    // Load session, verify message tree reconstruction
}
```

### Integration Tests
```go
// Test end-to-end workflow
func TestTUISessionPersistence(t *testing.T) {
    // 1. Start TUI
    // 2. Send messages
    // 3. Exit TUI
    // 4. Verify JSONL and SQLite have data
    // 5. Resume TUI with --continue
    // 6. Verify messages loaded correctly
}
```

### Manual Testing Checklist
- [ ] Create session, send messages, verify both storages updated
- [ ] Kill TUI process, verify data persisted
- [ ] Resume session, verify all messages present
- [ ] Test with multiple projects
- [ ] Test with empty session history
- [ ] Test session search/filter
- [ ] Test session deletion
- [ ] Test with SQLite locked (concurrent access)

## Benefits

1. **User Experience**
   - No lost context on crashes or exits
   - Resume conversations naturally
   - Search past conversations
   - See cost/token usage over time

2. **Development**
   - Debugging: can replay sessions
   - Testing: can save and load test scenarios
   - Analysis: query patterns in SQLite

3. **Portability**
   - JSONL files are human-readable
   - Can be version controlled
   - Easy to backup/restore
   - Share sessions with team members

## Risks and Mitigations

### Risk: Database Lock Contention
**Mitigation:** Use WAL mode in SQLite (already configured), single connection pool

### Risk: Large JSONL Files
**Mitigation:** Monitor file sizes, implement session splitting after N messages

### Risk: Corrupted JSONL
**Mitigation:** Each line is independent JSON, can skip bad lines, SQLite is source of truth

### Risk: Disk Space Usage
**Mitigation:** Add cleanup command for old sessions, compress archived sessions

## Future Enhancements

1. **Session Sharing**
   - Export session as shareable file
   - Import session from file
   - Share via registry (like agents)

2. **Session Analytics**
   - Cost dashboard
   - Token usage trends
   - Most used tools/agents

3. **Session Templates**
   - Save session as template
   - Start new sessions from templates
   - Common workflows as templates

4. **Cloud Sync**
   - Sync sessions across machines
   - Web UI to browse sessions
   - Mobile companion app

## References

- Claude Code CLI architecture study (above)
- Existing cagent session code: `pkg/session/`
- SQLite session store: `pkg/session/store.go`
- TUI dialog system: `pkg/tui/dialog/`
- Runtime session management: `pkg/runtime/runtime.go`

## Appendix: Example JSONL Session

```jsonl
{"type":"user","sessionId":"a1b2c3d4-...","parentUuid":null,"uuid":"msg-001","timestamp":"2025-10-24T10:00:00Z","cwd":"/Users/jake/dev/cagent","gitBranch":"main","message":{"role":"user","content":"Help me add session persistence"}}
{"type":"assistant","sessionId":"a1b2c3d4-...","parentUuid":"msg-001","uuid":"msg-002","timestamp":"2025-10-24T10:00:05Z","model":"anthropic/claude-sonnet-4-0","cost":0.0034,"inputTokens":1523,"outputTokens":892,"duration":5231,"message":{"role":"assistant","content":"I'll help you add session persistence..."}}
{"type":"tool_call","sessionId":"a1b2c3d4-...","parentUuid":"msg-002","uuid":"msg-003","timestamp":"2025-10-24T10:00:10Z","toolName":"read_file","toolArgs":"{\"path\":\"pkg/session/session.go\"}"}
{"type":"tool_result","sessionId":"a1b2c3d4-...","parentUuid":"msg-003","uuid":"msg-004","timestamp":"2025-10-24T10:00:11Z","toolCallId":"msg-003","result":"... file contents ..."}
{"type":"summary","sessionId":"a1b2c3d4-...","leafUuid":"msg-004","summary":"Added session persistence design based on Claude Code architecture","timestamp":"2025-10-24T10:05:00Z"}
```

Each line is a complete, valid JSON object that can be parsed independently.
