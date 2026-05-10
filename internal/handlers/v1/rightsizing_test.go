package v1_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "github.com/kubev2v/assisted-migration-agent/api/v1"
	"github.com/kubev2v/assisted-migration-agent/internal/config"
	handlers "github.com/kubev2v/assisted-migration-agent/internal/handlers/v1"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

var _ = Describe("Rightsizing Handlers", func() {
	var (
		mockSvc *MockRightsizingService
		handler *handlers.Handler
		router  *gin.Engine
		now     time.Time
	)

	BeforeEach(func() {
		gin.SetMode(gin.TestMode)
		now = time.Now().UTC().Truncate(time.Second)
		mockSvc = &MockRightsizingService{}
		handler = handlers.NewHandler(config.Configuration{}).WithRightsizingService(mockSvc)
		router = gin.New()
		router.GET("/rightsizing", handler.ListRightsizingReports)
		router.GET("/rightsizing/:report_id", func(c *gin.Context) {
			handler.GetRightsizingReport(c, c.Param("report_id"))
		})
		router.POST("/rightsizing", handler.TriggerRightsizingCollection)
		router.GET("/rightsizing/:report_id/clusters", func(c *gin.Context) {
			handler.ListRightsizingReportClusters(c, c.Param("report_id"), v1.ListRightsizingReportClustersParams{})
		})
		router.GET("/rightsizing/:report_id/clusters/:cluster_id", func(c *gin.Context) {
			handler.GetRightsizingReportCluster(c, c.Param("report_id"), c.Param("cluster_id"))
		})
	})

	Describe("ListRightsizingReports", func() {
		It("should return 200 with empty list when no reports exist", func() {
			mockSvc.ListResult = []models.RightsizingReportSummary{}
			req := httptest.NewRequest(http.MethodGet, "/rightsizing", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			var resp v1.RightsizingReportListResponse
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp.Total).To(Equal(0))
			Expect(resp.Reports).To(BeEmpty())
		})

		It("should return all reports without VM metrics", func() {
			mockSvc.ListResult = []models.RightsizingReportSummary{
				{ID: "a", VCenter: "https://vc1", WindowStart: now, WindowEnd: now, IntervalID: 7200, CreatedAt: now},
				{ID: "b", VCenter: "https://vc2", WindowStart: now, WindowEnd: now, IntervalID: 7200, CreatedAt: now},
			}
			req := httptest.NewRequest(http.MethodGet, "/rightsizing", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			var resp v1.RightsizingReportListResponse
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp.Total).To(Equal(2))
			Expect(resp.Reports).To(HaveLen(2))
			ids := []string{resp.Reports[0].Id, resp.Reports[1].Id}
			Expect(ids).To(ConsistOf("a", "b"))
		})

		It("should return 500 when service returns an error", func() {
			mockSvc.ListError = errors.New("storage failure")
			req := httptest.NewRequest(http.MethodGet, "/rightsizing", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusInternalServerError))
			var body map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &body)).To(Succeed())
			Expect(body["error"]).To(Equal("storage failure"))
		})
	})

	Describe("GetRightsizingReport", func() {
		It("should return 200 with the report", func() {
			report := models.RightsizingReport{
				ID:                  "report-1",
				VCenter:             "https://vcenter.example.com",
				WindowStart:         now.Add(-720 * time.Hour),
				WindowEnd:           now,
				IntervalID:          7200,
				ExpectedSampleCount: 360,
				VMs: []models.RightsizingVMReport{
					{
						Name: "vm-1",
						MOID: "vm-101",
						Metrics: map[string]models.RightsizingMetricStats{
							"cpu.usagemhz.average": {SampleCount: 360, Average: 1200, P95: 2400, P99: 2800, Max: 3000, Latest: 1100},
						},
					},
				},
				CreatedAt: now,
			}
			mockSvc.GetResult = &report

			req := httptest.NewRequest(http.MethodGet, "/rightsizing/report-1", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			var resp v1.RightsizingReport
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp.Id).To(Equal("report-1"))
			Expect(resp.Vcenter).To(Equal("https://vcenter.example.com"))
			Expect(resp.Vms).To(HaveLen(1))
			Expect(resp.Vms[0].Name).To(Equal("vm-1"))
			Expect(mockSvc.LastGetID).To(Equal("report-1"))
		})

		It("should return 404 when report does not exist", func() {
			mockSvc.GetError = srvErrors.NewResourceNotFoundError("rightsizing report", "missing-id")
			req := httptest.NewRequest(http.MethodGet, "/rightsizing/missing-id", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusNotFound))
			var body map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &body)).To(Succeed())
			Expect(body["error"]).To(ContainSubstring("rightsizing report 'missing-id' not found"))
		})

		It("should return 500 for unexpected errors", func() {
			mockSvc.GetError = errors.New("db connection lost")
			req := httptest.NewRequest(http.MethodGet, "/rightsizing/any-id", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusInternalServerError))
			var body map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &body)).To(Succeed())
			Expect(body["error"]).To(Equal("db connection lost"))
		})
	})

	Describe("TriggerRightsizingCollection", func() {
		It("should return 400 for invalid JSON", func() {
			req := httptest.NewRequest(http.MethodPost, "/rightsizing", bytes.NewReader([]byte("not json")))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
			var body map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &body)).To(Succeed())
			Expect(body["error"]).To(ContainSubstring("invalid request body"))
		})

		It("should return 400 when credentials are missing", func() {
			body := map[string]any{"lookback_hours": 720}
			bodyBytes, _ := json.Marshal(body)
			req := httptest.NewRequest(http.MethodPost, "/rightsizing", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
			var respBody map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &respBody)).To(Succeed())
			Expect(respBody["error"]).NotTo(BeEmpty())
		})

		It("should return 202 with the created report summary (no vms field)", func() {
			createdReport := models.RightsizingReportSummary{
				ID:                  "new-report-uuid",
				VCenter:             "https://vcenter.example.com",
				ClusterID:           "domain-c123",
				WindowStart:         now.Add(-720 * time.Hour),
				WindowEnd:           now,
				IntervalID:          7200,
				ExpectedSampleCount: 360,
				CreatedAt:           now,
			}
			mockSvc.TriggerResult = &createdReport

			lookbackHours := 720
			intervalID := 7200
			clusterId := "domain-c123"
			body := v1.RightsizingCollectRequest{
				Credentials: v1.VcenterCredentials{
					Url:      "https://vcenter.example.com",
					Username: "admin",
					Password: "secret",
				},
				LookbackHours: &lookbackHours,
				IntervalId:    &intervalID,
				ClusterId:     &clusterId,
			}
			bodyBytes, _ := json.Marshal(body)
			req := httptest.NewRequest(http.MethodPost, "/rightsizing", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusAccepted))
			var resp v1.RightsizingReportSummary
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp.Id).To(Equal("new-report-uuid"))
			Expect(mockSvc.TriggerCallCount).To(Equal(1))
			Expect(mockSvc.LastTriggerParams.URL).To(Equal("https://vcenter.example.com"))
			Expect(mockSvc.LastTriggerParams.LookbackH).To(Equal(720))
			Expect(mockSvc.LastTriggerParams.IntervalID).To(Equal(7200))
			Expect(mockSvc.LastTriggerParams.ClusterID).To(Equal("domain-c123"))
		})

		It("should return 500 when service returns an error", func() {
			mockSvc.TriggerError = errors.New("internal error")
			body := v1.RightsizingCollectRequest{
				Credentials: v1.VcenterCredentials{
					Url:      "https://vcenter.example.com",
					Username: "admin",
					Password: "secret",
				},
			}
			bodyBytes, _ := json.Marshal(body)
			req := httptest.NewRequest(http.MethodPost, "/rightsizing", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusInternalServerError))
		})
	})

	Describe("ListRightsizingReportClusters", func() {
		It("should return 404 when report does not exist", func() {
			mockSvc.GetError = srvErrors.NewResourceNotFoundError("rightsizing report", "no-such-id")
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/rightsizing/no-such-id/clusters", nil)
			router.ServeHTTP(w, req)
			Expect(w.Code).To(Equal(http.StatusNotFound))
		})

		It("should return 200 with cluster list when found", func() {
			mockSvc.ClusterUtilizationResult = []models.RightsizingClusterUtilization{
				{ClusterID: "domain-c1", ClusterName: "prod-cluster", VMCount: 3},
			}
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/rightsizing/report-123/clusters", nil)
			router.ServeHTTP(w, req)
			Expect(w.Code).To(Equal(http.StatusOK))
			var resp v1.RightsizingClusterListResponse
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp.ReportId).To(Equal("report-123"))
			Expect(resp.Clusters).To(HaveLen(1))
			Expect(resp.Clusters[0].ClusterId).To(Equal("domain-c1"))
		})
	})

	Describe("GetRightsizingReportCluster", func() {
		It("should return 404 when report does not exist", func() {
			mockSvc.GetError = srvErrors.NewResourceNotFoundError("rightsizing report", "no-such-id")
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/rightsizing/no-such-id/clusters/domain-c1", nil)
			router.ServeHTTP(w, req)
			Expect(w.Code).To(Equal(http.StatusNotFound))
		})

		It("should return 404 when cluster is not found in the report", func() {
			mockSvc.ClusterUtilizationResult = []models.RightsizingClusterUtilization{}
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/rightsizing/report-123/clusters/domain-c1", nil)
			router.ServeHTTP(w, req)
			Expect(w.Code).To(Equal(http.StatusNotFound))
		})

		It("should return 200 with the cluster when found", func() {
			mockSvc.ClusterUtilizationResult = []models.RightsizingClusterUtilization{
				{ClusterID: "domain-c1", ClusterName: "prod-cluster", VMCount: 3},
			}
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/rightsizing/report-123/clusters/domain-c1", nil)
			router.ServeHTTP(w, req)
			Expect(w.Code).To(Equal(http.StatusOK))
			var resp v1.RightsizingClusterResponse
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp.ReportId).To(Equal("report-123"))
			Expect(resp.Cluster.ClusterId).To(Equal("domain-c1"))
			Expect(resp.Cluster.VmCount).To(Equal(3))
		})
	})
})
