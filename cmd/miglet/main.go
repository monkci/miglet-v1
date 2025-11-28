package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/monkci/miglet/pkg/config"
	"github.com/monkci/miglet/pkg/controller"
	"github.com/monkci/miglet/pkg/events"
	"github.com/monkci/miglet/pkg/logger"
	"github.com/monkci/miglet/pkg/state"
)

var (
	version   = "dev"
	buildTime = "unknown"
)

func main() {
	var (
		configPath  = flag.String("config", "", "Path to configuration file")
		logLevel    = flag.String("log-level", "info", "Log level (debug, info, warn, error)")
		logFormat   = flag.String("log-format", "json", "Log format (json, text)")
		showVersion = flag.Bool("version", false, "Show version and exit")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("MIGlet version %s (built %s)\n", version, buildTime)
		os.Exit(0)
	}

	// Initialize logger first
	logger.Init(*logLevel, *logFormat)
	log := logger.Get()

	log.WithFields(map[string]interface{}{
		"version":    version,
		"build_time": buildTime,
	}).Info("MIGlet starting")

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.WithError(err).Fatal("Failed to load configuration")
	}

	log.WithFields(map[string]interface{}{
		"pool_id":    cfg.PoolID,
		"vm_id":      cfg.VMID,
		"org_id":     cfg.OrgID,
		"controller": cfg.Controller.Endpoint,
	}).Info("Configuration loaded successfully")

	// Create logger with context for future use
	ctxLog := logger.WithContext(cfg.VMID, cfg.PoolID, cfg.OrgID)
	ctxLog.Info("MIGlet initialized with context")

	// Create MIG Controller client
	ctrlClient, err := controller.NewClient(cfg)
	if err != nil {
		ctxLog.WithError(err).Fatal("Failed to create controller client")
	}

	// Create event emitter
	eventEmitter := events.NewEmitter()

	// Create state machine
	stateMachine := state.NewStateMachine(cfg, ctrlClient, eventEmitter)

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Run state machine in goroutine
	stateMachineDone := make(chan error, 1)
	go func() {
		stateMachineDone <- stateMachine.Run()
	}()

	ctxLog.Info("MIGlet state machine started")

	// Wait for shutdown signal or state machine error
	select {
	case err := <-stateMachineDone:
		if err != nil {
			ctxLog.WithError(err).Error("State machine exited with error")
			os.Exit(1)
		}
		ctxLog.Info("State machine completed")
	case sig := <-sigChan:
		ctxLog.WithField("signal", sig.String()).Info("Shutdown signal received")
		stateMachine.Shutdown()
		<-stateMachineDone
		ctxLog.Info("MIGlet shutdown complete")
	}
}
