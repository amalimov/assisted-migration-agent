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

// QueryVMMetrics queries historical performance metrics for a single VM.
// It validates metric availability at the given interval before querying, and
// records per-metric warnings for unavailable or empty metrics without failing the whole call.
//
// Returns nil metrics (not an error) if all desired metrics are unavailable.
func QueryVMMetrics(ctx context.Context, client *govmomi.Client, vm VMInfo, cfg Config, start, end time.Time) (map[string]MetricStats, []string) {
	pm := performance.NewManager(client.Client)

	// Look up all counter names this vCenter knows about.
	countersByName, err := pm.CounterInfoByName(ctx)
	if err != nil {
		return nil, []string{fmt.Sprintf("failed to get counter info: %v", err)}
	}

	// Determine which counters are available for this VM at the requested interval.
	available, err := pm.AvailableMetric(ctx, vm.Ref, int32(cfg.IntervalID))
	if err != nil {
		return nil, []string{fmt.Sprintf("failed to query available metrics for %s: %v", vm.Name, err)}
	}
	availableIDs := make(map[int32]bool, len(available))
	for _, m := range available {
		availableIDs[m.CounterId] = true
	}

	// Build the query list: desired metrics that are both recognized and available.
	var queryMetrics []string
	var warnings []string
	for _, name := range DesiredMetrics {
		info, ok := countersByName[name]
		if !ok {
			warnings = append(warnings, fmt.Sprintf("metric %q not recognized by this vCenter", name))
			continue
		}
		if !availableIDs[info.Key] {
			warnings = append(warnings, fmt.Sprintf("metric %q not available at interval %d for this VM", name, cfg.IntervalID))
			continue
		}
		queryMetrics = append(queryMetrics, name)
	}

	if len(queryMetrics) == 0 {
		return nil, append(warnings, "no desired metrics available for this VM at the requested interval")
	}

	// Compute how many samples fit in the lookback window at the chosen interval.
	// IntervalID is in seconds; Lookback is a time.Duration (nanoseconds).
	maxSamples := max(int32(cfg.Lookback/(time.Duration(cfg.IntervalID)*time.Second)), 1)

	spec := types.PerfQuerySpec{
		StartTime: &start,
		EndTime:   &end,
		// IntervalId 7200 = "month" historical rollup (one sample per 2 hours).
		// Other common values: 300=day (5 min), 1800=week (30 min), 86400=year (1 day).
		IntervalId: int32(cfg.IntervalID), // DEVIATION: Config.IntervalID is int; cast to int32 here at the API boundary
		MaxSample:  maxSamples,
		// Instance "" = aggregate rollup, not per-vCPU or per-disk breakdown.
		// Use Instance "*" if you want per-device granularity.
		MetricId: []types.PerfMetricId{{Instance: ""}},
	}

	// TODO: construct performance.Manager once per command run and pass it into this function.
	// Currently a fresh Manager is created per VM, bypassing its internal CounterInfoByName
	// cache and incurring one extra vCenter round-trip per VM. Also batch the VM refs so
	// SampleByName is called once for all VMs instead of once per VM.
	raw, err := pm.SampleByName(ctx, spec, queryMetrics, []types.ManagedObjectReference{vm.Ref})
	if err != nil {
		return nil, append(warnings, fmt.Sprintf("query failed: %v", err))
	}

	series, err := pm.ToMetricSeries(ctx, raw)
	if err != nil {
		return nil, append(warnings, fmt.Sprintf("failed to convert metric series: %v", err))
	}

	metrics := make(map[string]MetricStats)
	for _, em := range series {
		for _, ms := range em.Value {
			if _, exists := metrics[ms.Name]; exists {
				// Multiple instances returned (e.g. per-disk entries alongside the aggregate).
				// Keep the first occurrence (instance="", the aggregate) and skip the rest.
				continue
			}
			if len(ms.Value) == 0 {
				warnings = append(warnings, fmt.Sprintf("metric %q returned no samples (data may not exist for the requested window)", ms.Name))
				continue
			}
			metrics[ms.Name] = ComputeStats(ms.Value)
		}
	}

	if len(metrics) == 0 {
		warnings = append(warnings, "query succeeded but returned no samples (data gap or window too far in the past)")
	}

	return metrics, warnings
}
