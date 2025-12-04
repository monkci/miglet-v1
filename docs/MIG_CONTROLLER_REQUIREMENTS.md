# MIG Controller Requirements

## Overview

The MIG Controller is a service that manages MIGlet instances, handles GitHub App authentication, and coordinates VM lifecycle. It acts as the control plane for the MonkCI infrastructure.

## Core Responsibilities

### R1. VM Lifecycle Management
- **R1.1** Receive and acknowledge VM started events
- **R1.2** Track VM state (initializing, idle, running, draining, shutting down)
- **R1.3** Monitor VM health via heartbeats
- **R1.4** Detect unhealthy VMs (missing heartbeats)
- **R1.5** Send commands to VMs (drain, shutdown, config updates)

### R2. GitHub App Credential Management
- **R2.1** Securely store GitHub App credentials:
  - App ID
  - Private Key (RSA)
  - Installation ID (per organization)
  - Organization/Repository mappings
- **R2.2** Generate JWT tokens using App ID + Private Key
- **R2.3** Exchange JWT for installation access tokens
- **R2.4** Create runner registration tokens using installation tokens
- **R2.5** Handle token expiration and rotation

### R3. Registration Token Service
- **R3.1** Receive registration token requests from MIGlets
- **R3.2** Validate VM identity and pool membership
- **R3.3** Look up GitHub App credentials for the organization
- **R3.4** Generate short-lived registration tokens (1 hour expiry)
- **R3.5** Return tokens with runner configuration (URL, group, labels)

### R4. Event Processing
- **R4.1** Receive events from MIGlets:
  - `vm_started` - VM bootstrapped
  - `runner_registered` - Runner registered with GitHub
  - `job_started` - Job execution began
  - `job_completed` - Job finished (success/failure)
  - `runner_crashed` - Runner process died
  - `vm_shutting_down` - VM preparing for shutdown
- **R4.2** Store events for analytics and debugging
- **R4.3** Process events for billing and metrics
- **R4.4** Emit events to control plane/observability stack

### R5. Heartbeat Processing
- **R5.1** Receive periodic heartbeats (every 10-30s)
- **R5.2** Extract VM health metrics:
  - CPU load
  - Memory usage
  - Disk usage
  - Runner state (idle/running)
  - Current job info
- **R5.3** Detect missing heartbeats (unhealthy VMs)
- **R5.4** Store heartbeats for historical analysis
- **R5.5** Aggregate metrics for dashboards

### R6. Command Channel
- **R6.1** Support command delivery to MIGlets:
  - **Drain** - Stop accepting new jobs, finish current job
  - **Shutdown** - Shutdown immediately or after current job
  - **UpdateConfig** - Update runtime configuration
  - **SetLogLevel** - Change logging verbosity
- **R6.2** Support polling (`GET /api/v1/vms/{vm_id}/commands`) OR push (WebSocket)
- **R6.3** Queue commands if VM is offline
- **R6.4** Track command delivery and acknowledgment

### R7. Authentication & Authorization
- **R7.1** Authenticate MIGlet requests (Bearer token or mTLS)
- **R7.2** Validate VM identity (prevent spoofed VMs)
- **R7.3** Authorize pool/org access
- **R7.4** Rate limiting per VM/pool

### R8. Data Storage
- **R8.1** Store VM state and metadata
- **R8.2** Store events and heartbeats (MongoDB, database, or object storage)
- **R8.3** Store GitHub App credentials securely (Secret Manager)
- **R8.4** Support querying historical data

## API Endpoints

### Required Endpoints

#### 1. Health Check
```
GET /health
Response: 200 OK
```

#### 2. VM Events
```
POST /api/v1/vms/{vm_id}/events
Body: Event payload (VMStarted, RunnerRegistered, JobStarted, etc.)
Response: {status: "acknowledged", acknowledged: true}
Auth: Bearer token or mTLS
```

#### 3. Heartbeats
```
POST /api/v1/vms/{vm_id}/heartbeat
Body: HeartbeatEvent with VM health and runner state
Response: {status: "received"}
Auth: Bearer token or mTLS
Frequency: Every 10-30s per VM
```

#### 4. Registration Token Request
```
POST /api/v1/vms/{vm_id}/registration-token
Body: {
  org_id: string,
  pool_id: string,
  runner_group: string,
  labels: string[]
}
Response: {
  registration_token: string,
  expires_at: timestamp,
  runner_url: string,
  runner_group: string,
  labels: string[]
}
Auth: Bearer token or mTLS
```

#### 5. Commands (Polling)
```
GET /api/v1/vms/{vm_id}/commands
Response: {
  commands: [
    {
      type: "drain" | "shutdown" | "update_config" | "set_log_level",
      command: string,
      parameters: object,
      id: string
    }
  ]
}
Auth: Bearer token or mTLS
```

#### 6. Commands (Push - Optional)
```
WebSocket /api/v1/vms/{vm_id}/commands
Push commands to MIGlet when available
```

## GitHub App Integration

### Required Flow

1. **Store Credentials:**
   - App ID: `123456`
   - Private Key: RSA private key (PEM format)
   - Installation ID: Per-org installation ID
   - Mappings: `org-name → installation-id`

2. **Generate Registration Token:**
   ```
   JWT = GenerateJWT(AppID, PrivateKey, 10min expiry)
   InstallToken = GetInstallationToken(JWT, InstallationID)
   RegToken = CreateRegistrationToken(Org, RunnerGroup, InstallToken)
   Return RegToken to MIGlet
   ```

3. **Token Management:**
   - Tokens expire in 1 hour
   - Single-use tokens
   - Rate limit token generation

## Data Models

### VM State
```go
type VMState struct {
    VMID          string
    PoolID        string
    OrgID         string
    State         string  // initializing, idle, running, draining, shutting_down, error
    LastHeartbeat time.Time
    RunnerState   string  // idle, running, offline
    CurrentJob    *JobInfo
    Health        VMHealth
    CreatedAt     time.Time
    UpdatedAt     time.Time
}
```

### GitHub App Config
```go
type GitHubAppConfig struct {
    AppID         int
    PrivateKey    string  // RSA private key
    InstallationID int    // Per organization
    OrgName       string
    RunnerGroup   string
    DefaultLabels []string
}
```

### Command
```go
type Command struct {
    ID         string
    Type       string  // drain, shutdown, update_config, set_log_level
    Parameters map[string]interface{}
    CreatedAt  time.Time
    Status     string  // pending, sent, acknowledged, failed
}
```

## Storage Requirements

### Events & Heartbeats
- Store all events for analytics
- Store heartbeats (can be sampled/aggregated)
- Retention: Configurable (e.g., 30 days)
- Query support: By VM, pool, org, time range

### VM State
- In-memory cache for active VMs
- Persistent storage for VM metadata
- Update on every heartbeat/event

### GitHub App Credentials
- **MUST** be stored in secure secret manager:
  - GCP Secret Manager
  - AWS Secrets Manager
  - HashiCorp Vault
  - Kubernetes Secrets (encrypted)
- Never in code or config files
- Support key rotation

## Non-Functional Requirements

### Performance
- Handle 1000+ concurrent VMs
- Process heartbeats with < 100ms latency
- Support 10,000+ events/minute

### Reliability
- 99.9% uptime
- Graceful degradation if storage is down
- Retry logic for GitHub API calls

### Security
- Encrypt credentials at rest
- Use TLS for all communications
- Rate limiting to prevent abuse
- Audit logging

### Scalability
- Horizontal scaling (stateless design)
- Database connection pooling
- Caching for frequently accessed data

## Implementation Phases

### Phase 1: Basic Controller (Current Sample)
- ✅ Receive events
- ✅ Receive heartbeats
- ✅ Return hardcoded registration tokens
- ✅ Store data in files
- ❌ GitHub App integration
- ❌ Command queue
- ❌ Authentication

### Phase 2: GitHub App Integration
- [ ] Store GitHub App credentials securely
- [ ] Generate JWT tokens
- [ ] Exchange for installation tokens
- [ ] Create registration tokens
- [ ] Handle token expiration

### Phase 3: Command System
- [ ] Command queue per VM
- [ ] Polling endpoint
- [ ] Command acknowledgment
- [ ] Command retry logic

### Phase 4: Production Features
- [ ] Authentication (Bearer tokens, mTLS)
- [ ] Rate limiting
- [ ] Database storage (MongoDB/PostgreSQL)
- [ ] Metrics and monitoring
- [ ] Horizontal scaling

## Current Sample Controller Status

The `controller_sample/` is a **sketchy/test** implementation that:
- ✅ Receives and stores events
- ✅ Receives and stores heartbeats
- ✅ Returns hardcoded registration tokens
- ❌ No GitHub App integration
- ❌ No authentication
- ❌ No command queue
- ❌ File-based storage only

## Next Steps for Full Controller

1. **GitHub App Integration:**
   - Use `github.com/google/go-github` library
   - Implement JWT generation
   - Implement token exchange
   - Store credentials in Secret Manager

2. **Database Storage:**
   - MongoDB for events/heartbeats
   - Redis for VM state cache
   - PostgreSQL for metadata

3. **Authentication:**
   - Implement Bearer token validation
   - mTLS support
   - VM identity verification

4. **Command System:**
   - Command queue (Redis or database)
   - Polling endpoint
   - WebSocket support (optional)

5. **Observability:**
   - Prometheus metrics
   - Distributed tracing
   - Structured logging

