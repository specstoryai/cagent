# TUI Session Persistence and Resume - Design Document (JSONL-Only)

## Overview

This document outlines the design for implementing session persistence and resume functionality in the cagent TUI using **JSONL files only** - no SQLite database.

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
- **Note:** TUI will NOT use the API server's SQLite database

## Design Philosophy

**Simplicity First:**
- Single storage format (JSONL)
- File-system based discovery
- No database overhead
- Human-readable and portable
- Easy to backup/share/version control

### Storage Location
```
~/.cagent/
└── sessions/                     # JSONL session files
    └── {encoded-project-path}/
        ├── {uuid}.jsonl          # Session files
        └── {uuid}.jsonl
```

### Key Design Pattern: JSONL-Only Storage

**JSONL Files (`sessions/{encoded-path}/{uuid}.jsonl`)**
- Raw conversation data (one JSON object per line)
- First line contains session metadata
- Subsequent lines are messages
- Human-readable and portable
- Easy to backup/share
- Direct replay capability
- No database required

## Session Data Structure

### JSONL File Format

Each line is a complete JSON object (newline-delimited JSON):

**Line 1: Session Metadata**
```json
{"type":"session_metadata","sessionId":"7762b05e-...","projectPath":"/Users/jake/dev/cagent","title":"Code refactoring assistance","createdAt":"2025-10-24T17:15:39.798Z","updatedAt":"2025-10-24T17:16:00.000Z","gitBranch":"main","messageCount":2,"totalCost":0.0023}
```

**Subsequent Lines: Messages**
```jsonl
{"type":"user","sessionId":"7762b05e-...","parentUuid":null,"uuid":"78dafb20-...","timestamp":"2025-10-24T17:15:39.798Z","cwd":"/Users/jake/dev/cagent","gitBranch":"main","message":{"role":"user","content":"help me refactor this code"}}
{"type":"assistant","sessionId":"7762b05e-...","parentUuid":"78dafb20-...","uuid":"15aaa2c9-...","timestamp":"2025-10-24T17:15:43.235Z","model":"anthropic/claude-sonnet-4-0","cost":0.0023,"inputTokens":1234,"outputTokens":567,"message":{"role":"assistant","content":"I'll help you refactor..."}}
```

**Benefits of JSONL:**
- Append-only (just add new lines)
- First line = metadata (updated on changes)
- Can stream parse without loading entire file
- Grep-able for debugging
- Easy to merge/split sessions
- Each line is independently valid JSON
- No database required

### Directory Structure

```
~/.cagent/
└── sessions/                     # JSONL session files only
    └── {encoded-project-path}/
        ├── {session-uuid}.jsonl
        └── {session-uuid}.jsonl
```

### Session Discovery

**How to find sessions:**
1. List `.jsonl` files in `~/.cagent/sessions/{encoded-project-path}/`
2. Read first line of each file (session metadata)
3. Sort by `updatedAt` timestamp
4. Display in session selector

**Fast session listing:**
- Only read first line of each file for metadata
- Full message history loaded only when resuming
- File modification time as fallback for sorting

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
2. If `--continue`: auto-resume most recent session (by file mtime or metadata)
3. If `--resume`: show session selection dialog
4. Load session from JSONL (read all lines)
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
- Every message automatically persisted to JSONL
- No explicit "save" action needed
- Append-only JSONL writes are atomic

**Implementation Points:**
1. After user sends message → append to JSONL
2. After assistant responds → append to JSONL, update metadata line (rewrite first line)
3. On graceful exit → update session metadata line with final `updatedAt` timestamp
4. On crash → session remains valid with all completed messages (metadata may be stale)

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
    now := time.Now()

    sess := &Session{
        ID:          sessionID,
        ProjectPath: projectPath,
        CreatedAt:   now,
        UpdatedAt:   now,
        GitBranch:   gitBranch,
        WorkingDir:  projectPath,
        Messages:    []Item{},
    }

    // 1. Create JSONL file
    jsonlPath := getSessionJSONLPath(projectPath, sessionID)
    if err := ensureDirectoryExists(filepath.Dir(jsonlPath)); err != nil {
        return nil, err
    }

    // 2. Write initial metadata line
    if err := sess.WriteMetadata(); err != nil {
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

    // 2. Append message to JSONL file (atomic write)
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

    // 3. Update session metadata
    s.UpdatedAt = time.Now()
    s.MessageCount++
    if msg.Cost > 0 {
        s.TotalCost += msg.Cost
    }

    // 4. Rewrite first line with updated metadata
    return s.WriteMetadata()
}
```

#### Loading a Session

```go
func LoadSession(sessionID, projectPath string) (*Session, error) {
    // 1. Read JSONL file
    jsonlPath := getSessionJSONLPath(projectPath, sessionID)
    entries, err := readJSONL(jsonlPath)
    if err != nil {
        return nil, err
    }

    if len(entries) == 0 {
        return nil, fmt.Errorf("empty session file")
    }

    // 2. First line is metadata
    metadata := entries[0]
    sess := &Session{
        ID:          metadata.SessionID,
        ProjectPath: metadata.ProjectPath,
        Title:       metadata.Title,
        CreatedAt:   metadata.CreatedAt,
        UpdatedAt:   metadata.UpdatedAt,
        GitBranch:   metadata.GitBranch,
        MessageCount: metadata.MessageCount,
        TotalCost:   metadata.TotalCost,
    }

    // 3. Remaining lines are messages
    messages := reconstructMessages(entries[1:])
    sess.Messages = messages

    return sess, nil
}
```

#### Generating Session Summary

```go
// Triggered after N messages or on session close
func (s *Session) GenerateSummary(model provider.Provider) error {
    // Use existing summarization logic from pkg/runtime/runtime.go:1011
    summary := s.generateSummaryViaLLM(model)
    s.Title = summary

    // Update metadata line
    return s.WriteMetadata()
}
```

#### Writing Metadata (Rewriting First Line)

```go
func (s *Session) WriteMetadata() error {
    jsonlPath := getSessionJSONLPath(s.ProjectPath, s.ID)

    metadata := SessionMetadata{
        Type:         "session_metadata",
        SessionID:    s.ID,
        ProjectPath:  s.ProjectPath,
        Title:        s.Title,
        CreatedAt:    s.CreatedAt,
        UpdatedAt:    s.UpdatedAt,
        GitBranch:    s.GitBranch,
        MessageCount: s.MessageCount,
        TotalCost:    s.TotalCost,
    }

    // Read all lines
    lines, err := readAllLines(jsonlPath)
    if err != nil && !os.IsNotExist(err) {
        return err
    }

    // Marshal new metadata
    metadataJSON, err := json.Marshal(metadata)
    if err != nil {
        return err
    }

    // Replace first line or create new file
    if len(lines) > 0 {
        lines[0] = string(metadataJSON)
    } else {
        lines = []string{string(metadataJSON)}
    }

    // Write back atomically
    return atomicWriteLines(jsonlPath, lines)
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
        projectPath:       "",    // use CWD by default
    }

    for _, opt := range opts {
        opt(cfg)
    }

    var sessionMgr *session.Manager
    if cfg.enablePersistence {
        sm, err := session.NewManager(cfg.projectPath)
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
    currentSess  *Session
    projectPath  string
    autoSave     bool
    jsonlWriter  *JSONLWriter
}

func NewManager(projectPath string) (*Manager, error) {
    if projectPath == "" {
        cwd, _ := os.Getwd()
        projectPath = cwd
    }

    return &Manager{
        projectPath: projectPath,
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
    sess, err := LoadSession(sessionID, m.projectPath)
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
    // List JSONL files in project directory
    encoded := encodeProjectPath(m.projectPath)
    dir := filepath.Join(getBaseSessionDir(), "sessions", encoded)

    entries, err := os.ReadDir(dir)
    if err != nil {
        if os.IsNotExist(err) {
            return []*SessionMetadata{}, nil
        }
        return nil, err
    }

    var sessions []*SessionMetadata
    for _, entry := range entries {
        if filepath.Ext(entry.Name()) != ".jsonl" {
            continue
        }

        // Read first line (metadata) only
        sessionID := strings.TrimSuffix(entry.Name(), ".jsonl")
        metadata, err := readSessionMetadata(m.projectPath, sessionID)
        if err != nil {
            slog.Warn("Failed to read session metadata", "sessionID", sessionID, "error", err)
            continue
        }

        sessions = append(sessions, metadata)
    }

    // Sort by UpdatedAt descending
    sort.Slice(sessions, func(i, j int) bool {
        return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
    })

    return sessions, nil
}

func readSessionMetadata(projectPath, sessionID string) (*SessionMetadata, error) {
    jsonlPath := getSessionJSONLPath(projectPath, sessionID)

    // Read only first line
    file, err := os.Open(jsonlPath)
    if err != nil {
        return nil, err
    }
    defer file.Close()

    scanner := bufio.NewScanner(file)
    if !scanner.Scan() {
        return nil, fmt.Errorf("empty file")
    }

    var metadata SessionMetadata
    if err := json.Unmarshal(scanner.Bytes(), &metadata); err != nil {
        return nil, err
    }

    return &metadata, nil
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
- [ ] Create `pkg/session/types.go` for session data structures
- [ ] Add CLI flags: `--resume`, `--continue`, `--no-session`

### Phase 2: Session Persistence (Week 2)
- [ ] Integrate session manager into TUI initialization
- [ ] Auto-save messages to JSONL on send
- [ ] Implement metadata line rewriting
- [ ] Test crash recovery and data integrity

### Phase 3: Session Loading (Week 3)
- [ ] Implement session file discovery (filesystem-based)
- [ ] Create session selector dialog UI
- [ ] Implement session resume logic
- [ ] Test loading sessions with various message counts

### Phase 4: Polish and Features (Week 4)
- [ ] Add session rename functionality
- [ ] Add session delete with confirmation
- [ ] Implement fuzzy search in session selector
- [ ] Add session export (copy JSONL file)
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

func TestMetadataRewrite(t *testing.T) {
    // Test updating first line without corrupting subsequent lines
}

// pkg/session/manager_test.go
func TestSessionCreation(t *testing.T) {
    // Create session, verify JSONL file exists with metadata
}

func TestSessionResume(t *testing.T) {
    // Load session, verify message reconstruction
}

func TestSessionListing(t *testing.T) {
    // Create multiple sessions, verify listing and sorting
}
```

### Integration Tests
```go
// Test end-to-end workflow
func TestTUISessionPersistence(t *testing.T) {
    // 1. Start TUI
    // 2. Send messages
    // 3. Exit TUI
    // 4. Verify JSONL file has all data
    // 5. Resume TUI with --continue
    // 6. Verify messages loaded correctly
}
```

### Manual Testing Checklist
- [ ] Create session, send messages, verify JSONL updated
- [ ] Kill TUI process, verify data persisted
- [ ] Resume session, verify all messages present
- [ ] Test with multiple projects
- [ ] Test with empty session history
- [ ] Test session search/filter
- [ ] Test session deletion
- [ ] Test metadata updates don't corrupt message lines

## Benefits

1. **User Experience**
   - No lost context on crashes or exits
   - Resume conversations naturally
   - Search past conversations by reading files
   - See cost/token usage in session metadata

2. **Simplicity**
   - No database setup or management
   - Single file format (JSONL)
   - Human-readable session files
   - No schema migrations

3. **Development**
   - Debugging: can replay sessions easily
   - Testing: can save and load test scenarios
   - Analysis: grep/jq for pattern searching
   - Easy to inspect session files manually

4. **Portability**
   - JSONL files are human-readable
   - Can be version controlled
   - Easy to backup/restore (just copy files)
   - Share sessions with team members (send .jsonl file)
   - No database dependencies

## Risks and Mitigations

### Risk: Large JSONL Files
**Mitigation:** Monitor file sizes, implement session splitting after N messages, lazy loading

### Risk: Slow Session Listing with Many Sessions
**Mitigation:** Only read first line (metadata) of each file, cache file mtimes, limit displayed sessions

### Risk: Corrupted JSONL
**Mitigation:** Each line is independent JSON, can skip bad lines, metadata can be rebuilt from messages

### Risk: Disk Space Usage
**Mitigation:** Add cleanup command for old sessions, compress archived sessions

### Risk: Metadata Line Rewrite Performance
**Mitigation:** Batch metadata updates, only rewrite on significant changes (every N messages), use atomic writes

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

- Claude Code CLI architecture study (JSONL storage pattern)
- Existing cagent session code: `pkg/session/` (API server, not used by TUI)
- TUI dialog system: `pkg/tui/dialog/`
- Runtime session management: `pkg/runtime/runtime.go`
- JSONL format: http://jsonlines.org/

## Appendix: Example JSONL Session

Example of a complete session file (`~/.cagent/sessions/-Users-jake-dev-cagent/a1b2c3d4-e5f6-7890-abcd-ef1234567890.jsonl`):

```jsonl
{"type":"session_metadata","sessionId":"a1b2c3d4-e5f6-7890-abcd-ef1234567890","projectPath":"/Users/jake/dev/cagent","title":"Added session persistence design","createdAt":"2025-10-24T10:00:00Z","updatedAt":"2025-10-24T10:05:15Z","gitBranch":"main","messageCount":4,"totalCost":0.0034}
{"type":"user","sessionId":"a1b2c3d4-...","parentUuid":null,"uuid":"msg-001","timestamp":"2025-10-24T10:00:00Z","cwd":"/Users/jake/dev/cagent","gitBranch":"main","message":{"role":"user","content":"Help me add session persistence"}}
{"type":"assistant","sessionId":"a1b2c3d4-...","parentUuid":"msg-001","uuid":"msg-002","timestamp":"2025-10-24T10:00:05Z","model":"anthropic/claude-sonnet-4-0","cost":0.0034,"inputTokens":1523,"outputTokens":892,"duration":5231,"message":{"role":"assistant","content":"I'll help you add session persistence..."}}
{"type":"tool_call","sessionId":"a1b2c3d4-...","parentUuid":"msg-002","uuid":"msg-003","timestamp":"2025-10-24T10:00:10Z","toolName":"read_file","toolArgs":"{\"path\":\"pkg/session/session.go\"}"}
{"type":"tool_result","sessionId":"a1b2c3d4-...","parentUuid":"msg-003","uuid":"msg-004","timestamp":"2025-10-24T10:00:11Z","toolCallId":"msg-003","result":"... file contents ..."}
```

**Key Points:**
- **Line 1:** Session metadata (updated whenever session changes)
- **Lines 2+:** Messages in chronological order
- Each line is a complete, valid JSON object that can be parsed independently
- To list sessions: read only first line of each `.jsonl` file
- To resume session: read all lines and reconstruct message history
