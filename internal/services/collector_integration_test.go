package services

import (
	"context"
	"crypto/md5"
	"fmt"
	"testing"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/test"
)

// setupTestCollectorStore creates an in-memory DB with migrations applied and
// returns a *store.Store ready for use in collector pipeline tests.
func setupTestCollectorStore(t *testing.T) *store.Store {
	t.Helper()
	ctx := context.Background()

	db, err := store.NewDB(nil, ":memory:")
	if err != nil {
		t.Fatalf("create db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	st := store.NewStore(db, test.NewMockValidator())
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return st
}

func TestCopyForwardPersistentFields_PreservesExcludedAndLabels(t *testing.T) {
	st := setupTestCollectorStore(t)
	ctx := context.Background()

	col, err := st.Collection().Create(ctx, models.Collection{
		VCenterID: "default", State: models.CollectionStateRunning,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Insert a vinfo row simulating post-reIDVMs state:
	// "VM ID" is a hash, vmmoid is the original MOID.
	const vmMoid = "vm-moid-1"
	hashID := fmt.Sprintf("%x", md5.Sum([]byte(fmt.Sprintf("%d_%s", col.ID, vmMoid))))
	if _, err := st.DB().ExecContext(ctx,
		`INSERT INTO vinfo ("VM ID", "VM", collection_id, vmmoid, migration_excluded, labels)
         VALUES (?, 'Test VM', ?, ?, false, '[]')`,
		hashID, col.ID, vmMoid); err != nil {
		t.Fatalf("insert vinfo: %v", err)
	}

	result := &models.CollectorResult{
		CollectionID: col.ID,
		VMPersistentData: map[string]models.PersistentVMData{
			vmMoid: {MigrationExcluded: true, Labels: []string{"prod", "linux"}},
		},
	}

	if err := copyForwardPersistentFields(ctx, st, result); err != nil {
		t.Fatalf("copyForwardPersistentFields: %v", err)
	}

	var excluded bool
	var labelsJSON string
	if err := st.DB().QueryRowContext(ctx,
		`SELECT migration_excluded, labels FROM vinfo WHERE "VM ID" = ?`, hashID).
		Scan(&excluded, &labelsJSON); err != nil {
		t.Fatalf("scan vinfo: %v", err)
	}
	if !excluded {
		t.Error("expected migration_excluded=true after copy-forward")
	}
	if labelsJSON != `["prod","linux"]` {
		t.Errorf("expected labels [prod linux], got %s", labelsJSON)
	}
}

func TestCopyForwardPersistentFields_NoopWhenEmpty(t *testing.T) {
	st := setupTestCollectorStore(t)
	ctx := context.Background()

	col, _ := st.Collection().Create(ctx, models.Collection{VCenterID: "default", State: models.CollectionStateRunning})
	result := &models.CollectorResult{CollectionID: col.ID} // VMPersistentData is nil

	if err := copyForwardPersistentFields(ctx, st, result); err != nil {
		t.Fatalf("expected no-op, got error: %v", err)
	}
}
