package events

import (
	"time"
)

// EventType represents the type of event
type EventType string

const (
	EventTypeVMStarted       EventType = "vm_started"
	EventTypeRunnerRegistered EventType = "runner_registered"
	EventTypeJobStarted      EventType = "job_started"
	EventTypeJobHeartbeat    EventType = "job_heartbeat"
	EventTypeJobCompleted    EventType = "job_completed"
	EventTypeRunnerCrashed   EventType = "runner_crashed"
	EventTypeVMShuttingDown  EventType = "vm_shutting_down"
	EventTypeError           EventType = "error"
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
	MachineType string   `json:"machine_type,omitempty"`
	Region      string   `json:"region,omitempty"`
	CPU         int      `json:"cpu,omitempty"`
	Memory      int      `json:"memory,omitempty"` // in MB
	Disk        int      `json:"disk,omitempty"`   // in GB
	Version     string   `json:"version,omitempty"`
	BuildTime   string   `json:"build_time,omitempty"`
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

// NewEmitter creates a new event emitter
func NewEmitter() *Emitter {
	return &Emitter{}
}

