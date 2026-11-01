package estimations

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/kubev2v/migration-planner/pkg/estimations/complexity"
	"github.com/kubev2v/migration-planner/pkg/estimations/estimation/calculators"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

func TestComplexityFiltersExcludedAndRoundsAfterAggregation(t *testing.T) {
	service := NewService()
	vms := []models.VM{
		{ID: "one", EffectiveOS: "Red Hat Enterprise Linux 8", DiskSize: 4200},
		{ID: "two", EffectiveOS: "Red Hat Enterprise Linux 8", DiskSize: 4200},
		{ID: "excluded", EffectiveOS: "Windows Server 2019", DiskSize: 60 * mibPerTiB, MigrationExcluded: true},
	}
	response, err := service.EstimateComplexity(context.Background(), vms)
	if err != nil {
		t.Fatal(err)
	}
	if response.ComplexityByDisk[0].VmCount != 2 || response.ComplexityByDisk[0].TotalSizeTB != 0.01 {
		t.Fatalf("disk tier = %+v; want 2 VMs and 0.01 TiB", response.ComplexityByDisk[0])
	}
	for _, entry := range response.ComplexityByDisk[1:] {
		if entry.VmCount != 0 {
			t.Fatalf("excluded VM contributed to disk tier: %+v", entry)
		}
	}
	if len(response.ComplexityByOSName) != 1 || response.ComplexityByOSName[0].VmCount != 2 || response.ComplexityByOSName[0].OsName != "Red Hat Enterprise Linux 8" {
		t.Fatalf("OS breakdown = %+v", response.ComplexityByOSName)
	}
	if !reflect.DeepEqual(response.DiskSizeRatings, complexity.DiskSizeRangeRatings()) {
		t.Fatalf("disk ratings differ from Planner: %v", response.DiskSizeRatings)
	}
	if _, err := service.EstimateComplexity(context.Background(), vms[2:]); !errors.Is(err, ErrNoEligibleVMs) {
		t.Fatalf("only excluded VM: got %v", err)
	}
}

func TestComplexityUsesPlannerDiskTierBoundaries(t *testing.T) {
	response, err := NewService().EstimateComplexity(context.Background(), []models.VM{
		{ID: "below-ten", DiskSize: 10*mibPerTiB - 1},
		{ID: "ten", DiskSize: 10 * mibPerTiB},
		{ID: "twenty", DiskSize: 20 * mibPerTiB},
		{ID: "fifty", DiskSize: 50 * mibPerTiB},
	})
	if err != nil {
		t.Fatal(err)
	}
	for i, entry := range response.ComplexityByDisk {
		if entry.Score != i+1 || entry.VmCount != 1 {
			t.Fatalf("tier boundary %d: %+v", i, entry)
		}
	}
}

func TestTimeSchemesOverridesAndSourceUnitAggregation(t *testing.T) {
	service := NewService()
	ctx := context.Background()
	vms := []models.VM{
		{ID: "one", DiskSize: 400},
		{ID: "two", DiskSize: 400},
		{ID: "excluded", DiskSize: 100 * mibPerTiB, MigrationExcluded: true},
	}
	result, err := service.EstimateTime(ctx, vms, TimeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Estimation) != 2 || result.Estimation["network-based"].Breakdown == nil || result.Estimation["storage-offload"].Breakdown == nil {
		t.Fatalf("missing Planner time schemes: %+v", result.Estimation)
	}
	if reason := result.Estimation["network-based"].Breakdown["Storage Migration"].Reason; reason != "1.00 GB at 620 Mbps (110 min/500GB)" {
		t.Fatalf("Planner network calculator got disk input other than 1 GiB: %q", reason)
	}
	// Planner casts the sum of disk sizes to GiB once. Two 400 MiB disks
	// therefore produce the same storage estimate as one 800 MiB disk with
	// the same VM count.
	oneVM := []models.VM{{ID: "one", DiskSize: 800}, {ID: "two"}}
	equivalent, err := service.EstimateTime(ctx, oneVM, TimeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.Estimation, equivalent.Estimation) {
		t.Fatalf("disk aggregation differs for equal MiB totals")
	}
	tie, err := service.EstimateTime(ctx, []models.VM{{ID: "tie", DiskSize: 2560}}, TimeOptions{Schemas: []string{"network-based"}})
	if err != nil {
		t.Fatal(err)
	}
	if reason := tie.Estimation["network-based"].Breakdown["Storage Migration"].Reason; reason != "2.00 GB at 620 Mbps (110 min/500GB)" {
		t.Fatalf("Planner DuckDB cast should round 2.5 GiB to even: %q", reason)
	}

	override := TimeOptions{
		Schemas: []string{"network-based"},
		Params: map[string]any{
			calculators.ParamTransferRateMbps:       float64(100),
			calculators.ParamWorkHoursPerDay:        float64(6),
			calculators.ParamTroubleshootMinsPerVM:  float64(5),
			calculators.ParamPostMigrationEngineers: float64(2),
		},
	}
	custom, err := service.EstimateTime(ctx, vms, override)
	if err != nil {
		t.Fatal(err)
	}
	if len(custom.Estimation) != 1 || custom.Estimation["network-based"].Breakdown == nil {
		t.Fatalf("schema selection failed: %+v", custom.Estimation)
	}
	if (*custom.EstimationContext.Params)[calculators.ParamTransferRateMbps] != 100 || (*custom.EstimationContext.Params)[calculators.ParamPostMigrationEngineers] != 2 {
		t.Fatalf("override context = %+v", custom.EstimationContext)
	}
	if reflect.DeepEqual(custom.Estimation["network-based"], result.Estimation["network-based"]) {
		t.Fatal("parameter overrides did not affect estimation")
	}
	for _, options := range []TimeOptions{
		{Schemas: []string{"missing"}},
		{Params: map[string]any{"unknown": 1}},
		{Params: map[string]any{calculators.ParamTransferRateMbps: 0.01}},
		{Params: map[string]any{calculators.ParamPostMigrationEngineers: 1.5}},
	} {
		if _, err := service.EstimateTime(ctx, vms, options); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("options %+v: got %v", options, err)
		}
	}
}

func TestTimeByComplexityBuckets(t *testing.T) {
	service := NewService()
	result, err := service.EstimateTimeByComplexity(context.Background(), []models.VM{
		{ID: "one", OsDiskComplexity: 2, DiskSize: 40},
		{ID: "two", OsDiskComplexity: 2, DiskSize: 40},
		{ID: "excluded", OsDiskComplexity: 4, DiskSize: 10 * mibPerTiB, MigrationExcluded: true},
	}, TimeOptions{Schemas: []string{"storage-offload"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ComplexityByOsDisk) != len(complexity.OSScores) {
		t.Fatalf("buckets = %+v", result.ComplexityByOsDisk)
	}
	for _, entry := range result.ComplexityByOsDisk {
		if entry.Score == 2 {
			if entry.VmCount != 2 || entry.TotalDiskSizeTB != 0.0001 || entry.Estimation == nil || len(*entry.Estimation) != 1 {
				t.Fatalf("score 2 entry = %+v", entry)
			}
			if reason := (*entry.Estimation)["storage-offload"].Breakdown["Storage Offload"].Reason; reason != "0.10 GB @ 0.5–2.0 GB/s transfer rate; assumes same storage array with x-copy enabled" {
				t.Fatalf("Planner by-complexity input should be rounded TiB converted to GiB: %q", reason)
			}
		} else if entry.VmCount != 0 || entry.Estimation != nil {
			t.Fatalf("unexpected bucket = %+v", entry)
		}
	}
	if result.EstimationContext == nil || len(*result.EstimationContext.Schemas) != 1 || (*result.EstimationContext.Schemas)[0] != "storage-offload" {
		t.Fatalf("context = %+v", result.EstimationContext)
	}
	if len(result.ComplexityMatrix) != len(complexity.ComplexityMatrix) {
		t.Fatalf("matrix = %+v", result.ComplexityMatrix)
	}
	defaults, err := service.EstimateTimeByComplexity(context.Background(), []models.VM{{ID: "one", OsDiskComplexity: 2, DiskSize: 40}}, TimeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(*defaults.EstimationContext.Schemas) != 0 || len(*defaults.ComplexityByOsDisk[2].Estimation) != 2 {
		t.Fatalf("Planner default by-complexity schema behavior changed: %+v", defaults)
	}
}
