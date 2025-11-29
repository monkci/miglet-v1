package runner

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/monkci/miglet/pkg/logger"
)

const (
	runnerVersion     = "2.329.0"
	runnerPlatform    = "linux-x64"
	runnerDir         = "actions-runner"
	runnerArchiveName = "actions-runner-linux-x64-2.329.0.tar.gz"
	runnerURL         = "https://github.com/actions/runner/releases/download/v2.329.0/actions-runner-linux-x64-2.329.0.tar.gz"
	runnerSHA256      = "194f1e1e4bd02f80b7e9633fc546084d8d4e19f3928a324d512ea53430102e1d"
)

// Installer handles GitHub Actions runner installation
type Installer struct {
	baseDir string
}

// NewInstaller creates a new runner installer
func NewInstaller(baseDir string) *Installer {
	return &Installer{
		baseDir: baseDir,
	}
}

// Install downloads and installs the GitHub Actions runner
func (i *Installer) Install() error {
	runnerPath := filepath.Join(i.baseDir, runnerDir)

	// Check if runner is already installed
	if i.isInstalled(runnerPath) {
		logger.Get().WithField("path", runnerPath).Info("GitHub Actions runner already installed, removing existing installation for clean reinstall")

		// Remove existing installation for clean reinstall
		if err := i.removeExisting(runnerPath); err != nil {
			return fmt.Errorf("failed to remove existing runner installation: %w", err)
		}
	}

	logger.Get().Info("Installing GitHub Actions runner")

	// Create runner directory
	if err := os.MkdirAll(runnerPath, 0755); err != nil {
		return fmt.Errorf("failed to create runner directory: %w", err)
	}

	// Download runner archive
	archivePath := filepath.Join(i.baseDir, runnerArchiveName)
	if err := i.downloadRunner(archivePath); err != nil {
		return fmt.Errorf("failed to download runner: %w", err)
	}
	defer os.Remove(archivePath) // Clean up archive after extraction

	// Validate hash (optional but recommended)
	if err := i.validateHash(archivePath); err != nil {
		return fmt.Errorf("hash validation failed: %w", err)
	}

	// Extract archive
	if err := i.extractArchive(archivePath, runnerPath); err != nil {
		return fmt.Errorf("failed to extract runner: %w", err)
	}

	logger.Get().WithField("path", runnerPath).Info("GitHub Actions runner installed successfully")
	return nil
}

// isInstalled checks if the runner is already installed
func (i *Installer) isInstalled(runnerPath string) bool {
	// Check if runner binary exists
	runnerBin := filepath.Join(runnerPath, "run.sh")
	if _, err := os.Stat(runnerBin); err == nil {
		return true
	}
	return false
}

// downloadRunner downloads the runner archive
func (i *Installer) downloadRunner(destPath string) error {
	logger.Get().WithField("url", runnerURL).Info("Downloading GitHub Actions runner")

	// Create the file
	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer out.Close()

	// Get the data
	resp, err := http.Get(runnerURL)
	if err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	// Write the body to file
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	logger.Get().WithField("path", destPath).Info("Downloaded GitHub Actions runner")
	return nil
}

// validateHash validates the SHA256 hash of the downloaded archive
func (i *Installer) validateHash(filePath string) error {
	logger.Get().Debug("Validating runner archive hash")

	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return fmt.Errorf("failed to calculate hash: %w", err)
	}

	calculatedHash := hex.EncodeToString(hash.Sum(nil))
	expectedHash := strings.ToLower(runnerSHA256)

	if calculatedHash != expectedHash {
		return fmt.Errorf("hash mismatch: expected %s, got %s", expectedHash, calculatedHash)
	}

	logger.Get().Debug("Hash validation passed")
	return nil
}

// extractArchive extracts the tar.gz archive
func (i *Installer) extractArchive(archivePath, destPath string) error {
	logger.Get().WithFields(map[string]interface{}{
		"archive": archivePath,
		"dest":    destPath,
	}).Info("Extracting runner archive")

	// Use tar command to extract
	cmd := exec.Command("tar", "xzf", archivePath, "-C", destPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to extract archive: %w", err)
	}

	logger.Get().Info("Runner archive extracted successfully")
	return nil
}

// GetRunnerPath returns the path to the installed runner
func (i *Installer) GetRunnerPath() string {
	return filepath.Join(i.baseDir, runnerDir)
}

// removeExisting removes the existing runner installation directory
func (i *Installer) removeExisting(runnerPath string) error {
	logger.Get().WithField("path", runnerPath).Info("Removing existing runner installation")

	// Remove the entire directory
	if err := os.RemoveAll(runnerPath); err != nil {
		return fmt.Errorf("failed to remove runner directory: %w", err)
	}

	logger.Get().WithField("path", runnerPath).Info("Existing runner installation removed")
	return nil
}

// GetRunnerVersion returns the runner version
func GetRunnerVersion() string {
	return runnerVersion
}
