package logger

import (
	"os"

	"github.com/sirupsen/logrus"
)

var log *logrus.Logger

// Init initializes the logger with the specified level and format
func Init(level, format string) {
	log = logrus.New()
	log.SetOutput(os.Stdout)

	// Set log level
	switch level {
	case "debug":
		log.SetLevel(logrus.DebugLevel)
	case "info":
		log.SetLevel(logrus.InfoLevel)
	case "warn", "warning":
		log.SetLevel(logrus.WarnLevel)
	case "error":
		log.SetLevel(logrus.ErrorLevel)
	default:
		log.SetLevel(logrus.InfoLevel)
	}

	// Set formatter
	if format == "json" {
		log.SetFormatter(&logrus.JSONFormatter{
			TimestampFormat: "2006-01-02T15:04:05.000Z07:00",
		})
	} else {
		log.SetFormatter(&logrus.TextFormatter{
			FullTimestamp:   true,
			TimestampFormat: "15:04:05",
			ForceColors:     true,
			PadLevelText:    true,
		})
	}
}

// Get returns the logger instance
func Get() *logrus.Logger {
	if log == nil {
		Init("info", "text")
	}
	return log
}

// WithComponent returns a logger entry with component field
func WithComponent(component string) *logrus.Entry {
	return Get().WithField("component", component)
}

// WithFields returns a logger entry with multiple fields
func WithFields(fields map[string]interface{}) *logrus.Entry {
	return Get().WithFields(fields)
}

// WithPool returns a logger entry with pool context
func WithPool(poolID, poolType string) *logrus.Entry {
	return Get().WithFields(logrus.Fields{
		"pool_id":   poolID,
		"pool_type": poolType,
	})
}

// WithVM returns a logger entry with VM context
func WithVM(vmID, poolID string) *logrus.Entry {
	return Get().WithFields(logrus.Fields{
		"vm_id":   vmID,
		"pool_id": poolID,
	})
}

// WithJob returns a logger entry with job context
func WithJob(jobID, poolID string) *logrus.Entry {
	return Get().WithFields(logrus.Fields{
		"job_id":  jobID,
		"pool_id": poolID,
	})
}

