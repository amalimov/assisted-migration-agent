package cmd

import (
	"context"
	"fmt"
	"math"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	"github.com/kubev2v/assisted-migration-agent/poc/rightsizing"
)

// NewRightSizingCommand returns a cobra command that queries vCenter historical
// performance metrics and prints a JSON right-sizing report to stdout.
func NewRightSizingCommand() *cobra.Command {
	cfg := rightsizing.Config{}
	var lookbackStr string
	var intervalID int

	c := &cobra.Command{
		Use:   "rightsizing",
		Short: "Query vCenter historical performance metrics for right-sizing analysis (spike/PoC)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			d, err := time.ParseDuration(lookbackStr)
			if err != nil {
				return fmt.Errorf("invalid --lookback value %q: %w", lookbackStr, err)
			}
			cfg.Lookback = d
			cfg.IntervalID = intervalID
			return runRightSizing(cmd.Context(), cfg)
		},
	}

	c.Flags().StringVar(&cfg.VCenterURL, "vcenter-url", rsEnvStr("VCENTER_URL", ""), "vCenter SDK URL (e.g. https://vcenter/sdk)")
	c.Flags().StringVar(&cfg.Username, "username", rsEnvStr("VCENTER_USERNAME", ""), "vCenter username")
	c.Flags().StringVar(&cfg.Password, "password", rsEnvStr("VCENTER_PASSWORD", ""), "vCenter password")
	c.Flags().BoolVar(&cfg.Insecure, "insecure", rsEnvBool("VCENTER_INSECURE", true), "skip TLS certificate verification (default true for PoC; set false in production)")
	c.Flags().StringVar(&cfg.NameFilter, "name-filter", rsEnvStr("VM_NAME_FILTER", ""), "filter VMs by name substring")
	c.Flags().StringVar(&cfg.ClusterID, "cluster-id", rsEnvStr("CLUSTER_ID", ""), "MoRef value of a ClusterComputeResource to scope discovery (e.g. domain-c123); empty = all clusters")
	c.Flags().IntVar(&cfg.MaxVMs, "max-vms", rsEnvInt("MAX_VMS", 5), "maximum number of VMs to query")
	c.Flags().StringVar(&lookbackStr, "lookback", rsEnvStr("LOOKBACK", "720h"), "lookback window as Go duration (720h = 30 days)")
	c.Flags().IntVar(&intervalID, "interval-id", rsEnvInt("INTERVAL_ID", 7200), "vSphere historical interval ID (7200=month, 1800=week, 300=day)")
	c.Flags().IntVar(&cfg.BatchSize, "batch-size", rsEnvInt("BATCH_SIZE", 64), "number of VMs per QueryPerf round-trip")
	c.Flags().StringVar(&cfg.DBPath, "db-path", rsEnvStr("DB_PATH", ""), "path to DuckDB file for persisting results (empty = no persistence)")

	return c
}

func runRightSizing(ctx context.Context, cfg rightsizing.Config) error {
	if cfg.VCenterURL == "" || cfg.Username == "" || cfg.Password == "" {
		return fmt.Errorf("--vcenter-url, --username, and --password (or their env vars) are required")
	}
	if cfg.BatchSize <= 0 {
		return fmt.Errorf("--batch-size must be > 0 (got %d)", cfg.BatchSize)
	}
	if cfg.IntervalID <= 0 {
		return fmt.Errorf("--interval-id must be > 0 (got %d)", cfg.IntervalID)
	}

	// 5-minute total timeout covers connection + discovery + all metric queries.
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	zap.S().Infof("connecting to %s as %s (insecure=%v)", cfg.VCenterURL, cfg.Username, cfg.Insecure)
	client, err := rightsizing.Connect(ctx, cfg)
	if err != nil {
		return fmt.Errorf("connection error: %w", err)
	}
	defer func() { _ = client.Logout(ctx) }()
	zap.S().Info("connected successfully")

	zap.S().Infof("discovering VMs (name-filter=%q, max=%d)", cfg.NameFilter, cfg.MaxVMs)
	vms, err := rightsizing.DiscoverVMs(ctx, client, cfg)
	if err != nil {
		return fmt.Errorf("error discovering VMs: %w", err)
	}
	if len(vms) == 0 {
		return fmt.Errorf("no VMs found (check name filter or vCenter inventory)")
	}

	zap.S().Infof("selected %d VM(s):", len(vms))
	for _, vm := range vms {
		zap.S().Infof("  %s (moid=%s)", vm.Name, vm.Ref.Value)
	}

	now := time.Now().UTC()
	windowStart := now.Add(-cfg.Lookback)

	report := rightsizing.Report{
		VCenter:             cfg.VCenterURL,
		ClusterID:           cfg.ClusterID,
		WindowStart:         windowStart,
		WindowEnd:           now,
		IntervalID:          cfg.IntervalID,
		ExpectedSampleCount: int(cfg.Lookback / (time.Duration(cfg.IntervalID) * time.Second)),
	}

	zap.S().Infof("querying metrics for %d VM(s) in batches of %d ...", len(vms), cfg.BatchSize)
	vmResults, queryWarnings := rightsizing.QueryMetrics(ctx, client, vms, cfg, windowStart, now)
	report.Warnings = append(report.Warnings, queryWarnings...)
	for _, vm := range vms {
		report.VMs = append(report.VMs, vmResults[vm.Ref.Value])
	}

	if cfg.DBPath != "" {
		// Note: ctx carries the 5-minute timeout shared with connect/discover/query.
		// Storage writes run against the remaining budget; large inventories may be
		// partially written if the budget expires.
		if err := persistReport(ctx, cfg, report, vms, vmResults); err != nil {
			zap.S().Warnf("storage failed (results still printed): %v", err)
		}
	}

	return rightsizing.PrintReport(report)
}

// persistReport opens the DuckDB store, runs any pending migrations, and writes
// the rightsizing results in sub-batches matching cfg.BatchSize.
// WriteBatch and IncrementWrittenBatchCount are wrapped in a single transaction
// per batch so the counter always reflects the number of fully-written batches.
func persistReport(
	ctx context.Context,
	cfg rightsizing.Config,
	report rightsizing.Report,
	vms []rightsizing.VMInfo,
	vmResults map[string]rightsizing.VMReport,
) error {
	db, err := store.NewDB(nil, cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() { _ = db.Close() }()

	if err := migrations.Run(ctx, db); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}

	// nil validator: OPA-based VM filtering is not used here. persistReport
	// never calls s.Migrate(), s.Parser(), or s.VM() — the only paths that
	// invoke the parser — so passing nil is safe.
	s := store.NewStore(db, nil)

	r := models.RightSizingReport{
		VCenter:             report.VCenter,
		ClusterID:           report.ClusterID,
		IntervalID:          report.IntervalID,
		WindowStart:         report.WindowStart,
		WindowEnd:           report.WindowEnd,
		ExpectedSampleCount: report.ExpectedSampleCount,
	}
	reportID, err := s.RightSizing().CreateReport(ctx, r, len(vms), cfg.BatchSize)
	if err != nil {
		return fmt.Errorf("create report: %w", err)
	}
	zap.S().Infof("created rightsizing report %s in %s", reportID, cfg.DBPath)

	expectedBatches := int(math.Ceil(float64(len(vms)) / float64(cfg.BatchSize)))
	for i := 0; i < len(vms); i += cfg.BatchSize {
		batchVMs := vms[i:min(i+cfg.BatchSize, len(vms))]
		batchMetrics := toRightSizingMetrics(batchVMs, vmResults)
		batchNum := i/cfg.BatchSize + 1

		if err := s.WithTx(ctx, func(txCtx context.Context) error {
			if err := s.RightSizing().WriteBatch(txCtx, reportID, batchMetrics); err != nil {
				return err
			}
			return s.RightSizing().IncrementWrittenBatchCount(txCtx, reportID)
		}); err != nil {
			zap.S().Warnf("batch %d/%d write failed: %v", batchNum, expectedBatches, err)
		} else {
			zap.S().Infof("wrote batch %d/%d to %s", batchNum, expectedBatches, cfg.DBPath)
		}
	}
	return nil
}

// toRightSizingMetrics flattens the per-VM metric map into a flat slice
// for a sub-batch of VMs, preserving the original vms order.
func toRightSizingMetrics(batchVMs []rightsizing.VMInfo, vmResults map[string]rightsizing.VMReport) []models.RightSizingMetric {
	var out []models.RightSizingMetric
	for _, vm := range batchVMs {
		r := vmResults[vm.Ref.Value]
		for key, stats := range r.Metrics {
			out = append(out, models.RightSizingMetric{
				VMName:      r.Name,
				MOID:        r.MOID,
				MetricKey:   key,
				SampleCount: stats.SampleCount,
				Average:     stats.Average,
				P95:         stats.P95,
				P99:         stats.P99,
				Max:         stats.Max,
				Latest:      stats.Latest,
			})
		}
	}
	return out
}

// rsEnvStr / rsEnvBool / rsEnvInt are flag-default helpers scoped to this command.
// The rs prefix avoids collisions with any future helpers added to package cmd.
func rsEnvStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func rsEnvBool(key string, def bool) bool {
	switch os.Getenv(key) {
	case "true", "1", "yes":
		return true
	case "false", "0", "no":
		return false
	default:
		return def
	}
}

func rsEnvInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
