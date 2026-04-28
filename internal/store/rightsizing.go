package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"

	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"

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
	rsReportsColCreatedAt           = "created_at"

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

// UpdateExpectedBatchCount sets expected_batch_count = ceil(vmCount/batchSize).
// Called after VM discovery, once the real VM count is known.
func (s *RightSizingStore) UpdateExpectedBatchCount(ctx context.Context, reportID string, vmCount, batchSize int) error {
	if batchSize <= 0 {
		return fmt.Errorf("batchSize must be > 0, got %d", batchSize)
	}
	expectedBatches := int(math.Ceil(float64(vmCount) / float64(batchSize)))

	query, args, err := sq.Update(rsReportsTable).
		Set(rsReportsColExpectedBatchCount, expectedBatches).
		Where(sq.Eq{rsReportsColID: reportID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("building update expected batch count query: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("updating expected_batch_count: %w", err)
	}
	return nil
}

// ListReports returns metadata for all rightsizing reports ordered by creation time descending.
// VM metrics are not included; use GetReport to retrieve the full report with metrics.
// Returns an empty slice (not nil) when no reports exist.
func (s *RightSizingStore) ListReports(ctx context.Context) ([]models.RightsizingReportSummary, error) {
	query, args, err := sq.Select(
		rsReportsColID, rsReportsColVCenter, rsReportsColClusterID, rsReportsColIntervalID,
		rsReportsColWindowStart, rsReportsColWindowEnd, rsReportsColExpectedSampleCount, rsReportsColCreatedAt,
	).From(rsReportsTable).
		OrderBy(rsReportsColCreatedAt + " DESC").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("building list reports query: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("executing list reports query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	reports := []models.RightsizingReportSummary{}
	for rows.Next() {
		var r models.RightsizingReportSummary
		if err := rows.Scan(
			&r.ID, &r.VCenter, &r.ClusterID, &r.IntervalID,
			&r.WindowStart, &r.WindowEnd, &r.ExpectedSampleCount, &r.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning report row: %w", err)
		}
		reports = append(reports, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating report rows: %w", err)
	}

	return reports, nil
}

// GetReport returns a single rightsizing report by ID with all VM metrics populated.
// Returns a ResourceNotFoundError if the ID does not exist.
func (s *RightSizingStore) GetReport(ctx context.Context, id string) (*models.RightsizingReport, error) {
	query, args, err := sq.Select(
		rsReportsColID, rsReportsColVCenter, rsReportsColClusterID, rsReportsColIntervalID,
		rsReportsColWindowStart, rsReportsColWindowEnd, rsReportsColExpectedSampleCount, rsReportsColCreatedAt,
	).From(rsReportsTable).
		Where(sq.Eq{rsReportsColID: id}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("building get report query: %w", err)
	}

	var r models.RightsizingReport
	err = s.db.QueryRowContext(ctx, query, args...).Scan(
		&r.ID, &r.VCenter, &r.ClusterID, &r.IntervalID,
		&r.WindowStart, &r.WindowEnd, &r.ExpectedSampleCount, &r.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, srvErrors.NewResourceNotFoundError("rightsizing report", id)
	}
	if err != nil {
		return nil, fmt.Errorf("scanning report: %w", err)
	}

	r.VMs = []models.RightsizingVMReport{}
	reports := []models.RightsizingReport{r}
	if err := s.appendMetrics(ctx, reports, map[string]int{r.ID: 0}); err != nil {
		return nil, err
	}
	return &reports[0], nil
}

// appendMetrics fetches all metric rows for the given reports (by index map)
// and builds the nested VMs structure in place.
func (s *RightSizingStore) appendMetrics(ctx context.Context, reports []models.RightsizingReport, idxByID map[string]int) error {
	ids := make([]string, 0, len(idxByID))
	for id := range idxByID {
		ids = append(ids, id)
	}

	query, args, err := sq.Select(
		rsMetricsColReportID, rsMetricsColVMName, rsMetricsColMOID, rsMetricsColMetricKey,
		rsMetricsColSampleCount, rsMetricsColAverage, rsMetricsColP95, rsMetricsColP99,
		rsMetricsColMax, rsMetricsColLatest,
	).From(rsMetricsTable).
		Where(sq.Eq{rsMetricsColReportID: ids}).
		ToSql()
	if err != nil {
		return fmt.Errorf("building metrics query: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("executing metrics query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	// vmIdx[reportID][moid] = index in reports[rIdx].VMs
	vmIdx := make(map[string]map[string]int)

	for rows.Next() {
		var reportID, vmName, moid, metricKey string
		var stats models.RightsizingMetricStats
		if err := rows.Scan(
			&reportID, &vmName, &moid, &metricKey,
			&stats.SampleCount, &stats.Average, &stats.P95, &stats.P99, &stats.Max, &stats.Latest,
		); err != nil {
			return fmt.Errorf("scanning metric row: %w", err)
		}

		rIdx := idxByID[reportID]
		if vmIdx[reportID] == nil {
			vmIdx[reportID] = make(map[string]int)
		}

		vIdx, ok := vmIdx[reportID][moid]
		if !ok {
			reports[rIdx].VMs = append(reports[rIdx].VMs, models.RightsizingVMReport{
				Name:    vmName,
				MOID:    moid,
				Metrics: map[string]models.RightsizingMetricStats{},
			})
			vIdx = len(reports[rIdx].VMs) - 1
			vmIdx[reportID][moid] = vIdx
		}

		reports[rIdx].VMs[vIdx].Metrics[metricKey] = stats
	}

	return rows.Err()
}
