package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/monkci/mig-controller/internal/config"
	grpcserver "github.com/monkci/mig-controller/internal/grpc"
	"github.com/monkci/mig-controller/internal/redis"
	"github.com/monkci/mig-controller/internal/token"
	"github.com/monkci/mig-controller/internal/vm"
	"github.com/monkci/mig-controller/pkg/logger"
	"github.com/monkci/mig-controller/proto/commands"
)

// Scheduler handles job assignment to VMs
type Scheduler struct {
	cfg          *config.Config
	jobStore     *redis.JobStore
	vmStore      *redis.VMStatusStore
	vmManager    *vm.Manager
	grpcServer   *grpcserver.Server
	tokenService *token.Service

	// Control
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Metrics
	assignedJobs   int64
	failedJobs     int64
	startedVMs     int64
	createdVMs     int64
}

// NewScheduler creates a new scheduler
func NewScheduler(
	cfg *config.Config,
	jobStore *redis.JobStore,
	vmStore *redis.VMStatusStore,
	vmManager *vm.Manager,
	grpcServer *grpcserver.Server,
	tokenService *token.Service,
) *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())

	return &Scheduler{
		cfg:          cfg,
		jobStore:     jobStore,
		vmStore:      vmStore,
		vmManager:    vmManager,
		grpcServer:   grpcServer,
		tokenService: tokenService,
		ctx:          ctx,
		cancel:       cancel,
	}
}

// Start starts the scheduler loop
func (s *Scheduler) Start() {
	log := logger.WithComponent("scheduler")
	log.Info("Scheduler starting")

	s.wg.Add(1)
	go s.runSchedulerLoop()

	s.wg.Add(1)
	go s.runVMMaintenanceLoop()
}

// Stop stops the scheduler
func (s *Scheduler) Stop() {
	log := logger.WithComponent("scheduler")
	log.Info("Scheduler stopping")
	s.cancel()
	s.wg.Wait()
	log.Info("Scheduler stopped")
}

// runSchedulerLoop is the main scheduling loop
func (s *Scheduler) runSchedulerLoop() {
	defer s.wg.Done()

	log := logger.WithComponent("scheduler")
	ticker := time.NewTicker(s.cfg.Scheduler.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			if err := s.processNextJob(); err != nil {
				log.WithError(err).Debug("No jobs to process or error")
			}
		}
	}
}

// runVMMaintenanceLoop handles VM warm pool and cleanup
func (s *Scheduler) runVMMaintenanceLoop() {
	defer s.wg.Done()

	log := logger.WithComponent("scheduler")
	ticker := time.NewTicker(s.cfg.VMManager.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			// Ensure minimum ready VMs
			if err := s.vmManager.EnsureMinReadyVMs(s.ctx); err != nil {
				log.WithError(err).Warn("Failed to ensure min ready VMs")
			}

			// Cleanup idle VMs
			if err := s.vmManager.CleanupIdleVMs(s.ctx); err != nil {
				log.WithError(err).Warn("Failed to cleanup idle VMs")
			}

			// Refresh VM list from GCloud
			if err := s.vmManager.RefreshVMList(s.ctx); err != nil {
				log.WithError(err).Warn("Failed to refresh VM list")
			}
		}
	}
}

// processNextJob attempts to process the next job in the queue
func (s *Scheduler) processNextJob() error {
	log := logger.WithComponent("scheduler")

	// Peek at next job (don't dequeue yet)
	job, err := s.jobStore.Peek(s.ctx)
	if err != nil {
		return err
	}
	if job == nil {
		return nil // No jobs
	}

	logger.WithJob(job.ID, s.cfg.Pool.ID).Info("Processing job")

	// Find available VM
	vmStatus, err := s.findAvailableVM()
	if err != nil {
		log.WithError(err).Warn("Failed to find available VM")
		return err
	}

	if vmStatus == nil {
		// No VMs available - need to start or create one
		vmStatus, err = s.provisionVM()
		if err != nil {
			log.WithError(err).Warn("Failed to provision VM")
			return err
		}
	}

	// Dequeue the job
	job, err = s.jobStore.Dequeue(s.ctx)
	if err != nil {
		return err
	}

	// Assign job to VM
	if err := s.assignJobToVM(job, vmStatus); err != nil {
		log.WithError(err).Warn("Failed to assign job to VM")
		// Requeue the job
		s.jobStore.Requeue(s.ctx, job.ID)
		s.failedJobs++
		return err
	}

	s.assignedJobs++
	return nil
}

// findAvailableVM finds a VM ready to accept a job
func (s *Scheduler) findAvailableVM() (*redis.VMStatus, error) {
	// First check for ready/idle VMs
	return s.vmStore.GetFirstReady(s.ctx)
}

// provisionVM provisions a new VM (start stopped or create new)
func (s *Scheduler) provisionVM() (*redis.VMStatus, error) {
	log := logger.WithComponent("scheduler")

	// First try to find a stopped VM
	stoppedVM, err := s.vmStore.GetFirstStopped(s.ctx)
	if err != nil {
		return nil, err
	}

	if stoppedVM != nil {
		log.WithField("vm_id", stoppedVM.VMID).Info("Starting stopped VM")

		if err := s.vmManager.StartVM(s.ctx, stoppedVM.VMID); err != nil {
			return nil, fmt.Errorf("failed to start VM: %w", err)
		}

		// Wait for VM to become ready
		if err := s.grpcServer.WaitForState(s.ctx, stoppedVM.VMID, redis.MigletStateReady, s.cfg.Scheduler.AssignmentTimeout); err != nil {
			return nil, fmt.Errorf("VM did not become ready: %w", err)
		}

		s.startedVMs++
		return s.vmStore.Get(s.ctx, stoppedVM.VMID)
	}

	// No stopped VMs - need to scale up
	log.Info("No stopped VMs available, scaling up MIG")

	if err := s.vmManager.ScaleUp(s.ctx, 1); err != nil {
		return nil, fmt.Errorf("failed to scale up: %w", err)
	}

	s.createdVMs++

	// We can't return the VM immediately as it's still provisioning
	// The job will be retried on next scheduler loop
	return nil, fmt.Errorf("new VM provisioning, job will be retried")
}

// assignJobToVM assigns a job to a specific VM
func (s *Scheduler) assignJobToVM(job *redis.Job, vmStatus *redis.VMStatus) error {
	log := logger.WithJob(job.ID, s.cfg.Pool.ID).WithField("vm_id", vmStatus.VMID)
	log.Info("Assigning job to VM")

	// Generate registration token
	regToken, err := s.tokenService.GetRegistrationToken(
		s.ctx,
		job.InstallationID,
		job.RepoFullName,
		false, // isOrg - use repo-level token
	)
	if err != nil {
		return fmt.Errorf("failed to get registration token: %w", err)
	}

	// Build register_runner command
	cmd := &commands.Command{
		Id:        uuid.New().String(),
		Type:      "register_runner",
		CreatedAt: time.Now().Unix(),
		StringParams: map[string]string{
			"token":        regToken.Token,
			"url":          token.GetRunnerURL(job.RepoFullName, false),
			"runner_group": "default",
			"name":         vmStatus.VMID,
		},
		StringArrayParams: job.Labels,
	}

	// Send command to MIGlet
	ack, err := s.grpcServer.SendCommand(vmStatus.VMID, cmd, 30*time.Second)
	if err != nil {
		return fmt.Errorf("failed to send register command: %w", err)
	}

	if !ack.Success {
		return fmt.Errorf("registration failed: %s", ack.Message)
	}

	// Update job status
	if err := s.jobStore.AssignToVM(s.ctx, job.ID, vmStatus.VMID); err != nil {
		return fmt.Errorf("failed to update job status: %w", err)
	}

	log.Info("Job assigned successfully")
	return nil
}

// HandleJobEvent handles job events from MIGlets
func (s *Scheduler) HandleJobEvent(vmID string, event *commands.EventNotification) {
	log := logger.WithVM(vmID, s.cfg.Pool.ID).WithField("event_type", event.Type)

	switch event.Type {
	case "runner_registered":
		log.Info("Runner registered on VM")

	case "job_started":
		jobID := event.Data["job_id"]
		if jobID != "" {
			if err := s.jobStore.MarkRunning(s.ctx, jobID); err != nil {
				log.WithError(err).Warn("Failed to mark job as running")
			}
		}
		log.Info("Job started")

	case "job_completed":
		jobID := event.Data["job_id"]
		success := event.Data["success"] == "true"
		if jobID != "" {
			if success {
				if err := s.jobStore.MarkCompleted(s.ctx, jobID); err != nil {
					log.WithError(err).Warn("Failed to mark job as completed")
				}
			} else {
				errorMsg := event.Data["error"]
				if err := s.jobStore.MarkFailed(s.ctx, jobID, errorMsg); err != nil {
					log.WithError(err).Warn("Failed to mark job as failed")
				}
			}
		}
		log.Info("Job completed")

	case "runner_crashed":
		// Handle runner crash - may need to reassign job
		job, err := s.jobStore.GetByVM(s.ctx, vmID)
		if err == nil && job != nil && job.Status == redis.JobStatusRunning {
			// Attempt to requeue the job
			if job.RetryCount < job.MaxRetries {
				if err := s.jobStore.Requeue(s.ctx, job.ID); err != nil {
					log.WithError(err).Warn("Failed to requeue job after crash")
				} else {
					log.WithField("job_id", job.ID).Info("Job requeued after runner crash")
				}
			} else {
				s.jobStore.MarkFailed(s.ctx, job.ID, "runner crashed - max retries exceeded")
			}
		}
		log.Warn("Runner crashed")
	}
}

// GetStats returns scheduler statistics
func (s *Scheduler) GetStats() map[string]interface{} {
	queueLen, _ := s.jobStore.QueueLength(s.ctx)
	poolStats, _ := s.vmStore.GetStats(s.ctx)

	return map[string]interface{}{
		"queue_length":   queueLen,
		"assigned_jobs":  s.assignedJobs,
		"failed_jobs":    s.failedJobs,
		"started_vms":    s.startedVMs,
		"created_vms":    s.createdVMs,
		"connected_vms":  s.grpcServer.GetConnectionCount(),
		"pool_stats":     poolStats,
	}
}

