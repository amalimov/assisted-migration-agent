package migrations_test

import (
	"context"
	"database/sql"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
)

func TestMigrations(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Migrations Suite")
}

var _ = Describe("Migrations", func() {
	var (
		ctx context.Context
		db  *sql.DB
	)

	BeforeEach(func() {
		ctx = context.Background()

		var err error
		db, err = store.NewDB(nil, ":memory:")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		if db != nil {
			_ = db.Close()
		}
	})

	Context("Run", func() {
		It("should run all migrations successfully", func() {
			err := migrations.Run(ctx, db)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should create configuration table", func() {
			err := migrations.Run(ctx, db)
			Expect(err).NotTo(HaveOccurred())

			// Verify configuration table exists by inserting data
			_, err = db.ExecContext(ctx, `
				INSERT INTO configuration (id, agent_mode)
				VALUES (1, 'disconnected')
			`)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should create collection-versioned inventory table", func() {
			err := migrations.Run(ctx, db)
			Expect(err).NotTo(HaveOccurred())

			// First we need a collection row so the FK is satisfied.
			_, err = db.ExecContext(ctx, `
				INSERT INTO collections (vcenter_id, state, active)
				VALUES ('default', 'done', true)
			`)
			Expect(err).NotTo(HaveOccurred())

			var colID int64
			err = db.QueryRowContext(ctx, `SELECT id FROM collections WHERE vcenter_id = 'default' LIMIT 1`).Scan(&colID)
			Expect(err).NotTo(HaveOccurred())

			_, err = db.ExecContext(ctx, `INSERT INTO inventory (collection_id, data) VALUES (?, 'blob')`, colID)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should create group_inventory table", func() {
			err := migrations.Run(ctx, db)
			Expect(err).NotTo(HaveOccurred())

			var groupID string
			err = db.QueryRowContext(ctx, `
				INSERT INTO groups (id, name, filter, created_at, updated_at)
				VALUES (gen_random_uuid(), 'g1', 'memory >= 1', now(), now())
				RETURNING id::VARCHAR
			`).Scan(&groupID)
			Expect(err).NotTo(HaveOccurred())

			_, err = db.ExecContext(ctx, `
				INSERT INTO group_inventory (group_id, collection_id, inventory_data)
				VALUES (?, 1, NULL)
			`, groupID)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should be idempotent", func() {
			// Run migrations twice
			err := migrations.Run(ctx, db)
			Expect(err).NotTo(HaveOccurred())

			err = migrations.Run(ctx, db)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should track applied migrations in schema_migrations table", func() {
			err := migrations.Run(ctx, db)
			Expect(err).NotTo(HaveOccurred())

			// Verify schema_migrations table has entries
			rows, err := db.QueryContext(ctx, `SELECT version FROM schema_migrations ORDER BY version`)
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				_ = rows.Close()
			}()

			var versions []int
			for rows.Next() {
				var v int
				err := rows.Scan(&v)
				Expect(err).NotTo(HaveOccurred())
				versions = append(versions, v)
			}
			Expect(rows.Err()).NotTo(HaveOccurred())

			Expect(versions).To(ContainElements(1))
		})

		// Given migrations have been applied
		// When we check the version ordering
		// Then versions should be sequential starting from 1
		It("should apply migrations in sequential order", func() {
			err := migrations.Run(ctx, db)
			Expect(err).NotTo(HaveOccurred())

			rows, err := db.QueryContext(ctx, `SELECT version FROM schema_migrations ORDER BY version`)
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				_ = rows.Close()
			}()

			var versions []int
			for rows.Next() {
				var v int
				Expect(rows.Scan(&v)).To(Succeed())
				versions = append(versions, v)
			}
			Expect(rows.Err()).NotTo(HaveOccurred())

			// Versions should be sequential
			for i, v := range versions {
				Expect(v).To(Equal(i + 1))
			}
		})

		// Given migrations have been applied
		// When we insert a row into credentials using the new columns
		// Then it should succeed, confirming skip_tls and ca_cert exist
		It("should add skip_tls and ca_cert columns to credentials table", func() {
			err := migrations.Run(ctx, db)
			Expect(err).NotTo(HaveOccurred())

			_, err = db.ExecContext(ctx, `
				INSERT INTO credentials (id, url, username, password, skip_tls, ca_cert)
				VALUES ('test-tls', 'https://vc.local', 'u', 'p', true, 'cert')
			`)
			Expect(err).NotTo(HaveOccurred())
		})

		// Given migrations have been applied
		// When we check the vm_inspection_status table
		// Then it should exist and accept inserts
		It("should create vm_inspection_status table", func() {
			err := migrations.Run(ctx, db)
			Expect(err).NotTo(HaveOccurred())

			// Insert a row into vinfo first (FK constraint)
			_, err = db.ExecContext(ctx, `
				INSERT INTO vinfo ("VM ID", "VM") VALUES ('vm-1', 'test-vm')
			`)
			Expect(err).NotTo(HaveOccurred())

			_, err = db.ExecContext(ctx, `
				INSERT INTO vm_inspection_status ("VM ID", status)
				VALUES ('vm-1', 'pending')
			`)
			Expect(err).NotTo(HaveOccurred())
		})

		// Given migrations have been applied
		// When we insert a row into collections without specifying id
		// Then it should succeed and get a sequence-assigned ID > 0
		It("should create collections table with sequence-assigned id", func() {
			err := migrations.Run(ctx, db)
			Expect(err).NotTo(HaveOccurred())

			_, err = db.ExecContext(ctx, `
				INSERT INTO collections (vcenter_id, state, active)
				VALUES ('default', 'running', false)
			`)
			Expect(err).NotTo(HaveOccurred())

			var id int64
			err = db.QueryRowContext(ctx, `SELECT id FROM collections WHERE vcenter_id = 'default'`).Scan(&id)
			Expect(err).NotTo(HaveOccurred())
			Expect(id).To(BeNumerically(">", 0))
		})

		It("should add vmmoid and collection_id to vinfo", func() {
			err := migrations.Run(ctx, db)
			Expect(err).NotTo(HaveOccurred())

			_, err = db.ExecContext(ctx, `
				INSERT INTO vinfo ("VM ID", "VM", vmmoid, collection_id)
				VALUES ('hash-abc123', 'Test VM', 'vm-123', 1)
			`)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should rebuild group_matches with composite PK", func() {
			err := migrations.Run(ctx, db)
			Expect(err).NotTo(HaveOccurred())

			var groupID string
			err = db.QueryRowContext(ctx, `
				INSERT INTO groups (id, name, filter, created_at, updated_at)
				VALUES (gen_random_uuid(), 'gm-test', 'memory >= 1', now(), now())
				RETURNING id::VARCHAR
			`).Scan(&groupID)
			Expect(err).NotTo(HaveOccurred())

			// Composite PK: same group_id, different collection_ids should both insert.
			_, err = db.ExecContext(ctx, `
				INSERT INTO group_matches (group_id, collection_id, vm_ids)
				VALUES (?, 1, ['vm-a']), (?, 2, ['vm-b'])
			`, groupID, groupID)
			Expect(err).NotTo(HaveOccurred())
		})

	})
})
