# MIGlet Product Requirements Document (PRD)

## Document Information

| Field | Value |
|-------|-------|
| Product Name | MIGlet |
| Version | 1.0.0 |
| Last Updated | December 2024 |
| Status | In Development |
| Author | MonkCI Team |

---

## 1. Executive Summary

MIGlet is a lightweight Go agent designed to run inside each MonkCI virtual machine (VM). It serves as the bridge between ephemeral compute instances and the centralized MIG Controller, enabling automated GitHub Actions runner registration, lifecycle management, real-time monitoring, and bidirectional command execution.

The agent is optimized for cloud environments, particularly Google Cloud Platform (GCP) Managed Instance Groups (MIGs), where VMs are created and destroyed dynamically based on workload demand.

---

## 2. Problem Statement

### 2.1 Current Challenges

1. **Manual Runner Registration**: GitHub Actions self-hosted runners require manual setup, including downloading binaries, obtaining registration tokens, and configuring the runner.

2. **No Centralized Control**: Individual runners operate independently without coordination, making fleet management difficult.

3. **Limited Visibility**: No real-time insight into runner health, job status, or resource utilization across the fleet.

4. **Ephemeral VM Complexity**: Cloud VMs are short-lived, requiring automated setup and teardown of runners within seconds.

5. **Credential Management**: Storing GitHub App credentials on every VM creates security risks and complicates key rotation.

6. **Lifecycle Coordination**: No mechanism to gracefully drain runners before VM termination or respond to scaling events.

### 2.2 Target Users

- **DevOps Engineers**: Managing self-hosted runner infrastructure
- **Platform Teams**: Building CI/CD platforms on top of GitHub Actions
- **Enterprise Organizations**: Running large-scale, secure CI/CD pipelines

---

## 3. Product Vision

MIGlet transforms ephemeral VMs into fully managed GitHub Actions runners within seconds of boot, providing centralized control, real-time monitoring, and seamless lifecycle management—all without storing sensitive credentials on the VMs.

---

## 4. Goals and Objectives

### 4.1 Primary Goals

1. **Zero-Touch Runner Setup**: Automatically register GitHub Actions runners on VM boot without manual intervention.

2. **Centralized Command and Control**: Enable the MIG Controller to send commands to any runner in the fleet.

3. **Real-Time Observability**: Provide continuous visibility into runner health, job status, and resource metrics.

4. **Secure by Design**: Eliminate credential storage on VMs by using short-lived tokens from the controller.

5. **Graceful Lifecycle Management**: Support draining, shutdown, and replacement of runners without job disruption.

### 4.2 Success Metrics

| Metric | Target |
|--------|--------|
| Time from VM boot to runner ready | < 60 seconds |
| Heartbeat latency | < 100ms |
| Command delivery latency | < 500ms |
| Runner registration success rate | > 99.9% |
| Graceful shutdown completion rate | > 99% |

---

## 5. Functional Requirements

### 5.1 State Machine

MIGlet operates as a state machine with the following states:

| State | Description |
|-------|-------------|
| **Initializing** | Agent startup, configuration loading, runner binary installation |
| **WaitingForController** | Sends VM started event, waits for controller acknowledgment |
| **Ready** | Connected via gRPC, waiting for registration configuration |
| **RegisteringRunner** | Configuring and starting GitHub Actions runner |
| **Idle** | Runner registered and waiting for jobs |
| **JobRunning** | Actively executing a GitHub Actions job |
| **Draining** | Completing current job, rejecting new jobs |
| **ShuttingDown** | Graceful shutdown in progress |
| **Error** | Terminal error state |

#### State Transitions

- Initializing → WaitingForController (on successful initialization)
- WaitingForController → Ready (on controller acknowledgment)
- Ready → RegisteringRunner (on register_runner command received)
- RegisteringRunner → Idle (on successful registration)
- Idle ↔ JobRunning (on job start/completion)
- Any State → Draining (on drain command)
- Draining → ShuttingDown (after job completion)
- Any State → Error (on unrecoverable error)

### 5.2 Bootstrap and Initialization

#### 5.2.1 Configuration Loading

MIGlet loads configuration from multiple sources in priority order:

1. **Environment Variables**: Highest priority, format `MIGLET_*`
2. **Configuration File**: YAML file at specified path
3. **Cloud Metadata**: GCP instance metadata (for cloud deployments)
4. **Defaults**: Sensible default values

Required configuration parameters:
- Pool ID: Identifies the runner pool
- VM ID: Unique identifier for this VM instance
- Controller Endpoint: HTTP endpoint for initial communication
- gRPC Endpoint: Derived from controller endpoint for streaming

Optional configuration parameters:
- Organization ID
- Heartbeat interval
- Log level and format
- MongoDB connection string (for persistent storage)

#### 5.2.2 Runner Installation

During initialization, MIGlet:

1. Downloads the official GitHub Actions runner binary
2. Validates the binary using SHA256 checksum
3. Extracts to a designated directory
4. Removes any previous installation to ensure clean state
5. Verifies runner dependencies are satisfied

### 5.3 Controller Communication

#### 5.3.1 HTTP Communication (Initial Phase)

Used for initial controller acknowledgment:

- **VM Started Event**: Notifies controller that VM has booted
- **Acknowledgment**: Controller confirms VM is registered

#### 5.3.2 gRPC Bidirectional Streaming (Primary Channel)

After acknowledgment, MIGlet establishes a persistent gRPC stream:

**Messages from MIGlet to Controller:**
- Connect Request: Initial handshake with VM identity
- Command Acknowledgment: Confirms command execution result
- Event Notification: Reports state changes and job events
- Heartbeat: Periodic health and metrics report
- Error Notification: Reports errors and exceptions

**Messages from Controller to MIGlet:**
- Connect Acknowledgment: Confirms connection acceptance
- Command: Instructions for the agent to execute
- Error Notification: Reports controller-side errors

### 5.4 Command Execution

MIGlet receives and executes commands from the controller:

| Command | Description |
|---------|-------------|
| **register_runner** | Provides registration token and configuration to set up the runner |
| **drain** | Stops accepting new jobs, completes current job |
| **shutdown** | Initiates graceful shutdown |
| **update_config** | Updates runtime configuration |
| **set_log_level** | Changes logging verbosity dynamically |

Each command includes:
- Unique command ID
- Command type
- Parameters (string, integer, boolean, array)
- Timestamp

MIGlet responds with acknowledgment including:
- Command ID
- Success/failure status
- Error message (if failed)
- Result data (if applicable)

### 5.5 Runner Registration

Upon receiving the `register_runner` command:

1. **Extract Configuration**:
   - Registration token (short-lived, single-use)
   - Runner URL (GitHub organization or repository)
   - Runner group
   - Labels

2. **Configure Runner**:
   - Execute `config.sh` with non-interactive flags
   - Set ephemeral mode (runner removes itself after one job)
   - Apply labels and runner group

3. **Start Runner**:
   - Launch `run.sh` process
   - Capture stdout/stderr for monitoring
   - Monitor process health

4. **Report Status**:
   - Send `runner_registered` event to controller
   - Begin sending heartbeats

### 5.6 Runner Monitoring

MIGlet continuously monitors the runner process:

#### 5.6.1 Process Monitoring
- Tracks runner process state (running, exited, crashed)
- Detects unexpected termination
- Reports crashes to controller

#### 5.6.2 Log Parsing
- Captures runner stdout/stderr
- Parses log output to detect:
  - Job started events
  - Job completed events
  - State changes (idle, running)
  - Errors and warnings

#### 5.6.3 Job Tracking
- Extracts job ID and run ID from logs
- Tracks job start and completion times
- Reports job success/failure status

### 5.7 Heartbeat and Metrics

MIGlet sends periodic heartbeats containing:

#### 5.7.1 VM Health Metrics
- CPU usage percentage
- Memory usage (total, used, percentage)
- Disk usage (total, used, percentage)

#### 5.7.2 Runner State
- Current state (idle, running, offline)
- Configuration status
- Runner name
- Applied labels

#### 5.7.3 Current Job Information (if running)
- Job ID
- Run ID
- Repository
- Branch
- Commit
- Start time
- Status

Heartbeat frequency is configurable (default: 30 seconds).

### 5.8 Event Reporting

MIGlet emits events for significant state changes:

| Event Type | Trigger |
|------------|---------|
| **vm_started** | VM has booted and MIGlet initialized |
| **runner_registered** | Runner successfully registered with GitHub |
| **job_started** | GitHub Actions job execution began |
| **job_completed** | Job finished (includes success/failure) |
| **runner_crashed** | Runner process terminated unexpectedly |
| **vm_shutting_down** | Graceful shutdown initiated |

### 5.9 Data Persistence

MIGlet optionally stores data to MongoDB:

- **Heartbeats**: Historical VM health and runner state
- **Events**: Job and lifecycle events for analytics
- **Metrics**: Aggregated performance data

This enables:
- Historical analysis and troubleshooting
- Billing and usage tracking
- Performance monitoring dashboards

### 5.10 Graceful Shutdown

When receiving shutdown signal or drain command:

1. **Stop Runner**: Send termination signal to runner process
2. **Wait for Job**: If job is running, wait for completion (with timeout)
3. **Close Connections**: Clean up gRPC and HTTP connections
4. **Close Storage**: Flush and close MongoDB connection
5. **Exit**: Terminate with appropriate exit code

---

## 6. Non-Functional Requirements

### 6.1 Performance

| Requirement | Target |
|-------------|--------|
| Memory footprint | < 50 MB |
| CPU usage (idle) | < 1% |
| Startup time | < 5 seconds |
| Heartbeat processing | < 10ms |
| Command response time | < 100ms |

### 6.2 Reliability

| Requirement | Target |
|-------------|--------|
| Uptime (while VM is running) | 99.99% |
| Automatic reconnection | < 30 seconds |
| Data delivery guarantee | At least once |
| Crash recovery | Automatic restart via systemd |

### 6.3 Scalability

| Requirement | Target |
|-------------|--------|
| Concurrent VMs per controller | 10,000+ |
| Events per second (aggregate) | 1,000+ |
| Heartbeats per second (aggregate) | 500+ |

### 6.4 Security

| Requirement | Implementation |
|-------------|----------------|
| No credential storage | Registration tokens obtained from controller |
| Token lifetime | Short-lived (< 1 hour) |
| Token usage | Single-use tokens |
| Communication security | TLS encryption (production) |
| VM identity validation | Cloud metadata verification |

### 6.5 Observability

| Requirement | Implementation |
|-------------|----------------|
| Structured logging | JSON format with correlation IDs |
| Log levels | Debug, Info, Warn, Error, Fatal |
| Metrics export | Via heartbeats and events |
| Distributed tracing | Correlation IDs across requests |

---

## 7. Technical Architecture

### 7.1 Component Overview

| Component | Responsibility |
|-----------|----------------|
| **Config Manager** | Load and validate configuration from multiple sources |
| **Logger** | Structured logging with context and levels |
| **State Machine** | Manage agent lifecycle and state transitions |
| **Controller Client (HTTP)** | Initial communication and fallback |
| **Controller Client (gRPC)** | Bidirectional streaming for commands and events |
| **Runner Installer** | Download, validate, and extract runner binary |
| **Runner Manager** | Configure and control runner process |
| **Runner Monitor** | Track runner state, parse logs, detect events |
| **Metrics Collector** | Gather system metrics (CPU, memory, disk) |
| **Event Emitter** | Construct and emit events |
| **Storage Client** | Persist data to MongoDB |

### 7.2 Communication Protocol

#### 7.2.1 gRPC Service Definition

Service: CommandService
Method: StreamCommands (bidirectional streaming)

Message types:
- MIGletMessage (client to server)
- ControllerMessage (server to client)

#### 7.2.2 Message Flow

1. MIGlet opens gRPC stream to controller
2. MIGlet sends ConnectRequest with VM identity
3. Controller sends ConnectAck (accepted/rejected)
4. Controller sends Command messages as needed
5. MIGlet sends CommandAck for each command
6. MIGlet sends Events and Heartbeats continuously
7. Stream remains open for VM lifetime

### 7.3 Deployment Architecture

#### 7.3.1 Single VM Deployment

MIGlet runs as a systemd service on each VM:
- Started automatically on boot
- Restarts on failure
- Logs to journal

#### 7.3.2 GCP MIG Deployment

For Managed Instance Groups:
- Instance template includes startup script
- Startup script downloads and configures MIGlet
- MIGlet starts automatically via systemd
- MIG handles scaling and replacement

### 7.4 Configuration Hierarchy

1. Command-line flags (highest priority)
2. Environment variables
3. Configuration file
4. Cloud metadata
5. Default values (lowest priority)

---

## 8. Integration Points

### 8.1 MIG Controller

| Integration | Description |
|-------------|-------------|
| HTTP API | Initial VM registration and acknowledgment |
| gRPC Streaming | Commands, events, and heartbeats |
| Authentication | Bearer token or mTLS |

### 8.2 GitHub Actions

| Integration | Description |
|-------------|-------------|
| Runner Binary | Official actions/runner release |
| Registration | Via controller-provided token |
| Runner Type | Ephemeral (single job) |

### 8.3 Cloud Providers

| Provider | Integration |
|----------|-------------|
| GCP | Instance metadata for configuration |
| AWS | EC2 metadata service (future) |
| Azure | Instance metadata service (future) |

### 8.4 Storage

| System | Usage |
|--------|-------|
| MongoDB | Heartbeats, events, metrics |
| Local Filesystem | Runner binary, logs, temp files |

---

## 9. Security Considerations

### 9.1 Credential Management

- MIGlet never stores GitHub App credentials
- Registration tokens are short-lived (1 hour)
- Tokens are single-use
- Controller manages all GitHub API authentication

### 9.2 Communication Security

- gRPC connections use TLS in production
- HTTP connections use HTTPS in production
- Mutual TLS supported for high-security environments

### 9.3 VM Identity

- VM identity validated via cloud metadata
- Pool membership verified by controller
- Unauthorized VMs rejected

### 9.4 Access Control

- MIGlet runs as non-root user
- Minimal filesystem permissions
- Network access limited to controller endpoints

---

## 10. Operational Considerations

### 10.1 Deployment

- Binary distributed via artifact repository
- Configuration via environment variables or files
- Systemd service for process management
- Startup script for cloud deployments

### 10.2 Monitoring

- Heartbeats provide health status
- Events track lifecycle
- Logs provide detailed debugging
- MongoDB enables historical analysis

### 10.3 Troubleshooting

- Structured logs with correlation IDs
- State machine provides clear lifecycle view
- Events track all significant actions
- Error states include detailed context

### 10.4 Updates

- Binary replacement via deployment
- Configuration hot-reload via command
- Log level change without restart
- Graceful restart preserves job completion

---

## 11. Dependencies

### 11.1 Runtime Dependencies

| Dependency | Version | Purpose |
|------------|---------|---------|
| Go Runtime | 1.21+ | Application runtime |
| gRPC | 1.77+ | Bidirectional streaming |
| MongoDB Driver | 1.17+ | Data persistence |
| GitHub Runner | 2.329+ | Job execution |

### 11.2 Build Dependencies

| Dependency | Purpose |
|------------|---------|
| protoc | Protocol buffer compilation |
| protoc-gen-go | Go code generation |
| protoc-gen-go-grpc | gRPC code generation |

### 11.3 Infrastructure Dependencies

| Dependency | Purpose |
|------------|---------|
| MIG Controller | Command and control |
| MongoDB Atlas | Data persistence (optional) |
| GCP MIG | VM orchestration |

---

## 12. Limitations and Constraints

### 12.1 Current Limitations

| Limitation | Description |
|------------|-------------|
| Single runner per VM | One MIGlet manages one runner |
| Linux only | macOS and Windows not yet supported |
| GCP focus | AWS and Azure support planned |
| Ephemeral mode | Persistent runners not supported |

### 12.2 Technical Constraints

| Constraint | Reason |
|------------|--------|
| Go 1.21+ required | Modern language features |
| gRPC required | Bidirectional streaming |
| Systemd required | Process management |
| Network access | Controller communication |

---

## 13. Future Enhancements

### 13.1 Short-Term (Next Release)

- TLS support for gRPC connections
- Prometheus metrics export
- Health check endpoint
- Improved error recovery

### 13.2 Medium-Term (3-6 Months)

- AWS EC2 support
- Azure VM support
- Multi-runner per VM
- Web-based dashboard integration

### 13.3 Long-Term (6-12 Months)

- Windows support
- macOS support
- Persistent runner mode
- Advanced scheduling integration
- Auto-scaling recommendations

---

## 14. Glossary

| Term | Definition |
|------|------------|
| **MIGlet** | The lightweight agent running on each VM |
| **MIG Controller** | Centralized service managing all MIGlets |
| **MIG** | Managed Instance Group (GCP terminology) |
| **Ephemeral Runner** | Runner that processes one job then terminates |
| **Pool** | Logical grouping of VMs/runners |
| **Heartbeat** | Periodic health and status report |
| **Registration Token** | Short-lived token for runner registration |
| **gRPC Streaming** | Bidirectional communication channel |
| **State Machine** | Logic controlling agent lifecycle |
| **Runner Monitor** | Component tracking runner process and logs |

---

## 15. Document History

| Version | Date | Author | Changes |
|---------|------|--------|---------|
| 0.1 | Nov 2024 | MonkCI Team | Initial draft |
| 0.2 | Nov 2024 | MonkCI Team | Added gRPC streaming |
| 0.3 | Dec 2024 | MonkCI Team | Added Ready state, command flow |
| 1.0 | Dec 2024 | MonkCI Team | Complete implementation |

