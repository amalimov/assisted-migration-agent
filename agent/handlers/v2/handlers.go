package v2

import (
	"context"

	"github.com/kubev2v/assisted-migration-agent/internal/config"
	estimationhttp "github.com/kubev2v/assisted-migration-agent/internal/handlers/estimation"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	svc "github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/services/estimations"
	"github.com/kubev2v/assisted-migration-agent/internal/vmselection"
)

type ServiceProvider interface {
	ConsoleService() *svc.Console
	CollectionService() *svc.CollectionService
	InspectorService() (*svc.InspectorService, error)
	VddkService() *svc.VddkService
	CredentialsService() *svc.CredentialsService
	ForecasterService() *svc.ForecasterService

	ApplicationService(collectionID string) (*svc.ApplicationService, error)
	ComparisonService(aId, bId string) (*svc.ComparisonService, error)
	ExportService(collectionID string) (*svc.ExportService, error)
	VirtualMachineService(collectionID string) (*svc.VMService, error)
	GroupService(collectionID string) (*svc.GroupService, error)
	InventoryService(collectionID string) (*svc.InventoryService, error)
	RightsizingService(collectionID string) (*svc.RightsizingService, error)

	LatestVirtualMachineService() (*svc.VMService, error)
	LatestGroupService() (*svc.GroupService, error)
	LatestInventoryService() (*svc.InventoryService, error)
	LatestRightsizingService() (*svc.RightsizingService, error)
	LatestApplicationService() (*svc.ApplicationService, error)

	GetCollectorStatus() models.CollectorStatus
	StartCollecting(ctx context.Context) (models.CollectorStatus, error)
	StopCollecting() error
	StartRVToolsCollecting(rvtoolFiles []string) (models.CollectorStatus, error)

	DeleteData(ctx context.Context) error
	Stop(ctx context.Context)
}

type Handler struct {
	cfg        config.Configuration
	svc        ServiceProvider
	estimation *estimationhttp.Handler
}

type EstimationDependencies struct {
	Resolver *vmselection.Resolver
	Service  *estimations.Service
}

func NewHandler(cfg config.Configuration, svc ServiceProvider, estimation ...EstimationDependencies) *Handler {
	h := &Handler{cfg: cfg, svc: svc}
	if len(estimation) > 0 {
		h.estimation = estimationhttp.New(estimation[0].Resolver, estimation[0].Service)
	}
	return h
}
