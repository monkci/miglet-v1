package runner

import (
	"bufio"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/monkci/miglet/pkg/events"
	"github.com/monkci/miglet/pkg/logger"
)

// RunnerState is an alias for events.RunnerState
type RunnerState = events.RunnerState

// Monitor monitors the runner process and captures logs/state
type Monitor struct {
	state         RunnerState
	stateMutex    sync.RWMutex
	logs          []string
	logsMutex     sync.RWMutex
	maxLogLines   int
	currentJobID  string
	currentRunID  string
	lastHeartbeat time.Time
	onStateChange func(RunnerState)
	onJobStart    func(jobID, runID string)
	onJobComplete func(jobID, runID string, success bool)
}

// NewMonitor creates a new runner monitor
func NewMonitor() *Monitor {
	return &Monitor{
		state:       events.RunnerStateIdle,
		logs:        make([]string, 0),
		maxLogLines: 1000, // Keep last 1000 log lines
	}
}

// SetStateChangeCallback sets a callback for state changes
func (m *Monitor) SetStateChangeCallback(callback func(RunnerState)) {
	m.onStateChange = callback
}

// SetJobCallbacks sets callbacks for job lifecycle
func (m *Monitor) SetJobCallbacks(onStart func(jobID, runID string), onComplete func(jobID, runID string, success bool)) {
	m.onJobStart = onStart
	m.onJobComplete = onComplete
}

// GetState returns the current runner state
func (m *Monitor) GetState() RunnerState {
	m.stateMutex.RLock()
	defer m.stateMutex.RUnlock()
	return m.state
}

// SetState sets the runner state and triggers callback
func (m *Monitor) SetState(newState RunnerState) {
	m.stateMutex.Lock()
	oldState := m.state
	m.state = newState
	m.stateMutex.Unlock()

	if oldState != newState && m.onStateChange != nil {
		m.onStateChange(newState)
	}
}

// GetCurrentJob returns the current job ID and run ID
func (m *Monitor) GetCurrentJob() (jobID, runID string) {
	m.stateMutex.RLock()
	defer m.stateMutex.RUnlock()
	return m.currentJobID, m.currentRunID
}

// SetCurrentJob sets the current job information
func (m *Monitor) SetCurrentJob(jobID, runID string) {
	m.stateMutex.Lock()
	m.currentJobID = jobID
	m.currentRunID = runID
	m.stateMutex.Unlock()
}

// CaptureLogs captures logs from a reader (stdout/stderr)
func (m *Monitor) CaptureLogs(reader io.Reader, prefix string) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
		logLine := line
		if prefix != "" {
			logLine = prefix + ": " + line
		}

		// Add to logs
		m.addLog(logLine)

		// Parse for job events
		m.parseLogLine(line)

		// Also log to our logger
		logger.Get().WithField("source", "runner").Info(logLine)
	}

	if err := scanner.Err(); err != nil {
		logger.Get().WithError(err).Error("Error reading runner logs")
	}
}

// addLog adds a log line to the buffer
func (m *Monitor) addLog(line string) {
	m.logsMutex.Lock()
	defer m.logsMutex.Unlock()

	m.logs = append(m.logs, line)
	if len(m.logs) > m.maxLogLines {
		// Remove oldest logs
		m.logs = m.logs[len(m.logs)-m.maxLogLines:]
	}
}

// GetLogs returns the captured logs
func (m *Monitor) GetLogs(limit int) []string {
	m.logsMutex.RLock()
	defer m.logsMutex.RUnlock()

	if limit <= 0 || limit > len(m.logs) {
		limit = len(m.logs)
	}

	// Return last N lines
	start := len(m.logs) - limit
	if start < 0 {
		start = 0
	}

	logs := make([]string, limit)
	copy(logs, m.logs[start:])
	return logs
}

// parseLogLine parses log lines to detect job events
func (m *Monitor) parseLogLine(line string) {
	lineLower := strings.ToLower(line)

	// Detect job start
	// GitHub Actions runner logs typically contain patterns like:
	// "Running job: <job-id>"
	// "Job <job-id> started"
	if strings.Contains(lineLower, "running job") || strings.Contains(lineLower, "job") && strings.Contains(lineLower, "started") {
		// Try to extract job ID and run ID
		// This is a simple parser - can be enhanced
		if m.onJobStart != nil {
			// Extract job ID and run ID from log line
			jobID, runID := extractJobInfo(line)
			if jobID != "" {
				m.SetCurrentJob(jobID, runID)
				m.SetState(events.RunnerStateRunning)
				m.onJobStart(jobID, runID)
			}
		}
	}

	// Detect job completion
	if strings.Contains(lineLower, "job") && (strings.Contains(lineLower, "completed") || strings.Contains(lineLower, "finished")) {
		jobID, runID := m.GetCurrentJob()
		if jobID != "" && m.onJobComplete != nil {
			success := strings.Contains(lineLower, "succeeded") || strings.Contains(lineLower, "success")
			m.onJobComplete(jobID, runID, success)
			m.SetCurrentJob("", "")
			m.SetState(events.RunnerStateIdle)
		}
	}

	// Detect runner offline
	if strings.Contains(lineLower, "offline") || strings.Contains(lineLower, "disconnected") {
		m.SetState(events.RunnerStateOffline)
	}
}

// extractJobInfo extracts job ID and run ID from log line
func extractJobInfo(line string) (jobID, runID string) {
	// Simple extraction - can be enhanced with regex
	// Look for patterns like "job-123" or "run-456"
	parts := strings.Fields(line)
	for _, part := range parts {
		if strings.HasPrefix(part, "job-") || strings.Contains(part, "job") {
			jobID = part
		}
		if strings.HasPrefix(part, "run-") || strings.Contains(part, "run") {
			runID = part
		}
	}
	return jobID, runID
}

// UpdateLastHeartbeat updates the last heartbeat time
func (m *Monitor) UpdateLastHeartbeat() {
	m.stateMutex.Lock()
	m.lastHeartbeat = time.Now()
	m.stateMutex.Unlock()
}

// GetLastHeartbeat returns the last heartbeat time
func (m *Monitor) GetLastHeartbeat() time.Time {
	m.stateMutex.RLock()
	defer m.stateMutex.RUnlock()
	return m.lastHeartbeat
}
