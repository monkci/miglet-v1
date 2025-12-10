package grpc

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"

	"github.com/monkci/mig-controller/internal/config"
	"github.com/monkci/mig-controller/internal/redis"
	"github.com/monkci/mig-controller/pkg/logger"
	"github.com/monkci/mig-controller/proto/commands"
)

// MIGletConnection represents an active connection from a MIGlet
type MIGletConnection struct {
	VMID        string
	PoolID      string
	OrgID       string
	Stream      commands.CommandService_StreamCommandsServer
	MigletState string
	RunnerState string
	ConnectedAt time.Time
	LastSeen    time.Time
}

// PendingCommand represents a command waiting to be sent to a MIGlet
type PendingCommand struct {
	Command   *commands.Command
	ResultCh  chan *commands.CommandAck
	CreatedAt time.Time
}

// Server implements the gRPC CommandService
type Server struct {
	commands.UnimplementedCommandServiceServer
	cfg *config.Config

	// Active connections
	connections     map[string]*MIGletConnection // vmID -> connection
	connectionsLock sync.RWMutex

	// Pending commands (waiting for MIGlet to connect)
	pendingCommands     map[string][]*PendingCommand // vmID -> commands
	pendingCommandsLock sync.Mutex

	// Command acknowledgments
	commandAcks     map[string]chan *commands.CommandAck // commandID -> ack channel
	commandAcksLock sync.Mutex

	// VM status store
	vmStore *redis.VMStatusStore

	// Callbacks
	onHeartbeat func(vmID string, heartbeat *commands.Heartbeat)
	onEvent     func(vmID string, event *commands.EventNotification)
}

// NewServer creates a new gRPC server
func NewServer(cfg *config.Config, vmStore *redis.VMStatusStore) *Server {
	return &Server{
		cfg:             cfg,
		connections:     make(map[string]*MIGletConnection),
		pendingCommands: make(map[string][]*PendingCommand),
		commandAcks:     make(map[string]chan *commands.CommandAck),
		vmStore:         vmStore,
	}
}

// SetHeartbeatCallback sets the callback for heartbeat events
func (s *Server) SetHeartbeatCallback(cb func(vmID string, heartbeat *commands.Heartbeat)) {
	s.onHeartbeat = cb
}

// SetEventCallback sets the callback for event notifications
func (s *Server) SetEventCallback(cb func(vmID string, event *commands.EventNotification)) {
	s.onEvent = cb
}

// Start starts the gRPC server
func (s *Server) Start(port int) error {
	log := logger.WithComponent("grpc_server")

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	grpcServer := grpc.NewServer(
		grpc.Creds(insecure.NewCredentials()), // TODO: Add TLS
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle:     15 * time.Minute,
			MaxConnectionAge:      30 * time.Minute,
			MaxConnectionAgeGrace: 5 * time.Second,
			Time:                  5 * time.Second,
			Timeout:               1 * time.Second,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             5 * time.Second,
			PermitWithoutStream: true,
		}),
	)

	commands.RegisterCommandServiceServer(grpcServer, s)

	log.WithField("port", port).Info("gRPC server starting")
	return grpcServer.Serve(lis)
}

// StreamCommands handles bidirectional streaming with MIGlets
func (s *Server) StreamCommands(stream commands.CommandService_StreamCommandsServer) error {
	log := logger.WithComponent("grpc_server")

	var vmID, poolID, orgID string
	var connected bool

	defer func() {
		if connected {
			s.handleDisconnect(vmID)
		}
	}()

	for {
		msg, err := stream.Recv()
		if err != nil {
			if connected {
				log.WithError(err).WithField("vm_id", vmID).Warn("Stream error")
			}
			return err
		}

		switch m := msg.Message.(type) {
		case *commands.MIGletMessage_Connect:
			vmID = m.Connect.VmId
			poolID = m.Connect.PoolId
			orgID = m.Connect.OrgId

			log.WithFields(map[string]interface{}{
				"vm_id":   vmID,
				"pool_id": poolID,
				"version": m.Connect.Version,
			}).Info("MIGlet connected")

			// Register connection
			s.handleConnect(vmID, poolID, orgID, stream)
			connected = true

			// Send connect acknowledgment
			ack := &commands.ControllerMessage{
				Message: &commands.ControllerMessage_ConnectAck{
					ConnectAck: &commands.ConnectAck{
						Accepted:      true,
						Message:       "Connected to MIG Controller",
						ServerVersion: "1.0.0",
					},
				},
			}
			if err := stream.Send(ack); err != nil {
				log.WithError(err).Warn("Failed to send connect ack")
				return err
			}

			// Send any pending commands
			s.sendPendingCommands(vmID, stream)

		case *commands.MIGletMessage_Heartbeat:
			if !connected {
				continue
			}
			s.handleHeartbeat(vmID, m.Heartbeat)

		case *commands.MIGletMessage_CommandAck:
			if !connected {
				continue
			}
			s.handleCommandAck(m.CommandAck)

		case *commands.MIGletMessage_Event:
			if !connected {
				continue
			}
			s.handleEvent(vmID, m.Event)

		case *commands.MIGletMessage_Error:
			if !connected {
				continue
			}
			log.WithFields(map[string]interface{}{
				"vm_id": vmID,
				"code":  m.Error.Code,
				"msg":   m.Error.Message,
			}).Warn("Received error from MIGlet")
		}
	}
}

// handleConnect registers a new connection
func (s *Server) handleConnect(vmID, poolID, orgID string, stream commands.CommandService_StreamCommandsServer) {
	s.connectionsLock.Lock()
	defer s.connectionsLock.Unlock()

	s.connections[vmID] = &MIGletConnection{
		VMID:        vmID,
		PoolID:      poolID,
		OrgID:       orgID,
		Stream:      stream,
		ConnectedAt: time.Now(),
		LastSeen:    time.Now(),
	}

	// Update VM status
	ctx := context.Background()
	s.vmStore.SetConnected(ctx, vmID, true)
}

// handleDisconnect handles a disconnection
func (s *Server) handleDisconnect(vmID string) {
	log := logger.WithVM(vmID, s.cfg.Pool.ID)
	log.Info("MIGlet disconnected")

	s.connectionsLock.Lock()
	delete(s.connections, vmID)
	s.connectionsLock.Unlock()

	// Update VM status
	ctx := context.Background()
	s.vmStore.SetConnected(ctx, vmID, false)
}

// handleHeartbeat processes a heartbeat message
func (s *Server) handleHeartbeat(vmID string, heartbeat *commands.Heartbeat) {
	// Update last seen
	s.connectionsLock.Lock()
	if conn, ok := s.connections[vmID]; ok {
		conn.LastSeen = time.Now()
		conn.MigletState = heartbeat.MigletState
		if heartbeat.RunnerState != nil {
			conn.RunnerState = heartbeat.RunnerState.State
		}
	}
	s.connectionsLock.Unlock()

	// Update VM status in Redis
	ctx := context.Background()
	var cpuUsage, memUsage float64
	if heartbeat.Health != nil {
		cpuUsage = heartbeat.Health.CpuUsagePercent
		memUsage = heartbeat.Health.MemoryUsagePercent
	}

	var runnerState redis.RunnerState = redis.RunnerStateOffline
	if heartbeat.RunnerState != nil {
		runnerState = redis.RunnerState(heartbeat.RunnerState.State)
	}

	var currentJobID string
	if heartbeat.CurrentJob != nil {
		currentJobID = heartbeat.CurrentJob.JobId
	}

	s.vmStore.UpdateFromHeartbeat(
		ctx,
		vmID,
		redis.MigletState(heartbeat.MigletState),
		runnerState,
		cpuUsage,
		memUsage,
		currentJobID,
	)

	// Call callback if set
	if s.onHeartbeat != nil {
		s.onHeartbeat(vmID, heartbeat)
	}
}

// handleCommandAck processes a command acknowledgment
func (s *Server) handleCommandAck(ack *commands.CommandAck) {
	log := logger.WithComponent("grpc_server")

	s.commandAcksLock.Lock()
	ch, ok := s.commandAcks[ack.CommandId]
	if ok {
		delete(s.commandAcks, ack.CommandId)
	}
	s.commandAcksLock.Unlock()

	if ok && ch != nil {
		select {
		case ch <- ack:
		default:
			log.WithField("command_id", ack.CommandId).Warn("Ack channel full or closed")
		}
	}
}

// handleEvent processes an event notification
func (s *Server) handleEvent(vmID string, event *commands.EventNotification) {
	log := logger.WithVM(vmID, s.cfg.Pool.ID)
	log.WithField("event_type", event.Type).Info("Received event from MIGlet")

	if s.onEvent != nil {
		s.onEvent(vmID, event)
	}
}

// SendCommand sends a command to a specific VM
func (s *Server) SendCommand(vmID string, cmd *commands.Command, timeout time.Duration) (*commands.CommandAck, error) {
	log := logger.WithVM(vmID, s.cfg.Pool.ID)

	s.connectionsLock.RLock()
	conn, connected := s.connections[vmID]
	s.connectionsLock.RUnlock()

	if !connected {
		// Queue the command for when MIGlet connects
		return nil, s.queueCommand(vmID, cmd, timeout)
	}

	// Create ack channel
	ackCh := make(chan *commands.CommandAck, 1)
	s.commandAcksLock.Lock()
	s.commandAcks[cmd.Id] = ackCh
	s.commandAcksLock.Unlock()

	// Send command
	msg := &commands.ControllerMessage{
		Message: &commands.ControllerMessage_Command{
			Command: cmd,
		},
	}

	if err := conn.Stream.Send(msg); err != nil {
		s.commandAcksLock.Lock()
		delete(s.commandAcks, cmd.Id)
		s.commandAcksLock.Unlock()
		return nil, fmt.Errorf("failed to send command: %w", err)
	}

	log.WithField("command_id", cmd.Id).Info("Command sent")

	// Wait for acknowledgment
	select {
	case ack := <-ackCh:
		return ack, nil
	case <-time.After(timeout):
		s.commandAcksLock.Lock()
		delete(s.commandAcks, cmd.Id)
		s.commandAcksLock.Unlock()
		return nil, fmt.Errorf("command timeout")
	}
}

// queueCommand queues a command for later delivery
func (s *Server) queueCommand(vmID string, cmd *commands.Command, timeout time.Duration) error {
	pending := &PendingCommand{
		Command:   cmd,
		ResultCh:  make(chan *commands.CommandAck, 1),
		CreatedAt: time.Now(),
	}

	s.pendingCommandsLock.Lock()
	s.pendingCommands[vmID] = append(s.pendingCommands[vmID], pending)
	s.pendingCommandsLock.Unlock()

	return fmt.Errorf("command queued - VM not connected")
}

// sendPendingCommands sends any pending commands to a newly connected MIGlet
func (s *Server) sendPendingCommands(vmID string, stream commands.CommandService_StreamCommandsServer) {
	log := logger.WithVM(vmID, s.cfg.Pool.ID)

	s.pendingCommandsLock.Lock()
	pending := s.pendingCommands[vmID]
	delete(s.pendingCommands, vmID)
	s.pendingCommandsLock.Unlock()

	for _, p := range pending {
		// Skip expired commands
		if time.Since(p.CreatedAt) > 5*time.Minute {
			continue
		}

		msg := &commands.ControllerMessage{
			Message: &commands.ControllerMessage_Command{
				Command: p.Command,
			},
		}

		if err := stream.Send(msg); err != nil {
			log.WithError(err).WithField("command_id", p.Command.Id).Warn("Failed to send pending command")
			continue
		}

		log.WithField("command_id", p.Command.Id).Info("Sent pending command")
	}
}

// IsConnected checks if a VM is connected
func (s *Server) IsConnected(vmID string) bool {
	s.connectionsLock.RLock()
	defer s.connectionsLock.RUnlock()
	_, ok := s.connections[vmID]
	return ok
}

// GetConnectedVMs returns list of connected VM IDs
func (s *Server) GetConnectedVMs() []string {
	s.connectionsLock.RLock()
	defer s.connectionsLock.RUnlock()

	vmIDs := make([]string, 0, len(s.connections))
	for vmID := range s.connections {
		vmIDs = append(vmIDs, vmID)
	}
	return vmIDs
}

// GetConnectionCount returns the number of active connections
func (s *Server) GetConnectionCount() int {
	s.connectionsLock.RLock()
	defer s.connectionsLock.RUnlock()
	return len(s.connections)
}

// WaitForState waits for a VM to reach a specific state
func (s *Server) WaitForState(ctx context.Context, vmID string, targetState redis.MigletState, timeout time.Duration) error {
	log := logger.WithVM(vmID, s.cfg.Pool.ID)

	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("timeout waiting for state %s", targetState)
			}

			status, err := s.vmStore.Get(ctx, vmID)
			if err != nil {
				continue
			}
			if status != nil && status.MigletState == targetState {
				log.WithField("state", targetState).Info("VM reached target state")
				return nil
			}
		}
	}
}

