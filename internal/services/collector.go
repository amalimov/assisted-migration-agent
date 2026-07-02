package services

import (
	"context"
	"errors"
	"sync"

	sq "github.com/Masterminds/squirrel"

	"github.com/kubev2v/assisted-migration-agent/pkg/vmware"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/pkg/work"
)

type (
	collectorWorkUnit        = work.WorkUnit[models.CollectorStatus, models.CollectorResult]
	collectorWorkBuilderFunc func(creds models.Credentials) work.WorkBuilder[models.CollectorStatus, models.CollectorResult]
	postCollectionBuilderFn  func(creds models.Credentials) []collectorWorkUnit
)

type CollectorService struct {
	mu       sync.Mutex
	workSrv  *work.Service[models.CollectorStatus, models.CollectorResult]
	store    *store.Store
	buildFn  collectorWorkBuilderFunc
	credsSvc *CredentialsService
}

func NewCollectorService(st *store.Store, buildFn collectorWorkBuilderFunc, credsSvc *CredentialsService) *CollectorService {
	return &CollectorService{
		store:    st,
		buildFn:  buildFn,
		credsSvc: credsSvc,
	}
}

func (c *CollectorService) GetStatus() models.CollectorStatus {
	ctx := context.Background()

	// Check if any done+active collection exists — the authoritative source of truth.
	// Checked first so a successfully published collection is always reported as
	// CollectorStateCollected, even while the in-memory pipeline state is stale.
	done, err := c.store.Collection().List(ctx,
		sq.Eq{"vcenter_id": defaultVCenterID, "active": true})
	if err == nil && len(done) > 0 {
		return models.CollectorStatus{State: models.CollectorStateCollected}
	}

	// Fall back to in-memory work service state for transient states
	// (connecting, collecting, parsing, error) that the DB doesn't capture yet.
	c.mu.Lock()
	srv := c.workSrv
	c.mu.Unlock()

	if srv != nil {
		state := srv.State()
		if state.Err == nil {
			return state.State
		}
		if !errors.Is(state.Err, work.ErrStopped) {
			return models.CollectorStatus{State: models.CollectorStateError, Error: state.Err}
		}
	}

	// Check if a collection run is in progress in the DB (e.g. after a process restart
	// where workSrv is nil but a running collection row exists).
	running, dbErr := c.store.Collection().List(ctx,
		sq.Eq{"vcenter_id": defaultVCenterID, "state": string(models.CollectionStateRunning)})
	if dbErr == nil && len(running) > 0 {
		return models.CollectorStatus{State: models.CollectorStateConnecting}
	}

	return models.CollectorStatus{State: models.CollectorStateReady}
}

func (c *CollectorService) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.workSrv != nil && c.workSrv.IsRunning() {
		return srvErrors.NewCollectionInProgressError()
	}

	// Guard against a DB-running collection (e.g. from a previous process restart).
	running, err := c.store.Collection().List(ctx,
		sq.Eq{"vcenter_id": defaultVCenterID, "state": string(models.CollectionStateRunning)})
	if err != nil {
		return err
	}
	if len(running) > 0 {
		return srvErrors.NewCollectionInProgressError()
	}

	creds, err := c.credsSvc.Resolve(ctx)
	if err != nil {
		return err
	}

	url, err := vmware.NormalizeAndValidateURL(creds.URL)
	if err != nil {
		return err
	}
	creds.URL = url

	srv := work.NewService(models.CollectorStatus{State: models.CollectorStateConnecting}, c.buildFn(creds))
	if err := srv.Start(); err != nil {
		return err
	}

	c.workSrv = srv
	return nil
}

func (c *CollectorService) Stop() {
	c.mu.Lock()
	srv := c.workSrv
	c.mu.Unlock()

	if srv != nil {
		srv.Stop()
	}
}

func (c *CollectorService) WithWorkBuilder(fn collectorWorkBuilderFunc) *CollectorService {
	c.buildFn = fn
	return c
}
