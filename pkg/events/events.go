package events

import (
	"time"
)

// EventType represents the type of event
type EventType string

const (
	EventTypeVMStarted        EventType = "vm_started"
	EventTypeRunnerRegistered EventType = "runner_registered"
	EventTypeJobStarted       EventType = "job_started"
	EventTypeJobHeartbeat     EventType = "job_heartbeat"
	EventTypeJobCompleted     EventType = "job_completed"
	EventTypeRunnerCrashed    EventType = "runner_crashed"
	EventTypeVMShuttingDown   EventType = "vm_shutting_down"
	EventTypeError            EventType = "error"
)

// Event represents a base event structure
type Event struct {
	Type      EventType              `json:"type"`
	Timestamp time.Time              `json:"timestamp"`
	VMID      string                 `json:"vm_id"`
	PoolID    string                 `json:"pool_id"`
	OrgID     string                 `json:"org_id,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// VMStartedEvent represents a VM started event
type VMStartedEvent struct {
	Event
	MachineType string `json:"machine_type,omitempty"`
	Region      string `json:"region,omitempty"`
	CPU         int    `json:"cpu,omitempty"`
	Memory      int    `json:"memory,omitempty"` // in MB
	Disk        int    `json:"disk,omitempty"`   // in GB
	Version     string `json:"version,omitempty"`
	BuildTime   string `json:"build_time,omitempty"`
}

// NewVMStartedEvent creates a new VM started event
func NewVMStartedEvent(vmID, poolID, orgID string) *VMStartedEvent {
	return &VMStartedEvent{
		Event: Event{
			Type:      EventTypeVMStarted,
			Timestamp: time.Now(),
			VMID:      vmID,
			PoolID:    poolID,
			OrgID:     orgID,
			Metadata:  make(map[string]interface{}),
		},
		Version:   "dev",
		BuildTime: "unknown",
	}
}

// Emitter handles event emission (for future use)
type Emitter struct {
	// Future: could add buffering, batching, etc.
}

// RunnerRegisteredEvent represents a runner registered event
type RunnerRegisteredEvent struct {
	Event
	RunnerURL   string   `json:"runner_url"`
	RunnerID    string   `json:"runner_id,omitempty"`
	Labels      []string `json:"labels,omitempty"`
	RunnerGroup string   `json:"runner_group,omitempty"`
}

// NewRunnerRegisteredEvent creates a new runner registered event
func NewRunnerRegisteredEvent(vmID, poolID, orgID, runnerURL string) *RunnerRegisteredEvent {
	return &RunnerRegisteredEvent{
		Event: Event{
			Type:      EventTypeRunnerRegistered,
			Timestamp: time.Now(),
			VMID:      vmID,
			PoolID:    poolID,
			OrgID:     orgID,
			Metadata:  make(map[string]interface{}),
		},
		RunnerURL: runnerURL,
	}
}

// JobStartedEvent represents a job started event
type JobStartedEvent struct {
	Event
	JobID      string `json:"job_id"`
	RunID      string `json:"run_id"`
	Repository string `json:"repository,omitempty"`
	Branch     string `json:"branch,omitempty"`
	Commit     string `json:"commit,omitempty"`
}

// NewJobStartedEvent creates a new job started event
func NewJobStartedEvent(vmID, poolID, orgID, jobID, runID string) *JobStartedEvent {
	return &JobStartedEvent{
		Event: Event{
			Type:      EventTypeJobStarted,
			Timestamp: time.Now(),
			VMID:      vmID,
			PoolID:    poolID,
			OrgID:     orgID,
			Metadata:  make(map[string]interface{}),
		},
		JobID: jobID,
		RunID: runID,
	}
}

// JobCompletedEvent represents a job completed event
type JobCompletedEvent struct {
	Event
	JobID    string `json:"job_id"`
	RunID    string `json:"run_id"`
	Success  bool   `json:"success"`
	ExitCode int    `json:"exit_code,omitempty"`
	Duration int64  `json:"duration,omitempty"` // in seconds
}

// NewJobCompletedEvent creates a new job completed event
func NewJobCompletedEvent(vmID, poolID, orgID, jobID, runID string, success bool) *JobCompletedEvent {
	return &JobCompletedEvent{
		Event: Event{
			Type:      EventTypeJobCompleted,
			Timestamp: time.Now(),
			VMID:      vmID,
			PoolID:    poolID,
			OrgID:     orgID,
			Metadata:  make(map[string]interface{}),
		},
		JobID:   jobID,
		RunID:   runID,
		Success: success,
	}
}

// HeartbeatEvent represents a heartbeat event with VM and runner state
type HeartbeatEvent struct {
	Event
	VMHealth    VMHealth    `json:"vm_health"`
	RunnerState RunnerState `json:"runner_state"`
	CurrentJob  *JobInfo    `json:"current_job,omitempty"`
}

// VMHealth represents VM health metrics
type VMHealth struct {
	CPULoad     float64 `json:"cpu_load,omitempty"`
	MemoryUsed  int64   `json:"memory_used,omitempty"`  // in MB
	MemoryTotal int64   `json:"memory_total,omitempty"` // in MB
	DiskUsed    int64   `json:"disk_used,omitempty"`    // in GB
	DiskTotal   int64   `json:"disk_total,omitempty"`   // in GB
}

// RunnerState represents runner state
type RunnerState string

const (
	RunnerStateIdle    RunnerState = "idle"
	RunnerStateRunning RunnerState = "running"
	RunnerStateOffline RunnerState = "offline"
	RunnerStateStopped RunnerState = "stopped"
)

// JobInfo represents current job information
type JobInfo struct {
	JobID      string    `json:"job_id"`
	RunID      string    `json:"run_id"`
	Repository string    `json:"repository,omitempty"`
	StartedAt  time.Time `json:"started_at,omitempty"`
}

// NewHeartbeatEvent creates a new heartbeat event
func NewHeartbeatEvent(vmID, poolID, orgID string, vmHealth VMHealth, runnerState RunnerState, currentJob *JobInfo) *HeartbeatEvent {
	return &HeartbeatEvent{
		Event: Event{
			Type:      EventTypeJobHeartbeat,
			Timestamp: time.Now(),
			VMID:      vmID,
			PoolID:    poolID,
			OrgID:     orgID,
			Metadata:  make(map[string]interface{}),
		},
		VMHealth:    vmHealth,
		RunnerState: runnerState,
		CurrentJob:  currentJob,
	}
}

// NewEmitter creates a new event emitter
func NewEmitter() *Emitter {
	return &Emitter{}
}
