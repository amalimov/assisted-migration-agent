package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/kubev2v/migration-planner/pkg/inventory"
	"github.com/kubev2v/migration-planner/pkg/inventory/converters"

	"github.com/kubev2v/assisted-migration-agent/internal/config"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	collector "github.com/kubev2v/assisted-migration-agent/pkg/collector"
	"github.com/kubev2v/assisted-migration-agent/pkg/work"
)

type collectorWorkFactory struct {
	store                  *store.Store
	cfg                    config.Agent
	eventSrv               *EventService
	dataDir                string
	opaPoliciesDir         string
	postCollectionBuilders []postCollectionBuilderFn
}

func newCollectorWorkFactory(st *store.Store, cfg config.Agent, eventSrv *EventService, dataDir, opaPoliciesDir string) *collectorWorkFactory {
	return &collectorWorkFactory{
		store:          st,
		cfg:            cfg,
		eventSrv:       eventSrv,
		dataDir:        dataDir,
		opaPoliciesDir: opaPoliciesDir,
	}
}

// WithPostCollectionBuilder registers extra work units to be spliced into the
// pipeline immediately before the final "collected" event unit. Called after
// construction so that services can be wired in by the manager.
func (f *collectorWorkFactory) WithPostCollectionBuilder(fn postCollectionBuilderFn) *collectorWorkFactory {
	f.postCollectionBuilders = append(f.postCollectionBuilders, fn)
	return f
}

// withFailMark wraps a work unit's Work function so that on error, if
// r.CollectionID is non-zero, the collection is marked as failed in the DB.
// This ensures the collection row never stays in state=running after a pipeline
// failure that occurs after setupCollection has assigned an ID.
func (f *collectorWorkFactory) withFailMark(unit collectorWorkUnit) collectorWorkUnit {
	inner := unit.Work
	unit.Work = func(ctx context.Context, r models.CollectorResult) (models.CollectorResult, error) {
		r, err := inner(ctx, r)
		if err != nil && r.CollectionID != 0 {
			if markErr := f.store.Collection().MarkFailed(ctx, r.CollectionID, err.Error()); markErr != nil {
				zap.S().Named("collector_service").Warnw("failed to mark collection as failed",
					"collection_id", r.CollectionID, "error", markErr)
			}
		}
		return r, err
	}
	return unit
}

func (f *collectorWorkFactory) Build(creds models.Credentials) work.WorkBuilder[models.CollectorStatus, models.CollectorResult] {
	// setupCollection is the first unit: it finds the previous active collection,
	// reads persistent VM fields, and creates a new collection row in state=running.
	// It runs outside withFailMark because CollectionID is not yet assigned.
	setupUnit := collectorWorkUnit{
		Status: func() models.CollectorStatus {
			return models.CollectorStatus{State: models.CollectorStateConnecting}
		},
		Work: func(ctx context.Context, r models.CollectorResult) (models.CollectorResult, error) {
			if err := setupCollection(ctx, f.store, &r); err != nil {
				return r, err
			}
			return r, nil
		},
	}

	units := []collectorWorkUnit{
		setupUnit,
		f.withFailMark(collectorWorkUnit{
			Status: func() models.CollectorStatus {
				return models.CollectorStatus{State: models.CollectorStateConnecting}
			},
			Work: func(ctx context.Context, r models.CollectorResult) (models.CollectorResult, error) {
				err := f.verifyCredentials(ctx, creds)
				return r, err
			},
		}),
		f.withFailMark(collectorWorkUnit{
			Status: func() models.CollectorStatus {
				return models.CollectorStatus{State: models.CollectorStateCollecting}
			},
			Work: func(ctx context.Context, r models.CollectorResult) (models.CollectorResult, error) {
				sqlitePath, err := f.collect(ctx, creds)
				if err != nil {
					return r, err
				}
				r.SQLitePath = sqlitePath
				return r, nil
			},
		}),
		f.withFailMark(collectorWorkUnit{
			Status: func() models.CollectorStatus {
				return models.CollectorStatus{State: models.CollectorStateParsing}
			},
			Work: func(ctx context.Context, r models.CollectorResult) (models.CollectorResult, error) {
				zap.S().Named("collector_service").Info("ingesting sqlite data into duckdb")
				if err := f.ingestSqlite(ctx, r.SQLitePath, r.CollectionID); err != nil {
					zap.S().Named("collector_service").Errorw("ingest failed", "error", err)
					return r, err
				}
				zap.S().Named("collector_service").Info("sqlite data successfully ingested into duckdb")
				return r, nil
			},
		}),
		f.withFailMark(collectorWorkUnit{
			Status: func() models.CollectorStatus {
				return models.CollectorStatus{State: models.CollectorStateParsing}
			},
			Work: func(ctx context.Context, r models.CollectorResult) (models.CollectorResult, error) {
				if err := copyForwardPersistentFields(ctx, f.store, &r); err != nil {
					return r, err
				}
				return r, nil
			},
		}),
		f.withFailMark(collectorWorkUnit{
			Status: func() models.CollectorStatus {
				return models.CollectorStatus{State: models.CollectorStateParsing}
			},
			Work: func(ctx context.Context, r models.CollectorResult) (models.CollectorResult, error) {
				if err := refreshGroupMatches(ctx, f.store, r.CollectionID); err != nil {
					return r, fmt.Errorf("refreshing group matches: %w", err)
				}
				return r, nil
			},
		}),
	}

	for _, builder := range f.postCollectionBuilders {
		for _, u := range builder(creds) {
			units = append(units, f.withFailMark(u))
		}
	}

	units = append(units, []collectorWorkUnit{
		f.withFailMark(collectorWorkUnit{
			Status: func() models.CollectorStatus {
				return models.CollectorStatus{State: models.CollectorStateParsing}
			},
			Work: func(ctx context.Context, r models.CollectorResult) (models.CollectorResult, error) {
				zap.S().Named("collector_service").Info("building inventory with utilization from duckdb")
				inv, err := f.buildAndMarshalInventory(ctx, r.CollectionID)
				if err != nil {
					zap.S().Named("collector_service").Errorw("failed to build inventory", "error", err)
					return r, err
				}
				zap.S().Named("inventory").Info("successfully created inventory with clusters")
				r.Inventory = inv
				return r, nil
			},
		}),
		f.withFailMark(collectorWorkUnit{
			Status: func() models.CollectorStatus {
				return models.CollectorStatus{State: models.CollectorStateParsing}
			},
			Work: func(ctx context.Context, r models.CollectorResult) (models.CollectorResult, error) {
				if err := publishCollection(ctx, f.store, f.cfg, &r); err != nil {
					return r, err
				}
				return r, nil
			},
		}),
		{
			Status: func() models.CollectorStatus {
				return models.CollectorStatus{State: models.CollectorStateCollected}
			},
			Work: func(ctx context.Context, r models.CollectorResult) (models.CollectorResult, error) {
				if err := f.eventSrv.AddInventoryUpdateEvent(ctx, r.Inventory); err != nil {
					return r, err
				}
				return r, nil
			},
		},
	}...)

	return work.NewSliceWorkBuilder(units)
}

func (f *collectorWorkFactory) verifyCredentials(ctx context.Context, cred models.Credentials) error {
	dbPath := path.Join(f.dataDir, fmt.Sprintf("%s.db", uuid.New()))
	vc := collector.NewVSphereCollector(dbPath)
	defer vc.Close()

	zap.S().Named("collector_service").Info("verifying vCenter credentials")
	if err := vc.VerifyCredentials(ctx, &cred); err != nil {
		zap.S().Named("collector_service").Errorw("credential verification failed", "error", err)
		return err
	}
	zap.S().Named("collector_service").Info("vCenter credentials verified")
	return nil
}

func (f *collectorWorkFactory) collect(ctx context.Context, creds models.Credentials) (string, error) {
	dbPath := path.Join(f.dataDir, fmt.Sprintf("%s.db", uuid.New()))
	vc := collector.NewVSphereCollector(dbPath)
	defer vc.Close()

	zap.S().Named("collector_service").Info("starting vSphere inventory collection")
	if err := vc.Collect(ctx, &creds); err != nil {
		zap.S().Named("collector_service").Errorw("vSphere collection failed", "error", err)
		return "", err
	}
	zap.S().Named("collector_service").Info("vSphere inventory collection completed")

	return dbPath, nil
}

func (f *collectorWorkFactory) ingestSqlite(ctx context.Context, sqlitePath string, collectionID int64) error {
	if _, err := os.Stat(sqlitePath); err != nil {
		zap.S().Named("collector_service").Errorw("sqlite file not accessible", "path", sqlitePath, "error", err)
		return err
	}
	zap.S().Named("collector_service").Debugw("sqlite file ready", "path", sqlitePath)

	result, err := f.store.Parser().IngestSqliteWithCollection(ctx, sqlitePath, collectionID)
	if err != nil {
		zap.S().Named("collector_service").Errorw("failed to ingest sqlite data", "error", err)
		return err
	}

	if err := f.store.Checkpoint(); err != nil {
		zap.S().Named("collector_service").Warnw("checkpoint after ingest failed", "error", err)
	}

	if result.HasErrors() {
		zap.S().Named("collector_service").Errorw("schema validation errors", "errors", result.Errors)
		return fmt.Errorf("schema validation failed: %v", result.Errors)
	}

	if len(result.Warnings) > 0 {
		zap.S().Named("collector_service").Warnw("schema validation warnings", "warnings", result.Warnings)
	}

	zap.S().Named("collector_service").Info("data successfully parsed into duckdb")

	if err := os.Remove(sqlitePath); err != nil {
		zap.S().Named("collector_service").Warnw("failed to remove sqlite file", "path", sqlitePath, "error", err)
	}

	return nil
}

// collectionVMIDs returns the hashed "VM ID" values for all vinfo rows in a collection.
// Used to scope BuildInventory to a single collection's data.
func collectionVMIDs(ctx context.Context, db *sql.DB, collectionID int64) ([]string, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT "VM ID" FROM vinfo WHERE collection_id = ?`, collectionID)
	if err != nil {
		return nil, fmt.Errorf("querying collection VM IDs: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (f *collectorWorkFactory) buildAndMarshalInventory(ctx context.Context, collectionID int64) ([]byte, error) {
	vmIDs, err := collectionVMIDs(ctx, f.store.DB(), collectionID)
	if err != nil {
		return nil, fmt.Errorf("fetching collection VM IDs for inventory build: %w", err)
	}
	inv, err := f.store.Parser().BuildInventory(ctx, vmIDs)
	if err != nil {
		return nil, fmt.Errorf("error building inventory: %w", err)
	}

	zap.S().Named("collector_service").Info("attempting to embed cluster utilization into inventory")
	invCopy, err := f.embedClusterUtilization(ctx, collectionID, *inv)
	if err != nil {
		zap.S().Named("collector_service").Warnw("failed to embed cluster utilization, continuing without it", "error", err)
	} else {
		inv = &invCopy
	}

	inventory, err := json.Marshal(converters.ToAPI(inv))
	if err != nil {
		return nil, fmt.Errorf("failed to marshal the inventory: %w", err)
	}

	return inventory, nil
}

// embedClusterUtilization fetches the latest cluster utilization data from rightsizing tables
// and populates each cluster's ClusterUtilization field.
// Only utilization percentages (CPU/memory avg/p95/max, confidence) are mapped for sizing calculations.
func (f *collectorWorkFactory) embedClusterUtilization(ctx context.Context, collectionID int64, inv inventory.Inventory) (inventory.Inventory, error) {
	zap.S().Named("collector_service").Debug("fetching cluster utilization from rightsizing store")
	_, clusters, err := f.store.RightSizing().ListLatestClusterUtilization(ctx, "", collectionID)
	if err != nil {
		zap.S().Named("collector_service").Errorw("error fetching cluster utilization from store", "error", err)
		return inventory.Inventory{}, fmt.Errorf("fetching cluster utilization: %w", err)
	}

	zap.S().Named("collector_service").Debugw("fetched cluster utilization from store", "cluster_count", len(clusters))

	if len(clusters) == 0 {
		zap.S().Named("collector_service").Debug("no rightsizing data available, inventory will not include cluster utilization")
		return inv, nil
	}

	// Create a new clusters map to avoid mutating the input
	newClusters := make(map[string]inventory.InventoryData, len(inv.Clusters))
	for clusterID, clusterData := range inv.Clusters {
		newClusters[clusterID] = clusterData
	}

	// Build map for O(1) lookup
	utilizationByClusterID := make(map[string]*inventory.ClusterUtilization, len(clusters))
	for _, c := range clusters {
		utilizationByClusterID[c.ClusterID] = &inventory.ClusterUtilization{
			CpuAvg:     sanitizePercentage("cpu_avg", c.CpuAvg),
			CpuP95:     sanitizePercentage("cpu_p95", c.CpuP95),
			CpuMax:     sanitizePercentage("cpu_max", c.CpuMax),
			MemAvg:     sanitizePercentage("mem_avg", c.MemAvg),
			MemP95:     sanitizePercentage("mem_p95", c.MemP95),
			MemMax:     sanitizePercentage("mem_max", c.MemMax),
			Confidence: sanitizePercentage("confidence", c.Confidence),
		}
	}

	// Embed utilization into each cluster's InventoryData
	embeddedCount := 0
	for clusterID := range newClusters {
		if util, exists := utilizationByClusterID[clusterID]; exists {
			clusterData := newClusters[clusterID]
			clusterData.ClusterUtilization = util
			newClusters[clusterID] = clusterData
			embeddedCount++
			zap.S().Named("collector_service").Debugw("embedded utilization for cluster",
				"cluster_id", clusterID,
				"cpu_p95", util.CpuP95,
				"mem_p95", util.MemP95,
				"confidence", util.Confidence)
		}
	}

	inv.Clusters = newClusters

	zap.S().Named("collector_service").Infow("embedded cluster utilization into inventory", "embedded_count", embeddedCount, "total_clusters", len(inv.Clusters))
	return inv, nil
}

func sanitizePercentage(name string, val float64) float64 {
	if math.IsNaN(val) || math.IsInf(val, 0) {
		zap.S().Named("collector_service").Warnw("invalid utilization value, using 0",
			"field", name, "value", val)
		return 0
	}
	if val < 0 {
		zap.S().Named("collector_service").Warnw("utilization below 0%, correcting to 0",
			"field", name, "value", val)
		return 0
	}
	if val > 100 {
		zap.S().Named("collector_service").Warnw("utilization above 100%, correcting to 100",
			"field", name, "value", val)
		return 100
	}
	return val
}

// copyForwardPersistentFields writes migration_excluded and labels from the previous
// collection into the new collection's vinfo rows, matched by vmmoid.
// Must run after reIDVMs (so vmmoid is set on all new rows).
func copyForwardPersistentFields(ctx context.Context, st *store.Store, result *models.CollectorResult) error {
	if len(result.VMPersistentData) == 0 {
		return nil
	}
	for vmMoid, data := range result.VMPersistentData {
		labelsJSON, err := json.Marshal(data.Labels)
		if err != nil {
			return fmt.Errorf("marshalling labels for %s: %w", vmMoid, err)
		}
		if _, err := st.DB().ExecContext(ctx, `
            UPDATE vinfo
            SET migration_excluded = ?, labels = ?
            WHERE vmmoid = ? AND collection_id = ?`,
			data.MigrationExcluded, string(labelsJSON), vmMoid, result.CollectionID,
		); err != nil {
			return fmt.Errorf("copy-forward for VM %s: %w", vmMoid, err)
		}
	}
	return nil
}

// refreshGroupMatches rebuilds group_matches for the new collection.
// Must run after copyForwardPersistentFields (so all vinfo rows are fully
// stamped) and before postCollectionBuilders (which may depend on group membership).
func refreshGroupMatches(ctx context.Context, st *store.Store, collectionID int64) error {
	return st.Group().RefreshMatches(ctx, collectionID)
}

// publishCollection is the final pipeline unit in Build.
// It computes VM/cluster counters, atomically flips the active collection from
// the previous run to the new one, saves the inventory blob, and runs retention.
func publishCollection(ctx context.Context, st *store.Store, cfg config.Agent, result *models.CollectorResult) error {
	// Compute VM/cluster counts for the new collection.
	counters, err := computeCounters(ctx, st, result.CollectionID, result.PrevCollectionID)
	if err != nil {
		return fmt.Errorf("computing counters: %w", err)
	}
	if err := st.Collection().UpdateCounters(ctx, result.CollectionID, counters); err != nil {
		return fmt.Errorf("updating counters: %w", err)
	}

	// Atomic flip: deactivate previous, mark new as done+active.
	err = st.WithTx(ctx, func(txCtx context.Context) error {
		if result.PrevCollectionID != 0 {
			if err := st.Collection().Deactivate(txCtx, result.PrevCollectionID); err != nil {
				return err
			}
		}
		return st.Collection().MarkDone(txCtx, result.CollectionID)
	})
	if err != nil {
		return fmt.Errorf("publishing collection: %w", err)
	}

	// Save the inventory blob (non-transactional; acceptable to retry).
	if result.Inventory != nil {
		if err := st.Inventory().Save(ctx, result.CollectionID, result.Inventory); err != nil {
			return fmt.Errorf("saving inventory blob: %w", err)
		}
	}

	// Retention: delete old collections beyond cfg.RetainCollections.
	return runRetention(ctx, st, cfg.RetainCollections)
}

// computeCounters counts VMs and clusters for a collection and computes delta
// counters relative to the previous collection.
// A VM is migratable when it has no Critical concerns and is not manually excluded
// (mirrors the definition used by migration-planner's MigratableCountQuery).
func computeCounters(ctx context.Context, st *store.Store, collectionID, prevCollectionID int64) (store.CollectionCounters, error) {
	var counters store.CollectionCounters

	var rowCount int
	if err := st.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM vinfo WHERE collection_id = ?`, collectionID,
	).Scan(&rowCount); err != nil {
		return counters, fmt.Errorf("checking vinfo row count: %w", err)
	}

	if rowCount > 0 {
		// Migratable: no Critical concern AND not manually excluded.
		// Mirrors migration-planner's migratable_count_query.go.tmpl.
		err := st.DB().QueryRowContext(ctx, `
			SELECT
				COUNT(DISTINCT CASE WHEN c."VM_ID" IS NULL AND COALESCE(v.migration_excluded, false) = false THEN v."VM ID" END) AS migratable,
				COUNT(DISTINCT CASE WHEN c."VM_ID" IS NOT NULL OR COALESCE(v.migration_excluded, false) = true  THEN v."VM ID" END) AS non_migratable,
				COUNT(*)                                                                                                             AS total
			FROM vinfo v
			LEFT JOIN concerns c ON v."VM ID" = c."VM_ID" AND c."Category" = 'Critical'
			WHERE v.collection_id = ?
		`, collectionID).Scan(&counters.VMCountMigratable, &counters.VMCountNonMigratable, &counters.VMCountTotal)
		if err != nil {
			return counters, fmt.Errorf("counting VMs: %w", err)
		}

		err = st.DB().QueryRowContext(ctx, `
			SELECT COUNT(DISTINCT "Cluster") FROM vinfo WHERE collection_id = ?
		`, collectionID).Scan(&counters.ClusterCountTotal)
		if err != nil {
			return counters, fmt.Errorf("counting clusters: %w", err)
		}
	}

	// Delta counters: compare vmmoid sets between new and previous collection.
	if prevCollectionID != 0 {
		err := st.DB().QueryRowContext(ctx, `
			SELECT
				COUNT(*) FILTER (WHERE new_col.vmmoid IS NULL) AS missing,
				COUNT(*) FILTER (WHERE prev_col.vmmoid IS NULL) AS new_vms
			FROM (SELECT vmmoid FROM vinfo WHERE collection_id = ?) AS prev_col
			FULL OUTER JOIN (SELECT vmmoid FROM vinfo WHERE collection_id = ?) AS new_col
				USING (vmmoid)
		`, prevCollectionID, collectionID).Scan(
			&counters.VMCountMissingSincePrevious,
			&counters.VMCountNewSincePrevious,
		)
		if err != nil {
			return counters, fmt.Errorf("computing delta counters: %w", err)
		}
		counters.VMCountDeltaSincePrevious = counters.VMCountNewSincePrevious - counters.VMCountMissingSincePrevious
		counters.VMCountMigratableDeltaSincePrevious = 0 // TODO: compute if needed
	}

	return counters, nil
}

// runRetention deletes done+inactive collections beyond the retain count.
func runRetention(ctx context.Context, st *store.Store, retain int) error {
	// Find done+inactive collections beyond the retain count, ordered newest first (id DESC),
	// then skip the first `retain` rows. The remainder are the oldest and can be deleted.
	toDelete, err := st.Collection().List(ctx,
		sq.Eq{"vcenter_id": defaultVCenterID, "state": string(models.CollectionStateDone), "active": false},
		store.WithOrderBy("id DESC"),
		store.WithOffset(uint64(retain)),
	)
	if err != nil {
		return fmt.Errorf("listing old collections: %w", err)
	}

	for _, col := range toDelete {
		if err := deleteCollectionRelationalData(ctx, st, col.ID); err != nil {
			return fmt.Errorf("deleting relational data for collection %d: %w", col.ID, err)
		}
		if err := st.RightSizing().DeleteByCollectionID(ctx, col.ID); err != nil {
			return fmt.Errorf("deleting rightsizing data for collection %d: %w", col.ID, err)
		}
		if err := st.Inventory().DeleteByCollectionID(ctx, col.ID); err != nil {
			return fmt.Errorf("deleting inventory for collection %d: %w", col.ID, err)
		}
		if err := st.Group().DeleteGroupInventoryForCollection(ctx, col.ID); err != nil {
			return fmt.Errorf("deleting group inventory for collection %d: %w", col.ID, err)
		}
		if err := st.Collection().Delete(ctx, col.ID); err != nil {
			return fmt.Errorf("deleting collection %d: %w", col.ID, err)
		}
	}
	return nil
}

// deleteCollectionRelationalData removes all relational rows for a collection
// (vSphere detail tables and vinfo), cascading from child to parent so FK
// constraints are not violated.
func deleteCollectionRelationalData(ctx context.Context, st *store.Store, collectionID int64) error {
	db := st.DB()
	// Delete inspection tables first (they have FK to vinfo).
	for _, tbl := range []string{"vm_inspection_concerns", "vm_inspection_status"} {
		if _, err := db.ExecContext(ctx,
			fmt.Sprintf(`DELETE FROM %q WHERE "VM ID" IN (SELECT "VM ID" FROM vinfo WHERE collection_id = ?)`, tbl),
			collectionID,
		); err != nil {
			return fmt.Errorf("deleting from %s: %w", tbl, err)
		}
	}

	// Delete vm_applications (uses vm_id column, not "VM ID").
	if _, err := db.ExecContext(ctx,
		`DELETE FROM vm_applications WHERE vm_id IN (SELECT "VM ID" FROM vinfo WHERE collection_id = ?)`,
		collectionID,
	); err != nil {
		return fmt.Errorf("deleting from vm_applications: %w", err)
	}

	// Delete group matches for this collection.
	if err := st.Group().DeleteMatchesForCollection(ctx, collectionID); err != nil {
		return fmt.Errorf("deleting group matches for collection %d: %w", collectionID, err)
	}

	// Per-VM tables that use "VM ID" (space) as the join column.
	// vhba, vcluster, vhost, vdatastore, dvswitch, dvport, and about are
	// collection-global tables with no VM ID column — they are overwritten on
	// each ingest and are not filtered per collection.
	for _, tbl := range []string{"vcpu", "vmemory", "vdisk", "vnetwork"} {
		if _, err := db.ExecContext(ctx,
			fmt.Sprintf(`DELETE FROM %q WHERE "VM ID" IN (SELECT "VM ID" FROM vinfo WHERE collection_id = ?)`, tbl),
			collectionID,
		); err != nil {
			return fmt.Errorf("deleting from %s: %w", tbl, err)
		}
	}

	// concerns uses "VM_ID" (underscore) as its FK column.
	if _, err := db.ExecContext(ctx,
		`DELETE FROM concerns WHERE "VM_ID" IN (SELECT "VM ID" FROM vinfo WHERE collection_id = ?)`,
		collectionID,
	); err != nil {
		return fmt.Errorf("deleting from concerns: %w", err)
	}
	// Delete vinfo rows last (other tables join to it).
	if _, err := db.ExecContext(ctx, `DELETE FROM vinfo WHERE collection_id = ?`, collectionID); err != nil {
		return fmt.Errorf("deleting vinfo: %w", err)
	}
	return nil
}

// setupCollection is the first pipeline unit in Build.
// It finds the previous active collection, reads persistent VM fields from it,
// then creates a new collection row in state=running and stores the assigned ID
// in result.CollectionID.
func setupCollection(ctx context.Context, st *store.Store, result *models.CollectorResult) error {
	// Capture the current active collection (will be deactivated at publish time).
	prev, err := st.Collection().List(ctx, sq.Eq{"vcenter_id": defaultVCenterID, "active": true})
	if err != nil {
		return fmt.Errorf("finding previous collection: %w", err)
	}
	if len(prev) > 0 {
		result.PrevCollectionID = prev[0].ID

		// Read persistent VM fields from the previous collection keyed by vmmoid.
		// These are carried through the pipeline and written back by copyForwardPersistentFields
		// after reIDVMs has hashed the new collection's VM IDs.
		rows, err := st.DB().QueryContext(ctx,
			`SELECT vmmoid, migration_excluded, labels
             FROM vinfo WHERE collection_id = ?`, result.PrevCollectionID)
		if err != nil {
			return fmt.Errorf("reading persistent VM fields: %w", err)
		}
		defer rows.Close() //nolint:errcheck
		result.VMPersistentData = make(map[string]models.PersistentVMData)
		for rows.Next() {
			var vmMoid, labelsJSON string
			var excluded bool
			if err := rows.Scan(&vmMoid, &excluded, &labelsJSON); err != nil {
				continue
			}
			var labels []string
			_ = json.Unmarshal([]byte(labelsJSON), &labels)
			result.VMPersistentData[vmMoid] = models.PersistentVMData{
				MigrationExcluded: excluded,
				Labels:            labels,
			}
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterating persistent VM fields: %w", err)
		}
	}

	// Create the new collection row; DB assigns the ID via sequence.
	now := time.Now()
	col, err := st.Collection().Create(ctx, models.Collection{
		VCenterID: defaultVCenterID,
		State:     models.CollectionStateRunning,
		StartedAt: &now,
	})
	if err != nil {
		return fmt.Errorf("creating collection: %w", err)
	}
	result.CollectionID = col.ID
	return nil
}
