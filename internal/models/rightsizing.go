package models

import "time"

// Store I/O types (RightSizingXxx — capital S).
// These match the DuckDB schema in 008_rightsizing.sql and are used exclusively
// by RightSizingStore in internal/store/rightsizing.go.

// RightSizingReport is the input type for RightSizingStore.CreateReport.
type RightSizingReport struct {
	VCenter             string
	ClusterID           string
	IntervalID          int
	WindowStart         time.Time
	WindowEnd           time.Time
	ExpectedSampleCount int
}

// RightSizingMetric holds aggregated stats for one VM × metric key, used by RightSizingStore.WriteBatch.
// Its stat fields (SampleCount, Average, etc.) mirror RightsizingMetricStats intentionally:
// this is a flat row type for bulk DB insertion; RightsizingMetricStats composes into the nested API read model.
type RightSizingMetric struct {
	VMName      string
	MOID        string
	MetricKey   string
	SampleCount int
	Average     float64
	P95         float64
	P99         float64
	Max         float64
	Latest      float64
}

// API read-model types (RightsizingXxx — lowercase s).
// These are returned by RightsizingService and consumed by the HTTP handler layer.

// RightsizingParams holds request parameters for the TriggerCollection service call.
type RightsizingParams struct {
	Credentials
	NameFilter string
	ClusterID  string
	MaxVMs     int
	LookbackH  int // hours; e.g. 720 = 30 days
	IntervalID int // vSphere interval in seconds (300=day, 1800=week, 7200=month)
	BatchSize  int
}

// RightsizingMetricStats holds per-metric aggregated statistics for the API read model.
type RightsizingMetricStats struct {
	SampleCount int
	Average     float64
	P95         float64
	P99         float64
	Max         float64
	Latest      float64
}

// RightsizingVMReport groups all metric stats for a single VM in the API read model.
type RightsizingVMReport struct {
	Name    string
	MOID    string
	Metrics map[string]RightsizingMetricStats
}

// RightsizingReport is the API read model returned by ListReports and GetReport.
type RightsizingReport struct {
	ID                  string
	VCenter             string
	ClusterID           string
	WindowStart         time.Time
	WindowEnd           time.Time
	IntervalID          int
	ExpectedSampleCount int
	VMs                 []RightsizingVMReport
	CreatedAt           time.Time
}
