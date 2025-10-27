package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

// JSONLWriter handles thread-safe JSONL file writing
type JSONLWriter struct {
	file *os.File
	mu   sync.Mutex
	path string
}

// NewJSONLWriter creates a new JSONL writer
func NewJSONLWriter(path string) *JSONLWriter {
	return &JSONLWriter{path: path}
}

// Open opens the JSONL file for writing (append mode)
func (w *JSONLWriter) Open() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file != nil {
		return nil // Already open
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(w.path), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Open file in append mode
	f, err := os.OpenFile(w.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}

	w.file = f
	return nil
}

// Append appends a JSON entry to the file
func (w *JSONLWriter) Append(entry interface{}) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Ensure file is open
	if w.file == nil {
		if err := w.Open(); err != nil {
			return err
		}
	}

	// Marshal to JSON
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal entry: %w", err)
	}

	// Append newline
	data = append(data, '\n')

	// Write to file
	if _, err := w.file.Write(data); err != nil {
		return fmt.Errorf("failed to write to file: %w", err)
	}

	// Flush to disk immediately for durability
	return w.file.Sync()
}

// Close closes the JSONL file
func (w *JSONLWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file != nil {
		err := w.file.Close()
		w.file = nil
		return err
	}
	return nil
}

// ReadJSONL reads all entries from a JSONL file
func ReadJSONL(path string) (*TUISession, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)

	// Increase buffer size for large messages
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 10*1024*1024) // 10MB max

	var metadata TUISessionMetadata
	var entries []TUISessionEntry
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Bytes()

		if len(line) == 0 {
			continue
		}

		// First line should be metadata
		if lineNum == 1 {
			if err := json.Unmarshal(line, &metadata); err != nil {
				return nil, fmt.Errorf("failed to parse metadata (line %d): %w", lineNum, err)
			}

			// Validate it's metadata
			if metadata.Type != "session_metadata" {
				return nil, fmt.Errorf("first line is not session metadata (line %d)", lineNum)
			}
			continue
		}

		// Subsequent lines are entries
		var entry TUISessionEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			slog.Warn("Failed to parse JSONL entry, skipping", "line", lineNum, "error", err)
			continue
		}

		entries = append(entries, entry)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading file: %w", err)
	}

	if lineNum == 0 {
		return nil, fmt.Errorf("empty session file")
	}

	return &TUISession{
		Metadata: metadata,
		Entries:  entries,
	}, nil
}

// ReadSessionMetadata reads only the first line (metadata) from a JSONL file
func ReadSessionMetadata(path string) (*TUISessionMetadata, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("error reading file: %w", err)
		}
		return nil, fmt.Errorf("empty file")
	}

	line := scanner.Bytes()
	var metadata TUISessionMetadata
	if err := json.Unmarshal(line, &metadata); err != nil {
		return nil, fmt.Errorf("failed to parse metadata: %w", err)
	}

	if metadata.Type != "session_metadata" {
		return nil, fmt.Errorf("first line is not session metadata")
	}

	return &metadata, nil
}

// WriteMetadata rewrites the first line of the JSONL file with updated metadata
func WriteMetadata(path string, metadata *TUISessionMetadata) error {
	// Read all lines
	lines, err := readAllLines(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	// Marshal new metadata
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	// Replace first line or create new file
	if len(lines) > 0 {
		lines[0] = string(metadataJSON)
	} else {
		lines = []string{string(metadataJSON)}
	}

	// Write back atomically
	return atomicWriteLines(path, lines)
}

// readAllLines reads all lines from a file
func readAllLines(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)

	// Increase buffer size for large messages
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 10*1024*1024) // 10MB max

	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return lines, nil
}

// atomicWriteLines writes lines to a file atomically using a temp file
func atomicWriteLines(path string, lines []string) error {
	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Create temp file in same directory for atomic rename
	tempFile, err := os.CreateTemp(filepath.Dir(path), ".tmp-session-*.jsonl")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tempPath := tempFile.Name()

	// Write lines
	writer := bufio.NewWriter(tempFile)
	for _, line := range lines {
		if _, err := writer.WriteString(line + "\n"); err != nil {
			tempFile.Close()
			os.Remove(tempPath)
			return fmt.Errorf("failed to write line: %w", err)
		}
	}

	// Flush and sync
	if err := writer.Flush(); err != nil {
		tempFile.Close()
		os.Remove(tempPath)
		return fmt.Errorf("failed to flush: %w", err)
	}

	if err := tempFile.Sync(); err != nil {
		tempFile.Close()
		os.Remove(tempPath)
		return fmt.Errorf("failed to sync: %w", err)
	}

	if err := tempFile.Close(); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tempPath, path); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}

// AppendEntry is a convenience function to append an entry to a JSONL file
func AppendEntry(path string, entry *TUISessionEntry) error {
	writer := NewJSONLWriter(path)
	defer writer.Close()

	if err := writer.Open(); err != nil {
		return err
	}

	return writer.Append(entry)
}

// WriteSession writes a complete TUI session to a JSONL file
func WriteSession(path string, session *TUISession) error {
	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Open file for writing (truncate if exists)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	writer := bufio.NewWriter(file)

	// Write metadata as first line
	metadataJSON, err := json.Marshal(session.Metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}
	if _, err := writer.Write(metadataJSON); err != nil {
		return fmt.Errorf("failed to write metadata: %w", err)
	}
	if _, err := writer.WriteString("\n"); err != nil {
		return fmt.Errorf("failed to write newline: %w", err)
	}

	// Write entries
	for i, entry := range session.Entries {
		entryJSON, err := json.Marshal(entry)
		if err != nil {
			return fmt.Errorf("failed to marshal entry %d: %w", i, err)
		}
		if _, err := writer.Write(entryJSON); err != nil {
			return fmt.Errorf("failed to write entry %d: %w", i, err)
		}
		if _, err := writer.WriteString("\n"); err != nil {
			return fmt.Errorf("failed to write newline: %w", i, err)
		}
	}

	// Flush and sync
	if err := writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush: %w", err)
	}

	return file.Sync()
}

// CountLines counts the number of lines in a file without loading into memory
func CountLines(path string) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	count := 0
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		count++
	}

	if err := scanner.Err(); err != nil {
		return 0, err
	}

	return count, nil
}

// StreamJSONL reads a JSONL file line by line using a callback
func StreamJSONL(path string, callback func(lineNum int, data []byte) error) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)

	// Increase buffer size
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 10*1024*1024)

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		if err := callback(lineNum, scanner.Bytes()); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
	}

	return scanner.Err()
}
