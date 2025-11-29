package runner

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/monkci/miglet/pkg/logger"
)

// Manager handles GitHub Actions runner lifecycle
type Manager struct {
	runnerPath string
}

// NewManager creates a new runner manager
func NewManager(runnerPath string) *Manager {
	return &Manager{
		runnerPath: runnerPath,
	}
}

// RemoveRunnerConfiguration removes existing runner configuration
func (m *Manager) RemoveRunnerConfiguration() error {
	configScript := filepath.Join(m.runnerPath, "config.sh")

	// Check if config script exists
	if _, err := os.Stat(configScript); os.IsNotExist(err) {
		return fmt.Errorf("runner config script not found at %s: %w", configScript, err)
	}

	// Check if runner is configured
	if !m.IsConfigured() {
		logger.Get().Debug("Runner not configured, nothing to remove")
		return nil
	}

	logger.Get().Info("Removing existing runner configuration")

	// Execute config.sh remove
	cmd := exec.Command(configScript, "remove", "--unattended")
	cmd.Dir = m.runnerPath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to remove runner configuration: %w", err)
	}

	logger.Get().Info("Runner configuration removed successfully")
	return nil
}

// ConfigureRunner configures the runner with the provided token and settings
// Returns error if configuration fails
func (m *Manager) ConfigureRunner(token, runnerURL, runnerGroup string, labels []string) error {
	configScript := filepath.Join(m.runnerPath, "config.sh")

	// Check if config script exists
	if _, err := os.Stat(configScript); os.IsNotExist(err) {
		return fmt.Errorf("runner config script not found at %s: %w", configScript, err)
	}

	// Remove existing configuration if present
	if m.IsConfigured() {
		logger.Get().Info("Runner already configured, removing existing configuration first")
		if err := m.RemoveRunnerConfiguration(); err != nil {
			return fmt.Errorf("failed to remove existing configuration: %w", err)
		}
	}

	logger.Get().WithFields(map[string]interface{}{
		"runner_path": m.runnerPath,
		"url":         runnerURL,
		"group":       runnerGroup,
		"labels":      labels,
	}).Info("Configuring GitHub Actions runner")

	// Build config command
	args := []string{
		"--url", runnerURL,
		"--token", token,
		"--ephemeral",  // Ephemeral runner
		"--unattended", // Non-interactive mode
		"--replace",    // Replace existing configuration
	}

	// Add runner group if provided
	if runnerGroup != "" {
		args = append(args, "--runnergroup", runnerGroup)
	}

	// Add labels if provided
	if len(labels) > 0 {
		labelsStr := strings.Join(labels, ",")
		args = append(args, "--labels", labelsStr)
	}

	// Execute config.sh
	cmd := exec.Command(configScript, args...)
	cmd.Dir = m.runnerPath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	logger.Get().WithField("command", fmt.Sprintf("%s %s", configScript, strings.Join(args, " "))).Debug("Running runner configuration")

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("runner configuration failed: %w", err)
	}

	// Verify configuration by checking if .runner file exists
	runnerFile := filepath.Join(m.runnerPath, ".runner")
	if _, err := os.Stat(runnerFile); os.IsNotExist(err) {
		return fmt.Errorf("runner configuration verification failed: .runner file not found")
	}

	logger.Get().Info("GitHub Actions runner configured successfully")
	return nil
}

// StartRunner starts the runner process with log capture
// Returns the command, monitor, and error
func (m *Manager) StartRunner(monitor *Monitor) (*exec.Cmd, *Monitor, error) {
	runScript := filepath.Join(m.runnerPath, "run.sh")

	// Check if run script exists
	if _, err := os.Stat(runScript); os.IsNotExist(err) {
		return nil, nil, fmt.Errorf("runner run script not found at %s: %w", runScript, err)
	}

	// Check if runner is configured
	runnerFile := filepath.Join(m.runnerPath, ".runner")
	if _, err := os.Stat(runnerFile); os.IsNotExist(err) {
		return nil, nil, fmt.Errorf("runner not configured: .runner file not found")
	}

	logger.Get().WithField("runner_path", m.runnerPath).Info("Starting GitHub Actions runner")

	// Create command to run the runner
	cmd := exec.Command(runScript)
	cmd.Dir = m.runnerPath

	// Create pipes for stdout and stderr
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start log capture
	if monitor == nil {
		monitor = NewMonitor()
	}

	// Capture stdout and stderr
	go monitor.CaptureLogs(stdoutPipe, "stdout")
	go monitor.CaptureLogs(stderrPipe, "stderr")

	// Set environment variables if needed
	cmd.Env = os.Environ()

	logger.Get().Debug("Runner process command created with log capture")
	return cmd, monitor, nil
}

// StopRunner stops the runner process
func (m *Manager) StopRunner(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil // Already stopped or never started
	}

	logger.Get().Info("Stopping GitHub Actions runner")

	// Try graceful shutdown first
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		logger.Get().WithError(err).Warn("Failed to send interrupt signal, trying kill")
		return cmd.Process.Kill()
	}

	return nil
}

// IsConfigured checks if the runner is already configured
func (m *Manager) IsConfigured() bool {
	runnerFile := filepath.Join(m.runnerPath, ".runner")
	if _, err := os.Stat(runnerFile); err == nil {
		return true
	}
	return false
}

// GetRunnerPath returns the runner installation path
func (m *Manager) GetRunnerPath() string {
	return m.runnerPath
}
