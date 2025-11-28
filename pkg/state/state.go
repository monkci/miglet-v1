package state

import (
	"context"
	"time"

	"github.com/monkci/miglet/pkg/config"
	"github.com/monkci/miglet/pkg/controller"
	"github.com/monkci/miglet/pkg/events"
	"github.com/monkci/miglet/pkg/logger"
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
	vmStartedEventSent bool // Track if VM started event has been sent
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
		// TODO: Phase 3
		return nil
	case StateIdle:
		// In Phase 2, idle state just waits
		// Phase 3 will implement runner registration
		// For now, sleep to prevent tight loop
		select {
		case <-sm.ctx.Done():
			return nil
		case <-time.After(5 * time.Second):
			// Just wait, don't do anything yet
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

	// Validate prerequisites (Docker, network, etc.)
	// For now, just transition to waiting for controller
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
		ack, err := sm.controller.SendVMStartedEvent(sm.ctx, vmStartedEvent)
		if err != nil {
			log.WithError(err).WithField("attempt", attempt+1).Warn("Failed to send VM started event")
			continue
		}

		if ack {
			log.Info("Controller acknowledged VM started event")
			ackReceived = true
			break
		}
	}

	if !ackReceived {
		log.Error("Failed to get controller acknowledgment after retries")
		sm.Transition(StateError)
		return nil // Don't return error, just transition to error state
	}

	// Transition to next state (will be RegisteringRunner in Phase 3)
	// For now, transition to Idle to indicate we're waiting for next phase
	log.Info("Controller acknowledgment received, transitioning to idle (Phase 2 complete)")
	sm.Transition(StateIdle)

	// In Idle state, we'll just wait (Phase 3 will implement runner registration)
	return nil
}

// Shutdown gracefully shuts down the state machine
func (sm *StateMachine) Shutdown() {
	log := logger.WithContext(sm.config.VMID, sm.config.PoolID, sm.config.OrgID)
	log.Info("Shutting down state machine")
	sm.cancel()
}
