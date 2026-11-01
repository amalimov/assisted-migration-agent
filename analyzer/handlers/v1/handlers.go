package analyzer

import (
	"context"

	"github.com/kubev2v/assisted-migration-agent/internal/config"
	estimationhttp "github.com/kubev2v/assisted-migration-agent/internal/handlers/estimation"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/services/estimations"
	"github.com/kubev2v/assisted-migration-agent/internal/vmselection"
)

type ServiceProvider interface {
	CollectionService() *services.CollectionService
	ExportService(string) (*services.ExportService, error)
	VirtualMachineService(string) (*services.VMService, error)
	GroupService(string) (*services.GroupService, error)
	InventoryService(string) (*services.InventoryService, error)
	RightsizingService(string) (*services.RightsizingService, error)
	LatestVirtualMachineService() (*services.VMService, error)
	LatestGroupService() (*services.GroupService, error)
	LatestInventoryService() (*services.InventoryService, error)
	LatestRightsizingService() (*services.RightsizingService, error)
	LatestApplicationService() (*services.ApplicationService, error)
	GetCollectorStatus() models.CollectorStatus
	StopCollecting() error
	StartRVToolsCollecting([]string) (models.CollectorStatus, error)
	DeleteCollections(context.Context) error
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
