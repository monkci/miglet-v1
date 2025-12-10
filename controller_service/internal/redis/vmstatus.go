package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/monkci/mig-controller/internal/config"
	"github.com/monkci/mig-controller/pkg/logger"
)

// VMInfraState represents the infrastructure state from GCloud
type VMInfraState string

const (
	VMInfraRunning      VMInfraState = "RUNNING"
	VMInfraStopped      VMInfraState = "TERMINATED"
	VMInfraStaging      VMInfraState = "STAGING"
	VMInfraStopping     VMInfraState = "STOPPING"
	VMInfraProvisioning VMInfraState = "PROVISIONING"
	VMInfraUnknown      VMInfraState = "UNKNOWN"
)

// MigletState represents the MIGlet state machine state
type MigletState string

const (
	MigletStateInitializing      MigletState = "initializing"
	MigletStateConnecting        MigletState = "connecting"
	MigletStateReady             MigletState = "ready"
	MigletStateRegisteringRunner MigletState = "registering_runner"
	MigletStateIdle              MigletState = "idle"
	MigletStateJobRunning        MigletState = "job_running"
	MigletStateDraining          MigletState = "draining"
	MigletStateShuttingDown      MigletState = "shutting_down"
	MigletStateError             MigletState = "error"
	MigletStateUnknown           MigletState = "unknown"
)

// RunnerState represents the GitHub Actions runner state
type RunnerState string

const (
	RunnerStateIdle    RunnerState = "idle"
	RunnerStateRunning RunnerState = "running"
	RunnerStateOffline RunnerState = "offline"
)

// EffectiveState represents the combined effective state
type EffectiveState string

const (
	EffectiveStateStopped    EffectiveState = "STOPPED"
	EffectiveStateStarting   EffectiveState = "STARTING"
	EffectiveStateBooting    EffectiveState = "BOOTING"
	EffectiveStateConnecting EffectiveState = "CONNECTING"
	EffectiveStateReady      EffectiveState = "READY"
	EffectiveStateIdle       EffectiveState = "IDLE"
	EffectiveStateBusy       EffectiveState = "BUSY"
	EffectiveStateError      EffectiveState = "ERROR"
	EffectiveStateStopping   EffectiveState = "STOPPING"
	EffectiveStateUnknown    EffectiveState = "UNKNOWN"
)

// VMStatus represents the full status of a VM
type VMStatus struct {
	VMID           string         `json:"vm_id"`
	PoolID         string         `json:"pool_id"`
	Zone           string         `json:"zone"`
	InfraState     VMInfraState   `json:"infra_state"`
	MigletState    MigletState    `json:"miglet_state"`
	RunnerState    RunnerState    `json:"runner_state"`
	EffectiveState EffectiveState `json:"effective_state"`
	CurrentJobID   string         `json:"current_job_id,omitempty"`
	CPUUsage       float64        `json:"cpu_usage"`
	MemoryUsage    float64        `json:"memory_usage"`
	LastHeartbeat  time.Time      `json:"last_heartbeat"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	IsConnected    bool           `json:"is_connected"` // gRPC connection status
}

// VMStatusStore handles VM status persistence in Redis
type VMStatusStore struct {
	client *redis.Client
	poolID string
}

// NewVMStatusStore creates a new VM status store
func NewVMStatusStore(cfg *config.RedisInstanceConfig, poolID string) (*VMStatusStore, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	log := logger.WithComponent("vm_status_store")
	log.Info("Connected to VM Status Redis")

	return &VMStatusStore{
		client: client,
		poolID: poolID,
	}, nil
}

// Close closes the Redis connection
func (s *VMStatusStore) Close() error {
	return s.client.Close()
}

// Get retrieves VM status by ID
func (s *VMStatusStore) Get(ctx context.Context, vmID string) (*VMStatus, error) {
	key := fmt.Sprintf("vms:%s:%s", s.poolID, vmID)
	data, err := s.client.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get VM status: %w", err)
	}

	var status VMStatus
	if err := json.Unmarshal(data, &status); err != nil {
		return nil, fmt.Errorf("failed to unmarshal VM status: %w", err)
	}

	return &status, nil
}

// Update updates VM status
func (s *VMStatusStore) Update(ctx context.Context, status *VMStatus) error {
	status.UpdatedAt = time.Now()
	status.EffectiveState = s.calculateEffectiveState(status)

	key := fmt.Sprintf("vms:%s:%s", s.poolID, status.VMID)
	data, err := json.Marshal(status)
	if err != nil {
		return fmt.Errorf("failed to marshal VM status: %w", err)
	}

	// Store with 24-hour expiry (will be refreshed by heartbeats)
	if err := s.client.Set(ctx, key, data, 24*time.Hour).Err(); err != nil {
		return fmt.Errorf("failed to save VM status: %w", err)
	}

	// Update state index sets
	if err := s.updateStateIndex(ctx, status); err != nil {
		return fmt.Errorf("failed to update state index: %w", err)
	}

	return nil
}

// UpdateFromInfra updates VM status from GCloud infrastructure data
func (s *VMStatusStore) UpdateFromInfra(ctx context.Context, vmID, zone string, infraState VMInfraState) error {
	status, err := s.Get(ctx, vmID)
	if err != nil {
		return err
	}

	if status == nil {
		status = &VMStatus{
			VMID:        vmID,
			PoolID:      s.poolID,
			Zone:        zone,
			MigletState: MigletStateUnknown,
			RunnerState: RunnerStateOffline,
			CreatedAt:   time.Now(),
		}
	}

	status.InfraState = infraState
	status.Zone = zone

	return s.Update(ctx, status)
}

// UpdateFromHeartbeat updates VM status from MIGlet heartbeat
func (s *VMStatusStore) UpdateFromHeartbeat(ctx context.Context, vmID string, migletState MigletState, runnerState RunnerState, cpuUsage, memoryUsage float64, currentJobID string) error {
	status, err := s.Get(ctx, vmID)
	if err != nil {
		return err
	}

	if status == nil {
		status = &VMStatus{
			VMID:       vmID,
			PoolID:     s.poolID,
			InfraState: VMInfraRunning, // Assume running if we get heartbeat
			CreatedAt:  time.Now(),
		}
	}

	status.MigletState = migletState
	status.RunnerState = runnerState
	status.CPUUsage = cpuUsage
	status.MemoryUsage = memoryUsage
	status.CurrentJobID = currentJobID
	status.LastHeartbeat = time.Now()
	status.IsConnected = true

	return s.Update(ctx, status)
}

// SetConnected sets the gRPC connection status
func (s *VMStatusStore) SetConnected(ctx context.Context, vmID string, connected bool) error {
	status, err := s.Get(ctx, vmID)
	if err != nil {
		return err
	}
	if status == nil {
		return nil // VM not tracked yet
	}

	status.IsConnected = connected
	if !connected {
		status.MigletState = MigletStateUnknown
	}

	return s.Update(ctx, status)
}

// Delete removes VM status
func (s *VMStatusStore) Delete(ctx context.Context, vmID string) error {
	key := fmt.Sprintf("vms:%s:%s", s.poolID, vmID)

	// Get current state to clean up index
	status, _ := s.Get(ctx, vmID)
	if status != nil {
		// Remove from all state indexes
		for _, state := range []EffectiveState{
			EffectiveStateStopped, EffectiveStateStarting, EffectiveStateBooting,
			EffectiveStateConnecting, EffectiveStateReady, EffectiveStateIdle,
			EffectiveStateBusy, EffectiveStateError, EffectiveStateStopping,
		} {
			indexKey := fmt.Sprintf("vms:by_state:%s:%s", s.poolID, state)
			s.client.SRem(ctx, indexKey, vmID)
		}
	}

	return s.client.Del(ctx, key).Err()
}

// GetAll returns all VM statuses for the pool
func (s *VMStatusStore) GetAll(ctx context.Context) ([]*VMStatus, error) {
	pattern := fmt.Sprintf("vms:%s:*", s.poolID)
	keys, err := s.client.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to list VM keys: %w", err)
	}

	var statuses []*VMStatus
	for _, key := range keys {
		data, err := s.client.Get(ctx, key).Bytes()
		if err != nil {
			continue
		}

		var status VMStatus
		if err := json.Unmarshal(data, &status); err != nil {
			continue
		}
		statuses = append(statuses, &status)
	}

	return statuses, nil
}

// GetByEffectiveState returns VMs with a specific effective state
func (s *VMStatusStore) GetByEffectiveState(ctx context.Context, state EffectiveState) ([]*VMStatus, error) {
	indexKey := fmt.Sprintf("vms:by_state:%s:%s", s.poolID, state)
	vmIDs, err := s.client.SMembers(ctx, indexKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get VMs by state: %w", err)
	}

	var statuses []*VMStatus
	for _, vmID := range vmIDs {
		status, err := s.Get(ctx, vmID)
		if err != nil || status == nil {
			continue
		}
		statuses = append(statuses, status)
	}

	return statuses, nil
}

// GetFirstReady returns the first ready VM (for job assignment)
func (s *VMStatusStore) GetFirstReady(ctx context.Context) (*VMStatus, error) {
	// First try "ready" state (MIGlet is ready but runner not started)
	statuses, err := s.GetByEffectiveState(ctx, EffectiveStateReady)
	if err != nil {
		return nil, err
	}
	if len(statuses) > 0 {
		return statuses[0], nil
	}

	// Then try "idle" state (runner is idle)
	statuses, err = s.GetByEffectiveState(ctx, EffectiveStateIdle)
	if err != nil {
		return nil, err
	}
	if len(statuses) > 0 {
		return statuses[0], nil
	}

	return nil, nil
}

// GetFirstStopped returns the first stopped VM (for starting)
func (s *VMStatusStore) GetFirstStopped(ctx context.Context) (*VMStatus, error) {
	statuses, err := s.GetByEffectiveState(ctx, EffectiveStateStopped)
	if err != nil {
		return nil, err
	}
	if len(statuses) > 0 {
		return statuses[0], nil
	}
	return nil, nil
}

// CountByState returns count of VMs in each state
func (s *VMStatusStore) CountByState(ctx context.Context) (map[EffectiveState]int64, error) {
	counts := make(map[EffectiveState]int64)

	for _, state := range []EffectiveState{
		EffectiveStateStopped, EffectiveStateStarting, EffectiveStateBooting,
		EffectiveStateConnecting, EffectiveStateReady, EffectiveStateIdle,
		EffectiveStateBusy, EffectiveStateError, EffectiveStateStopping,
	} {
		indexKey := fmt.Sprintf("vms:by_state:%s:%s", s.poolID, state)
		count, err := s.client.SCard(ctx, indexKey).Result()
		if err != nil {
			continue
		}
		counts[state] = count
	}

	return counts, nil
}

// GetStats returns pool statistics
func (s *VMStatusStore) GetStats(ctx context.Context) (*PoolStats, error) {
	counts, err := s.CountByState(ctx)
	if err != nil {
		return nil, err
	}

	stats := &PoolStats{
		PoolID:      s.poolID,
		TotalVMs:    0,
		RunningVMs:  0,
		ReadyVMs:    counts[EffectiveStateReady] + counts[EffectiveStateIdle],
		BusyVMs:     counts[EffectiveStateBusy],
		StoppedVMs:  counts[EffectiveStateStopped],
		ErrorVMs:    counts[EffectiveStateError],
		StartingVMs: counts[EffectiveStateStarting] + counts[EffectiveStateBooting] + counts[EffectiveStateConnecting],
	}

	for _, count := range counts {
		stats.TotalVMs += count
	}
	stats.RunningVMs = stats.TotalVMs - stats.StoppedVMs

	return stats, nil
}

// PoolStats represents pool statistics
type PoolStats struct {
	PoolID      string `json:"pool_id"`
	TotalVMs    int64  `json:"total_vms"`
	RunningVMs  int64  `json:"running_vms"`
	ReadyVMs    int64  `json:"ready_vms"`
	BusyVMs     int64  `json:"busy_vms"`
	StoppedVMs  int64  `json:"stopped_vms"`
	ErrorVMs    int64  `json:"error_vms"`
	StartingVMs int64  `json:"starting_vms"`
}

// calculateEffectiveState determines the effective state based on infra and miglet states
func (s *VMStatusStore) calculateEffectiveState(status *VMStatus) EffectiveState {
	switch status.InfraState {
	case VMInfraStopped:
		return EffectiveStateStopped
	case VMInfraStaging, VMInfraProvisioning:
		return EffectiveStateStarting
	case VMInfraStopping:
		return EffectiveStateStopping
	case VMInfraRunning:
		switch status.MigletState {
		case MigletStateInitializing:
			return EffectiveStateBooting
		case MigletStateConnecting:
			return EffectiveStateConnecting
		case MigletStateReady:
			return EffectiveStateReady
		case MigletStateRegisteringRunner:
			return EffectiveStateConnecting
		case MigletStateIdle:
			return EffectiveStateIdle
		case MigletStateJobRunning:
			return EffectiveStateBusy
		case MigletStateDraining:
			return EffectiveStateBusy
		case MigletStateError:
			return EffectiveStateError
		case MigletStateShuttingDown:
			return EffectiveStateStopping
		default:
			return EffectiveStateUnknown
		}
	default:
		return EffectiveStateUnknown
	}
}

// updateStateIndex updates the state index sets in Redis
func (s *VMStatusStore) updateStateIndex(ctx context.Context, status *VMStatus) error {
	// Remove from all state indexes first
	for _, state := range []EffectiveState{
		EffectiveStateStopped, EffectiveStateStarting, EffectiveStateBooting,
		EffectiveStateConnecting, EffectiveStateReady, EffectiveStateIdle,
		EffectiveStateBusy, EffectiveStateError, EffectiveStateStopping, EffectiveStateUnknown,
	} {
		indexKey := fmt.Sprintf("vms:by_state:%s:%s", s.poolID, state)
		s.client.SRem(ctx, indexKey, status.VMID)
	}

	// Add to current state index
	indexKey := fmt.Sprintf("vms:by_state:%s:%s", s.poolID, status.EffectiveState)
	return s.client.SAdd(ctx, indexKey, status.VMID).Err()
}

