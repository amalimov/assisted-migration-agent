package store

import (
	"context"
	"fmt"
	"math"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

const (
	rsReportsTable                  = "rightsizing_reports"
	rsReportsColID                  = "id"
	rsReportsColVCenter             = "vcenter"
	rsReportsColClusterID           = "cluster_id"
	rsReportsColIntervalID          = "interval_id"
	rsReportsColWindowStart         = "window_start"
	rsReportsColWindowEnd           = "window_end"
	rsReportsColExpectedSampleCount = "expected_sample_count"
	rsReportsColExpectedBatchCount  = "expected_batch_count"
	rsReportsColWrittenBatchCount   = "written_batch_count"

	rsMetricsTable          = "rightsizing_metrics"
	rsMetricsColReportID    = "report_id"
	rsMetricsColVMName      = "vm_name"
	rsMetricsColMOID        = "moid"
	rsMetricsColMetricKey   = "metric_key"
	rsMetricsColSampleCount = "sample_count"
	rsMetricsColAverage     = "average"
	rsMetricsColP95         = "p95"
	rsMetricsColP99         = "p99"
	rsMetricsColMax         = "max"
	rsMetricsColLatest      = "latest"
)

// RightSizingStore persists rightsizing report metadata and per-VM metric aggregates.
type RightSizingStore struct {
	db QueryInterceptor
}

func NewRightSizingStore(db QueryInterceptor) *RightSizingStore {
	return &RightSizingStore{db: db}
}

// CreateReport inserts a new report record and returns its UUID.
// expectedBatchCount = ceil(vmCount / batchSize).
func (s *RightSizingStore) CreateReport(ctx context.Context, r models.RightSizingReport, vmCount, batchSize int) (string, error) {
	if batchSize <= 0 {
		return "", fmt.Errorf("batchSize must be > 0, got %d", batchSize)
	}
	id := uuid.New().String()
	expectedBatches := int(math.Ceil(float64(vmCount) / float64(batchSize)))

	query, args, err := sq.Insert(rsReportsTable).
		Columns(
			rsReportsColID, rsReportsColVCenter, rsReportsColClusterID, rsReportsColIntervalID,
			rsReportsColWindowStart, rsReportsColWindowEnd,
			rsReportsColExpectedSampleCount, rsReportsColExpectedBatchCount,
		).
		Values(
			id, r.VCenter, r.ClusterID, r.IntervalID,
			r.WindowStart, r.WindowEnd,
			r.ExpectedSampleCount, expectedBatches,
		).
		ToSql()
	if err != nil {
		return "", fmt.Errorf("building create report query: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return "", fmt.Errorf("inserting report: %w", err)
	}
	return id, nil
}

// WriteBatch inserts metric rows for a slice of RightSizingMetrics.
// Rows with zero SampleCount are skipped. Duplicate rows are silently ignored.
func (s *RightSizingStore) WriteBatch(ctx context.Context, reportID string, metrics []models.RightSizingMetric) error {
	builder := sq.Insert(rsMetricsTable).
		Columns(
			rsMetricsColReportID, rsMetricsColVMName, rsMetricsColMOID, rsMetricsColMetricKey,
			rsMetricsColSampleCount, rsMetricsColAverage, rsMetricsColP95, rsMetricsColP99,
			rsMetricsColMax, rsMetricsColLatest,
		)

	hasRows := false
	for _, m := range metrics {
		if m.SampleCount == 0 {
			continue
		}
		builder = builder.Values(
			reportID, m.VMName, m.MOID, m.MetricKey,
			m.SampleCount, m.Average, m.P95, m.P99, m.Max, m.Latest,
		)
		hasRows = true
	}

	if !hasRows {
		return nil
	}

	query, args, err := builder.Suffix(
		"ON CONFLICT (" + rsMetricsColReportID + ", " + rsMetricsColMOID + ", " + rsMetricsColMetricKey + ") DO NOTHING",
	).ToSql()
	if err != nil {
		return fmt.Errorf("building write batch query: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("inserting metrics batch: %w", err)
	}
	return nil
}

// IncrementWrittenBatchCount increments written_batch_count by 1 for the given report.
// Call this inside the same WithTx block as WriteBatch so the increment is atomic with the inserts.
func (s *RightSizingStore) IncrementWrittenBatchCount(ctx context.Context, reportID string) error {
	query, args, err := sq.Update(rsReportsTable).
		Set(rsReportsColWrittenBatchCount, sq.Expr(rsReportsColWrittenBatchCount+" + 1")).
		Where(sq.Eq{rsReportsColID: reportID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("building increment query: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("incrementing written_batch_count: %w", err)
	}
	return nil
}
