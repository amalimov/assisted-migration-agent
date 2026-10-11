package v2

import (
	"context"
	"github.com/kubev2v/rvtools-analyser/internal/config"
	"github.com/kubev2v/rvtools-analyser/internal/models"
	svc "github.com/kubev2v/rvtools-analyser/internal/services"
)

type ServiceProvider interface {
	CollectionService() *svc.CollectionService
	ComparisonService(aID, bID string) (*svc.ComparisonService, error)
	ExportService(collectionID string) (*svc.ExportService, error)
	VirtualMachineService(collectionID string) (*svc.VMService, error)
	GroupService(collectionID string) (*svc.GroupService, error)
	InventoryService(collectionID string) (*svc.InventoryService, error)
	LatestVirtualMachineService() (*svc.VMService, error)
	LatestGroupService() (*svc.GroupService, error)
	LatestInventoryService() (*svc.InventoryService, error)
	GetCollectorStatus() models.CollectorStatus
	StopCollecting() error
	StartRVToolsCollecting(files []string) (models.CollectorStatus, error)
	DeleteData(ctx context.Context) error
	Stop(ctx context.Context)
}

type Handler struct {
	cfg config.Configuration
	svc ServiceProvider
}

func NewHandler(cfg config.Configuration, svc ServiceProvider) *Handler {
	return &Handler{cfg: cfg, svc: svc}
}
