package services_test

import (
	"context"
	"database/sql"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/test"
)

var _ = Describe("RightsizingService", func() {
	var (
		ctx context.Context
		db  *sql.DB
		st  *store.Store
		svc *services.RightsizingService
	)

	BeforeEach(func() {
		ctx = context.Background()

		var err error
		db, err = store.NewDB(nil, ":memory:")
		Expect(err).NotTo(HaveOccurred())

		st = store.NewStore(db, test.NewMockValidator())
		Expect(st.Migrate(ctx)).To(Succeed())

		svc = services.NewRightsizingService(st)
	})

	AfterEach(func() {
		if db != nil {
			_ = db.Close()
		}
	})

	// seedReport creates a report with one VM and one metric via the store.
	seedReport := func(vcenter string) string {
		r := models.RightSizingReport{
			VCenter:             vcenter,
			ClusterID:           "domain-c123",
			IntervalID:          7200,
			WindowStart:         time.Now().Add(-720 * time.Hour).UTC(),
			WindowEnd:           time.Now().UTC(),
			ExpectedSampleCount: 360,
		}
		id, err := st.RightSizing().CreateReport(ctx, r, 1, 1)
		Expect(err).NotTo(HaveOccurred())

		Expect(st.RightSizing().WriteBatch(ctx, id, []models.RightSizingMetric{
			{VMName: "vm-a", MOID: "vm-100", MetricKey: "cpu.usagemhz.average",
				SampleCount: 360, Average: 500, P95: 1200, P99: 1500, Max: 2000, Latest: 450},
		})).To(Succeed())
		return id
	}

	Describe("ListReports", func() {
		It("should return an empty slice when no reports exist", func() {
			reports, err := svc.ListReports(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(reports).To(BeEmpty())
		})

		It("should return stored reports with VM metrics populated", func() {
			id := seedReport("https://vcenter.example.com")

			reports, err := svc.ListReports(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(reports).To(HaveLen(1))
			Expect(reports[0].ID).To(Equal(id))
			Expect(reports[0].VMs).To(HaveLen(1))
			Expect(reports[0].VMs[0].Metrics).To(HaveKey("cpu.usagemhz.average"))
		})
	})

	Describe("GetReport", func() {
		It("should return the report with all VM metrics", func() {
			id := seedReport("https://vcenter.example.com")

			report, err := svc.GetReport(ctx, id)
			Expect(err).NotTo(HaveOccurred())
			Expect(report.ID).To(Equal(id))
			Expect(report.VMs).To(HaveLen(1))
			Expect(report.VMs[0].Metrics["cpu.usagemhz.average"].SampleCount).To(Equal(360))
		})

		It("should return a ResourceNotFoundError for unknown IDs", func() {
			_, err := svc.GetReport(ctx, "does-not-exist")
			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
		})
	})

	Describe("TriggerCollection", func() {
		It("should return an error (not yet implemented)", func() {
			_, err := svc.TriggerCollection(ctx, models.RightsizingParams{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not yet implemented"))
		})
	})
})
