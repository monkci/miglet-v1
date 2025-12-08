package controller

import (
	"context"
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"

	"github.com/monkci/miglet/pkg/config"
	"github.com/monkci/miglet/pkg/logger"
	"github.com/monkci/miglet/proto/commands"
)

// GRPCClient handles gRPC bidirectional streaming with the controller
type GRPCClient struct {
	config          *config.Config
	conn            *grpc.ClientConn
	client          commands.CommandServiceClient
	stream          commands.CommandService_StreamCommandsClient
	mu              sync.RWMutex
	connected       bool
	shouldReconnect bool
	commandCh       chan *commands.Command
	ctx             context.Context
	cancel          context.CancelFunc
}

// NewGRPCClient creates a new gRPC client for command streaming
func NewGRPCClient(cfg *config.Config) (*GRPCClient, error) {
	ctx, cancel := context.WithCancel(context.Background())

	client := &GRPCClient{
		config:          cfg,
		commandCh:       make(chan *commands.Command, 10),
		ctx:             ctx,
		cancel:          cancel,
		shouldReconnect: true,
	}

	return client, nil
}

// Connect establishes the gRPC connection and starts streaming
func (c *GRPCClient) Connect() error {
	log := logger.WithContext(c.config.VMID, c.config.PoolID, c.config.OrgID)

	// Get gRPC endpoint from config
	grpcEndpoint := c.config.Controller.GRPCEndpoint
	if grpcEndpoint == "" {
		// Fallback: derive from HTTP endpoint
		if c.config.Controller.Endpoint == "" {
			return fmt.Errorf("controller gRPC endpoint not configured")
		}
		grpcEndpoint = convertHTTPToGRPC(c.config.Controller.Endpoint)
	}

	log.WithField("endpoint", grpcEndpoint).Info("Connecting to controller via gRPC")

	// Create gRPC connection with keepalive
	conn, err := grpc.NewClient(
		grpcEndpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()), // TODO: Add TLS support
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                10 * time.Second,
			Timeout:             3 * time.Second,
			PermitWithoutStream: true,
		}),
	)
	if err != nil {
		return fmt.Errorf("failed to create gRPC connection: %w", err)
	}

	c.mu.Lock()
	c.conn = conn
	c.client = commands.NewCommandServiceClient(conn)
	c.mu.Unlock()

	// Start streaming in goroutine
	go c.streamLoop()

	return nil
}

// createStream creates a new gRPC stream
func (c *GRPCClient) createStream() (commands.CommandService_StreamCommandsClient, error) {
	c.mu.RLock()
	client := c.client
	ctx := c.ctx
	c.mu.RUnlock()

	if client == nil {
		return nil, fmt.Errorf("gRPC client not initialized")
	}

	stream, err := client.StreamCommands(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create stream: %w", err)
	}

	return stream, nil
}

// streamLoop handles the bidirectional streaming
func (c *GRPCClient) streamLoop() {
	log := logger.WithContext(c.config.VMID, c.config.PoolID, c.config.OrgID)

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		// Check if we have a connection
		c.mu.RLock()
		conn := c.conn
		client := c.client
		stream := c.stream
		c.mu.RUnlock()

		// Reconnect only if connection is nil (not just because connected is false)
		if conn == nil || client == nil {
			if err := c.reconnect(); err != nil {
				log.WithError(err).Warn("Failed to reconnect, retrying in 5s")
				time.Sleep(5 * time.Second)
				continue
			}
		}

		// Create stream if needed
		if stream == nil {
			newStream, err := c.createStream()
			if err != nil {
				log.WithError(err).Warn("Failed to create stream, retrying in 5s")
				// Mark as needing reconnection
				c.mu.Lock()
				c.connected = false
				c.stream = nil
				c.mu.Unlock()
				time.Sleep(5 * time.Second)
				continue
			}

			c.mu.Lock()
			c.stream = newStream
			c.mu.Unlock()
			stream = newStream
			log.Info("gRPC stream created successfully")
		}

		// Send connect request
		connectReq := &commands.ConnectRequest{
			VmId:    c.config.VMID,
			PoolId:  c.config.PoolID,
			OrgId:   c.config.OrgID,
			Version: "dev", // TODO: Get from build info
		}

		connectMsg := &commands.MIGletMessage{
			Message: &commands.MIGletMessage_Connect{
				Connect: connectReq,
			},
		}

		if err := stream.Send(connectMsg); err != nil {
			log.WithError(err).Error("Failed to send connect request")
			c.mu.Lock()
			c.connected = false
			c.stream = nil
			c.mu.Unlock()
			time.Sleep(5 * time.Second)
			continue
		}

		log.Info("Connect request sent, waiting for acknowledgment")

		// Receive messages in a loop
		for {
			select {
			case <-c.ctx.Done():
				return
			default:
			}

			// Receive message from stream
			msg, err := stream.Recv()
			if err != nil {
				log.WithError(err).Warn("Stream receive error, reconnecting")
				c.mu.Lock()
				c.connected = false
				c.stream = nil
				c.mu.Unlock()
				break // Break inner loop to reconnect
			}

			// Process received message
			switch m := msg.Message.(type) {
			case *commands.ControllerMessage_ConnectAck:
				ack := m.ConnectAck
				if ack.Accepted {
					log.WithField("server_version", ack.ServerVersion).Info("Connection accepted by controller")
					c.mu.Lock()
					c.connected = true
					c.mu.Unlock()
				} else {
					log.WithField("message", ack.Message).Error("Connection rejected by controller")
					c.mu.Lock()
					c.connected = false
					c.mu.Unlock()
					return
				}
			case *commands.ControllerMessage_Command:
				cmd := m.Command
				// Received a command - send to channel
				log.WithFields(map[string]interface{}{
					"command_id": cmd.Id,
					"type":       cmd.Type,
				}).Info("Received command from controller")

				select {
				case c.commandCh <- cmd:
				default:
					log.Warn("Command channel full, dropping command")
				}
			case *commands.ControllerMessage_Error:
				errMsg := m.Error
				log.WithFields(map[string]interface{}{
					"code":    errMsg.Code,
					"message": errMsg.Message,
				}).Error("Received error from controller")
			}
		}
	}
}

// reconnect attempts to reconnect to the controller
func (c *GRPCClient) reconnect() error {
	log := logger.WithContext(c.config.VMID, c.config.PoolID, c.config.OrgID)

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		c.conn.Close()
	}

	endpoint := convertHTTPToGRPC(c.config.Controller.Endpoint)
	log.WithField("endpoint", endpoint).Info("Reconnecting to controller")

	conn, err := grpc.NewClient(
		endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                10 * time.Second,
			Timeout:             3 * time.Second,
			PermitWithoutStream: true,
		}),
	)
	if err != nil {
		return fmt.Errorf("failed to reconnect: %w", err)
	}

	c.conn = conn
	c.connected = false // Will be set to true after connect ack

	return nil
}

// GetCommandChannel returns the channel for receiving commands
func (c *GRPCClient) GetCommandChannel() <-chan *commands.Command {
	return c.commandCh
}

// SendCommandAck sends a command acknowledgment to the controller
func (c *GRPCClient) SendCommandAck(commandID string, success bool, message string, result map[string]string) error {
	c.mu.RLock()
	stream := c.stream
	c.mu.RUnlock()

	if stream == nil {
		return fmt.Errorf("not connected")
	}

	ack := &commands.CommandAck{
		CommandId: commandID,
		Success:   success,
		Message:   message,
		Result:    result,
	}

	msg := &commands.MIGletMessage{
		Message: &commands.MIGletMessage_CommandAck{
			CommandAck: ack,
		},
	}

	return stream.Send(msg)
}

// SendEvent sends an event notification to the controller
func (c *GRPCClient) SendEvent(eventType, vmID, poolID, orgID string, data map[string]string) error {
	c.mu.RLock()
	stream := c.stream
	c.mu.RUnlock()

	if stream == nil {
		return fmt.Errorf("not connected")
	}

	event := &commands.EventNotification{
		Type:      eventType,
		VmId:      vmID,
		PoolId:    poolID,
		OrgId:     orgID,
		Data:      data,
		Timestamp: time.Now().Unix(),
	}

	msg := &commands.MIGletMessage{
		Message: &commands.MIGletMessage_Event{
			Event: event,
		},
	}

	return stream.Send(msg)
}

// SendHeartbeat sends a heartbeat to the controller
func (c *GRPCClient) SendHeartbeat(vmID, poolID, orgID string, health *commands.VMHealth, runnerState *commands.RunnerState, jobInfo *commands.JobInfo) error {
	c.mu.RLock()
	stream := c.stream
	c.mu.RUnlock()

	if stream == nil {
		return fmt.Errorf("not connected")
	}

	heartbeat := &commands.Heartbeat{
		VmId:        vmID,
		PoolId:      poolID,
		OrgId:       orgID,
		Health:      health,
		RunnerState: runnerState,
		CurrentJob:  jobInfo,
		Timestamp:   time.Now().Unix(),
	}

	msg := &commands.MIGletMessage{
		Message: &commands.MIGletMessage_Heartbeat{
			Heartbeat: heartbeat,
		},
	}

	return stream.Send(msg)
}

// Close closes the gRPC connection
func (c *GRPCClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.shouldReconnect = false
	c.cancel()

	if c.stream != nil {
		c.stream.CloseSend()
	}

	if c.conn != nil {
		return c.conn.Close()
	}

	return nil
}

// convertHTTPToGRPC converts HTTP endpoint to gRPC endpoint
func convertHTTPToGRPC(endpoint string) string {
	// Remove http:// or https://
	if len(endpoint) > 7 && endpoint[:7] == "http://" {
		endpoint = endpoint[7:]
	} else if len(endpoint) > 8 && endpoint[:8] == "https://" {
		endpoint = endpoint[8:]
	}

	// Add default port if not specified
	// gRPC typically uses port 50051 or 9090
	// For now, assume same host but different port convention
	// Controller should expose gRPC on a different port (e.g., :50051)
	// Or use the same port if it supports both HTTP and gRPC

	// For development, if endpoint is localhost:8080, try localhost:50051
	// In production, this should be configured separately
	if endpoint == "localhost:8080" || endpoint == "127.0.0.1:8080" {
		return "localhost:50051"
	}

	// Try to replace :8080 with :50051 as a convention
	// Better: controller should have separate gRPC endpoint config
	return endpoint
}
