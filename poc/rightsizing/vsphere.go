package rightsizing

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/performance"
	"github.com/vmware/govmomi/session"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/soap"
	"github.com/vmware/govmomi/vim25/types"
)

// DesiredMetrics is the ordered list of metric names attempted for each VM.
// vSphere units:
//   - cpu.usagemhz.average    MHz consumed by the VM
//   - cpu.usage.average       hundredths of a percent (5000 = 50.00 %)
//   - mem.active.average      KB of memory actively used by the guest
//   - mem.consumed.average    KB consumed (guest + overhead)
//   - disk.used.latest        KB of disk actually used
//   - disk.provisioned.latest KB of disk provisioned (thin + thick)
var DesiredMetrics = []string{
	"cpu.usagemhz.average",
	"cpu.usage.average",
	"mem.active.average",
	"mem.consumed.average",
	"disk.used.latest",
	"disk.provisioned.latest",
}

// VMInfo carries the display name and managed object reference of a discovered VM.
type VMInfo struct {
	Name string
	Ref  types.ManagedObjectReference
}

// Connect creates and authenticates a govmomi client.
// It never logs the password.
func Connect(ctx context.Context, cfg Config) (*govmomi.Client, error) {
	u, err := soap.ParseURL(cfg.VCenterURL)
	if err != nil {
		return nil, fmt.Errorf("invalid vCenter URL: %w", err)
	}
	u.User = url.UserPassword(cfg.Username, cfg.Password)

	soapClient := soap.NewClient(u, cfg.Insecure)
	vimClient, err := vim25.NewClient(ctx, soapClient)
	if err != nil {
		return nil, fmt.Errorf("failed to create vim25 client: %w", err)
	}

	client := &govmomi.Client{
		Client:         vimClient,
		SessionManager: session.NewManager(vimClient),
	}
	if err := client.Login(ctx, u.User); err != nil {
		return nil, fmt.Errorf("authentication failed: %w", err)
	}
	return client, nil
}

// DiscoverVMs lists VMs from vCenter, preferring powered-on VMs, filtered by
// name substring (when cfg.NameFilter is set), up to cfg.MaxVMs results.
func DiscoverVMs(ctx context.Context, client *govmomi.Client, cfg Config) ([]VMInfo, error) {
	container := client.ServiceContent.RootFolder
	if cfg.ClusterID != "" {
		// Scope discovery to a single cluster by using its MoRef as the container.
		// The MoRef value is the "domain-cXXX" portion of the cluster's vSphere ID.
		container = types.ManagedObjectReference{
			Type:  "ClusterComputeResource",
			Value: cfg.ClusterID,
		}
	}

	m := view.NewManager(client.Client)
	v, err := m.CreateContainerView(ctx, container, []string{"VirtualMachine"}, true)
	if err != nil {
		return nil, fmt.Errorf("failed to create container view: %w", err)
	}
	defer func() { _ = v.Destroy(ctx) }()

	var vms []mo.VirtualMachine
	if err := v.Retrieve(ctx, []string{"VirtualMachine"}, []string{"name", "runtime.powerState"}, &vms); err != nil {
		return nil, fmt.Errorf("failed to retrieve VMs: %w", err)
	}

	// Put powered-on VMs first so they are preferred when MaxVMs is reached.
	var poweredOn, other []mo.VirtualMachine
	for _, vm := range vms {
		if vm.Runtime.PowerState == types.VirtualMachinePowerStatePoweredOn {
			poweredOn = append(poweredOn, vm)
		} else {
			other = append(other, vm)
		}
	}
	ordered := make([]mo.VirtualMachine, 0, len(vms))
	ordered = append(ordered, poweredOn...)
	ordered = append(ordered, other...)

	var result []VMInfo
	for _, vm := range ordered {
		if cfg.NameFilter != "" && !strings.Contains(vm.Name, cfg.NameFilter) {
			continue
		}
		result = append(result, VMInfo{Name: vm.Name, Ref: vm.Self})
		if len(result) >= cfg.MaxVMs {
			break
		}
	}
	return result, nil
}

// resolveMetricIDs translates DesiredMetrics names to PerfMetricId values using the
// vCenter's counter registry. Unrecognized names are skipped and reported as warnings.
// Instance "" requests the aggregate rollup (not per-vCPU or per-disk breakdown).
func resolveMetricIDs(countersByName map[string]*types.PerfCounterInfo) ([]types.PerfMetricId, []string) {
	var ids []types.PerfMetricId
	var warnings []string
	for _, name := range DesiredMetrics {
		info, ok := countersByName[name]
		if !ok {
			warnings = append(warnings, fmt.Sprintf("metric %q not recognized by this vCenter", name))
			continue
		}
		ids = append(ids, types.PerfMetricId{CounterId: info.Key, Instance: ""})
	}
	return ids, warnings
}

// buildBatchSpecs builds one PerfQuerySpec per VM in the batch.
// All specs share the same metric IDs, interval, and time window.
func buildBatchSpecs(batch []VMInfo, metricIDs []types.PerfMetricId, intervalID int, start, end time.Time, maxSamples int32) []types.PerfQuerySpec {
	specs := make([]types.PerfQuerySpec, len(batch))
	for i, vm := range batch {
		// Copy start/end per iteration so each spec holds an independent pointer.
		s, e := start, end
		specs[i] = types.PerfQuerySpec{
			Entity: vm.Ref,
			// IntervalId 7200 = "month" rollup (2 h samples); 300=day, 1800=week, 86400=year.
			IntervalId: int32(intervalID),
			MetricId:   metricIDs,
			StartTime:  &s,
			EndTime:    &e,
			MaxSample:  maxSamples,
		}
	}
	return specs
}

// parseEntityMetrics extracts a MetricStats map from a single PerfEntityMetric result.
// Empty sample sets and duplicate instances are recorded as warnings, not errors.
func parseEntityMetrics(em *types.PerfEntityMetric, countersByKey map[int32]*types.PerfCounterInfo) (map[string]MetricStats, []string) {
	metrics := make(map[string]MetricStats)
	var warnings []string
	for _, v := range em.Value {
		series, ok := v.(*types.PerfMetricIntSeries)
		if !ok {
			continue
		}
		info, ok := countersByKey[series.Id.CounterId]
		if !ok {
			continue
		}
		name := info.Name()
		if _, exists := metrics[name]; exists {
			// Defensive guard: duplicate counter IDs should not occur when Instance="" is
			// requested, but skip any unexpected extra series rather than overwriting.
			continue
		}
		if len(series.Value) == 0 {
			warnings = append(warnings, fmt.Sprintf("metric %q returned no samples (data may not exist for the requested window)", name))
			continue
		}
		metrics[name] = ComputeStats(series.Value)
	}
	if len(metrics) == 0 && len(warnings) == 0 {
		// em.Value was empty — vCenter returned no series at all for this VM.
		warnings = append(warnings, "query succeeded but returned no samples (data gap or window too far in the past)")
	}
	return metrics, warnings
}

// backfillMissingVMs records a warning for every VM in the batch that vCenter
// returned no result for, and sets the Name field on entries that do exist.
func backfillMissingVMs(batch []VMInfo, results map[string]VMReport) {
	for _, vm := range batch {
		r, exists := results[vm.Ref.Value]
		if !exists {
			results[vm.Ref.Value] = VMReport{
				Name:     vm.Name,
				MOID:     vm.Ref.Value,
				Warnings: []string{"vCenter returned no data for this VM (powered off throughout window, or data not yet collected)"},
			}
			continue
		}
		r.Name = vm.Name
		results[vm.Ref.Value] = r
	}
}

// QueryMetrics queries historical performance metrics for all VMs in batches.
// Counter info is resolved once; one QueryPerf call is made per batch of cfg.BatchSize VMs.
// The returned map is keyed by VM MoRef value and always contains an entry for every input VM.
func QueryMetrics(ctx context.Context, client *govmomi.Client, vms []VMInfo, cfg Config, start, end time.Time) (map[string]VMReport, []string) {
	pm := performance.NewManager(client.Client)

	countersByName, err := pm.CounterInfoByName(ctx)
	if err != nil {
		return nil, []string{fmt.Sprintf("failed to get counter info: %v", err)}
	}
	countersByKey, err := pm.CounterInfoByKey(ctx)
	if err != nil {
		return nil, []string{fmt.Sprintf("failed to get counter info by key: %v", err)}
	}

	// We skip the per-VM AvailableMetric check: QueryPerf returns no data for unavailable
	// metrics rather than erroring; missing data is surfaced as per-VM warnings below.
	metricIDs, globalWarnings := resolveMetricIDs(countersByName)
	if len(metricIDs) == 0 {
		return nil, append(globalWarnings, "no desired metrics recognized by this vCenter")
	}

	if cfg.IntervalID <= 0 {
		return nil, []string{fmt.Sprintf("IntervalID must be > 0 (got %d)", cfg.IntervalID)}
	}
	maxSamples := max(int32(cfg.Lookback/(time.Duration(cfg.IntervalID)*time.Second)), 1)
	results := make(map[string]VMReport, len(vms))

	for i := 0; i < len(vms); i += cfg.BatchSize {
		batch := vms[i:min(i+cfg.BatchSize, len(vms))]
		specs := buildBatchSpecs(batch, metricIDs, cfg.IntervalID, start, end, maxSamples)

		raw, err := pm.Query(ctx, specs)
		if err != nil {
			for _, vm := range batch {
				results[vm.Ref.Value] = VMReport{
					Name:     vm.Name,
					MOID:     vm.Ref.Value,
					Warnings: []string{fmt.Sprintf("batch query failed: %v", err)},
				}
			}
			continue
		}

		for _, base := range raw {
			em, ok := base.(*types.PerfEntityMetric)
			if !ok {
				continue
			}
			metrics, warnings := parseEntityMetrics(em, countersByKey)
			results[em.Entity.Value] = VMReport{MOID: em.Entity.Value, Metrics: metrics, Warnings: warnings}
		}

		backfillMissingVMs(batch, results)
	}

	return results, globalWarnings
}
