package store_test

import (
	"context"
	"database/sql"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/test"
)

var _ = Describe("InventoryStore", func() {
	var (
		ctx context.Context
		s   *store.Store
		db  *sql.DB
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		db, err = store.NewDB(nil, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		Expect(migrations.Run(ctx, db)).To(Succeed())
		s = store.NewStore(db, test.NewMockValidator())
	})

	AfterEach(func() { _ = db.Close() })

	// helper: create a done+active collection and return its ID
	createActiveCollection := func() int64 {
		now := time.Now()
		col, err := s.Collection().Create(ctx, models.Collection{
			VCenterID: "default",
			State:     models.CollectionStateRunning,
			StartedAt: &now,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(s.Collection().MarkDone(ctx, col.ID)).To(Succeed())
		return col.ID
	}

	Describe("Save and GetByCollectionID", func() {
		It("stores and retrieves a blob", func() {
			colID := createActiveCollection()
			data := []byte(`{"vms":[]}`)
			Expect(s.Inventory().Save(ctx, colID, data)).To(Succeed())

			got, err := s.Inventory().GetByCollectionID(ctx, colID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Data).To(Equal(data))
		})

		It("upserts on second save", func() {
			colID := createActiveCollection()
			Expect(s.Inventory().Save(ctx, colID, []byte(`{"v":1}`))).To(Succeed())
			Expect(s.Inventory().Save(ctx, colID, []byte(`{"v":2}`))).To(Succeed())

			got, err := s.Inventory().GetByCollectionID(ctx, colID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Data).To(MatchJSON(`{"v":2}`))
		})

		It("returns ResourceNotFoundError for unknown collection", func() {
			_, err := s.Inventory().GetByCollectionID(ctx, 9999)
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
		})
	})

	Describe("GetActive", func() {
		It("returns ResourceNotFoundError when no active collection has inventory", func() {
			_, err := s.Inventory().GetActive(ctx, s.Collection())
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
		})

		It("returns the blob for the active collection", func() {
			colID := createActiveCollection()
			data := []byte(`{"active":true}`)
			Expect(s.Inventory().Save(ctx, colID, data)).To(Succeed())

			got, err := s.Inventory().GetActive(ctx, s.Collection())
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Data).To(Equal(data))
		})
	})

	Describe("DeleteByCollectionID", func() {
		It("removes the blob", func() {
			colID := createActiveCollection()
			Expect(s.Inventory().Save(ctx, colID, []byte(`{}`))).To(Succeed())
			Expect(s.Inventory().DeleteByCollectionID(ctx, colID)).To(Succeed())

			_, err := s.Inventory().GetByCollectionID(ctx, colID)
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
		})
	})
})
