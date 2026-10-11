package migrations_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"

	"github.com/kubev2v/rvtools-analyser/internal/store"
	"github.com/kubev2v/rvtools-analyser/internal/store/migrations"
)

func TestMigrations(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Migrations Suite")
}

var _ = Describe("Migrations", func() {
	var (
		ctx    context.Context
		pool   *store.Pool
		mainSt *store.Store
		collSt *store.Store
		tmpDir string
	)

	BeforeEach(func() {
		ctx = context.Background()

		var err error
		tmpDir, err = os.MkdirTemp("", "migrations-test-*")
		Expect(err).NotTo(HaveOccurred())

		pool = store.NewPool(5 * time.Minute)

		mainDB, err := pool.NewDatabase(store.MainDatabaseID, filepath.Join(tmpDir, "agent.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
		Expect(err).NotTo(HaveOccurred())
		Expect(mainDB.Migrate(ctx, migrations.RunMain)).To(Succeed())
		pool.Add(mainDB)
		mainSt, err = mainDB.Store()
		Expect(err).NotTo(HaveOccurred())

		collDB, err := pool.NewDatabase("coll", filepath.Join(tmpDir, "collection.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
		Expect(err).NotTo(HaveOccurred())
		Expect(collDB.Migrate(ctx, func(ctx context.Context, db *sql.DB) error {
			st, stErr := collDB.Store()
			if stErr != nil {
				return stErr
			}
			if pErr := duckdb_parser.New(st.Querier(), nil).Init(); pErr != nil {
				return pErr
			}
			return migrations.RunCollection(ctx, db, "collection")
		})).To(Succeed())
		pool.Add(collDB)
		collSt, err = collDB.Store()
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		if pool != nil {
			pool.Close()
		}
		if tmpDir != "" {
			_ = os.RemoveAll(tmpDir)
		}
	})

	Context("Main migrations", func() {
		It("does not create Console or credential tables", func() {
			for _, table := range []string{"configuration", "credentials", "vddk", "forecast_runs"} {
				var count int
				Expect(mainSt.Querier().QueryRowContext(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_name = ?`, table).Scan(&count)).To(Succeed())
				Expect(count).To(Equal(0))
			}
		})

		It("should create collections table", func() {
			_, err := mainSt.Querier().ExecContext(ctx, `
				INSERT INTO collections ("database", state)
				VALUES ('collection_default', 'running')
			`)
			Expect(err).NotTo(HaveOccurred())

			var ns string
			err = mainSt.Querier().QueryRowContext(ctx, `SELECT "database" FROM collections WHERE "database" = 'collection_default'`).Scan(&ns)
			Expect(err).NotTo(HaveOccurred())
			Expect(ns).To(Equal("collection_default"))
		})

		It("should track applied main migrations", func() {
			rows, err := mainSt.Querier().QueryContext(ctx, `SELECT version FROM schema_migrations ORDER BY version`)
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = rows.Close() }()

			var versions []int
			for rows.Next() {
				var v int
				Expect(rows.Scan(&v)).To(Succeed())
				versions = append(versions, v)
			}
			Expect(rows.Err()).NotTo(HaveOccurred())
			Expect(versions).To(ContainElement(1))

			for i, v := range versions {
				Expect(v).To(Equal(i + 1))
			}
		})
	})

	Context("Collection migrations", func() {
		It("should create inventory table", func() {
			_, err := collSt.Querier().ExecContext(ctx, `
				INSERT INTO inventory (id, data)
				VALUES (1, 'test data')
			`)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should track applied collection migrations", func() {
			rows, err := collSt.Querier().QueryContext(ctx, `SELECT version FROM collection_schema_migrations ORDER BY version`)
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = rows.Close() }()

			var versions []int
			for rows.Next() {
				var v int
				Expect(rows.Scan(&v)).To(Succeed())
				versions = append(versions, v)
			}
			Expect(rows.Err()).NotTo(HaveOccurred())
			Expect(versions).To(ContainElement(1))
		})
	})

	Context("Idempotency", func() {
		It("should be safe to run main migrations twice", func() {
			mainDB, err := pool.Get(store.MainDatabaseID)
			Expect(err).NotTo(HaveOccurred())
			Expect(mainDB.Migrate(ctx, migrations.RunMain)).To(Succeed())
		})

		It("should be safe to run collection migrations twice", func() {
			collDB, err := pool.Get("coll")
			Expect(err).NotTo(HaveOccurred())
			Expect(collDB.Migrate(ctx, func(ctx context.Context, db *sql.DB) error {
				return migrations.RunCollection(ctx, db, "collection")
			})).To(Succeed())
		})
	})

	Context("Upgrade from the previous schema", func() {
		It("removes obsolete main tables and preserves collection markers", func() {
			_, err := mainSt.Querier().ExecContext(ctx, `
				INSERT INTO collections ("database", state) VALUES ('collection_legacy', 'running');
				CREATE TABLE credentials (id VARCHAR PRIMARY KEY, url VARCHAR);
				INSERT INTO credentials VALUES ('old', 'https://example.invalid');
				DELETE FROM schema_migrations WHERE version = 2;
			`)
			Expect(err).NotTo(HaveOccurred())
			mainDB, err := pool.Get(store.MainDatabaseID)
			Expect(err).NotTo(HaveOccurred())
			Expect(mainDB.Migrate(ctx, migrations.RunMain)).To(Succeed())
			var state string
			Expect(mainSt.Querier().QueryRowContext(ctx, `SELECT state FROM collections WHERE "database" = 'collection_legacy'`).Scan(&state)).To(Succeed())
			Expect(state).To(Equal("running"))
			var count int
			Expect(mainSt.Querier().QueryRowContext(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_name = 'credentials'`).Scan(&count)).To(Succeed())
			Expect(count).To(Equal(0))
		})

		It("removes obsolete collection tables and preserves inventory, groups, and VM edits", func() {
			_, err := collSt.Querier().ExecContext(ctx, `
				INSERT INTO vinfo ("VM ID", "VM", "migration_excluded", "labels") VALUES ('vm-old', 'old VM', true, '["keep"]');
				INSERT INTO inventory (id, data) VALUES (1, 'old inventory');
				INSERT INTO groups (id, name, filter) VALUES ('00000000-0000-0000-0000-000000000001', 'keep-group', 'name = ''old VM''');
				CREATE TABLE vm_inspection_status ("VM ID" VARCHAR PRIMARY KEY, status VARCHAR);
				INSERT INTO vm_inspection_status VALUES ('vm-old', 'completed');
				CREATE TABLE rightsizing_reports (id VARCHAR PRIMARY KEY);
				CREATE TABLE rightsizing_metrics (report_id VARCHAR, FOREIGN KEY (report_id) REFERENCES rightsizing_reports(id));
				INSERT INTO rightsizing_reports VALUES ('old-report');
				INSERT INTO rightsizing_metrics VALUES ('old-report');
				ALTER TABLE vdatastore ADD COLUMN "Backing Devices" VARCHAR;
				DELETE FROM collection_schema_migrations WHERE version = 2;
			`)
			Expect(err).NotTo(HaveOccurred())
			collDB, err := pool.Get("coll")
			Expect(err).NotTo(HaveOccurred())
			Expect(collDB.Migrate(ctx, func(ctx context.Context, db *sql.DB) error {
				return migrations.RunCollection(ctx, db, "collection")
			})).To(Succeed())
			var excluded bool
			var labels string
			Expect(collSt.Querier().QueryRowContext(ctx, `SELECT "migration_excluded", "labels" FROM vinfo WHERE "VM ID" = 'vm-old'`).Scan(&excluded, &labels)).To(Succeed())
			Expect(excluded).To(BeTrue())
			Expect(labels).To(Equal(`["keep"]`))
			var count int
			Expect(collSt.Querier().QueryRowContext(ctx, `SELECT count(*) FROM groups WHERE name = 'keep-group'`).Scan(&count)).To(Succeed())
			Expect(count).To(Equal(1))
			Expect(collSt.Querier().QueryRowContext(ctx, `SELECT count(*) FROM inventory WHERE id = 1`).Scan(&count)).To(Succeed())
			Expect(count).To(Equal(1))
			for _, table := range []string{"vm_inspection_status", "rightsizing_reports", "rightsizing_metrics"} {
				Expect(collSt.Querier().QueryRowContext(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_name = ?`, table).Scan(&count)).To(Succeed())
				Expect(count).To(Equal(0))
			}
			Expect(collSt.Querier().QueryRowContext(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_name = 'vdatastore' AND column_name = 'Backing Devices'`).Scan(&count)).To(Succeed())
			Expect(count).To(Equal(0))
		})
	})
})
