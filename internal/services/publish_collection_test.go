package services

import (
	"context"
	"database/sql"
	"time"

	sq "github.com/Masterminds/squirrel"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/config"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	"github.com/kubev2v/assisted-migration-agent/test"
)

var _ = Describe("publishCollection", func() {
	var (
		ctx context.Context
		db  *sql.DB
		st  *store.Store
		cfg config.Agent
	)

	// createDoneActiveCollectionForPublish creates a running collection and marks it done+active.
	// Returns the created collection.
	createDoneActiveCollectionForPublish := func(ctx context.Context, st *store.Store) *models.Collection {
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
		cfg = config.Agent{RetainCollections: 1}
	})

	AfterEach(func() {
		if db != nil {
			_ = db.Close()
		}
	})

	It("atomically deactivates previous and marks new as done+active", func() {
		prev := createDoneActiveCollectionForPublish(ctx, st)

		now := time.Now()
		newCol, err := st.Collection().Create(ctx, models.Collection{
			VCenterID: "default", State: models.CollectionStateRunning, StartedAt: &now,
		})
		Expect(err).NotTo(HaveOccurred())

		result := &models.CollectorResult{
			CollectionID: newCol.ID, PrevCollectionID: prev.ID,
		}

		Expect(publishCollection(ctx, st, cfg, result)).To(Succeed())

		// New collection is now active.
		active, err := st.Collection().List(ctx, sq.Eq{"vcenter_id": "default", "active": true})
		Expect(err).NotTo(HaveOccurred())
		Expect(active).To(HaveLen(1))
		Expect(active[0].ID).To(Equal(newCol.ID))

		// Previous is no longer active.
		prevCols, err := st.Collection().List(ctx, sq.Eq{"id": prev.ID})
		Expect(err).NotTo(HaveOccurred())
		Expect(prevCols[0].Active).To(BeFalse())
	})

	It("succeeds when there is no previous collection", func() {
		now := time.Now()
		newCol, err := st.Collection().Create(ctx, models.Collection{
			VCenterID: "default", State: models.CollectionStateRunning, StartedAt: &now,
		})
		Expect(err).NotTo(HaveOccurred())

		result := &models.CollectorResult{
			CollectionID: newCol.ID, PrevCollectionID: 0,
		}

		Expect(publishCollection(ctx, st, cfg, result)).To(Succeed())

		active, err := st.Collection().List(ctx, sq.Eq{"vcenter_id": "default", "active": true})
		Expect(err).NotTo(HaveOccurred())
		Expect(active).To(HaveLen(1))
		Expect(active[0].ID).To(Equal(newCol.ID))
	})
})
