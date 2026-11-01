package estimations

import (
	"context"
	"math"

	plannerapi "github.com/kubev2v/migration-planner/api/v1alpha1"
	"github.com/kubev2v/migration-planner/pkg/estimations/complexity"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

const mibPerTiB = 1024 * 1024

func (s *Service) EstimateComplexity(ctx context.Context, vms []models.VM) (plannerapi.MigrationComplexityResponse, error) {
	if err := ctx.Err(); err != nil {
		return plannerapi.MigrationComplexityResponse{}, err
	}
	selected, err := eligibleVMs(vms)
	if err != nil {
		return plannerapi.MigrationComplexityResponse{}, err
	}

	osCounts := make(map[string]int)
	type tier struct {
		count int
		miB   int64
	}
	tiers := map[string]*tier{
		"0-10TiB": {}, "10-20TiB": {}, "20-50TiB": {}, "50+TiB": {},
	}
	for _, vm := range selected {
		osName := vm.EffectiveOS
		if osName == "" {
			osName = vm.GuestName
		}
		// Planner's osInfo query omits VMs with no configured or Tools OS name.
		if osName != "" {
			osCounts[osName]++
		}
		label := diskTier(vm.DiskSize)
		tiers[label].count++
		tiers[label].miB += vm.DiskSize
	}

	osEntries := make([]complexity.VMOsEntry, 0, len(osCounts))
	for name, count := range osCounts {
		osEntries = append(osEntries, complexity.VMOsEntry{Name: name, Count: count})
	}
	tierEntries := make([]complexity.DiskTierInput, 0, len(tiers))
	for label, data := range tiers {
		tierEntries = append(tierEntries, complexity.DiskTierInput{
			Label: label, VMCount: data.count,
			TotalSizeTB: roundTo(float64(data.miB)/mibPerTiB, 2),
		})
	}

	byDisk := complexity.DiskBreakdown(tierEntries)
	byOS := complexity.OSBreakdown(osEntries)
	byOSName := complexity.OSNameBreakdown(osEntries)
	response := plannerapi.MigrationComplexityResponse{
		ComplexityByDisk:   make([]plannerapi.ComplexityDiskScoreEntry, len(byDisk)),
		ComplexityByOS:     make([]plannerapi.ComplexityOSScoreEntry, len(byOS)),
		ComplexityByOSName: make([]plannerapi.ComplexityOSNameEntry, len(byOSName)),
		DiskSizeRatings:    complexity.DiskSizeRangeRatings(),
		OsRatings:          complexity.OSRatings(osEntries),
	}
	for i, entry := range byDisk {
		response.ComplexityByDisk[i] = plannerapi.ComplexityDiskScoreEntry{
			Score: entry.Score, VmCount: entry.VMCount, TotalSizeTB: entry.TotalSizeTB,
		}
	}
	for i, entry := range byOS {
		response.ComplexityByOS[i] = plannerapi.ComplexityOSScoreEntry{
			Score: entry.Score, VmCount: entry.VMCount,
		}
	}
	for i, entry := range byOSName {
		response.ComplexityByOSName[i] = plannerapi.ComplexityOSNameEntry{
			OsName: entry.Name, Score: entry.Score, VmCount: entry.VMCount,
		}
	}
	return response, nil
}

func diskTier(diskMiB int64) string {
	switch {
	case diskMiB < 10*mibPerTiB:
		return "0-10TiB"
	case diskMiB < 20*mibPerTiB:
		return "10-20TiB"
	case diskMiB < 50*mibPerTiB:
		return "20-50TiB"
	default:
		return "50+TiB"
	}
}

func roundTo(value float64, places int) float64 {
	factor := math.Pow10(places)
	return math.Round(value*factor) / factor
}
