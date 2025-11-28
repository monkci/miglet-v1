package state

import (
	"context"
	"os"
	"os/exec"
	"time"

	"github.com/monkci/miglet/pkg/config"
	"github.com/monkci/miglet/pkg/controller"
	"github.com/monkci/miglet/pkg/events"
	"github.com/monkci/miglet/pkg/logger"
	"github.com/monkci/miglet/pkg/runner"
)

// State represents the current state of MIGlet
type State string

const (
	StateInitializing         State = "initializing"
	StateWaitingForController State = "waiting_for_controller"
	StateRegisteringRunner    State = "registering_runner"
	StateIdle                 State = "idle"
	StateJobRunning           State = "job_running"
	StateDraining             State = "draining"
	StateShuttingDown         State = "shutting_down"
	StateError                State = "error"
)

// StateMachine manages MIGlet state transitions
type StateMachine struct {
	currentState       State
	config             *config.Config
	controller         *controller.Client
	eventEmitter       *events.Emitter
	ctx                context.Context
	cancel             context.CancelFunc
	vmStartedEventSent bool      // Track if VM started event has been sent
	registrationToken  string    // Registration token received from controller
	runnerURL          string    // Runner URL for registration
	runnerGroup        string    // Runner group
	runnerLabels       []string  // Runner labels
	runnerPath         string    // Path to installed runner
	runnerCmd          *exec.Cmd // Runner process command
}

// NewStateMachine creates a new state machine
func NewStateMachine(cfg *config.Config, ctrl *controller.Client, emitter *events.Emitter) *StateMachine {
	ctx, cancel := context.WithCancel(context.Background())
	return &StateMachine{
		currentState: StateInitializing,
		config:       cfg,
		controller:   ctrl,
		eventEmitter: emitter,
		ctx:          ctx,
		cancel:       cancel,
	}
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
}

// Run starts the state machine and executes state handlers
func (sm *StateMachine) Run() error {
	log := logger.WithContext(sm.config.VMID, sm.config.PoolID, sm.config.OrgID)
	log.Info("State machine starting")

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

// executeState executes the handler for the current state
func (sm *StateMachine) executeState() error {
	switch sm.currentState {
	case StateInitializing:
		return sm.handleInitializing()
	case StateWaitingForController:
		return sm.handleWaitingForController()
	case StateRegisteringRunner:
		return sm.handleRegisteringRunner()
	case StateIdle:
		// Runner is running, just wait and monitor
		// The runner process is monitored in a separate goroutine
		select {
		case <-sm.ctx.Done():
			return nil
		case <-time.After(10 * time.Second):
			// Runner is monitored in separate goroutine via cmd.Wait()
			// Just continue waiting here
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

	// Transition to waiting for controller
	sm.Transition(StateWaitingForController)
	return nil
}

// handleWaitingForController handles waiting for controller acknowledgment
// This function sends the VM started event and waits for acknowledgment
func (sm *StateMachine) handleWaitingForController() error {
	log := logger.WithContext(sm.config.VMID, sm.config.PoolID, sm.config.OrgID)

	// If we've already sent the event and are still in this state, just wait
	// (This shouldn't happen, but protects against edge cases)
	if sm.vmStartedEventSent {
		log.Debug("VM started event already sent, waiting for acknowledgment or state transition")
		select {
		case <-sm.ctx.Done():
			return nil
		case <-time.After(5 * time.Second):
			// Check if we should still be in this state
			return nil
		}
	}

	// Mark as sent to prevent duplicate sends
	sm.vmStartedEventSent = true

	// Send VM started event
	vmStartedEvent := events.NewVMStartedEvent(
		sm.config.VMID,
		sm.config.PoolID,
		sm.config.OrgID,
	)

	log.Info("Sending VM started event to controller")

	// Send event with retry
	ackReceived := false
	maxRetries := sm.config.Controller.Retry.MaxAttempts
	backoff := sm.config.Controller.Retry.InitialBackoff

	for attempt := 0; attempt < maxRetries && !ackReceived; attempt++ {
		if attempt > 0 {
			log.WithField("attempt", attempt+1).Info("Retrying VM started event")
			select {
			case <-sm.ctx.Done():
				return nil
			case <-time.After(backoff):
			}
			// Exponential backoff
			backoff = time.Duration(float64(backoff) * 1.5)
			if backoff > sm.config.Controller.Retry.MaxBackoff {
				backoff = sm.config.Controller.Retry.MaxBackoff
			}
		}

		// Send event to controller
		ackResponse, err := sm.controller.SendVMStartedEvent(sm.ctx, vmStartedEvent)
		if err != nil {
			log.WithError(err).WithField("attempt", attempt+1).Warn("Failed to send VM started event")
			continue
		}

		if ackResponse != nil && (ackResponse.Acknowledged || ackResponse.Status == "acknowledged" || ackResponse.Status == "received") {
			log.Info("Controller acknowledged VM started event")

			// Store registration token and runner config from acknowledgment
			if ackResponse.RegistrationToken != "" {
				sm.registrationToken = ackResponse.RegistrationToken
				sm.runnerURL = ackResponse.RunnerURL
				sm.runnerGroup = ackResponse.RunnerGroup
				sm.runnerLabels = ackResponse.Labels

				log.WithFields(map[string]interface{}{
					"token_length": len(ackResponse.RegistrationToken),
					"runner_url":   ackResponse.RunnerURL,
					"runner_group": ackResponse.RunnerGroup,
					"labels":       ackResponse.Labels,
				}).Info("Received registration token and runner config")
			} else {
				log.Warn("Controller acknowledged but did not provide registration token")
			}

			ackReceived = true
			break
		}
	}

	if !ackReceived {
		log.Error("Failed to get controller acknowledgment after retries")
		sm.Transition(StateError)
		return nil // Don't return error, just transition to error state
	}

	// Transition to registering runner state
	log.Info("Controller acknowledgment received, transitioning to registering runner")
	sm.Transition(StateRegisteringRunner)
	return nil
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

	// Start runner process
	log.Info("Starting runner process")
	runnerCmd, err := runnerMgr.StartRunner()
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

	// Monitor runner process in a goroutine
	go sm.monitorRunner(runnerCmd)

	// Transition to idle state (runner is running)
	log.Info("Runner registered and running, transitioning to idle")
	sm.Transition(StateIdle)
	return nil
}

// monitorRunner monitors the runner process and handles crashes
func (sm *StateMachine) monitorRunner(cmd *exec.Cmd) {
	log := logger.WithContext(sm.config.VMID, sm.config.PoolID, sm.config.OrgID)

	// Wait for process to exit
	err := cmd.Wait()
	if err != nil {
		log.WithError(err).Error("Runner process exited with error")
		// TODO: Emit runner crashed event
		// For now, transition to error state
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

	// Stop runner if running
	if sm.runnerCmd != nil && sm.runnerCmd.Process != nil {
		log.Info("Stopping GitHub Actions runner")
		runnerMgr := runner.NewManager(sm.runnerPath)
		if err := runnerMgr.StopRunner(sm.runnerCmd); err != nil {
			log.WithError(err).Warn("Error stopping runner")
		}
	}

	sm.cancel()
}
