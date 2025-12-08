# MIG Controller Product Requirements Document (PRD)


## 1. Executive Summary

The MIG Controller is a pool-level management service responsible for orchestrating virtual machines within a specific Google Cloud Platform Managed Instance Group (MIG). It serves as the bridge between the centralized Scheduler Service and individual MIGlet agents running on VMs, handling job assignment, VM lifecycle management, GitHub App authentication, and real-time monitoring.

Each MIG Controller instance manages a single pool (or MIG), enabling horizontal scaling where each pool has its own controller. The controller maintains an in-memory view of all VMs in its pool, processes job assignments, manages GCP resources, and provides registration tokens to MIGlets for GitHub Actions runner setup.

---

## 2. System Context

### 2.1 Position in Architecture

The MIG Controller sits between:
- **Upstream**: Scheduler Service (receives job assignments)
- **Downstream**: MIGlet agents (sends commands, receives events)
- **External**: GCP APIs (VM management), GitHub APIs (token generation)

### 2.2 Scope Boundaries

- VM lifecycle management within assigned pool
- Job-to-VM assignment
- GitHub App authentication and token generation
- MIGlet communication (HTTP + gRPC)
- GCP API calls for the assigned MIG
- Pool-level metrics and monitoring


---

## 3. Goals and Objectives

### 3.1 Primary Goals

1. **Efficient Job Assignment**: Match incoming jobs to available VMs with minimal latency.

2. **Optimal Resource Utilization**: Maintain warm pool of VMs while minimizing idle costs.

3. **Secure Token Management**: Generate and distribute short-lived GitHub registration tokens.

4. **Real-Time VM Visibility**: Track all VMs in the pool with up-to-date status.

5. **Graceful Lifecycle Management**: Handle VM creation, scaling, and termination without job disruption.

---

## 4. Functional Requirements

### 4.1 Job Queue Subscription

#### 4.1.1 Queue Consumption

The MIG Controller subscribes to a pool-specific message queue:
- Queue name format: `jobs.pool.<pool_id>`
- Message format: Job assignment with job details
- Consumption: Pull-based with acknowledgment
- Ordering: FIFO within priority levels

#### 4.1.2 Job Message Contents

Each job message contains:
- Job ID and Run ID (from GitHub)
- Organization and Repository
- Installation ID (for GitHub App)
- Labels requested by the workflow
- Priority level
- Timestamp received
- Timeout settings

#### 4.1.3 Message Handling

For each received message:
1. Parse and validate job details
2. Check for duplicate (idempotency)
3. Attempt VM assignment
4. Acknowledge or reject message based on outcome
5. Update job state in database

### 4.2 VM State Management

#### 4.2.1 In-Memory VM Registry

The controller maintains a real-time view of all VMs:

| Field | Description |
|-------|-------------|
| VM ID | Unique identifier from GCP |
| Instance Name | GCP instance name |
| Status | Current VM state (see below) |
| Current Job ID | Assigned job (if any) |
| Last Heartbeat | Timestamp of last heartbeat |
| Runner State | Idle, Running, Offline |
| Health Metrics | CPU, memory, disk from heartbeats |
| Created At | VM creation timestamp |
| Labels | Applied runner labels |

#### 4.2.2 VM Status States

| Status | Description |
|--------|-------------|
| **CREATING** | GCP create instance API called |
| **BOOTING** | Instance running, waiting for MIGlet |
| **READY** | MIGlet connected, awaiting registration config |
| **REGISTERING** | Runner registration in progress |
| **IDLE** | Runner registered, no active job |
| **ASSIGNED** | Job assigned, runner picking up |
| **RUNNING** | Job execution in progress |
| **DRAINING** | Completing job, will shut down after |
| **STOPPING** | GCP stop API called |
| **STOPPED** | Instance stopped (warm pool) |
| **DELETING** | GCP delete API called |
| **UNHEALTHY** | Missing heartbeats, needs attention |
| **ERROR** | VM in error state |

#### 4.2.3 State Transitions

Valid transitions:

- CREATING → BOOTING (instance started)
- BOOTING → READY (MIGlet connected via gRPC)
- READY → REGISTERING (registration config sent)
- REGISTERING → IDLE (runner registered)
- IDLE → ASSIGNED (job assigned)
- ASSIGNED → RUNNING (job started)
- RUNNING → IDLE (job completed, reuse VM)
- RUNNING → DRAINING (drain requested)
- DRAINING → STOPPING (job completed)
- IDLE → STOPPING (idle timeout or scale down)
- STOPPING → STOPPED (instance stopped)
- STOPPED → BOOTING (instance started)
- Any → UNHEALTHY (heartbeat timeout)
- Any → ERROR (unrecoverable error)
- Any → DELETING (VM deletion)

#### 4.2.4 Registry Synchronization

The registry is synchronized via:
- MIGlet heartbeats (primary source)
- MIGlet events (state changes)
- GCP API polling (backup/reconciliation)
- Database state (recovery after restart)

### 4.3 Job Assignment Logic

#### 4.3.1 Assignment Algorithm

For each incoming job:

1. **Find IDLE VM**: Search for VM with status=IDLE and matching labels
   - If found: Assign job, transition to ASSIGNED

2. **Find STOPPED VM**: Search for VM with status=STOPPED and matching labels
   - If found: Start VM via GCP API, assign job, transition to BOOTING

3. **Create New VM**: If pool has capacity
   - Create instance via GCP API
   - Assign job, set status=CREATING

4. **Queue or Reject**: If no capacity available
   - Queue job for retry (with backoff)
   - Or reject if queue is full or timeout exceeded

#### 4.3.2 Label Matching

Jobs specify required labels; VMs have configured labels:
- Exact match required for all job labels
- VM may have additional labels (ignored)
- Pool configuration defines available label sets

#### 4.3.3 Assignment Priority

When multiple idle VMs available:
1. Most recently active (warm cache)
2. Lowest resource utilization
3. Oldest instance (for eventual replacement)

### 4.4 GitHub App Authentication

#### 4.4.1 Credential Storage

MIG Controller securely stores GitHub App credentials:

| Credential | Storage | Rotation |
|------------|---------|----------|
| App ID | Configuration | Static |
| Private Key | Secret Manager | Manual rotation |
| Installation IDs | Database | Per organization |
| Webhook Secret | Secret Manager | Manual rotation |

#### 4.4.2 JWT Generation

To authenticate with GitHub:
1. Load private key from Secret Manager
2. Generate JWT token with:
   - Issuer: App ID
   - Issued At: Current time
   - Expiration: 10 minutes
3. Sign with RS256 algorithm
4. Cache JWT until near expiration

#### 4.4.3 Installation Token Generation

For each organization:
1. Look up Installation ID from database
2. Use JWT to call GitHub API: `POST /app/installations/{installation_id}/access_tokens`
3. Receive Installation Access Token (expires in 1 hour)
4. Cache token with organization as key
5. Refresh before expiration

#### 4.4.4 Registration Token Generation

When MIGlet needs to register a runner:
1. Obtain Installation Access Token for the organization
2. Call GitHub API: `POST /orgs/{org}/actions/runners/registration-token`
   - Or for repo: `POST /repos/{owner}/{repo}/actions/runners/registration-token`
3. Receive Registration Token (expires in 1 hour, single use)
4. Return to MIGlet with runner configuration

### 4.5 MIGlet Communication

#### 4.5.1 HTTP API Endpoints

For initial MIGlet communication:

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/api/v1/vms/{vm_id}/events` | POST | Receive VM events |
| `/api/v1/vms/{vm_id}/heartbeat` | POST | Receive heartbeats |
| `/api/v1/vms/{vm_id}/registration-token` | POST | Request registration token |
| `/api/v1/vms/{vm_id}/commands` | GET | Poll for commands (fallback) |
| `/health` | GET | Health check |

#### 4.5.2 gRPC Streaming Service

Primary communication channel:

**Service**: CommandService
**Method**: StreamCommands (bidirectional streaming)

**Inbound Messages (from MIGlet):**
- ConnectRequest: VM registration with identity
- CommandAck: Acknowledgment of command execution
- EventNotification: State changes, job events
- Heartbeat: Health metrics and runner state
- ErrorNotification: Error reports

**Outbound Messages (to MIGlet):**
- ConnectAck: Accept/reject connection
- Command: Instructions (register_runner, drain, shutdown, etc.)
- ErrorNotification: Controller-side errors

#### 4.5.3 Connection Management

For each connected MIGlet:
1. Validate VM identity (match against registry)
2. Store stream reference for command delivery
3. Track connection state (connected/disconnected)
4. Handle reconnection (VM was temporarily disconnected)
5. Clean up on disconnection

#### 4.5.4 Command Delivery

Commands sent to MIGlets:

| Command | Parameters | Purpose |
|---------|------------|---------|
| register_runner | registration_token, runner_url, runner_group, labels | Configure and start runner |
| assign_job | job_id, run_id, repository | Notify of assigned job |
| drain | reason, timeout | Stop accepting new jobs |
| shutdown | reason, force | Graceful shutdown |
| update_config | config_key, config_value | Runtime configuration change |
| set_log_level | level | Change logging verbosity |

### 4.6 GCP API Integration

#### 4.6.1 Required APIs

| API | Purpose |
|-----|---------|
| Compute Engine | Instance management |
| Instance Groups | MIG operations |
| Cloud Monitoring | Metrics export |
| Secret Manager | Credential retrieval |

#### 4.6.2 Instance Operations

| Operation | When Used |
|-----------|-----------|
| instances.get | Query instance status |
| instances.start | Start stopped instance |
| instances.stop | Stop running instance |
| instances.delete | Remove instance |
| instances.list | Sync registry with GCP |
| instanceGroupManagers.resize | Scale pool size |
| instanceGroupManagers.listManagedInstances | Get all instances in MIG |

#### 4.6.3 Rate Limiting

GCP APIs have quotas; controller implements:
- Token bucket rate limiter per API
- Exponential backoff on 429 responses
- Request batching where possible
- Priority queue for critical operations

#### 4.6.4 Error Handling

| Error Type | Handling |
|------------|----------|
| Rate limit (429) | Exponential backoff, retry |
| Not found (404) | Remove from registry, log |
| Permission denied (403) | Alert, fail operation |
| Server error (5xx) | Retry with backoff |
| Timeout | Retry, mark VM as unknown |

### 4.7 Database Operations

#### 4.7.1 Tables Accessed

| Table | Operations |
|-------|------------|
| job_runs | Read, Update status |
| vm_instances | Read, Write, Update |
| organizations | Read (for GitHub App config) |
| pools | Read (for pool configuration) |
| github_installations | Read (for Installation IDs) |

#### 4.7.2 Job State Updates

Controller updates job state throughout lifecycle:

| State | When Set |
|-------|----------|
| SCHEDULED | Job received from queue |
| ASSIGNED | VM assigned to job |
| VM_BOOTING | VM is starting |
| VM_READY | MIGlet connected, runner registered |
| RUNNING | Job execution started (from MIGlet) |
| COMPLETED | Job finished successfully (from MIGlet) |
| FAILED | Job failed (from MIGlet or timeout) |
| CANCELLED | Job cancelled |

#### 4.7.3 VM State Persistence

VM states are persisted for:
- Recovery after controller restart
- Multi-controller coordination (future)
- Billing and analytics
- Troubleshooting

### 4.8 Pool Management

#### 4.8.1 Pool Configuration

Each pool is configured with:

| Setting | Description |
|---------|-------------|
| Pool ID | Unique identifier |
| MIG Name | GCP Managed Instance Group name |
| Region/Zone | GCP location |
| Min Size | Minimum VM count (warm pool) |
| Max Size | Maximum VM count (hard limit) |
| Instance Template | GCP instance template |
| Labels | Runner labels for this pool |
| Idle Timeout | How long to keep idle VMs |
| Organization | Owning organization |

#### 4.8.2 Scaling Decisions

**Scale Up** when:
- Job received and no idle VMs
- All VMs busy and below max size
- Predicted demand increase (future)

**Scale Down** when:
- VMs idle beyond timeout
- Current size exceeds demand
- Cost optimization schedule (future)

**Maintain Warm Pool**:
- Keep min_size VMs ready (running or stopped)
- Prefer stopped over deleted (faster start)

#### 4.8.3 Post-Job Policy

After job completion, decide VM fate:

| Policy | Action |
|--------|--------|
| KEEP_RUNNING | Leave as IDLE (warm pool) |
| STOP | Stop instance (cold warm pool) |
| DELETE | Delete instance (ephemeral) |

Policy based on:
- Current pool utilization
- Time of day / demand prediction
- Cost optimization settings
- VM age (eventual replacement)

### 4.9 Health Monitoring

#### 4.9.1 Heartbeat Processing

For each received heartbeat:
1. Update last_heartbeat_at timestamp
2. Update health metrics (CPU, memory, disk)
3. Update runner state
4. Check for anomalies
5. Store to database (sampled)

#### 4.9.2 Unhealthy VM Detection

VM marked UNHEALTHY when:
- No heartbeat for 3x heartbeat interval
- Repeated errors from MIGlet
- gRPC connection dropped without reconnection
- Health metrics exceed thresholds

#### 4.9.3 Unhealthy VM Handling

| Condition | Action |
|-----------|--------|
| Missed heartbeats, no job | Stop or delete VM |
| Missed heartbeats, job running | Wait, then force terminate |
| Repeated errors | Mark for replacement |
| Resource exhaustion | Drain and replace |

#### 4.9.4 Alerting

Alerts generated for:
- VM stuck in BOOTING too long
- High rate of unhealthy VMs
- Job assignment failures
- GCP API errors
- Token generation failures
- Queue backup

### 4.10 Event Processing

#### 4.10.1 Events from MIGlet

| Event | Processing |
|-------|------------|
| vm_started | Update status to READY, send registration config |
| runner_registered | Update status to IDLE, mark job VM_READY |
| job_started | Update status to RUNNING, update job state |
| job_completed | Update status to IDLE or DRAINING, update job state |
| runner_crashed | Mark VM UNHEALTHY, handle job failure |
| vm_shutting_down | Clean up, update status |

#### 4.10.2 Event Storage

Events stored for:
- Audit trail
- Debugging and troubleshooting
- Analytics and reporting
- Billing verification

---

## 5. Non-Functional Requirements

### 5.1 Performance

| Requirement | Target |
|-------------|--------|
| Job assignment throughput | 100 jobs/second per controller |
| Heartbeat processing | 1000/second |
| gRPC connections | 1000 concurrent per controller |
| Memory footprint | < 1 GB per 1000 VMs |
| API response time (p99) | < 100ms |

### 5.2 Reliability

| Requirement | Target |
|-------------|--------|
| Controller availability | 99.9% |
| Job assignment success rate | 99.9% |
| Token generation success rate | 99.99% |
| Data consistency | Eventual (5 second window) |
| Recovery time (after crash) | < 30 seconds |

### 5.3 Scalability

| Requirement | Target |
|-------------|--------|
| VMs per controller | 1000 |
| Controllers per region | 100 |
| Total VMs (system-wide) | 100,000 |
| Jobs per day | 10,000,000 |

### 5.4 Security

| Requirement | Implementation |
|-------------|----------------|
| GitHub credentials | Secret Manager, never in memory long-term |
| Token lifetime | JWT: 10 min, Installation: 1 hour, Registration: 1 hour |
| Communication | TLS for all external, mTLS for internal |
| API authentication | Bearer tokens for MIGlet, mTLS for services |
| Audit logging | All privileged operations logged |

### 5.5 Observability

| Requirement | Implementation |
|-------------|----------------|
| Metrics | Prometheus format, exported to Cloud Monitoring |
| Logging | Structured JSON, correlation IDs |
| Tracing | OpenTelemetry, distributed tracing |
| Dashboards | Grafana for operations |
| Alerting | PagerDuty integration |

---

## 6. Technical Architecture

### 6.1 Component Overview

| Component | Responsibility |
|-----------|----------------|
| **Job Consumer** | Subscribe to queue, process job messages |
| **VM Registry** | In-memory VM state, synchronization |
| **Assignment Engine** | Match jobs to VMs |
| **GitHub Auth Service** | JWT, Installation tokens, Registration tokens |
| **GCP Client** | Instance operations, rate limiting |
| **gRPC Server** | Bidirectional streaming with MIGlets |
| **HTTP Server** | REST API for MIGlets (fallback) |
| **Health Monitor** | Heartbeat processing, unhealthy detection |
| **Scaler** | Scale up/down decisions |
| **Database Client** | Persistence operations |
| **Metrics Exporter** | Prometheus metrics |

### 6.2 Data Flow

#### 6.2.1 Job Assignment Flow

1. Scheduler publishes job to `jobs.pool.<pool_id>` queue
2. Job Consumer receives message
3. Assignment Engine finds or creates VM
4. Database updated with job assignment
5. If VM is READY: Send register_runner command via gRPC
6. MIGlet registers runner, sends runner_registered event
7. Runner picks up job from GitHub
8. MIGlet sends job_started event
9. MIGlet sends job_completed event
10. Controller updates database, decides VM fate

#### 6.2.2 VM Lifecycle Flow

1. Assignment Engine requests new VM
2. GCP Client creates instance
3. VM boots, MIGlet starts
4. MIGlet sends vm_started event (HTTP)
5. Controller acknowledges, MIGlet connects via gRPC
6. Controller sends register_runner command
7. MIGlet registers runner
8. VM is IDLE, awaits jobs
9. Jobs assigned and executed
10. Idle timeout or drain → VM stopped/deleted

### 6.3 Deployment Model

#### 6.3.1 Single Controller per Pool

Each pool has exactly one active controller:
- Simplifies state management
- Avoids coordination complexity
- Clear ownership of VMs

#### 6.3.2 High Availability

For HA deployment:
- Primary/standby with leader election
- Shared database for state
- Standby takes over on primary failure
- Recovery from database state

#### 6.3.3 Multi-Region

For multi-region pools:
- One controller per region
- Scheduler routes to appropriate controller
- Cross-region failover (future)

### 6.4 Configuration

#### 6.4.1 Controller Configuration

| Setting | Description |
|---------|-------------|
| pool_id | Pool this controller manages |
| queue_subscription | Pub/Sub subscription name |
| database_url | Database connection string |
| gcp_project | GCP project ID |
| gcp_region | GCP region for API calls |
| grpc_port | Port for gRPC server |
| http_port | Port for HTTP server |
| heartbeat_timeout | Missing heartbeat threshold |
| idle_timeout | How long before stopping idle VMs |

#### 6.4.2 GitHub App Configuration

| Setting | Description |
|---------|-------------|
| app_id | GitHub App ID |
| private_key_secret | Secret Manager path to private key |
| webhook_secret | Secret Manager path to webhook secret |

---

## 7. Integration Specifications

### 7.1 Scheduler Integration

**Queue Format**: Pub/Sub

**Message Schema**:
- job_id: string
- run_id: string
- org_id: string
- repo: string
- installation_id: string
- labels: array of strings
- priority: integer
- received_at: timestamp
- timeout_at: timestamp

**Acknowledgment**: After successful assignment or permanent failure

### 7.2 MIGlet Integration

**Protocol**: gRPC (primary), HTTP (fallback)

**gRPC Service**: CommandService with StreamCommands method

**Authentication**: VM identity validated against registry

**Commands Sent**:
- register_runner (with GitHub registration token)
- drain
- shutdown
- update_config
- set_log_level

**Events Received**:
- vm_started
- runner_registered
- job_started
- job_completed
- runner_crashed

### 7.3 GitHub API Integration

**Authentication**: GitHub App (JWT → Installation Token → API calls)

**APIs Used**:
- `POST /app/installations/{id}/access_tokens` - Get installation token
- `POST /orgs/{org}/actions/runners/registration-token` - Get registration token
- `POST /repos/{owner}/{repo}/actions/runners/registration-token` - Get repo registration token
- `GET /orgs/{org}/actions/runners` - List runners (optional)
- `DELETE /orgs/{org}/actions/runners/{id}` - Remove runner (cleanup)

### 7.4 GCP Integration

**Authentication**: Service account with Compute Admin role

**APIs Used**:
- Compute Engine API
- Instance Groups API
- Secret Manager API
- Cloud Monitoring API

---

## 8. Error Handling

### 8.1 Job Assignment Failures

| Failure | Handling |
|---------|----------|
| No available VMs | Queue job, retry with backoff |
| GCP API error | Retry with backoff, fail after limit |
| Token generation failed | Retry, fail job if persistent |
| MIGlet connection failed | Reassign to different VM |
| Timeout waiting for runner | Fail job, mark VM unhealthy |

### 8.2 VM Failures

| Failure | Handling |
|---------|----------|
| VM creation failed | Log, try again on next job |
| VM stuck in BOOTING | Force delete after timeout |
| MIGlet never connected | Delete VM, create new |
| Runner registration failed | Retry, then replace VM |
| VM became unhealthy | Drain if possible, delete |

### 8.3 External Service Failures

| Service | Failure Handling |
|---------|-----------------|
| Database unavailable | Retry, cache writes, alert |
| Pub/Sub unavailable | Retry, exponential backoff |
| GCP API unavailable | Retry, mark operations pending |
| GitHub API unavailable | Retry, cache tokens if possible |
| Secret Manager unavailable | Fail startup, alert |

---

## 9. Security Considerations

### 9.1 Credential Security

- Private keys stored in Secret Manager, loaded at startup
- Installation tokens cached with short TTL
- Registration tokens generated on-demand, single use
- All tokens have expiration, never stored long-term

### 9.2 Communication Security

- All gRPC connections use TLS
- All HTTP connections use HTTPS
- Internal services use mTLS
- VM identity validated before accepting commands

### 9.3 Access Control

- Controller runs with minimal GCP permissions
- Each pool controller has scoped access
- API endpoints authenticated
- Audit logging for all privileged operations

### 9.4 VM Trust

- VMs validated against MIG membership
- VM ID must match GCP instance name
- Pool ID must match controller's pool
- Reject connections from unknown VMs

---

## 10. Operational Considerations

### 10.1 Deployment

- Containerized deployment (Docker/Kubernetes)
- Configuration via environment variables
- Secrets via Secret Manager
- Health check endpoint for load balancer

### 10.2 Monitoring

**Key Metrics**:
- Jobs in queue
- Job assignment latency
- VMs by status
- Heartbeat rate
- Token generation rate
- GCP API call rate
- Error rates by type

**Dashboards**:
- Pool overview (VMs, jobs, health)
- Job lifecycle (assignment to completion)
- Resource utilization
- Error analysis

### 10.3 Troubleshooting

**Common Issues**:
- Jobs stuck in queue → Check GCP quotas, scaling limits
- VMs not connecting → Check network, MIGlet logs
- Token generation failing → Check GitHub App permissions
- High latency → Check database, GCP API quotas

**Debugging Tools**:
- Structured logs with correlation IDs
- Distributed tracing
- VM registry dump endpoint
- Force refresh from GCP API

### 10.4 Maintenance

- Rolling updates with zero downtime
- Database migrations coordinated across controllers
- Graceful drain before shutdown
- Configuration reload without restart (limited)
