package estimations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	plannerapi "github.com/kubev2v/migration-planner/api/v1alpha1"
	"github.com/kubev2v/migration-planner/pkg/estimations/complexity"
	"github.com/kubev2v/migration-planner/pkg/estimations/engines"
	"github.com/kubev2v/migration-planner/pkg/estimations/estimation"
	"github.com/kubev2v/migration-planner/pkg/estimations/estimation/calculators"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

var ErrInvalidOptions = errors.New("invalid estimation options")

// TimeOptions mirrors Planner's schema and calculator override choices.
type TimeOptions struct {
	Schemas []string
	Params  map[string]any
}

type resolvedOptions struct {
	schemas []engines.Schema
	params  map[string]float64
}

var paramDefaults = map[string]float64{
	calculators.ParamTransferRateMbps:       calculators.DefaultTransferRateMbps,
	calculators.ParamWorkHoursPerDay:        calculators.DefaultWorkHoursPerDay,
	calculators.ParamTroubleshootMinsPerVM:  calculators.DefaultTroubleshootMinsPerVM,
	calculators.ParamPostMigrationEngineers: calculators.DefaultEngineerCount,
}

var paramMinimums = map[string]float64{
	calculators.ParamTransferRateMbps:       0.1,
	calculators.ParamWorkHoursPerDay:        0.5,
	calculators.ParamTroubleshootMinsPerVM:  1,
	calculators.ParamPostMigrationEngineers: 1,
}

func resolveOptions(options TimeOptions) (resolvedOptions, error) {
	result := resolvedOptions{params: make(map[string]float64, len(paramDefaults))}
	for key, value := range paramDefaults {
		result.params[key] = value
	}
	requestedSchemas := options.Schemas
	if len(requestedSchemas) == 0 {
		requestedSchemas = []string{string(engines.SchemaNetworkBased), string(engines.SchemaStorageOffload)}
	}
	seen := make(map[engines.Schema]bool)
	for _, raw := range requestedSchemas {
		schema := engines.Schema(raw)
		if schema != engines.SchemaNetworkBased && schema != engines.SchemaStorageOffload {
			return resolvedOptions{}, fmt.Errorf("%w: unknown estimation schema %q", ErrInvalidOptions, raw)
		}
		if !seen[schema] {
			result.schemas = append(result.schemas, schema)
			seen[schema] = true
		}
	}
	for key, raw := range options.Params {
		minimum, ok := paramMinimums[key]
		if !ok {
			return resolvedOptions{}, fmt.Errorf("%w: unknown param key %q", ErrInvalidOptions, key)
		}
		value, ok := numeric(raw)
		if !ok || math.IsNaN(value) || math.IsInf(value, 0) {
			return resolvedOptions{}, fmt.Errorf("%w: param %q must be numeric", ErrInvalidOptions, key)
		}
		if key == calculators.ParamPostMigrationEngineers && math.Trunc(value) != value {
			return resolvedOptions{}, fmt.Errorf("%w: param %q must be a whole number", ErrInvalidOptions, key)
		}
		if value < minimum {
			return resolvedOptions{}, fmt.Errorf("%w: param %q must be at least %g", ErrInvalidOptions, key, minimum)
		}
		result.params[key] = value
	}
	return result, nil
}

func numeric(value any) (float64, bool) {
	switch n := value.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		v, err := strconv.ParseFloat(string(n), 64)
		return v, err == nil
	default:
		return 0, false
	}
}

func (s *Service) EstimateTime(ctx context.Context, vms []models.VM, options TimeOptions) (plannerapi.MigrationEstimationResponse, error) {
	if err := ctx.Err(); err != nil {
		return plannerapi.MigrationEstimationResponse{}, err
	}
	resolved, err := resolveOptions(options)
	if err != nil {
		return plannerapi.MigrationEstimationResponse{}, err
	}
	selected, err := eligibleVMs(vms)
	if err != nil {
		return plannerapi.MigrationEstimationResponse{}, err
	}
	var diskMiB int64
	for _, vm := range selected {
		diskMiB += vm.DiskSize
	}
	// Planner casts the summed GiB value from DOUBLE to INTEGER in DuckDB.
	// That cast rounds halfway values to even, after all source MiB are summed.
	diskGB := math.RoundToEven(float64(diskMiB) / 1024)
	results, err := runSchemas(resolved, len(selected), diskGB)
	if err != nil {
		return plannerapi.MigrationEstimationResponse{}, err
	}
	return plannerapi.MigrationEstimationResponse{
		Estimation: results, EstimationContext: estimationContext(resolved),
	}, nil
}

func (s *Service) EstimateTimeByComplexity(ctx context.Context, vms []models.VM, options TimeOptions) (plannerapi.MigrationEstimationByComplexityResponse, error) {
	if err := ctx.Err(); err != nil {
		return plannerapi.MigrationEstimationByComplexityResponse{}, err
	}
	resolved, err := resolveOptions(options)
	if err != nil {
		return plannerapi.MigrationEstimationByComplexityResponse{}, err
	}
	selected, err := eligibleVMs(vms)
	if err != nil {
		return plannerapi.MigrationEstimationByComplexityResponse{}, err
	}
	type bucket struct {
		count int
		miB   int64
	}
	buckets := make(map[int]*bucket, len(complexity.OSScores))
	for _, score := range complexity.OSScores {
		buckets[score] = &bucket{}
	}
	for _, vm := range selected {
		if b, ok := buckets[vm.OsDiskComplexity]; ok {
			b.count++
			b.miB += vm.DiskSize
		}
	}
	response := plannerapi.MigrationEstimationByComplexityResponse{
		ComplexityByOsDisk: make([]plannerapi.OsDiskEstimationEntry, 0, len(complexity.OSScores)),
		ComplexityMatrix:   make(map[string]map[string]int, len(complexity.ComplexityMatrix)),
	}
	for _, score := range complexity.OSScores {
		b := buckets[score]
		// Planner's complexity-distribution query rounds each bucket to 4
		// decimals in TiB before converting that value back to GiB.
		totalTB := roundTo(float64(b.miB)/mibPerTiB, 4)
		entry := plannerapi.OsDiskEstimationEntry{
			Score: score, VmCount: b.count, TotalDiskSizeTB: float32(totalTB),
		}
		if b.count > 0 {
			results, err := runSchemas(resolved, b.count, totalTB*1024)
			if err != nil {
				return plannerapi.MigrationEstimationByComplexityResponse{}, err
			}
			entry.Estimation = &results
		}
		response.ComplexityByOsDisk = append(response.ComplexityByOsDisk, entry)
	}
	for osScore, row := range complexity.ComplexityMatrix {
		inner := make(map[string]int, len(row))
		for diskScore, combined := range row {
			inner[strconv.Itoa(diskScore)] = combined
		}
		response.ComplexityMatrix[strconv.Itoa(osScore)] = inner
	}
	estimationCtx := estimationContext(resolved)
	// Planner's by-complexity handler echoes the requested schema list in its
	// context. When omitted, both schemas run but this list is empty.
	requestedSchemas := make([]string, len(options.Schemas))
	copy(requestedSchemas, options.Schemas)
	estimationCtx.Schemas = &requestedSchemas
	response.EstimationContext = &estimationCtx
	return response, nil
}

func runSchemas(options resolvedOptions, vmCount int, diskGB float64) (map[string]plannerapi.SchemaEstimationResult, error) {
	engineMap, err := engines.BuildEngines(options.schemas)
	if err != nil {
		return nil, fmt.Errorf("building Planner engines: %w", err)
	}
	params := []estimation.Param{
		{Key: calculators.ParamVMCount, Value: vmCount},
		{Key: calculators.ParamTotalDiskGB, Value: diskGB},
	}
	for key, value := range options.params {
		params = append(params, estimation.Param{Key: key, Value: value})
	}
	results := make(map[string]plannerapi.SchemaEstimationResult, len(options.schemas))
	for _, schema := range options.schemas {
		breakdown := engineMap[schema].Run(params)
		apiBreakdown := make(map[string]plannerapi.EstimationDetail, len(breakdown))
		var minTotal, maxTotal time.Duration
		for name, value := range breakdown {
			detail := plannerapi.EstimationDetail{Reason: value.Reason}
			if value.IsRanged() {
				minTotal += *value.MinDuration
				maxTotal += *value.MaxDuration
				minDuration := value.MinDuration.String()
				maxDuration := value.MaxDuration.String()
				detail.MinDuration = &minDuration
				detail.MaxDuration = &maxDuration
			} else {
				minTotal += *value.Duration
				maxTotal += *value.Duration
				duration := value.Duration.String()
				detail.Duration = &duration
			}
			apiBreakdown[name] = detail
		}
		results[string(schema)] = plannerapi.SchemaEstimationResult{
			MinTotalDuration: minTotal.String(), MaxTotalDuration: maxTotal.String(),
			Breakdown: apiBreakdown,
		}
	}
	return results, nil
}

func estimationContext(options resolvedOptions) plannerapi.EstimationContext {
	schemas := make([]string, len(options.schemas))
	for i, schema := range options.schemas {
		schemas[i] = string(schema)
	}
	params := make(map[string]float32, len(options.params))
	for key, value := range options.params {
		params[key] = float32(value)
	}
	return plannerapi.EstimationContext{Schemas: &schemas, Params: &params}
}
