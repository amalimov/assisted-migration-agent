package estimations

import (
	"errors"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

var ErrNoEligibleVMs = errors.New("selection contains no VMs eligible for estimation")

// Service adapts concrete VMs into the pinned Planner estimators. It has no
// knowledge of collections, Groups, clusters, or API request types.
type Service struct{}

func NewService() *Service { return &Service{} }

func eligibleVMs(vms []models.VM) ([]models.VM, error) {
	eligible := make([]models.VM, 0, len(vms))
	for _, vm := range vms {
		if !vm.MigrationExcluded {
			eligible = append(eligible, vm)
		}
	}
	if len(eligible) == 0 {
		return nil, ErrNoEligibleVMs
	}
	return eligible, nil
}
