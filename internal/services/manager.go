package services

import (
	"context"
	"errors"
	"sync"

	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"
	"github.com/kubev2v/migration-planner/pkg/opa"

	"github.com/kubev2v/assisted-migration-agent/internal/config"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

type ServiceManager struct {
	cfg        *config.Configuration
	pool       *store.Pool
	validator  *opa.Validator
	collection *CollectionService
	mu         sync.Mutex
	collector  *CollectorService
}

type ServiceManagerOption func(*ServiceManager)

func WithConfig(cfg *config.Configuration) ServiceManagerOption {
	return func(m *ServiceManager) { m.cfg = cfg }
}
func WithPool(pool *store.Pool) ServiceManagerOption {
	return func(m *ServiceManager) { m.pool = pool }
}
func WithOpaValidator(v *opa.Validator) ServiceManagerOption {
	return func(m *ServiceManager) { m.validator = v }
}

func NewServiceManager(opts ...ServiceManagerOption) *ServiceManager {
	m := &ServiceManager{}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

func (m *ServiceManager) Initialize() error {
	if m.cfg == nil {
		return errors.New("config is required")
	}
	if m.pool == nil {
		return errors.New("pool is required")
	}
	m.collection = NewCollectionService(m.pool)
	return nil
}

func (m *ServiceManager) GetCollectorStatus() models.CollectorStatus {
	m.mu.Lock()
	collector := m.collector
	m.mu.Unlock()
	if collector != nil {
		return collector.GetStatus()
	}
	if _, err := m.pool.Latest(); err == nil {
		return models.CollectorStatus{State: models.CollectorStateCollected}
	}
	return models.CollectorStatus{State: models.CollectorStateReady}
}

func (m *ServiceManager) StartRVToolsCollecting(files []string) (models.CollectorStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.collector != nil && m.collector.IsRunning() {
		return models.CollectorStatus{}, srvErrors.NewCollectionInProgressError()
	}
	factory, err := newRvtoolWorkFactory(m.pool, files, m.cfg.Agent.DataFolder, m.validator)
	if err != nil {
		return models.CollectorStatus{}, err
	}
	m.collector = NewCollectorService(factory)
	if err := m.collector.Start(context.Background()); err != nil {
		m.collector = nil
		return models.CollectorStatus{}, err
	}
	return m.collector.GetStatus(), nil
}

func (m *ServiceManager) StopCollecting() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.collector != nil {
		m.collector.Stop()
		m.collector = nil
	}
	return nil
}

func (m *ServiceManager) CollectionService() *CollectionService { return m.collection }

func (m *ServiceManager) InventoryService(id string) (*InventoryService, error) {
	db, err := m.pool.Get(id)
	if err != nil {
		return nil, err
	}
	return m.inventoryService(db)
}
func (m *ServiceManager) VirtualMachineService(id string) (*VMService, error) {
	db, err := m.pool.Get(id)
	if err != nil {
		return nil, err
	}
	return m.vmService(db)
}
func (m *ServiceManager) GroupService(id string) (*GroupService, error) {
	db, err := m.pool.Get(id)
	if err != nil {
		return nil, err
	}
	return m.groupService(db)
}
func (m *ServiceManager) ExportService(id string) (*ExportService, error) {
	db, err := m.pool.Get(id)
	if err != nil {
		return nil, err
	}
	st, err := db.Store()
	if err != nil {
		return nil, err
	}
	return NewExportService(st), nil
}
func (m *ServiceManager) LatestVirtualMachineService() (*VMService, error) {
	db, err := m.pool.Latest()
	if err != nil {
		return nil, err
	}
	return m.vmService(db)
}
func (m *ServiceManager) LatestGroupService() (*GroupService, error) {
	db, err := m.pool.Latest()
	if err != nil {
		return nil, err
	}
	return m.groupService(db)
}
func (m *ServiceManager) LatestInventoryService() (*InventoryService, error) {
	db, err := m.pool.Latest()
	if err != nil {
		return nil, err
	}
	return m.inventoryService(db)
}
func (m *ServiceManager) Stop(ctx context.Context) { _ = m.StopCollecting() }
func (m *ServiceManager) DeleteData(ctx context.Context) error {
	m.Stop(ctx)
	return m.collection.DeleteAll(ctx)
}
func (m *ServiceManager) vmService(db *store.Database) (*VMService, error) {
	st, err := db.Store()
	if err != nil {
		return nil, err
	}
	return NewVMService(st), nil
}
func (m *ServiceManager) groupService(db *store.Database) (*GroupService, error) {
	st, err := db.Store()
	if err != nil {
		return nil, err
	}
	return NewGroupService(st, duckdb_parser.New(st.Querier(), nil)), nil
}
func (m *ServiceManager) inventoryService(db *store.Database) (*InventoryService, error) {
	st, err := db.Store()
	if err != nil {
		return nil, err
	}
	return NewInventoryService(st), nil
}
func (m *ServiceManager) ComparisonService(aID, bID string) (*ComparisonService, error) {
	dbA, err := m.pool.Get(aID)
	if err != nil {
		return nil, err
	}
	dbB, err := m.pool.Get(bID)
	if err != nil {
		return nil, err
	}
	stA, err := dbA.Store()
	if err != nil {
		return nil, err
	}
	stB, err := dbB.Store()
	if err != nil {
		return nil, err
	}
	return NewComparisonService(stA, stB,
		models.CollectionMeta{ID: dbA.ID, CreatedAt: dbA.CreatedAt},
		models.CollectionMeta{ID: dbB.ID, CreatedAt: dbB.CreatedAt}), nil
}
