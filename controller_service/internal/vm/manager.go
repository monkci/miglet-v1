package vm

import (
	"context"
	"fmt"
	"time"

	compute "cloud.google.com/go/compute/apiv1"
	"cloud.google.com/go/compute/apiv1/computepb"
	"google.golang.org/api/iterator"

	"github.com/monkci/mig-controller/internal/config"
	"github.com/monkci/mig-controller/internal/redis"
	"github.com/monkci/mig-controller/pkg/logger"
)

// Manager handles VM lifecycle management via GCloud API
type Manager struct {
	cfg             *config.Config
	instancesClient *compute.InstancesClient
	migClient       *compute.InstanceGroupManagersClient
	vmStore         *redis.VMStatusStore
}

// NewManager creates a new VM manager
func NewManager(cfg *config.Config, vmStore *redis.VMStatusStore) (*Manager, error) {
	ctx := context.Background()

	instancesClient, err := compute.NewInstancesRESTClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create instances client: %w", err)
	}

	migClient, err := compute.NewInstanceGroupManagersRESTClient(ctx)
	if err != nil {
		instancesClient.Close()
		return nil, fmt.Errorf("failed to create MIG client: %w", err)
	}

	log := logger.WithComponent("vm_manager")
	log.WithFields(map[string]interface{}{
		"project":  cfg.GCP.ProjectID,
		"zone":     cfg.GCP.Zone,
		"mig_name": cfg.GCP.MIGName,
	}).Info("VM Manager initialized")

	return &Manager{
		cfg:             cfg,
		instancesClient: instancesClient,
		migClient:       migClient,
		vmStore:         vmStore,
	}, nil
}

// Close closes the GCloud clients
func (m *Manager) Close() error {
	if err := m.instancesClient.Close(); err != nil {
		return err
	}
	return m.migClient.Close()
}

// StartVM starts a stopped VM
func (m *Manager) StartVM(ctx context.Context, vmName string) error {
	log := logger.WithVM(vmName, m.cfg.Pool.ID)
	log.Info("Starting VM")

	req := &computepb.StartInstanceRequest{
		Project:  m.cfg.GCP.ProjectID,
		Zone:     m.cfg.GCP.Zone,
		Instance: vmName,
	}

	op, err := m.instancesClient.Start(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to start VM: %w", err)
	}

	// Wait for operation to complete
	if err := op.Wait(ctx); err != nil {
		return fmt.Errorf("failed waiting for VM start: %w", err)
	}

	// Update VM status in Redis
	if err := m.vmStore.UpdateFromInfra(ctx, vmName, m.cfg.GCP.Zone, redis.VMInfraStaging); err != nil {
		log.WithError(err).Warn("Failed to update VM status")
	}

	log.Info("VM start initiated")
	return nil
}

// StopVM stops a running VM
func (m *Manager) StopVM(ctx context.Context, vmName string) error {
	log := logger.WithVM(vmName, m.cfg.Pool.ID)
	log.Info("Stopping VM")

	req := &computepb.StopInstanceRequest{
		Project:  m.cfg.GCP.ProjectID,
		Zone:     m.cfg.GCP.Zone,
		Instance: vmName,
	}

	op, err := m.instancesClient.Stop(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to stop VM: %w", err)
	}

	// Wait for operation to complete
	if err := op.Wait(ctx); err != nil {
		return fmt.Errorf("failed waiting for VM stop: %w", err)
	}

	// Update VM status in Redis
	if err := m.vmStore.UpdateFromInfra(ctx, vmName, m.cfg.GCP.Zone, redis.VMInfraStopping); err != nil {
		log.WithError(err).Warn("Failed to update VM status")
	}

	log.Info("VM stop initiated")
	return nil
}

// ScaleUp increases the MIG size by the specified count
func (m *Manager) ScaleUp(ctx context.Context, count int) error {
	log := logger.WithComponent("vm_manager")

	// Get current MIG size
	mig, err := m.getMIG(ctx)
	if err != nil {
		return fmt.Errorf("failed to get MIG: %w", err)
	}

	currentSize := int(mig.GetTargetSize())
	newSize := currentSize + count

	// Check against max VMs
	if newSize > m.cfg.VMManager.MaxVMs {
		return fmt.Errorf("cannot scale up: would exceed max VMs (%d > %d)", newSize, m.cfg.VMManager.MaxVMs)
	}

	log.WithFields(map[string]interface{}{
		"current_size": currentSize,
		"new_size":     newSize,
		"count":        count,
	}).Info("Scaling up MIG")

	req := &computepb.ResizeInstanceGroupManagerRequest{
		Project:              m.cfg.GCP.ProjectID,
		Zone:                 m.cfg.GCP.Zone,
		InstanceGroupManager: m.cfg.GCP.MIGName,
		Size:                 int32(newSize),
	}

	op, err := m.migClient.Resize(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to resize MIG: %w", err)
	}

	// Don't wait for completion - VMs will be provisioned asynchronously
	_ = op

	log.Info("MIG scale up initiated")
	return nil
}

// ScaleDown decreases the MIG size by removing specific VMs
func (m *Manager) ScaleDown(ctx context.Context, vmNames []string) error {
	log := logger.WithComponent("vm_manager")

	if len(vmNames) == 0 {
		return nil
	}

	log.WithField("vms", vmNames).Info("Scaling down MIG")

	// Delete specific instances
	for _, vmName := range vmNames {
		instanceURL := fmt.Sprintf("zones/%s/instances/%s", m.cfg.GCP.Zone, vmName)

		req := &computepb.DeleteInstancesInstanceGroupManagerRequest{
			Project:              m.cfg.GCP.ProjectID,
			Zone:                 m.cfg.GCP.Zone,
			InstanceGroupManager: m.cfg.GCP.MIGName,
			InstanceGroupManagersDeleteInstancesRequestResource: &computepb.InstanceGroupManagersDeleteInstancesRequest{
				Instances: []string{instanceURL},
			},
		}

		_, err := m.migClient.DeleteInstances(ctx, req)
		if err != nil {
			log.WithError(err).WithField("vm", vmName).Warn("Failed to delete instance")
			continue
		}

		// Remove from Redis
		if err := m.vmStore.Delete(ctx, vmName); err != nil {
			log.WithError(err).WithField("vm", vmName).Warn("Failed to remove VM from store")
		}
	}

	return nil
}

// RefreshVMList updates the VM list from GCloud
func (m *Manager) RefreshVMList(ctx context.Context) error {
	log := logger.WithComponent("vm_manager")

	instances, err := m.listManagedInstances(ctx)
	if err != nil {
		return fmt.Errorf("failed to list managed instances: %w", err)
	}

	log.WithField("count", len(instances)).Debug("Retrieved managed instances from GCloud")

	// Update each instance in Redis
	for _, inst := range instances {
		infraState := mapInstanceStatus(inst.GetInstanceStatus())

		if err := m.vmStore.UpdateFromInfra(ctx, inst.GetInstance(), m.cfg.GCP.Zone, infraState); err != nil {
			log.WithError(err).WithField("vm", inst.GetInstance()).Warn("Failed to update VM status")
		}
	}

	// TODO: Clean up stale entries (VMs that no longer exist in GCloud)

	return nil
}

// GetAvailableVM returns the first available VM for job assignment
func (m *Manager) GetAvailableVM(ctx context.Context) (*redis.VMStatus, error) {
	// First try to find a ready/idle VM
	vm, err := m.vmStore.GetFirstReady(ctx)
	if err != nil {
		return nil, err
	}
	if vm != nil {
		return vm, nil
	}

	return nil, nil
}

// GetStoppedVM returns the first stopped VM for starting
func (m *Manager) GetStoppedVM(ctx context.Context) (*redis.VMStatus, error) {
	return m.vmStore.GetFirstStopped(ctx)
}

// EnsureMinReadyVMs ensures minimum number of ready VMs are maintained
func (m *Manager) EnsureMinReadyVMs(ctx context.Context) error {
	log := logger.WithComponent("vm_manager")

	stats, err := m.vmStore.GetStats(ctx)
	if err != nil {
		return fmt.Errorf("failed to get pool stats: %w", err)
	}

	readyCount := stats.ReadyVMs
	minReady := int64(m.cfg.VMManager.MinReadyVMs)

	if readyCount >= minReady {
		return nil // We have enough ready VMs
	}

	deficit := int(minReady - readyCount)
	log.WithFields(map[string]interface{}{
		"ready":   readyCount,
		"min":     minReady,
		"deficit": deficit,
	}).Info("Ensuring minimum ready VMs")

	// First try to start stopped VMs
	stoppedVMs, err := m.vmStore.GetByEffectiveState(ctx, redis.EffectiveStateStopped)
	if err != nil {
		return fmt.Errorf("failed to get stopped VMs: %w", err)
	}

	toStart := min(len(stoppedVMs), deficit)
	for i := 0; i < toStart; i++ {
		if err := m.StartVM(ctx, stoppedVMs[i].VMID); err != nil {
			log.WithError(err).WithField("vm", stoppedVMs[i].VMID).Warn("Failed to start VM")
		}
	}

	// If still need more, scale up MIG
	stillNeeded := deficit - toStart
	if stillNeeded > 0 {
		// Respect rate limiting
		scaleCount := min(stillNeeded, m.cfg.VMManager.MaxScaleUpPerMinute)
		if err := m.ScaleUp(ctx, scaleCount); err != nil {
			return fmt.Errorf("failed to scale up: %w", err)
		}
	}

	return nil
}

// CleanupIdleVMs stops VMs that have been idle too long
func (m *Manager) CleanupIdleVMs(ctx context.Context) error {
	log := logger.WithComponent("vm_manager")

	stats, err := m.vmStore.GetStats(ctx)
	if err != nil {
		return err
	}

	// Only cleanup if we have more than minimum ready VMs
	if stats.ReadyVMs <= int64(m.cfg.VMManager.MinReadyVMs) {
		return nil
	}

	// Get idle VMs
	idleVMs, err := m.vmStore.GetByEffectiveState(ctx, redis.EffectiveStateIdle)
	if err != nil {
		return err
	}

	idleTimeout := m.cfg.VMManager.IdleTimeout
	now := time.Now()

	for _, vm := range idleVMs {
		// Keep minimum ready VMs
		if stats.ReadyVMs <= int64(m.cfg.VMManager.MinReadyVMs) {
			break
		}

		// Check if idle too long
		if now.Sub(vm.LastHeartbeat) > idleTimeout {
			log.WithField("vm", vm.VMID).Info("Stopping idle VM")

			if err := m.StopVM(ctx, vm.VMID); err != nil {
				log.WithError(err).WithField("vm", vm.VMID).Warn("Failed to stop idle VM")
			} else {
				stats.ReadyVMs--
			}
		}
	}

	return nil
}

// getMIG retrieves the MIG details
func (m *Manager) getMIG(ctx context.Context) (*computepb.InstanceGroupManager, error) {
	req := &computepb.GetInstanceGroupManagerRequest{
		Project:              m.cfg.GCP.ProjectID,
		Zone:                 m.cfg.GCP.Zone,
		InstanceGroupManager: m.cfg.GCP.MIGName,
	}

	return m.migClient.Get(ctx, req)
}

// listManagedInstances lists all instances in the MIG
func (m *Manager) listManagedInstances(ctx context.Context) ([]*computepb.ManagedInstance, error) {
	req := &computepb.ListManagedInstancesInstanceGroupManagersRequest{
		Project:              m.cfg.GCP.ProjectID,
		Zone:                 m.cfg.GCP.Zone,
		InstanceGroupManager: m.cfg.GCP.MIGName,
	}

	var instances []*computepb.ManagedInstance
	it := m.migClient.ListManagedInstances(ctx, req)

	for {
		inst, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		instances = append(instances, inst)
	}

	return instances, nil
}

// mapInstanceStatus maps GCloud instance status to our VMInfraState
func mapInstanceStatus(status string) redis.VMInfraState {
	switch status {
	case "RUNNING":
		return redis.VMInfraRunning
	case "TERMINATED", "STOPPED":
		return redis.VMInfraStopped
	case "STAGING":
		return redis.VMInfraStaging
	case "STOPPING":
		return redis.VMInfraStopping
	case "PROVISIONING":
		return redis.VMInfraProvisioning
	default:
		return redis.VMInfraUnknown
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

