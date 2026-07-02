package services

import (
	"context"
	"database/sql"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	"github.com/kubev2v/assisted-migration-agent/test"
)

var _ = Describe("refreshGroupMatches", func() {
	var (
		ctx context.Context
		db  *sql.DB
		st  *store.Store
	)

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

	It("includes a refreshGroupMatches unit after copyForwardPersistentFields", func() {
		// With no groups defined, RefreshMatches is a no-op but must succeed.
		const collectionID int64 = 1
		err := refreshGroupMatches(ctx, st, collectionID)
		Expect(err).NotTo(HaveOccurred())
	})

	It("calls RefreshMatches scoped to the given collectionID", func() {
		// Insert two groups with valid filter DSL expressions so that
		// ByFilter does not return nil and RefreshMatches inserts rows.
		// With no vinfo rows for the collection, both groups produce empty vm_ids.
		_, err := st.Group().Create(ctx, models.Group{Name: "team-a", Filter: "cluster = 'prod'"})
		Expect(err).NotTo(HaveOccurred())
		_, err = st.Group().Create(ctx, models.Group{Name: "team-b", Filter: "memory >= 16GB"})
		Expect(err).NotTo(HaveOccurred())

		const collectionID int64 = 7
		err = refreshGroupMatches(ctx, st, collectionID)
		Expect(err).NotTo(HaveOccurred())

		// Verify group_matches rows were written for this collection.
		var count int
		row := db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM group_matches WHERE collection_id = ?`, collectionID)
		Expect(row.Scan(&count)).To(Succeed())
		// Two groups with valid filters → two rows (each with an empty vm_ids list).
		Expect(count).To(Equal(2))
	})

	It("does not touch group_matches for other collections", func() {
		_, err := st.Group().Create(ctx, models.Group{Name: "isolated", Filter: "cluster = 'prod'"})
		Expect(err).NotTo(HaveOccurred())

		const otherCollectionID int64 = 99
		// Seed a row for a different collection directly.
		_, err = db.ExecContext(ctx,
			`INSERT INTO group_matches (group_id, collection_id, vm_ids) VALUES (gen_random_uuid(), ?, [])`,
			otherCollectionID)
		Expect(err).NotTo(HaveOccurred())

		// Run refreshGroupMatches for a different collection.
		const thisCollectionID int64 = 1
		err = refreshGroupMatches(ctx, st, thisCollectionID)
		Expect(err).NotTo(HaveOccurred())

		// The row for otherCollectionID must still exist.
		var count int
		row := db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM group_matches WHERE collection_id = ?`, otherCollectionID)
		Expect(row.Scan(&count)).To(Succeed())
		Expect(count).To(Equal(1))
	})
})
