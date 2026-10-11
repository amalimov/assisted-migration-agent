package v2

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
)

// NewVirtualMachineFromSummary converts a VM summary to the API type.
func NewVirtualMachineFromSummary(vm models.VirtualMachineSummary) VirtualMachine {
	result := VirtualMachine{
		Id:                vm.ID,
		Name:              vm.Name,
		VCenterID:         vm.VCenterID,
		VCenterState:      vm.PowerState,
		Cluster:           vm.Cluster,
		Datacenter:        vm.Datacenter,
		DiskSize:          vm.DiskSize,
		Memory:            int64(vm.Memory),
		IssueCount:        vm.IssueCount,
		Migratable:        &vm.IsMigratable,
		Template:          &vm.IsTemplate,
		MigrationExcluded: &vm.MigrationExcluded,
	}

	if len(vm.Groups) > 0 {
		result.Groups = &vm.Groups
	}
	if len(vm.Labels) > 0 {
		result.Labels = &vm.Labels
	}

	return result
}

// NewVirtualMachineDetailFromModel converts a models.VM to a v2 VirtualMachineDetail.
func NewVirtualMachineDetailFromModel(vm models.VM) VirtualMachineDetail {
	details := VirtualMachineDetail{
		Id:              vm.ID,
		Name:            vm.Name,
		VCenterID:       vm.VCenterID,
		PowerState:      vm.PowerState,
		ConnectionState: vm.ConnectionState,
		CpuCount:        vm.CpuCount,
		CoresPerSocket:  vm.CoresPerSocket,
		MemoryMB:        vm.MemoryMB,
		Disks:           make([]VirtualMachineDisk, 0, len(vm.Disks)),
		Nics:            make([]VirtualMachineNIC, 0, len(vm.NICs)),
	}

	if vm.UUID != "" {
		details.Uuid = &vm.UUID
	}
	if vm.Firmware != "" {
		details.Firmware = &vm.Firmware
	}
	if vm.Host != "" {
		details.Host = &vm.Host
	}
	if vm.Datacenter != "" {
		details.Datacenter = &vm.Datacenter
	}
	if vm.Cluster != "" {
		details.Cluster = &vm.Cluster
	}
	if vm.Folder != "" {
		details.Folder = &vm.Folder
	}
	if vm.GuestName != "" {
		details.GuestName = &vm.GuestName
	}
	if vm.GuestID != "" {
		details.GuestId = &vm.GuestID
	}
	if vm.HostName != "" {
		details.HostName = &vm.HostName
	}
	if vm.IPAddress != "" {
		details.IpAddress = &vm.IPAddress
	}
	if vm.StorageUsed > 0 {
		details.StorageUsed = &vm.StorageUsed
	}
	if vm.ToolsStatus != "" {
		details.ToolsStatus = &vm.ToolsStatus
	}
	if vm.ToolsRunningStatus != "" {
		details.ToolsRunningStatus = &vm.ToolsRunningStatus
	}
	if len(vm.CpuAffinity) > 0 {
		details.CpuAffinity = &vm.CpuAffinity
	}

	details.Template = &vm.IsTemplate
	details.Migratable = &vm.IsMigratable
	details.MigrationExcluded = &vm.MigrationExcluded

	details.FaultToleranceEnabled = &vm.FaultToleranceEnabled
	details.NestedHVEnabled = &vm.NestedHVEnabled
	if len(vm.Labels) > 0 {
		details.Labels = &vm.Labels
	}

	for _, d := range vm.Disks {
		capacityBytes := d.Capacity * 1024 * 1024
		disk := VirtualMachineDisk{
			File:     &d.File,
			Capacity: &capacityBytes,
			Shared:   &d.Shared,
			Rdm:      &d.RDM,
			Bus:      &d.Bus,
			Mode:     &d.Mode,
		}
		if d.Key != 0 {
			key := d.Key
			disk.Key = &key
		}
		details.Disks = append(details.Disks, disk)
	}

	for _, n := range vm.NICs {
		nic := VirtualMachineNIC{
			Mac:     &n.MAC,
			Network: &n.Network,
			Index:   &n.Index,
		}
		details.Nics = append(details.Nics, nic)
	}

	if len(vm.Devices) > 0 {
		devices := make([]VirtualMachineDevice, 0, len(vm.Devices))
		for _, d := range vm.Devices {
			devices = append(devices, VirtualMachineDevice{Kind: &d.Kind})
		}
		details.Devices = &devices
	}

	if len(vm.GuestNetworks) > 0 {
		networks := make([]GuestNetwork, 0, len(vm.GuestNetworks))
		for _, g := range vm.GuestNetworks {
			gn := GuestNetwork{
				Mac: &g.MAC,
				Ip:  &g.IP,
			}
			if g.Device != "" {
				gn.Device = &g.Device
			}
			if g.Network != "" {
				gn.Network = &g.Network
			}
			if g.PrefixLength > 0 {
				gn.PrefixLength = &g.PrefixLength
			}
			networks = append(networks, gn)
		}
		details.GuestNetworks = &networks
	}

	if len(vm.Issues) > 0 {
		issues := make([]VirtualMachineIssue, 0, len(vm.Issues))
		for _, issue := range vm.Issues {
			description := issue.Description
			if description == "" {
				description = issue.Label
			}
			issues = append(issues, VirtualMachineIssue{
				Label:       issue.Label,
				Category:    VirtualMachineIssueCategory(issue.Category),
				Description: description,
			})
		}
		details.Issues = &issues
	}

	return details
}

func NewCollectionFromDatabase(db *store.Database) Collection {
	name := strings.TrimSuffix(db.Path, filepath.Ext(db.Path))
	return Collection{
		Id:        db.ID,
		Name:      name,
		CreatedAt: db.CreatedAt,
	}
}

// NewCollectorStatus converts a models.CollectorStatus to a v2 CollectorStatus.
func NewCollectorStatus(status models.CollectorStatus) CollectorStatus {
	var c CollectorStatus

	switch status.State {
	case models.CollectorStateReady:
		c.Status = CollectorStatusStatusReady
	case models.CollectorStateCollecting:
		c.Status = CollectorStatusStatusCollecting
	case models.CollectorStateParsing:
		c.Status = CollectorStatusStatusParsing
	case models.CollectorStateCollected:
		c.Status = CollectorStatusStatusCollected
	case models.CollectorStateError:
		c.Status = CollectorStatusStatusError
	default:
		c.Status = CollectorStatusStatusReady
	}

	if status.Error != nil {
		e := status.Error.Error()
		c.Error = &e
	}

	return c
}

// NewGroupFromModel converts a group to the API type.
func NewGroupFromModel(g models.Group) Group {
	createdAt := g.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	updatedAt := g.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now()
	}
	group := Group{
		Id:        g.ID.String(),
		Name:      g.Name,
		Filter:    g.Filter,
		CreatedAt: &createdAt,
		UpdatedAt: &updatedAt,
	}
	if g.Description != "" {
		group.Description = &g.Description
	}
	if g.VmCount > 0 {
		group.VmCount = &g.VmCount
	}
	return group
}

func comparisonDiffEntry(e models.ComparisonDiffEntry) ComparisonDiffEntry {
	onlyInA := e.OnlyInA
	onlyInB := e.OnlyInB
	return ComparisonDiffEntry{Delta: e.Delta, OnlyInA: &onlyInA, OnlyInB: &onlyInB}
}

func comparisonDiffEntryDeltaOnly(e models.ComparisonDiffEntry) ComparisonDiffEntry {
	// Clusters has no per-VM identity; OnlyInA/OnlyInB do not apply.
	return ComparisonDiffEntry{Delta: e.Delta}
}

func collectionAggregateFromModel(a models.CollectionAggregate) CollectionAggregate {
	return CollectionAggregate{
		Id:            a.ID,
		CreatedAt:     a.CreatedAt,
		TotalVMs:      a.TotalVMs,
		Migratable:    a.Migratable,
		NonMigratable: a.NonMigratable,
		Clusters:      a.Clusters,
	}
}

// NewCollectionComparisonSummaryFromModel converts a models.ComparisonSummary to the V2 API type.
func NewCollectionComparisonSummaryFromModel(s models.ComparisonSummary) CollectionComparisonSummary {
	return CollectionComparisonSummary{
		Collections: []CollectionAggregate{collectionAggregateFromModel(s.A), collectionAggregateFromModel(s.B)},
		Diff: struct {
			Clusters      ComparisonDiffEntry `json:"clusters"`
			Migratable    ComparisonDiffEntry `json:"migratable"`
			NonMigratable ComparisonDiffEntry `json:"nonMigratable"`
			TotalVMs      ComparisonDiffEntry `json:"totalVMs"`
		}{
			TotalVMs:      comparisonDiffEntry(s.TotalVMs),
			Migratable:    comparisonDiffEntry(s.Migratable),
			NonMigratable: comparisonDiffEntry(s.NonMigratable),
			Clusters:      comparisonDiffEntryDeltaOnly(s.Clusters),
		},
	}
}

// NewCollectionComparisonDiffFromModel converts a models.ComparisonDiff to the V2 API type.
func NewCollectionComparisonDiffFromModel(d models.ComparisonDiff) CollectionComparisonDiff {
	toPage := func(p models.ComparisonDiffPage) ComparisonDiffPage {
		vmIds := p.VMIDs
		if vmIds == nil {
			vmIds = []string{}
		}
		return ComparisonDiffPage{
			Total:     p.Total,
			Page:      p.Page,
			PageCount: p.PageCount,
			VmIds:     vmIds,
		}
	}
	return CollectionComparisonDiff{
		Dimension: CollectionComparisonDiffDimension(d.Dimension),
		OnlyInA:   toPage(d.OnlyInA),
		OnlyInB:   toPage(d.OnlyInB),
	}
}
