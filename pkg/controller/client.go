package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/monkci/miglet/pkg/config"
	"github.com/monkci/miglet/pkg/events"
	"github.com/monkci/miglet/pkg/logger"
)

// Client handles communication with MIG Controller
type Client struct {
	endpoint   string
	httpClient *http.Client
	vmID       string
	authToken  string
}

// NewClient creates a new MIG Controller client
func NewClient(cfg *config.Config) (*Client, error) {
	client := &Client{
		endpoint: cfg.Controller.Endpoint,
		httpClient: &http.Client{
			Timeout: cfg.Controller.Timeout,
		},
		vmID: cfg.VMID,
	}

	// Load auth token if configured
	if cfg.Controller.Auth.Type == "bearer" && cfg.Controller.Auth.TokenPath != "" {
		token, err := os.ReadFile(cfg.Controller.Auth.TokenPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read auth token: %w", err)
		}
		client.authToken = string(bytes.TrimSpace(token))
	}

	return client, nil
}

// VMStartedAckResponse represents the acknowledgment response from controller
type VMStartedAckResponse struct {
	Status            string    `json:"status"`
	VMID              string    `json:"vm_id"`
	Message           string    `json:"message"`
	Acknowledged      bool      `json:"acknowledged,omitempty"`
	RegistrationToken string    `json:"registration_token,omitempty"` // Token for runner config
	RunnerURL         string    `json:"runner_url,omitempty"`
	RunnerGroup       string    `json:"runner_group,omitempty"`
	Labels            []string  `json:"labels,omitempty"`
	ExpiresAt         time.Time `json:"expires_at,omitempty"`
}

// SendVMStartedEvent sends a VM started event to the controller
// Returns acknowledgment response if successful, nil otherwise
func (c *Client) SendVMStartedEvent(ctx context.Context, event *events.VMStartedEvent) (*VMStartedAckResponse, error) {
	log := logger.WithContext(c.vmID, event.PoolID, event.OrgID)

	// Marshal event to JSON
	body, err := json.Marshal(event)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal event: %w", err)
	}

	// Create request
	url := fmt.Sprintf("%s/api/v1/vms/%s/events", c.endpoint, c.vmID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}

	// Send request
	log.WithField("url", url).Debug("Sending VM started event")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(respBody))
	}

	// Parse response
	var ackResponse VMStartedAckResponse
	if err := json.Unmarshal(respBody, &ackResponse); err != nil {
		// If response doesn't match expected format, check if status is OK
		log.WithField("response", string(respBody)).Debug("Response doesn't match expected format")
		// Try to parse as basic acknowledgment
		var basicAck struct {
			Status       string `json:"status"`
			Acknowledged bool   `json:"acknowledged"`
		}
		if json.Unmarshal(respBody, &basicAck) == nil {
			if basicAck.Acknowledged || basicAck.Status == "received" || basicAck.Status == "acknowledged" {
				// Return minimal response if basic acknowledgment works
				return &VMStartedAckResponse{
					Status:       basicAck.Status,
					Acknowledged: true,
				}, nil
			}
		}
		return nil, fmt.Errorf("failed to parse acknowledgment response: %w", err)
	}

	log.WithField("response body", string(respBody)).Debug("Received acknowledgment response body")
	log.WithField("response", ackResponse).Debug("Received acknowledgment response")

	// Check if explicitly acknowledged
	if ackResponse.Acknowledged || ackResponse.Status == "received" || ackResponse.Status == "acknowledged" {
		if ackResponse.RegistrationToken != "" {
			log.WithField("token_length", len(ackResponse.RegistrationToken)).Debug("Received registration token in acknowledgment")
		}
		return &ackResponse, nil
	}

	return nil, fmt.Errorf("controller did not acknowledge: %s", ackResponse.Message)
}

// RequestRegistrationToken requests a registration token from the controller
func (c *Client) RequestRegistrationToken(ctx context.Context, req *RegistrationTokenRequest) (*RegistrationTokenResponse, error) {
	log := logger.WithContext(c.vmID, req.PoolID, "")

	// Marshal request to JSON
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create request
	url := fmt.Sprintf("%s/api/v1/vms/%s/registration-token", c.endpoint, c.vmID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if c.authToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.authToken)
	}

	// Send request
	log.WithField("url", url).Debug("Requesting registration token")
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(respBody))
	}

	// Parse response
	var tokenResp RegistrationTokenResponse
	if err := json.Unmarshal(respBody, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &tokenResp, nil
}

// RegistrationTokenRequest represents a registration token request
type RegistrationTokenRequest struct {
	OrgID       string   `json:"org_id"`
	PoolID      string   `json:"pool_id"`
	RunnerGroup string   `json:"runner_group"`
	Labels      []string `json:"labels"`
}

// RegistrationTokenResponse represents a registration token response
type RegistrationTokenResponse struct {
	RegistrationToken string    `json:"registration_token"`
	ExpiresAt         time.Time `json:"expires_at"`
	RunnerURL         string    `json:"runner_url"`
	RunnerGroup       string    `json:"runner_group"`
	Labels            []string  `json:"labels"`
}

// SendEvent sends an event to the controller
func (c *Client) SendEvent(ctx context.Context, event interface{}) error {
	log := logger.WithContext(c.vmID, "", "")

	// Marshal event to JSON
	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	// Create request
	url := fmt.Sprintf("%s/api/v1/vms/%s/events", c.endpoint, c.vmID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}

	// Send request
	log.Debug("Sending event to controller")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// SendHeartbeat sends a heartbeat to the controller
func (c *Client) SendHeartbeat(ctx context.Context, heartbeat *events.HeartbeatEvent) error {
	log := logger.WithContext(c.vmID, heartbeat.PoolID, heartbeat.OrgID)

	// Marshal heartbeat to JSON
	body, err := json.Marshal(heartbeat)
	if err != nil {
		return fmt.Errorf("failed to marshal heartbeat: %w", err)
	}

	// Create request
	url := fmt.Sprintf("%s/api/v1/vms/%s/heartbeat", c.endpoint, c.vmID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}

	// Send request
	log.Debug("Sending heartbeat to controller")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// Command represents a command from the controller
type Command struct {
	ID         string                 `json:"id"`
	Type       string                 `json:"type"` // register_runner, drain, shutdown, update_config, set_log_level
	Parameters map[string]interface{} `json:"parameters"`
	CreatedAt  time.Time              `json:"created_at"`
}

// CommandsResponse represents the response from polling commands
type CommandsResponse struct {
	Commands []Command `json:"commands"`
	VMID     string    `json:"vm_id"`
}

// RegisterRunnerCommandParams represents parameters for register_runner command
type RegisterRunnerCommandParams struct {
	RegistrationToken string   `json:"registration_token"`
	RunnerURL         string   `json:"runner_url"`
	RunnerGroup       string   `json:"runner_group"`
	Labels            []string `json:"labels"`
	ExpiresAt         string   `json:"expires_at,omitempty"`
}

// PollCommands polls the controller for pending commands
func (c *Client) PollCommands(ctx context.Context) (*CommandsResponse, error) {
	log := logger.WithContext(c.vmID, "", "")

	// Create request
	url := fmt.Sprintf("%s/api/v1/vms/%s/commands", c.endpoint, c.vmID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}

	// Send request
	log.Debug("Polling for commands from controller")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(respBody))
	}

	// Parse response
	var commandsResp CommandsResponse
	if err := json.Unmarshal(respBody, &commandsResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &commandsResp, nil
}
