package models

import "time"

// RightSizingReport is the input type for RightSizingStore.CreateReport.
type RightSizingReport struct {
	VCenter             string
	ClusterID           string
	IntervalID          int
	WindowStart         time.Time
	WindowEnd           time.Time
	ExpectedSampleCount int
}

// RightSizingMetric holds aggregated stats for one VM × metric combination.
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
