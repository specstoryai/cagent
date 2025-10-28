package filepicker

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/v2/key"
	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/lipgloss/v2"

	"github.com/docker/cagent/pkg/tui/core"
)

// FileSelectedMsg is sent when a file is selected
type FileSelectedMsg struct {
	Path string
}

// FilePickerCancelledMsg is sent when the file picker is cancelled
type FilePickerCancelledMsg struct{}

// Model represents the file picker component
type Model struct {
	workDir      string
	currentDir   string
	files        []fileEntry
	cursor       int
	filter       string
	width        int
	height       int
	visible      bool
	showHidden   bool
}

type fileEntry struct {
	name  string
	path  string
	isDir bool
}

// KeyMap defines key bindings for the file picker
type KeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Enter  key.Binding
	Escape key.Binding
	Parent key.Binding
}

var defaultKeyMap = KeyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "down"),
	),
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select"),
	),
	Escape: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "cancel"),
	),
	Parent: key.NewBinding(
		key.WithKeys("backspace", "left", "h"),
		key.WithHelp("←/h/backspace", "parent dir"),
	),
}

var (
	// Styles
	containerStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62")).
			Padding(1, 2)

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("205"))

	dirStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("39"))

	fileStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))

	selectedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("15")).
			Background(lipgloss.Color("62")).
			Bold(true)

	filterStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("205")).
			Italic(true)
)

// New creates a new file picker model
func New(workDir string) *Model {
	m := &Model{
		workDir:    workDir,
		currentDir: workDir,
		visible:    false,
		showHidden: false,
	}
	m.loadFiles()
	return m
}

// Init initializes the file picker
func (m *Model) Init() tea.Cmd {
	return nil
}

// Update handles messages and updates the file picker state
func (m *Model) Update(msg tea.Msg) (*Model, tea.Cmd) {
	if !m.visible {
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, defaultKeyMap.Escape):
			m.visible = false
			return m, core.CmdHandler(FilePickerCancelledMsg{})

		case key.Matches(msg, defaultKeyMap.Enter):
			if len(m.files) == 0 {
				return m, nil
			}
			selected := m.files[m.cursor]
			if selected.isDir {
				// Navigate into directory
				m.currentDir = selected.path
				m.cursor = 0
				m.filter = ""
				m.loadFiles()
				return m, nil
			}
			// File selected
			m.visible = false
			return m, core.CmdHandler(FileSelectedMsg{Path: selected.path})

		case key.Matches(msg, defaultKeyMap.Up):
			if m.cursor > 0 {
				m.cursor--
			}

		case key.Matches(msg, defaultKeyMap.Down):
			if m.cursor < len(m.files)-1 {
				m.cursor++
			}

		case key.Matches(msg, defaultKeyMap.Parent):
			// If there's a filter active, backspace removes from filter
			if m.filter != "" {
				if len(m.filter) > 0 {
					m.filter = m.filter[:len(m.filter)-1]
					m.applyFilter()
					m.cursor = 0
				}
			} else {
				// Go to parent directory
				if m.currentDir != m.workDir && m.currentDir != "/" {
					m.currentDir = filepath.Dir(m.currentDir)
					m.cursor = 0
					m.filter = ""
					m.loadFiles()
				}
			}

		case msg.String() == "backspace":
			// Backspace removes last character from filter
			if len(m.filter) > 0 {
				m.filter = m.filter[:len(m.filter)-1]
				m.applyFilter()
				m.cursor = 0
			}

		case msg.String() != "":
			// Typing to filter
			char := msg.String()
			// Ignore special keys
			if len(char) == 1 && char[0] >= 32 && char[0] <= 126 {
				m.filter += char
				m.applyFilter()
				m.cursor = 0
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	}

	return m, nil
}

// View renders the file picker
func (m *Model) View() string {
	if !m.visible {
		return ""
	}

	var b strings.Builder

	// Header with current directory
	relDir, _ := filepath.Rel(m.workDir, m.currentDir)
	if relDir == "." {
		relDir = "./"
	}
	b.WriteString(headerStyle.Render("Select file: " + relDir))
	b.WriteString("\n\n")

	// Show filter if active
	if m.filter != "" {
		b.WriteString(filterStyle.Render("Filter: " + m.filter))
		b.WriteString("\n\n")
	}

	// File list
	maxVisible := 10
	if m.height > 0 {
		maxVisible = m.height - 8 // Account for header, footer, padding
	}

	start := m.cursor
	if start > len(m.files)-maxVisible {
		start = len(m.files) - maxVisible
	}
	if start < 0 {
		start = 0
	}

	for i := start; i < len(m.files) && i < start+maxVisible; i++ {
		entry := m.files[i]
		icon := "  "
		style := fileStyle

		if entry.isDir {
			icon = " "
			style = dirStyle
		}

		line := icon + entry.name
		if entry.isDir {
			line += "/"
		}

		if i == m.cursor {
			line = selectedStyle.Render("▸ " + line)
		} else {
			line = style.Render("  " + line)
		}

		b.WriteString(line)
		b.WriteString("\n")
	}

	if len(m.files) == 0 {
		b.WriteString(fileStyle.Render("  (no files)"))
		b.WriteString("\n")
	}

	// Footer with hints
	b.WriteString("\n")
	b.WriteString(lipgloss.NewStyle().Faint(true).Render(
		"↑/↓: navigate • enter: select • esc: cancel • ←: parent dir • type to filter",
	))

	content := b.String()

	// Apply container style
	maxWidth := 60
	if m.width > 0 && m.width < maxWidth+10 {
		maxWidth = m.width - 10
	}

	return lipgloss.Place(
		m.width,
		m.height,
		lipgloss.Center,
		lipgloss.Center,
		containerStyle.Width(maxWidth).Render(content),
	)
}

// loadFiles loads files and directories from the current directory
func (m *Model) loadFiles() {
	m.files = []fileEntry{}

	entries, err := os.ReadDir(m.currentDir)
	if err != nil {
		return
	}

	// Separate directories and files
	var dirs, files []fileEntry

	for _, entry := range entries {
		// Skip hidden files unless showHidden is true
		if !m.showHidden && strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		fe := fileEntry{
			name:  entry.Name(),
			path:  filepath.Join(m.currentDir, entry.Name()),
			isDir: entry.IsDir(),
		}

		// Skip non-regular files (symlinks, devices, etc.) unless they're directories
		if !entry.IsDir() && !info.Mode().IsRegular() {
			continue
		}

		if entry.IsDir() {
			dirs = append(dirs, fe)
		} else {
			files = append(files, fe)
		}
	}

	// Sort alphabetically
	sort.Slice(dirs, func(i, j int) bool {
		return strings.ToLower(dirs[i].name) < strings.ToLower(dirs[j].name)
	})
	sort.Slice(files, func(i, j int) bool {
		return strings.ToLower(files[i].name) < strings.ToLower(files[j].name)
	})

	// Combine: directories first, then files
	m.files = append(dirs, files...)
}

// applyFilter filters the file list based on the current filter string
func (m *Model) applyFilter() {
	if m.filter == "" {
		m.loadFiles()
		return
	}

	m.loadFiles()

	filter := strings.ToLower(m.filter)
	var filtered []fileEntry

	for _, entry := range m.files {
		if strings.Contains(strings.ToLower(entry.name), filter) {
			filtered = append(filtered, entry)
		}
	}

	m.files = filtered
}

// SetSize sets the dimensions of the file picker
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// IsVisible returns whether the file picker is currently visible
func (m *Model) IsVisible() bool {
	return m.visible
}

// SetVisible sets the visibility of the file picker
func (m *Model) SetVisible(visible bool) {
	m.visible = visible
	if visible {
		m.filter = ""
		m.cursor = 0
		m.currentDir = m.workDir
		m.loadFiles()
	}
}
