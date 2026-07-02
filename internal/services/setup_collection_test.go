package services

import (
	"context"
	"database/sql"
	"time"

	sq "github.com/Masterminds/squirrel"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	"github.com/kubev2v/assisted-migration-agent/test"
)

var _ = Describe("setupCollection", func() {
	var (
		ctx context.Context
		db  *sql.DB
		st  *store.Store
	)

	// createDoneActiveCollection creates a running collection and marks it done+active.
	// Returns the created collection.
	createDoneActiveCollection := func(ctx context.Context, st *store.Store) *models.Collection {
		now := time.Now()
		col := models.Collection{
			VCenterID: "default",
			VCenter:   "vc-01.example.com",
			State:     models.CollectionStateRunning,
			Active:    false,
			StartedAt: &now,
		}
		created, err := st.Collection().Create(ctx, col)
		ExpectWithOffset(1, err).NotTo(HaveOccurred())
		ExpectWithOffset(1, st.Collection().MarkDone(ctx, created.ID)).To(Succeed())

		// Re-read to get the updated active=true state.
		cols, err := st.Collection().List(ctx, sq.Eq{"id": created.ID})
		ExpectWithOffset(1, err).NotTo(HaveOccurred())
		ExpectWithOffset(1, cols).To(HaveLen(1))
		return &cols[0]
	}

	BeforeEach(func() {
		ctx = context.Background()

		var err error
		db, err = store.NewDB(nil, ":memory:")
		Expect(err).NotTo(HaveOccurred())

		err = migrations.Run(ctx, db)
		Expect(err).NotTo(HaveOccurred())

		st = store.NewStore(db, test.NewMockValidator())
	})

	AfterEach(func() {
		if db != nil {
			_ = db.Close()
		}
	})

	It("creates a running collection row and captures its ID", func() {
		// Setup: real in-memory DB with migrations applied.
		result := &models.CollectorResult{}
		err := setupCollection(ctx, st, result)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.CollectionID).To(BeNumerically(">", 0))

		// Verify a running collection exists in the DB.
		cols, err := st.Collection().List(ctx,
			sq.Eq{"vcenter_id": "default", "state": "running"})
		Expect(err).NotTo(HaveOccurred())
		Expect(cols).To(HaveLen(1))
		Expect(cols[0].ID).To(Equal(result.CollectionID))
	})

	It("captures the previous active collection ID and its persistent VM data", func() {
		prev := createDoneActiveCollection(ctx, st) // helper that creates + marks done+active

		// Insert a vinfo row in the previous collection with non-default persistent fields.
		_, err := st.DB().ExecContext(ctx,
			`INSERT INTO vinfo ("VM ID", "VM", collection_id, vmmoid, migration_excluded, labels)
             VALUES ('hash-prev', 'Prev VM', ?, 'vm-orig-1', true, '["tag1"]')`,
			prev.ID)
		Expect(err).NotTo(HaveOccurred())

		result := &models.CollectorResult{}
		Expect(setupCollection(ctx, st, result)).To(Succeed())
		Expect(result.PrevCollectionID).To(Equal(prev.ID))
		Expect(result.VMPersistentData).To(HaveKey("vm-orig-1"))
		Expect(result.VMPersistentData["vm-orig-1"].MigrationExcluded).To(BeTrue())
		Expect(result.VMPersistentData["vm-orig-1"].Labels).To(ConsistOf("tag1"))
	})
})
