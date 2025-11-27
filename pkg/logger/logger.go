package logger

import (
	"os"

	"github.com/sirupsen/logrus"
)

var log *logrus.Logger

// Init initializes the logger with structured JSON output
func Init(level string, format string) {
	log = logrus.New()

	// Set log level
	switch level {
	case "debug":
		log.SetLevel(logrus.DebugLevel)
	case "info":
		log.SetLevel(logrus.InfoLevel)
	case "warn":
		log.SetLevel(logrus.WarnLevel)
	case "error":
		log.SetLevel(logrus.ErrorLevel)
	default:
		log.SetLevel(logrus.InfoLevel)
	}

	// Set output format
	if format == "json" {
		log.SetFormatter(&logrus.JSONFormatter{
			TimestampFormat: "2006-01-02T15:04:05.000Z07:00",
		})
	} else {
		log.SetFormatter(&logrus.TextFormatter{
			FullTimestamp: true,
		})
	}

	log.SetOutput(os.Stdout)
}

// Get returns the logger instance
func Get() *logrus.Logger {
	if log == nil {
		// Initialize with defaults if not initialized
		Init("info", "json")
	}
	return log
}

// WithFields creates a log entry with structured fields
func WithFields(fields map[string]interface{}) *logrus.Entry {
	return log.WithFields(logrus.Fields(fields))
}

// WithContext creates a log entry with VM context fields
func WithContext(vmID, poolID, orgID string) *logrus.Entry {
	fields := logrus.Fields{
		"component": "miglet",
	}
	if vmID != "" {
		fields["vm_id"] = vmID
	}
	if poolID != "" {
		fields["pool_id"] = poolID
	}
	if orgID != "" {
		fields["org_id"] = orgID
	}
	return log.WithFields(fields)
}

// WithJobContext adds job context to a log entry
func WithJobContext(entry *logrus.Entry, jobID, runID string) *logrus.Entry {
	if jobID != "" {
		entry = entry.WithField("job_id", jobID)
	}
	if runID != "" {
		entry = entry.WithField("run_id", runID)
	}
	return entry
}

// SetLevel updates the log level at runtime
func SetLevel(level string) {
	switch level {
	case "debug":
		log.SetLevel(logrus.DebugLevel)
	case "info":
		log.SetLevel(logrus.InfoLevel)
	case "warn":
		log.SetLevel(logrus.WarnLevel)
	case "error":
		log.SetLevel(logrus.ErrorLevel)
	}
}
