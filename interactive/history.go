package interactive

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/joshyorko/rcc/common"
)

const (
	maxHistoryEntries = 50
	historyFileName   = "run_history.json"
)

// RunStatus represents the outcome of a robot run
type RunStatus string

const (
	RunSuccess RunStatus = "success"
	RunFailed  RunStatus = "failed"
	RunUnknown RunStatus = "unknown"
)

// RunHistoryEntry represents a single run in history
type RunHistoryEntry struct {
	ID         int64     `json:"id"`
	RobotPath  string    `json:"robot_path"`
	RobotName  string    `json:"robot_name"`
	Task       string    `json:"task,omitempty"`
	StartTime  time.Time `json:"start_time"`
	EndTime    time.Time `json:"end_time,omitempty"`
	Duration   string    `json:"duration,omitempty"`
	Status     RunStatus `json:"status"`
	ExitCode   int       `json:"exit_code,omitempty"`
	Controller string    `json:"controller,omitempty"`
}

// RunHistory manages the run history
type RunHistory struct {
	Entries []RunHistoryEntry `json:"entries"`
	mu      sync.RWMutex
}

var (
	globalHistory     *RunHistory
	globalHistoryOnce sync.Once
)

// GetRunHistory returns the global run history instance
func GetRunHistory() *RunHistory {
	globalHistoryOnce.Do(func() {
		globalHistory = &RunHistory{
			Entries: make([]RunHistoryEntry, 0),
		}
		globalHistory.Load()
	})
	return globalHistory
}

// historyFilePath returns the path to the history file
func historyFilePath() string {
	return filepath.Join(common.Product.Home(), historyFileName)
}

// Load loads history from disk
func (h *RunHistory) Load() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	path := historyFilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No history file yet
		}
		return err
	}

	return json.Unmarshal(data, h)
}

// Save persists history to disk
func (h *RunHistory) Save() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Ensure directory exists
	dir := filepath.Dir(historyFilePath())
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(historyFilePath(), data, 0644)
}

// AddEntry adds a new entry to history
func (h *RunHistory) AddEntry(entry RunHistoryEntry) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Assign ID
	maxID := int64(0)
	for _, e := range h.Entries {
		if e.ID > maxID {
			maxID = e.ID
		}
	}
	entry.ID = maxID + 1

	// Prepend (newest first)
	h.Entries = append([]RunHistoryEntry{entry}, h.Entries...)

	// Trim to max entries
	if len(h.Entries) > maxHistoryEntries {
		h.Entries = h.Entries[:maxHistoryEntries]
	}
}

// UpdateEntry updates an existing entry by ID
func (h *RunHistory) UpdateEntry(id int64, endTime time.Time, status RunStatus, exitCode int) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for i := range h.Entries {
		if h.Entries[i].ID == id {
			h.Entries[i].EndTime = endTime
			h.Entries[i].Status = status
			h.Entries[i].ExitCode = exitCode
			h.Entries[i].Duration = endTime.Sub(h.Entries[i].StartTime).Round(time.Second).String()
			break
		}
	}
}

// GetLatest returns the most recent entries
func (h *RunHistory) GetLatest(n int) []RunHistoryEntry {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if n > len(h.Entries) {
		n = len(h.Entries)
	}
	result := make([]RunHistoryEntry, n)
	copy(result, h.Entries[:n])
	return result
}

// GetLastRun returns the most recent run, or nil if no history
func (h *RunHistory) GetLastRun() *RunHistoryEntry {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if len(h.Entries) == 0 {
		return nil
	}
	entry := h.Entries[0]
	return &entry
}

// Clear removes all history entries
func (h *RunHistory) Clear() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.Entries = make([]RunHistoryEntry, 0)
}
