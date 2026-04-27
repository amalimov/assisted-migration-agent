package rightsizing

import (
	"encoding/json"
	"os"
	"time"
)

// Report is the top-level JSON output structure.
type Report struct {
	VCenter     string    `json:"vcenter"`
	ClusterID   string    `json:"cluster_id,omitempty"` // empty = all clusters
	WindowStart time.Time `json:"window_start"`
	WindowEnd   time.Time `json:"window_end"`
	IntervalID  int       `json:"interval_id"`
	// ExpectedSampleCount is the theoretical maximum number of samples for the
	// queried window: floor(lookback / interval). Compare against MetricStats.SampleCount
	// to gauge data coverage. A low ratio may indicate the VM was powered off, recently
	// migrated, or that vCenter has a data gap — not necessarily missing or bad data.
	ExpectedSampleCount int        `json:"expected_sample_count"`
	VMs                 []VMReport `json:"vms"`
	Warnings            []string   `json:"warnings"`
}

// VMReport holds per-VM metric summaries and any per-VM warnings.
type VMReport struct {
	Name     string                 `json:"name"`
	MOID     string                 `json:"moid"`
	Metrics  map[string]MetricStats `json:"metrics"`
	Warnings []string               `json:"warnings"`
}

// PrintReport writes the report as indented JSON to stdout.
// nil slices are normalized to empty arrays for clean JSON output.
func PrintReport(r Report) error {
	if r.Warnings == nil {
		r.Warnings = []string{}
	}
	for i := range r.VMs {
		if r.VMs[i].Warnings == nil {
			r.VMs[i].Warnings = []string{}
		}
		if r.VMs[i].Metrics == nil {
			r.VMs[i].Metrics = map[string]MetricStats{}
		}
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}
