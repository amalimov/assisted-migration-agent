package vmselection_test

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	"github.com/kubev2v/assisted-migration-agent/internal/vmselection"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/test"
)

func collection(t *testing.T, pool *store.Pool, id string) *store.Store {
	t.Helper()
	db, err := pool.NewDatabase(id, filepath.Join(t.TempDir(), id+".duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(context.Background(), func(ctx context.Context, sqlDB *sql.DB) error {
		st, err := db.Store()
		if err != nil {
			return err
		}
		if err := duckdb_parser.New(st.Querier(), test.NewMockValidator()).Init(); err != nil {
			return err
		}
		return migrations.RunCollection(ctx, sqlDB, id)
	}); err != nil {
		t.Fatal(err)
	}
	pool.Add(db)
	st, err := db.Store()
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func insertVM(t *testing.T, st *store.Store, id, cluster string, excluded bool, diskMiB int64) {
	t.Helper()
	ctx := context.Background()
	_, err := st.Querier().ExecContext(ctx, `INSERT INTO vinfo ("VM ID", "VM", "Cluster", "migration_excluded", "OS according to the VMware Tools", "OsDiskComplexity") VALUES (?, ?, ?, ?, 'Red Hat Enterprise Linux 8', 2)`, id, id, cluster, excluded)
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.Querier().ExecContext(ctx, `INSERT INTO vdisk ("VM ID", "Capacity MiB") VALUES (?, ?)`, id, diskMiB)
	if err != nil {
		t.Fatal(err)
	}
}

func ids(vms []models.VM) []string {
	result := make([]string, len(vms))
	for i, vm := range vms {
		result[i] = vm.ID
	}
	return result
}

func TestResolverCollectionScopeAndSelectors(t *testing.T) {
	ctx := context.Background()
	pool := store.NewPool(time.Minute)
	t.Cleanup(pool.Close)
	first := collection(t, pool, "first")
	var duckDBRounded int
	if err := first.Querier().QueryRowContext(ctx, `SELECT CAST(CAST(2.5 AS DOUBLE) AS INTEGER)`).Scan(&duckDBRounded); err != nil {
		t.Fatal(err)
	}
	if duckDBRounded != int(math.RoundToEven(2.5)) {
		t.Fatalf("DuckDB integer cast rounds 2.5 to %d, unlike the estimation adapter", duckDBRounded)
	}
	insertVM(t, first, "vm-1", "cluster-a", false, 400)
	insertVM(t, first, "vm-2", "cluster-b", false, 600)
	insertVM(t, first, "vm-3", "cluster-a", true, 900)
	group, err := first.Group().Create(ctx, models.Group{Name: "selected", Filter: "name = 'vm-1'"})
	if err != nil {
		t.Fatal(err)
	}
	emptyGroup, err := first.Group().Create(ctx, models.Group{Name: "empty", Filter: "name = 'never'"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = first.Querier().ExecContext(ctx, `INSERT INTO group_matches (group_id, vm_ids) VALUES (?, ['vm-1', 'vm-2'])`, group.ID)
	if err != nil {
		t.Fatal(err)
	}
	second := collection(t, pool, "second")
	insertVM(t, second, "vm-latest", "cluster-c", false, 1024)
	resolver := vmselection.NewResolver(pool)

	tests := []struct {
		name      string
		id        string
		selectors vmselection.VMSelectors
		want      []string
	}{
		{"all first", "first", vmselection.VMSelectors{All: true}, []string{"vm-1", "vm-2", "vm-3"}},
		{"all second", "second", vmselection.VMSelectors{All: true}, []string{"vm-latest"}},
		{"VM IDs", "first", vmselection.VMSelectors{VMIDs: []string{"vm-2"}}, []string{"vm-2"}},
		{"Groups", "first", vmselection.VMSelectors{GroupIDs: []string{group.ID.String()}}, []string{"vm-1", "vm-2"}},
		{"empty Group", "first", vmselection.VMSelectors{GroupIDs: []string{emptyGroup.ID.String()}}, []string{}},
		{"clusters", "first", vmselection.VMSelectors{Clusters: []string{"cluster-a"}}, []string{"vm-1", "vm-3"}},
		{"mixed union and dedup", "first", vmselection.VMSelectors{VMIDs: []string{"vm-1", "vm-1"}, GroupIDs: []string{group.ID.String()}, Clusters: []string{"cluster-a"}}, []string{"vm-1", "vm-2", "vm-3"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolver.Resolve(ctx, tt.id, vmselection.VMSelection{Include: tt.selectors})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(ids(got), tt.want) {
				t.Fatalf("IDs = %v, want %v", ids(got), tt.want)
			}
			if tt.name == "all first" {
				if !got[2].MigrationExcluded || got[0].DiskSize != 400 || got[0].OsDiskComplexity != 2 {
					t.Fatalf("estimation fields or excluded VM missing: %+v", got)
				}
			}
		})
	}
	id, err := resolver.LatestCollectionID()
	if err != nil || id != "second" {
		t.Fatalf("latest = %q, %v", id, err)
	}

	for _, selection := range []vmselection.VMSelection{
		{},
		{Include: vmselection.VMSelectors{All: true, VMIDs: []string{"vm-1"}}},
		{Include: vmselection.VMSelectors{GroupIDs: []string{"not-a-uuid"}}},
	} {
		_, err := resolver.Resolve(ctx, "first", selection)
		if !errors.Is(err, vmselection.ErrInvalidSelection) {
			t.Fatalf("selection %+v: got %v, want invalid selection", selection, err)
		}
	}
	for _, tc := range []struct {
		id        string
		selectors vmselection.VMSelectors
	}{
		{"missing", vmselection.VMSelectors{All: true}},
		{"first", vmselection.VMSelectors{VMIDs: []string{"vm-latest"}}},
		{"first", vmselection.VMSelectors{Clusters: []string{"cluster-c"}}},
		{"first", vmselection.VMSelectors{GroupIDs: []string{"00000000-0000-0000-0000-000000000000"}}},
	} {
		_, err := resolver.Resolve(ctx, tc.id, vmselection.VMSelection{Include: tc.selectors})
		if !srvErrors.IsResourceNotFoundError(err) {
			t.Fatalf("collection %q, selectors %+v: got %v, want not found", tc.id, tc.selectors, err)
		}
	}
}

func TestLatestCollectionRequiresCollection(t *testing.T) {
	pool := store.NewPool(time.Minute)
	t.Cleanup(pool.Close)
	resolver := vmselection.NewResolver(pool)
	if _, err := resolver.LatestCollectionID(); !srvErrors.IsResourceNotFoundError(err) {
		t.Fatalf("latest without a collection: got %v", err)
	}
	if _, err := resolver.Resolve(context.Background(), store.MainDatabaseID, vmselection.VMSelection{Include: vmselection.VMSelectors{All: true}}); !srvErrors.IsResourceNotFoundError(err) {
		t.Fatalf("main database must not be an estimation collection: got %v", err)
	}
}
