package state

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/monkci/miglet/pkg/config"
	"github.com/monkci/miglet/pkg/controller"
	"github.com/monkci/miglet/pkg/events"
	"github.com/monkci/miglet/pkg/logger"
	"github.com/monkci/miglet/pkg/metrics"
	"github.com/monkci/miglet/pkg/runner"
	"github.com/monkci/miglet/pkg/storage"
	"github.com/monkci/miglet/proto/commands"
)

// State represents the current state of MIGlet
type State string

const (
	StateInitializing      State = "initializing"
	StateConnecting        State = "connecting" // Connecting to controller via gRPC
	StateReady             State = "ready"      // Connected, waiting for registration config
	StateRegisteringRunner State = "registering_runner"
	StateIdle              State = "idle"
	StateJobRunning        State = "job_running"
	StateDraining          State = "draining"
	StateShuttingDown      State = "shutting_down"
	StateError             State = "error"
)

// StateMachine manages MIGlet state transitions
type StateMachine struct {
	currentState       State
	config             *config.Config
	controller         *controller.Client
	grpcClient         *controller.GRPCClient // gRPC client for bidirectional streaming
	eventEmitter       *events.Emitter
	ctx                context.Context
	cancel             context.CancelFunc
	vmStartedEventSent bool                    // Track if VM started event has been sent
	registrationToken  string                  // Registration token received from controller
	runnerURL          string                  // Runner URL for registration
	runnerGroup        string                  // Runner group
	runnerLabels       []string                // Runner labels
	runnerPath         string                  // Path to installed runner
	runnerCmd          *exec.Cmd               // Runner process command
	runnerMonitor      *runner.Monitor         // Runner monitor for logs/state
	metricsCollector   *metrics.Collector      // Metrics collector
	lastHeartbeat      time.Time               // Last heartbeat time
	mongoStorage       *storage.MongoDBStorage // MongoDB storage (optional)
	heartbeatStop      chan struct{}           // Signal to stop heartbeat goroutine
	heartbeatWg        sync.WaitGroup          // Wait group for heartbeat goroutine
}

// NewStateMachine creates a new state machine
func NewStateMachine(cfg *config.Config, ctrl *controller.Client, emitter *events.Emitter) *StateMachine {
	ctx, cancel := context.WithCancel(context.Background())
	sm := &StateMachine{
		currentState:     StateInitializing,
		config:           cfg,
		controller:       ctrl,
		eventEmitter:     emitter,
		ctx:              ctx,
		cancel:           cancel,
		metricsCollector: metrics.NewCollector(),
		heartbeatStop:    make(chan struct{}),
	}

	// Initialize MongoDB storage if enabled
	if cfg.Storage.MongoDB.Enabled && cfg.Storage.MongoDB.ConnectionString != "" {
		log := logger.WithContext(cfg.VMID, cfg.PoolID, cfg.OrgID)
		log.Info("Initializing MongoDB storage")

		mongoStorage, err := storage.NewMongoDBStorage(
			cfg.Storage.MongoDB.ConnectionString,
			cfg.Storage.MongoDB.Database,
			cfg.Storage.MongoDB.Collection,
		)
		if err != nil {
			log.WithError(err).Warn("Failed to initialize MongoDB storage, continuing without it")
		} else {
			sm.mongoStorage = mongoStorage
			log.Info("MongoDB storage initialized successfully")
		}
	}

	return sm
}

// GetCurrentState returns the current state
func (sm *StateMachine) GetCurrentState() State {
	return sm.currentState
}

// Transition transitions to a new state
func (sm *StateMachine) Transition(newState State) {
	oldState := sm.currentState
	sm.currentState = newState

	log := logger.WithContext(sm.config.VMID, sm.config.PoolID, sm.config.OrgID)
	log.WithFields(map[string]interface{}{
		"old_state": oldState,
		"new_state": newState,
	}).Info("State transition")

	// Send immediate heartbeat on state transition (non-blocking)
	go sm.sendHeartbeat()
}

// Run starts the state machine and executes state handlers
func (sm *StateMachine) Run() error {
	log := logger.WithContext(sm.config.VMID, sm.config.PoolID, sm.config.OrgID)
	log.Info("State machine starting")

	// Start background heartbeat loop
	sm.startHeartbeatLoop()

	for {
		select {
		case <-sm.ctx.Done():
			log.Info("State machine context cancelled")
			return nil
		default:
			// Execute current state handler
			// State handlers should block or transition to next state
			// Terminal states (Error, ShuttingDown) will return
			if err := sm.executeState(); err != nil {
				log.WithError(err).Error("State execution failed")
				sm.Transition(StateError)
				return err
			}

			// Check if we're in a terminal state
			if sm.currentState == StateError || sm.currentState == StateShuttingDown {
				log.WithField("state", sm.currentState).Info("Reached terminal state")
				return nil
			}

			// Small delay to prevent tight loop (only for states that don't block)
			time.Sleep(1 * time.Second)
		}
	}
}

// startHeartbeatLoop starts a background goroutine that sends heartbeats at regular intervals
func (sm *StateMachine) startHeartbeatLoop() {
	log := logger.WithContext(sm.config.VMID, sm.config.PoolID, sm.config.OrgID)
	log.Info("Starting background heartbeat loop")

	sm.heartbeatWg.Add(1)
	go func() {
		defer sm.heartbeatWg.Done()

		ticker := time.NewTicker(sm.config.Heartbeat.Interval)
		defer ticker.Stop()

		for {
			select {
			case <-sm.heartbeatStop:
				log.Info("Heartbeat loop stopped")
				return
			case <-sm.ctx.Done():
				log.Info("Heartbeat loop cancelled")
				return
			case <-ticker.C:
				// Only send heartbeat if gRPC is connected (to avoid spam during initialization)
				if sm.grpcClient != nil {
					sm.sendHeartbeat()
				}
			}
		}
	}()
}

// stopHeartbeatLoop stops the background heartbeat goroutine
func (sm *StateMachine) stopHeartbeatLoop() {
	log := logger.WithContext(sm.config.VMID, sm.config.PoolID, sm.config.OrgID)
	log.Info("Stopping heartbeat loop")

	close(sm.heartbeatStop)
	sm.heartbeatWg.Wait()
}

// executeState executes the handler for the current state
func (sm *StateMachine) executeState() error {
	switch sm.currentState {
	case StateInitializing:
		return sm.handleInitializing()
	case StateConnecting:
		return sm.handleConnecting()
	case StateReady:
		return sm.handleReady()
	case StateRegisteringRunner:
		return sm.handleRegisteringRunner()
	case StateIdle:
		// Runner is running, heartbeats are sent by background goroutine
		// The runner process is monitored in a separate goroutine
		// Just wait for context cancellation or state change
		select {
		case <-sm.ctx.Done():
			return nil
		case <-time.After(1 * time.Second):
			// Small delay to prevent tight loop
			return nil
		}
	case StateError:
		// Terminal state
		return nil
	case StateShuttingDown:
		// TODO: Phase 4
		return nil
	default:
		return nil
	}
}

// handleInitializing handles the initializing state
func (sm *StateMachine) handleInitializing() error {
	log := logger.WithContext(sm.config.VMID, sm.config.PoolID, sm.config.OrgID)
	log.Info("Initializing MIGlet")

	// Determine base directory for runner installation
	// Use /tmp/miglet-runner or current directory if /tmp is not writable
	baseDir := "/tmp/miglet-runner"
	if _, err := os.Stat(baseDir); err != nil {
		// Try to create it, if it fails, use current directory
		if err := os.MkdirAll(baseDir, 0755); err != nil {
			log.WithError(err).Warn("Failed to create /tmp/miglet-runner, using current directory")
			baseDir = "."
		}
	}

	// Install GitHub Actions runner
	log.Info("Installing GitHub Actions runner")
	installer := runner.NewInstaller(baseDir)
	if err := installer.Install(); err != nil {
		log.WithError(err).Error("Failed to install GitHub Actions runner")
		// For now, we'll continue even if installation fails
		// In production, you might want to fail here
		log.Warn("Continuing despite runner installation failure")
	} else {
		runnerPath := installer.GetRunnerPath()
		sm.runnerPath = runnerPath
		log.WithFields(map[string]interface{}{
			"runner_path": runnerPath,
			"version":     runner.GetRunnerVersion(),
		}).Info("GitHub Actions runner installed and ready")
	}

	// Validate prerequisites (Docker, network, etc.)
	// TODO: Add Docker check, network connectivity check, etc.

	// Transition to connecting state (gRPC only)
	sm.Transition(StateConnecting)
	return nil
}

// handleConnecting handles establishing gRPC connection to controller
// All communication happens via gRPC - no HTTP
func (sm *StateMachine) handleConnecting() error {
	log := logger.WithContext(sm.config.VMID, sm.config.PoolID, sm.config.OrgID)

	log.Info("Connecting to controller via gRPC")

	// Initialize gRPC client
	if sm.grpcClient == nil {
		grpcClient, err := controller.NewGRPCClient(sm.config)
		if err != nil {
			log.WithError(err).Error("Failed to create gRPC client")
			sm.Transition(StateError)
			return nil
		}
		sm.grpcClient = grpcClient
	}

	// Connect to controller via gRPC
	if err := sm.grpcClient.Connect(); err != nil {
		log.WithError(err).Error("Failed to connect to controller via gRPC")
		sm.Transition(StateError)
		return nil
	}

	log.Info("gRPC connection established, transitioning to ready state")
	sm.Transition(StateReady)
	return nil
}

// handleReady handles the ready state - waiting for commands via gRPC
func (sm *StateMachine) handleReady() error {
	log := logger.WithContext(sm.config.VMID, sm.config.PoolID, sm.config.OrgID)

	log.Info("MIGlet is ready - waiting for commands via gRPC")

	// Verify gRPC client is connected
	if sm.grpcClient == nil {
		log.Error("gRPC client not initialized")
		sm.Transition(StateError)
		return nil
	}

	// Listen for commands from gRPC stream
	commandCh := sm.grpcClient.GetCommandChannel()

	for {
		select {
		case <-sm.ctx.Done():
			return nil
		case cmd := <-commandCh:
			if cmd == nil {
				log.Warn("Command channel closed, reconnecting...")
				// Reconnection will be handled by gRPC client
				time.Sleep(5 * time.Second)
				continue
			}

			log.WithFields(map[string]interface{}{
				"command_id": cmd.Id,
				"type":       cmd.Type,
			}).Info("Received command from controller via gRPC")

			if cmd.Type == "register_runner" {
				// Extract registration token
				token, ok := cmd.StringParams["registration_token"]
				if !ok || token == "" {
					log.Error("Register runner command missing registration_token")
					sm.grpcClient.SendCommandAck(cmd.Id, false, "Missing registration_token", nil)
					continue
				}

				// Extract runner URL
				runnerURL, ok := cmd.StringParams["runner_url"]
				if !ok || runnerURL == "" {
					log.Error("Register runner command missing runner_url")
					sm.grpcClient.SendCommandAck(cmd.Id, false, "Missing runner_url", nil)
					continue
				}

				// Extract runner group (optional)
				runnerGroup := cmd.StringParams["runner_group"]

				// Extract labels
				labels := cmd.StringArrayParams

				// Store registration config
				sm.registrationToken = token
				sm.runnerURL = runnerURL
				sm.runnerGroup = runnerGroup
				sm.runnerLabels = labels

				log.WithFields(map[string]interface{}{
					"token_length": len(token),
					"runner_url":   runnerURL,
					"runner_group": runnerGroup,
					"labels":       labels,
				}).Info("Registration config received, transitioning to registering runner")

				// Send acknowledgment
				sm.grpcClient.SendCommandAck(cmd.Id, true, "Registration config received", nil)

				// Transition to registering runner state
				sm.Transition(StateRegisteringRunner)
				return nil
			} else {
				// Handle other command types (drain, shutdown, etc.)
				log.WithField("command_type", cmd.Type).Info("Received command (not register_runner)")
				// TODO: Handle other command types
				sm.grpcClient.SendCommandAck(cmd.Id, false, "Command type not yet implemented", nil)
			}
		}
	}
}

// GetRegistrationToken returns the registration token received from controller
func (sm *StateMachine) GetRegistrationToken() string {
	return sm.registrationToken
}

// GetRunnerConfig returns runner configuration received from controller
func (sm *StateMachine) GetRunnerConfig() (url, group string, labels []string) {
	return sm.runnerURL, sm.runnerGroup, sm.runnerLabels
}

// handleRegisteringRunner handles the runner registration state
func (sm *StateMachine) handleRegisteringRunner() error {
	log := logger.WithContext(sm.config.VMID, sm.config.PoolID, sm.config.OrgID)

	// Check if we have all required information
	if sm.registrationToken == "" {
		log.Error("Registration token not available")
		sm.Transition(StateError)
		return nil
	}

	if sm.runnerURL == "" {
		log.Error("Runner URL not available")
		sm.Transition(StateError)
		return nil
	}

	if sm.runnerPath == "" {
		log.Error("Runner path not available")
		sm.Transition(StateError)
		return nil
	}

	log.Info("Starting GitHub Actions runner registration")

	// Create runner manager
	runnerMgr := runner.NewManager(sm.runnerPath)

	// Configure runner (non-interactive)
	log.Info("Configuring runner with token")
	if err := runnerMgr.ConfigureRunner(
		sm.registrationToken,
		sm.runnerURL,
		sm.runnerGroup,
		sm.runnerLabels,
	); err != nil {
		log.WithError(err).Error("Failed to configure runner")
		sm.Transition(StateError)
		return nil
	}

	// Create runner monitor
	monitor := runner.NewMonitor()
	sm.setupRunnerCallbacks(monitor)
	sm.runnerMonitor = monitor

	// Start runner process with log capture
	log.Info("Starting runner process")
	runnerCmd, _, err := runnerMgr.StartRunner(monitor)
	if err != nil {
		log.WithError(err).Error("Failed to start runner")
		sm.Transition(StateError)
		return nil
	}

	// Store runner command for later shutdown
	sm.runnerCmd = runnerCmd

	// Start the runner process
	if err := runnerCmd.Start(); err != nil {
		log.WithError(err).Error("Failed to start runner process")
		sm.Transition(StateError)
		return nil
	}

	log.WithField("pid", runnerCmd.Process.Pid).Info("GitHub Actions runner started successfully")

	// Send runner registered event (prefer gRPC, fallback to HTTP)
	registeredEvent := events.NewRunnerRegisteredEvent(
		sm.config.VMID,
		sm.config.PoolID,
		sm.config.OrgID,
		sm.runnerURL,
	)
	registeredEvent.Labels = sm.runnerLabels
	registeredEvent.RunnerGroup = sm.runnerGroup

	// Try gRPC first, fallback to HTTP
	if sm.grpcClient != nil {
		eventData := map[string]string{
			"runner_url":   sm.runnerURL,
			"runner_group": sm.runnerGroup,
		}
		if err := sm.grpcClient.SendEvent("runner_registered", sm.config.VMID, sm.config.PoolID, sm.config.OrgID, eventData); err != nil {
			log.WithError(err).Warn("Failed to send runner registered event via gRPC, falling back to HTTP")
			if err := sm.controller.SendEvent(sm.ctx, registeredEvent); err != nil {
				log.WithError(err).Warn("Failed to send runner registered event via HTTP")
			}
		} else {
			log.Debug("Runner registered event sent via gRPC")
		}
	} else {
		if err := sm.controller.SendEvent(sm.ctx, registeredEvent); err != nil {
			log.WithError(err).Warn("Failed to send runner registered event")
		}
	}

	// Monitor runner process in a goroutine
	go sm.monitorRunner(runnerCmd)

	// Transition to idle state (runner is running)
	log.Info("Runner registered and running, transitioning to idle")
	sm.Transition(StateIdle)
	return nil
}

// setupRunnerCallbacks sets up callbacks for runner state changes
func (sm *StateMachine) setupRunnerCallbacks(monitor *runner.Monitor) {
	log := logger.WithContext(sm.config.VMID, sm.config.PoolID, sm.config.OrgID)

	// State change callback
	monitor.SetStateChangeCallback(func(state events.RunnerState) {
		log.WithField("runner_state", state).Info("Runner state changed")
	})

	// Job start callback
	monitor.SetJobCallbacks(
		func(jobID, runID string) {
			log.WithFields(map[string]interface{}{
				"job_id": jobID,
				"run_id": runID,
			}).Info("Job started")

			// Send job started event (prefer gRPC, fallback to HTTP)
			eventData := map[string]string{
				"job_id": jobID,
				"run_id": runID,
			}
			if sm.grpcClient != nil {
				if err := sm.grpcClient.SendEvent("job_started", sm.config.VMID, sm.config.PoolID, sm.config.OrgID, eventData); err != nil {
					log.WithError(err).Warn("Failed to send job started event via gRPC, falling back to HTTP")
					jobEvent := events.NewJobStartedEvent(sm.config.VMID, sm.config.PoolID, sm.config.OrgID, jobID, runID)
					if err := sm.controller.SendEvent(sm.ctx, jobEvent); err != nil {
						log.WithError(err).Warn("Failed to send job started event via HTTP")
					}
				} else {
					log.Debug("Job started event sent via gRPC")
				}
			} else {
				jobEvent := events.NewJobStartedEvent(sm.config.VMID, sm.config.PoolID, sm.config.OrgID, jobID, runID)
				if err := sm.controller.SendEvent(sm.ctx, jobEvent); err != nil {
					log.WithError(err).Warn("Failed to send job started event")
				}
			}
		},
		func(jobID, runID string, success bool) {
			log.WithFields(map[string]interface{}{
				"job_id":  jobID,
				"run_id":  runID,
				"success": success,
			}).Info("Job completed")

			// Send job completed event (prefer gRPC, fallback to HTTP)
			eventData := map[string]string{
				"job_id":  jobID,
				"run_id":  runID,
				"success": fmt.Sprintf("%t", success),
			}
			if sm.grpcClient != nil {
				if err := sm.grpcClient.SendEvent("job_completed", sm.config.VMID, sm.config.PoolID, sm.config.OrgID, eventData); err != nil {
					log.WithError(err).Warn("Failed to send job completed event via gRPC, falling back to HTTP")
					jobEvent := events.NewJobCompletedEvent(sm.config.VMID, sm.config.PoolID, sm.config.OrgID, jobID, runID, success)
					if err := sm.controller.SendEvent(sm.ctx, jobEvent); err != nil {
						log.WithError(err).Warn("Failed to send job completed event via HTTP")
					}
				} else {
					log.Debug("Job completed event sent via gRPC")
				}
			} else {
				jobEvent := events.NewJobCompletedEvent(sm.config.VMID, sm.config.PoolID, sm.config.OrgID, jobID, runID, success)
				if err := sm.controller.SendEvent(sm.ctx, jobEvent); err != nil {
					log.WithError(err).Warn("Failed to send job completed event")
				}
			}
		},
	)
}

// sendHeartbeat sends a heartbeat to the controller (via gRPC if available, otherwise HTTP)
func (sm *StateMachine) sendHeartbeat() {
	log := logger.WithContext(sm.config.VMID, sm.config.PoolID, sm.config.OrgID)

	// Collect VM health metrics
	vmHealth := sm.metricsCollector.CollectVMHealth()

	// Get runner state
	runnerState := events.RunnerStateIdle
	var currentJob *events.JobInfo
	if sm.runnerMonitor != nil {
		runnerState = sm.runnerMonitor.GetState()
		jobID, runID := sm.runnerMonitor.GetCurrentJob()
		if jobID != "" {
			currentJob = &events.JobInfo{
				JobID:     jobID,
				RunID:     runID,
				StartedAt: time.Now(), // TODO: Track actual start time
			}
		}
	}

	// Create heartbeat event (for MongoDB storage)
	heartbeat := events.NewHeartbeatEvent(
		sm.config.VMID,
		sm.config.PoolID,
		sm.config.OrgID,
		vmHealth,
		runnerState,
		currentJob,
	)

	// Send heartbeat via gRPC if available, otherwise fall back to HTTP
	if sm.grpcClient != nil {
		// Convert to proto format
		protoHealth := &commands.VMHealth{
			CpuUsagePercent:    vmHealth.CPULoad,
			MemoryUsagePercent: float64(vmHealth.MemoryUsed) / float64(vmHealth.MemoryTotal) * 100,
			DiskUsagePercent:   0,                                  // TODO: Calculate from disk stats
			MemoryTotalBytes:   vmHealth.MemoryTotal * 1024 * 1024, // Convert MB to bytes
			MemoryUsedBytes:    vmHealth.MemoryUsed * 1024 * 1024,
			DiskTotalBytes:     vmHealth.DiskTotal * 1024 * 1024 * 1024, // Convert GB to bytes
			DiskUsedBytes:      vmHealth.DiskUsed * 1024 * 1024 * 1024,
		}

		protoRunnerState := &commands.RunnerState{
			State:      string(runnerState),
			Configured: sm.runnerMonitor != nil,
			RunnerName: sm.config.VMID,
			Labels:     sm.runnerLabels,
		}

		var protoJobInfo *commands.JobInfo
		if currentJob != nil {
			protoJobInfo = &commands.JobInfo{
				JobId:      currentJob.JobID,
				RunId:      currentJob.RunID,
				Repository: "",        // TODO: Get from job metadata if available
				Branch:     "",        // TODO: Get from job metadata if available
				Commit:     "",        // TODO: Get from job metadata if available
				Status:     "running", // TODO: Get actual status
				StartedAt:  currentJob.StartedAt.Unix(),
			}
		}

		// Send via gRPC
		if err := sm.grpcClient.SendHeartbeat(
			sm.config.VMID,
			sm.config.PoolID,
			sm.config.OrgID,
			protoHealth,
			protoRunnerState,
			protoJobInfo,
		); err != nil {
			log.WithError(err).Warn("Failed to send heartbeat via gRPC, falling back to HTTP")
			// Fall back to HTTP
			if err := sm.controller.SendHeartbeat(sm.ctx, heartbeat); err != nil {
				log.WithError(err).Warn("Failed to send heartbeat to controller")
			}
		} else {
			log.Debug("Heartbeat sent to controller via gRPC successfully")
		}
	} else {
		// Use HTTP fallback
		if err := sm.controller.SendHeartbeat(sm.ctx, heartbeat); err != nil {
			log.WithError(err).Warn("Failed to send heartbeat to controller")
		} else {
			log.Debug("Heartbeat sent to controller via HTTP successfully")
		}
	}

	// Store heartbeat in MongoDB if enabled (non-blocking)
	if sm.mongoStorage != nil && sm.mongoStorage.IsConnected() {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			if err := sm.mongoStorage.StoreHeartbeat(ctx, heartbeat); err != nil {
				log.WithError(err).Debug("Failed to store heartbeat in MongoDB (non-blocking)")
			} else {
				log.Debug("Heartbeat stored in MongoDB successfully")
			}
		}()
	}

	sm.lastHeartbeat = time.Now()
	if sm.runnerMonitor != nil {
		sm.runnerMonitor.UpdateLastHeartbeat()
	}
}

// monitorRunner monitors the runner process and handles crashes
func (sm *StateMachine) monitorRunner(cmd *exec.Cmd) {
	log := logger.WithContext(sm.config.VMID, sm.config.PoolID, sm.config.OrgID)

	// Wait for process to exit
	err := cmd.Wait()
	if err != nil {
		log.WithError(err).Error("Runner process exited with error")

		// Send runner crashed event
		crashedEvent := &events.Event{
			Type:      events.EventTypeRunnerCrashed,
			Timestamp: time.Now(),
			VMID:      sm.config.VMID,
			PoolID:    sm.config.PoolID,
			OrgID:     sm.config.OrgID,
			Metadata: map[string]interface{}{
				"error": err.Error(),
			},
		}
		// Send runner crashed event (prefer gRPC, fallback to HTTP)
		if sm.grpcClient != nil {
			eventData := map[string]string{
				"reason": "process_exited",
			}
			if err := sm.grpcClient.SendEvent("runner_crashed", sm.config.VMID, sm.config.PoolID, sm.config.OrgID, eventData); err != nil {
				log.WithError(err).Warn("Failed to send runner crashed event via gRPC, falling back to HTTP")
				if sendErr := sm.controller.SendEvent(sm.ctx, crashedEvent); sendErr != nil {
					log.WithError(sendErr).Warn("Failed to send runner crashed event via HTTP")
				}
			} else {
				log.Debug("Runner crashed event sent via gRPC")
			}
		} else {
			if sendErr := sm.controller.SendEvent(sm.ctx, crashedEvent); sendErr != nil {
				log.WithError(sendErr).Warn("Failed to send runner crashed event")
			}
		}

		sm.Transition(StateError)
	} else {
		log.Info("Runner process exited normally")
		// Runner exited - could be normal shutdown or crash
		// TODO: Determine if it was intentional shutdown or crash
	}
}

// Shutdown gracefully shuts down the state machine
func (sm *StateMachine) Shutdown() {
	log := logger.WithContext(sm.config.VMID, sm.config.PoolID, sm.config.OrgID)
	log.Info("Shutting down state machine")

	// Stop heartbeat loop first
	sm.stopHeartbeatLoop()

	// Stop runner if running
	if sm.runnerCmd != nil && sm.runnerCmd.Process != nil {
		log.Info("Stopping GitHub Actions runner")
		runnerMgr := runner.NewManager(sm.runnerPath)
		if err := runnerMgr.StopRunner(sm.runnerCmd); err != nil {
			log.WithError(err).Warn("Error stopping runner")
		}
	}

	// Close gRPC connection if connected
	if sm.grpcClient != nil {
		if err := sm.grpcClient.Close(); err != nil {
			log.WithError(err).Warn("Error closing gRPC connection")
		} else {
			log.Debug("gRPC connection closed")
		}
	}

	// Close MongoDB connection if connected
	if sm.mongoStorage != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := sm.mongoStorage.Close(ctx); err != nil {
			log.WithError(err).Warn("Error closing MongoDB connection")
		} else {
			log.Debug("MongoDB connection closed")
		}
	}

	sm.cancel()
}
