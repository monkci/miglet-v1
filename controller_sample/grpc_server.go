package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/monkci/miglet/proto/commands"
)

const (
	grpcPort = "50051"
)

// VMConnection represents a connected VM's gRPC stream
type VMConnection struct {
	VMID      string
	PoolID    string
	OrgID     string
	Stream    commands.CommandService_StreamCommandsServer
	Connected bool
	mu        sync.RWMutex
}

// GRPCServer implements the CommandService server
type GRPCServer struct {
	commands.UnimplementedCommandServiceServer
	connections  map[string]*VMConnection // vmID -> connection
	mu           sync.RWMutex
	commandQueue map[string][]*commands.Command // vmID -> pending commands
	queueMu      sync.RWMutex
}

// NewGRPCServer creates a new gRPC server
func NewGRPCServer() *GRPCServer {
	return &GRPCServer{
		connections:  make(map[string]*VMConnection),
		commandQueue: make(map[string][]*commands.Command),
	}
}

// StreamCommands handles bidirectional streaming with MIGlet
func (s *GRPCServer) StreamCommands(stream commands.CommandService_StreamCommandsServer) error {
	var vmID string
	var vmConn *VMConnection

	// Receive messages from MIGlet
	for {
		msg, err := stream.Recv()
		if err != nil {
			if vmID != "" {
				log.Printf("Stream closed for VM %s: %v", vmID, err)
				s.removeConnection(vmID)
			}
			return err
		}

		// Process message based on type
		switch m := msg.Message.(type) {
		case *commands.MIGletMessage_Connect:
			connect := m.Connect
			vmID = connect.VmId
			poolID := connect.PoolId
			orgID := connect.OrgId

			log.Printf("VM %s (Pool: %s, Org: %s) connecting via gRPC", vmID, poolID, orgID)

			// Store connection
			vmConn = &VMConnection{
				VMID:      vmID,
				PoolID:    poolID,
				OrgID:     orgID,
				Stream:    stream,
				Connected: true,
			}
			s.addConnection(vmID, vmConn)

			// Mark VM as ready
			readyVMs[vmID] = true
			vmRegistrationSent[vmID] = false

			// Send connection acknowledgment
			ack := &commands.ControllerMessage{
				Message: &commands.ControllerMessage_ConnectAck{
					ConnectAck: &commands.ConnectAck{
						Accepted:      true,
						Message:       "Connection accepted",
						ServerVersion: "1.0.0",
					},
				},
			}
			if err := stream.Send(ack); err != nil {
				log.Printf("Failed to send connect ack to VM %s: %v", vmID, err)
				s.removeConnection(vmID)
				return err
			}

			log.Printf("Connection accepted for VM %s", vmID)

			// Send pending commands if any
			go s.sendPendingCommands(vmID)

			// Send register_runner command if VM is ready and we haven't sent it yet
			if readyVMs[vmID] && !vmRegistrationSent[vmID] {
				go s.sendRegisterRunnerCommand(vmID, poolID, orgID)
			}

		case *commands.MIGletMessage_CommandAck:
			ack := m.CommandAck
			log.Printf("Received command ack from VM %s: command_id=%s, success=%t, message=%s",
				vmID, ack.CommandId, ack.Success, ack.Message)

		case *commands.MIGletMessage_Event:
			event := m.Event
			log.Printf("Received event from VM %s: type=%s", vmID, event.Type)

			// Store event
			storeGRPCEvent(vmID, event)

			// Handle vm_started event (if sent via gRPC)
			if event.Type == "vm_started" {
				readyVMs[vmID] = true
				vmRegistrationSent[vmID] = false

				// Send register_runner command if not already sent
				if !vmRegistrationSent[vmID] {
					go s.sendRegisterRunnerCommand(vmID, event.PoolId, event.OrgId)
				}
			}

		case *commands.MIGletMessage_Heartbeat:
			heartbeat := m.Heartbeat
			log.Printf("Received heartbeat from VM %s: runner_state=%s, cpu=%.2f%%, memory=%.2f%%",
				vmID, heartbeat.RunnerState.State, heartbeat.Health.CpuUsagePercent, heartbeat.Health.MemoryUsagePercent)

			// Store heartbeat
			storeGRPCHeartbeat(vmID, heartbeat)

		case *commands.MIGletMessage_Error:
			errMsg := m.Error
			log.Printf("Received error from VM %s: code=%s, message=%s", vmID, errMsg.Code, errMsg.Message)
		}
	}
}

// addConnection adds a VM connection
func (s *GRPCServer) addConnection(vmID string, conn *VMConnection) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.connections[vmID] = conn
	log.Printf("VM %s added to connection pool (total: %d)", vmID, len(s.connections))
}

// removeConnection removes a VM connection
func (s *GRPCServer) removeConnection(vmID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.connections, vmID)
	log.Printf("VM %s removed from connection pool (total: %d)", vmID, len(s.connections))
}

// GetConnection returns a VM connection
func (s *GRPCServer) GetConnection(vmID string) *VMConnection {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.connections[vmID]
}

// SendCommand sends a command to a specific VM
func (s *GRPCServer) SendCommand(vmID string, cmd *commands.Command) error {
	conn := s.GetConnection(vmID)
	if conn == nil {
		// Queue command for later
		s.queueCommand(vmID, cmd)
		return status.Errorf(codes.NotFound, "VM %s not connected", vmID)
	}

	conn.mu.RLock()
	stream := conn.Stream
	conn.mu.RUnlock()

	if stream == nil {
		s.queueCommand(vmID, cmd)
		return status.Errorf(codes.Unavailable, "Stream not available for VM %s", vmID)
	}

	msg := &commands.ControllerMessage{
		Message: &commands.ControllerMessage_Command{
			Command: cmd,
		},
	}

	if err := stream.Send(msg); err != nil {
		log.Printf("Failed to send command to VM %s: %v", vmID, err)
		s.queueCommand(vmID, cmd)
		return err
	}

	log.Printf("Command sent to VM %s: type=%s, id=%s", vmID, cmd.Type, cmd.Id)
	return nil
}

// sendRegisterRunnerCommand sends a register_runner command to a VM
func (s *GRPCServer) sendRegisterRunnerCommand(vmID, poolID, orgID string) {
	// Wait a bit to ensure connection is established
	time.Sleep(1 * time.Second)

	conn := s.GetConnection(vmID)
	if conn == nil {
		log.Printf("VM %s not connected yet, will retry", vmID)
		// Retry after a delay
		go func() {
			time.Sleep(5 * time.Second)
			s.sendRegisterRunnerCommand(vmID, poolID, orgID)
		}()
		return
	}

	// Create register_runner command
	cmd := &commands.Command{
		Id:   fmt.Sprintf("register-%s-%d", vmID, time.Now().Unix()),
		Type: "register_runner",
		StringParams: map[string]string{
			"registration_token": registrationToken,
			"runner_url":         "https://github.com/leaffyAdmin/django_repo",
			"runner_group":       "default",
		},
		StringArrayParams: []string{"self-hosted", "monkci-miglet-tst1", "linux", "x64"},
		CreatedAt:         time.Now().Unix(),
	}

	if err := s.SendCommand(vmID, cmd); err != nil {
		log.Printf("Failed to send register_runner command to VM %s: %v", vmID, err)
		return
	}

	vmRegistrationSent[vmID] = true
	log.Printf("Register runner command sent to VM %s", vmID)
}

// sendPendingCommands sends queued commands to a VM
func (s *GRPCServer) sendPendingCommands(vmID string) {
	s.queueMu.Lock()
	pending := s.commandQueue[vmID]
	delete(s.commandQueue, vmID)
	s.queueMu.Unlock()

	for _, cmd := range pending {
		if err := s.SendCommand(vmID, cmd); err != nil {
			log.Printf("Failed to send pending command to VM %s: %v", vmID, err)
			// Re-queue if failed
			s.queueCommand(vmID, cmd)
		}
	}
}

// queueCommand queues a command for later delivery
func (s *GRPCServer) queueCommand(vmID string, cmd *commands.Command) {
	s.queueMu.Lock()
	defer s.queueMu.Unlock()
	s.commandQueue[vmID] = append(s.commandQueue[vmID], cmd)
	log.Printf("Command queued for VM %s: type=%s, id=%s (queue size: %d)", vmID, cmd.Type, cmd.Id, len(s.commandQueue[vmID]))
}

// storeGRPCEvent stores an event received via gRPC
func storeGRPCEvent(vmID string, event *commands.EventNotification) {
	// Create VM-specific directory
	vmDir := filepath.Join(dataDir, vmID)
	if err := os.MkdirAll(vmDir, 0755); err != nil {
		log.Printf("Failed to create directory for VM %s: %v", vmID, err)
		return
	}

	// Marshal event to JSON
	data, err := json.MarshalIndent(event, "", "  ")
	if err != nil {
		log.Printf("Failed to marshal event: %v", err)
		return
	}

	// Write to file
	timestamp := time.Now().Format("20060102-150405.000")
	filename := filepath.Join(vmDir, fmt.Sprintf("grpc-event-%s-%s.json", event.Type, timestamp))
	if err := os.WriteFile(filename, data, 0644); err != nil {
		log.Printf("Failed to write event file: %v", err)
	}
}

// storeGRPCHeartbeat stores a heartbeat received via gRPC
func storeGRPCHeartbeat(vmID string, heartbeat *commands.Heartbeat) {
	// Create VM-specific directory
	vmDir := filepath.Join(dataDir, vmID)
	if err := os.MkdirAll(vmDir, 0755); err != nil {
		log.Printf("Failed to create directory for VM %s: %v", vmID, err)
		return
	}

	// Marshal heartbeat to JSON
	data, err := json.MarshalIndent(heartbeat, "", "  ")
	if err != nil {
		log.Printf("Failed to marshal heartbeat: %v", err)
		return
	}

	// Write to file
	timestamp := time.Now().Format("20060102-150405.000")
	filename := filepath.Join(vmDir, fmt.Sprintf("grpc-heartbeat-%s.json", timestamp))
	if err := os.WriteFile(filename, data, 0644); err != nil {
		log.Printf("Failed to write heartbeat file: %v", err)
	}
}

// StartGRPCServer starts the gRPC server
func StartGRPCServer(grpcServer *GRPCServer) error {
	lis, err := net.Listen("tcp", ":"+grpcPort)
	if err != nil {
		return fmt.Errorf("failed to listen on port %s: %w", grpcPort, err)
	}

	s := grpc.NewServer(grpc.Creds(insecure.NewCredentials())) // TODO: Add TLS support
	commands.RegisterCommandServiceServer(s, grpcServer)

	log.Printf("gRPC server starting on port %s", grpcPort)
	if err := s.Serve(lis); err != nil {
		return fmt.Errorf("gRPC server failed: %w", err)
	}

	return nil
}
