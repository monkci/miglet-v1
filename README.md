# MIGlet - Technical Architecture

## Overview

MIGlet is a lightweight Go agent that runs inside every MonkCI VM (MIG instance) to bootstrap, register, and manage GitHub Actions runners. It acts as the bridge between the VM lifecycle, GitHub Actions service, and the MIG Controller.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                      MIGlet Agent                            │
├─────────────────────────────────────────────────────────────┤
│                                                               │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │   Bootstrap  │  │   State      │  │   Runner     │      │
│  │   Manager    │→ │   Machine    │← │   Manager    │      │
│  └──────────────┘  └──────────────┘  └──────────────┘      │
│         │                │                    │              │
│         └────────────────┼────────────────────┘              │
│                          │                                    │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │   Config     │  │   Event      │  │   Metrics    │      │
│  │   Manager    │  │   Emitter    │  │   Collector  │      │
│  └──────────────┘  └──────────────┘  └──────────────┘      │
│         │                │                    │              │
└─────────┼────────────────┼────────────────────┼──────────────┘
          │                │                    │
          ▼                ▼                    ▼
    ┌──────────┐    ┌──────────┐        ┌──────────┐
    │ Metadata │    │   MIG    │        │  GitHub  │
    │  Server  │    │Controller│        │  Actions │
    └──────────┘    └──────────┘        └──────────┘
```

## Core Components

### 1. Bootstrap Manager (`pkg/bootstrap/`)
- **Responsibility**: Initialize VM, validate prerequisites, load configuration
- **Key Functions**:
  - Read configuration from metadata server, env vars, or config file
  - Validate required fields (pool ID, VM ID, org ID, endpoints, credentials)
  - Verify prerequisites (Docker, network, mounted volumes)
  - Fail fast with structured error events on misconfiguration

### 2. State Machine (`pkg/state/`)
- **Responsibility**: Manage MIGlet lifecycle states and transitions
- **States**:
  - `Initializing` → Reading config, verifying environment
  - `WaitingForControllerAck` → VM started event sent, awaiting acknowledgment
  - `RegisteringRunner` → Contacting GitHub to register runner
  - `Idle` → Runner registered, no job running, sending heartbeats
  - `JobRunning` → Runner executing job, collecting metrics
  - `Draining` → Stop accepting new jobs, finish current job
  - `ShuttingDown` → Final events, preparing for shutdown
  - `Error` → Terminal error, signaling controller
- **Implementation**: State pattern with context and transition handlers

### 3. Runner Manager (`pkg/runner/`)
- **Responsibility**: Manage GitHub Actions runner lifecycle
- **Key Functions**:
  - Register ephemeral runner with GitHub (labels, runner group)
  - Monitor runner process (start, stop, health)
  - Detect job lifecycle events (started, running, completed, failed)
  - Handle runner crashes and recovery
  - Deregister runner on shutdown

### 4. MIG Controller Client (`pkg/controller/`)
- **Responsibility**: Communication with MIG Controller service
- **Key Functions**:
  - Request GitHub runner registration token (does NOT store GitHub App credentials)
  - Send VM lifecycle events (started, registered, shutting down)
  - Send job events (started, completed, failed, crashed)
  - Send periodic heartbeats with metrics
  - Receive and handle commands (drain, shutdown, config update)
  - Retry logic with exponential backoff
  - Authentication and request signing

### 5. Event Emitter (`pkg/events/`)
- **Responsibility**: Structured event generation and emission
- **Event Types**:
  - `VMStarted` - VM bootstrapped successfully
  - `RunnerRegistered` - Runner registered with GitHub
  - `JobStarted` - Job execution began
  - `JobHeartbeat` - Periodic job status update
  - `JobCompleted` - Job finished (success/failure/cancelled)
  - `RunnerCrashed` - Runner process terminated abnormally
  - `VMShuttingDown` - VM preparing for shutdown
  - `Error` - Error events with classification
- **Format**: Structured JSON with correlation IDs, timestamps, metadata

### 6. Metrics Collector (`pkg/metrics/`)
- **Responsibility**: Collect system and job metrics
- **Metrics**:
  - VM health: CPU load, memory usage, disk usage
  - Runner state: idle/busy, current job ID
  - Job metrics: duration, resource usage, exit codes
  - Network connectivity status
- **Collection**: Periodic sampling (configurable interval, default 10-30s)

### 7. Config Manager (`pkg/config/`)
- **Responsibility**: Configuration loading, validation, and updates
- **Sources** (priority order):
  1. Metadata server (GCP/AWS instance metadata)
  2. Environment variables
  3. Config file (fallback)
- **Configuration Structure**:
  ```go
  type Config struct {
      // Identifiers
      PoolID      string
      VMID        string
      OrgID       string
      
      // MIG Controller
      ControllerEndpoint string
      ControllerAuth     AuthConfig
      
      // GitHub Runner (NO App credentials - only metadata)
      GitHubConfig       GitHubConfig  // org, runner_group, labels only
      
      // Behavior
      HeartbeatInterval  time.Duration
      RetryConfig        RetryConfig
      ShutdownTimeout    time.Duration
      
      // Feature Flags
      Features           FeatureFlags
  }
  ```

### 8. Command Handler (`pkg/commands/`)
- **Responsibility**: Process commands from MIG Controller
- **Command Types**:
  - `Drain` - Stop accepting new jobs
  - `Shutdown` - Shutdown immediately or after current job
  - `UpdateConfig` - Update runtime configuration
  - `SetLogLevel` - Change logging verbosity
- **Implementation**: HTTP/gRPC endpoint or polling mechanism

### 9. Logger (`pkg/logger/`)
- **Responsibility**: Structured logging with correlation
- **Features**:
  - Structured JSON logging
  - Correlation IDs (VM ID, job ID, run ID)
  - Log levels (debug, info, warning, error)
  - Remote log level switching
  - Sensitive data redaction
  - Output to stdout/stderr (for log agents)

## State Machine Implementation

```go
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

type StateMachine struct {
    currentState State
    context      *StateContext
    handlers     map[State]StateHandler
}

type StateContext struct {
    Config        *config.Config
    Runner        *runner.Runner
    Controller    *controller.Client
    EventEmitter  *events.Emitter
    Metrics       *metrics.Collector
    JobContext    *JobContext
}

type JobContext struct {
    JobID       string
    RunID       string
    Repository  string
    Branch      string
    Commit      string
    StartedAt   time.Time
    Status      string
}
```

## API Interfaces

### MIG Controller API

#### Registration Token Request (Outbound)
- `POST /api/v1/vms/{vm_id}/registration-token`
  - Body: `{org_id, pool_id, runner_group, labels}`
  - Response: `{registration_token, expires_at, runner_url, runner_group, labels}`
  - Auth: Bearer token or mutual TLS
  - **Note**: MIG Controller uses GitHub App credentials to generate token

#### Events (Outbound)
- `POST /api/v1/vms/{vm_id}/events`
  - Body: Event payload (VMStarted, RunnerRegistered, JobStarted, etc.)
  - Auth: Bearer token or mutual TLS

#### Heartbeats (Outbound)
- `POST /api/v1/vms/{vm_id}/heartbeat`
  - Body: Heartbeat with metrics
  - Frequency: Configurable (default 10-30s)

#### Commands (Inbound)
- `GET /api/v1/vms/{vm_id}/commands` (polling) OR
- `WebSocket /api/v1/vms/{vm_id}/commands` (push)
  - Commands: Drain, Shutdown, UpdateConfig, SetLogLevel

### GitHub Actions Runner
- Use official `actions/runner` binary or SDK
- Registration: `./config.sh --url <org-url> --token <token> --ephemeral --labels <labels>`
- Lifecycle: Monitor runner process and log output for job events

#### Registration Token Flow
- MIGlet requests registration token from MIG Controller (not directly from GitHub)
- MIG Controller uses GitHub App credentials to generate short-lived registration token
- MIGlet receives token and uses it for runner registration
- Token is single-use and expires quickly (typically 1 hour)

## GitHub App Authentication Flow

### Overview
MIGlet **does NOT** store or manage GitHub App credentials (App ID, private key, installation ID). These sensitive credentials are managed by the **MIG Controller**, which acts as a secure token service.

### Architecture

```
┌─────────────┐         ┌──────────────┐         ┌─────────────┐
│   MIGlet    │────────▶│ MIG Controller│────────▶│   GitHub    │
│   (VM)      │ Request │   (Service)   │  App    │     API     │
│             │◀────────│              │◀────────│             │
│             │  Token  │              │  JWT    │             │
└─────────────┘         └──────────────┘         └─────────────┘
                              │
                              │ Stores:
                              │ - App ID
                              │ - Private Key
                              │ - Installation ID
                              │ - Org/Repo mappings
```

### Flow Details

#### 1. GitHub App Credentials Storage (MIG Controller)
The MIG Controller securely stores:
- **GitHub App ID**: Unique identifier for the GitHub App
- **Private Key**: RSA private key for JWT generation
- **Installation ID**: Per-organization installation ID
- **Organization/Repository mappings**: Which orgs/repos can use which installations

**Storage Location Options:**
- Secret management service (GCP Secret Manager, AWS Secrets Manager, HashiCorp Vault)
- Encrypted database with key rotation
- Kubernetes secrets (if controller runs in K8s)

#### 2. MIGlet Registration Token Request

**API Endpoint:**
```
POST /api/v1/vms/{vm_id}/registration-token
```

**Request:**
```json
{
  "org_id": "org-789",
  "pool_id": "pool-123",
  "runner_group": "default",
  "labels": ["self-hosted", "linux", "x64"]
}
```

**Response:**
```json
{
  "registration_token": "AHTXXXXXXXXXXXXXXXXXXXX",
  "expires_at": "2024-01-15T10:30:00Z",
  "runner_url": "https://github.com/myorg",
  "runner_group": "default",
  "labels": ["self-hosted", "linux", "x64"]
}
```

#### 3. MIG Controller Token Generation

**Process:**
1. MIG Controller receives request from MIGlet
2. Validates VM identity and pool membership
3. Looks up GitHub App credentials for the organization
4. Generates JWT using App ID and private key:
   ```go
   // Pseudo-code
   jwt := githubapp.GenerateJWT(
       appID: config.GitHubAppID,
       privateKey: config.PrivateKey,
       expiration: time.Now().Add(10 * time.Minute),
   )
   ```
5. Exchanges JWT for installation access token:
   ```go
   installToken := githubapp.GetInstallationToken(
       jwt: jwt,
       installationID: config.InstallationID,
   )
   ```
6. Creates runner registration token using installation token:
   ```go
   regToken := githubapi.CreateRegistrationToken(
       org: orgName,
       runnerGroup: runnerGroup,
       accessToken: installToken,
   )
   ```
7. Returns registration token to MIGlet

#### 4. MIGlet Runner Registration

**Process:**
1. MIGlet receives registration token from MIG Controller
2. Uses token to register ephemeral runner:
   ```bash
   ./config.sh \
     --url https://github.com/myorg \
     --token AHTXXXXXXXXXXXXXXXXXXXX \
     --ephemeral \
     --labels self-hosted,linux,x64 \
     --runnergroup default
   ```
3. Runner starts and connects to GitHub Actions
4. Token is single-use and expires after registration

### Implementation in MIG Controller Client

```go
// pkg/controller/client.go

type RegistrationTokenRequest struct {
    OrgID       string   `json:"org_id"`
    PoolID      string   `json:"pool_id"`
    RunnerGroup string   `json:"runner_group"`
    Labels      []string `json:"labels"`
}

type RegistrationTokenResponse struct {
    RegistrationToken string    `json:"registration_token"`
    ExpiresAt        time.Time `json:"expires_at"`
    RunnerURL        string    `json:"runner_url"`
    RunnerGroup      string    `json:"runner_group"`
    Labels           []string  `json:"labels"`
}

func (c *Client) RequestRegistrationToken(ctx context.Context, req *RegistrationTokenRequest) (*RegistrationTokenResponse, error) {
    endpoint := fmt.Sprintf("%s/api/v1/vms/%s/registration-token", c.endpoint, c.vmID)
    
    resp, err := c.httpClient.Post(ctx, endpoint, req)
    if err != nil {
        return nil, fmt.Errorf("failed to request registration token: %w", err)
    }
    
    var tokenResp RegistrationTokenResponse
    if err := json.Unmarshal(resp.Body, &tokenResp); err != nil {
        return nil, fmt.Errorf("failed to parse response: %w", err)
    }
    
    return &tokenResp, nil
}
```

### Security Considerations

1. **Token Lifetime**: Registration tokens should be short-lived (1 hour max)
2. **Single Use**: Tokens are consumed immediately upon runner registration
3. **No Credential Storage in MIGlet**: MIGlet never sees App ID, private key, or installation ID
4. **Request Authentication**: MIGlet must authenticate to MIG Controller (bearer token, mTLS)
5. **Token Rotation**: MIG Controller should rotate GitHub App credentials periodically
6. **Audit Logging**: All token requests should be logged for security auditing

### Error Handling

**Token Request Failures:**
- **401 Unauthorized**: MIGlet authentication failed → Retry with backoff
- **403 Forbidden**: VM not authorized for this org/pool → Emit error, request shutdown
- **404 Not Found**: GitHub App not configured for org → Emit error, request shutdown
- **429 Rate Limited**: Too many requests → Exponential backoff
- **500 Server Error**: Controller issue → Retry with backoff

**Token Expiration:**
- If registration token expires before use, MIGlet requests a new one
- Maximum retries: 3 attempts before emitting error

### Alternative: Direct Token from Metadata (Optional)

For environments where MIG Controller is unavailable during bootstrap, an alternative flow:

1. MIG Controller pre-generates registration tokens
2. Tokens stored in VM metadata service (GCP/AWS)
3. MIGlet reads token from metadata during bootstrap
4. Token still expires and requires refresh via MIG Controller

**Configuration:**
```yaml
github:
  token_source: "controller"  # or "metadata"
  metadata_path: "/metadata/github/registration_token"  # if using metadata
```

## Configuration Structure

```yaml
# miglet.yaml (example)
pool_id: "pool-123"
vm_id: "vm-456"
org_id: "org-789"

controller:
  endpoint: "https://controller.monkci.io"
  auth:
    type: "bearer"
    token_path: "/var/run/secrets/controller-token"
  timeout: 30s
  retry:
    max_attempts: 5
    initial_backoff: 1s
    max_backoff: 30s

github:
  # Note: MIGlet does NOT store GitHub App credentials (App ID, private key, installation ID)
  # These are managed by MIG Controller, which generates registration tokens on demand
  org: "myorg"
  runner_group: "default"
  labels: ["self-hosted", "linux", "x64"]
  token_source: "controller"  # Request token from MIG Controller (recommended)
  # token_source: "metadata"   # Alternative: read pre-generated token from metadata
  registration_timeout: 60s

heartbeat:
  interval: 15s
  timeout: 60s

shutdown:
  grace_period: 30s
  force_after: 5m

logging:
  level: "info"
  format: "json"
  redact_secrets: true

metrics:
  collection_interval: 10s
  include_disk: true
  include_network: true
```

## Error Handling Strategy

### Error Classification
```go
type ErrorCategory string

const (
    ErrorCategoryTransient    ErrorCategory = "transient"     // Retry
    ErrorCategoryPermanent    ErrorCategory = "permanent"     // Fail fast
    ErrorCategoryMisconfig    ErrorCategory = "misconfig"     // Fail fast
    ErrorCategoryInfrastructure ErrorCategory = "infrastructure" // Retry with backoff
    ErrorCategoryUser          ErrorCategory = "user"          // Report, don't retry
)
```

### Retry Strategy
- **Transient errors**: Exponential backoff (1s → 30s max), max 5 attempts
- **Permanent errors**: Fail immediately, emit error event, request shutdown
- **Misconfiguration**: Fail immediately, emit error event, stop retrying

## Security

### Authentication
- **MIG Controller**: Bearer token from metadata server or mounted secret
- **GitHub**: Registration token from MIG Controller (short-lived, single-use)
  - **Important**: MIGlet does NOT store GitHub App credentials (App ID, private key, installation ID)
  - MIG Controller manages GitHub App credentials and generates registration tokens on demand
- Mutual TLS option for controller communication

### Credential Management
- Never log sensitive data (tokens, passwords)
- Use secure metadata service or mounted secrets
- Rotate credentials via controller commands

### Isolation
- Run with minimal privileges
- Use service account with least privilege
- Isolate runner process from MIGlet process

## Observability

### Logging
- Structured JSON logs to stdout/stderr
- Correlation IDs: `vm_id`, `pool_id`, `org_id`, `job_id`, `run_id`
- Log levels: DEBUG, INFO, WARN, ERROR
- Component tagging: `bootstrap`, `registration`, `runner`, `heartbeat`, `shutdown`

### Metrics
- Exposed via events to MIG Controller
- Optional: Local metrics endpoint (Prometheus format)
- Metrics include: CPU, memory, disk, network, job duration, exit codes

### Tracing
- Correlation IDs propagated across all events
- Support for distributed tracing (OpenTelemetry compatible)

## Implementation Phases

### Phase 1: Core Infrastructure
- [ ] Project structure and Go module setup
- [ ] Configuration management
- [ ] Logger with structured output
- [ ] Basic state machine
- [ ] Error handling framework

### Phase 2: Bootstrap & Registration
- [ ] Bootstrap manager
- [ ] MIG Controller client (events, heartbeats)
- [ ] GitHub runner registration
- [ ] State transitions (Initializing → Idle)

### Phase 3: Job Lifecycle
- [ ] Runner manager
- [ ] Job detection and monitoring
- [ ] Job event emission
- [ ] Metrics collection

### Phase 4: Command Handling
- [ ] Command receiver (HTTP/gRPC)
- [ ] Command handlers (drain, shutdown, config update)
- [ ] Graceful shutdown

### Phase 5: Resilience & Polish
- [ ] Retry logic with backoff
- [ ] Health checks
- [ ] Crash detection and recovery
- [ ] Comprehensive error classification
- [ ] Integration tests

### Phase 6: Production Readiness
- [ ] Performance optimization
- [ ] Resource usage profiling
- [ ] Security audit
- [ ] Documentation
- [ ] Deployment automation

## Project Structure

```
miglet-v1/
├── cmd/
│   └── miglet/
│       └── main.go              # Entry point
├── pkg/
│   ├── bootstrap/               # Bootstrap manager
│   ├── config/                  # Configuration management
│   ├── controller/              # MIG Controller client
│   ├── events/                  # Event emission
│   ├── logger/                  # Structured logging
│   ├── metrics/                 # Metrics collection
│   ├── runner/                  # GitHub runner management
│   ├── state/                   # State machine
│   ├── commands/                # Command handling
│   └── security/                # Security utilities
├── internal/
│   ├── metadata/                # Metadata server client
│   └── utils/                   # Internal utilities
├── api/
│   └── v1/                      # API definitions (protobuf/OpenAPI)
├── configs/
│   └── miglet.yaml.example      # Example configuration
├── scripts/
│   └── build.sh                 # Build scripts
├── tests/
│   ├── integration/             # Integration tests
│   └── unit/                    # Unit tests
├── docs/
│   └── architecture.md          # Detailed architecture docs
├── go.mod
├── go.sum
└── README.md                    # This file
```

## Development Setup

### Prerequisites
- Go 1.21+ 
- Docker (for runner testing)
- Access to metadata server (or mock)

### Build
```bash
go build -o bin/miglet ./cmd/miglet
```

### Run
```bash
./bin/miglet --config /path/to/config.yaml
```

### Test
```bash
go test ./...
```

## Dependencies (Initial)

- `github.com/google/go-github` - GitHub API client
- `github.com/sirupsen/logrus` or `go.uber.org/zap` - Structured logging
- `golang.org/x/oauth2` - OAuth2 for GitHub
- `google.golang.org/grpc` - gRPC client (if using gRPC)
- `github.com/spf13/cobra` - CLI framework
- `github.com/spf13/viper` - Configuration management

## Non-Functional Requirements

- **Startup Time**: < 1 second overhead (excluding external calls)
- **Memory**: < 50MB baseline, < 100MB under load
- **CPU**: < 1% baseline, < 5% under load
- **Reliability**: Run for 24+ hours without memory leaks
- **Observability**: 100% job traceability with correlation IDs

## Open Questions / Decisions Needed

1. **Communication Protocol**: HTTP REST vs gRPC vs WebSocket for controller communication?
2. **Runner Binary**: Use official `actions/runner` binary or Go SDK?
3. **Job Detection**: Parse runner logs or use GitHub API polling?
4. **Command Channel**: Polling vs push (WebSocket/gRPC streaming)?
5. **Metrics Format**: Custom events vs Prometheus vs OpenTelemetry?
6. **Deployment**: Single binary vs container vs systemd service?

## Next Steps

1. Set up Go module and project structure
2. Implement configuration management
3. Build basic state machine
4. Implement bootstrap flow
5. Add MIG Controller client
6. Integrate GitHub runner registration
7. Add job lifecycle monitoring
8. Implement command handling
9. Add comprehensive error handling
10. Write integration tests

