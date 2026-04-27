package cmd

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

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
		VCenter:     cfg.VCenterURL,
		WindowStart: windowStart,
		WindowEnd:   now,
		IntervalID:  cfg.IntervalID,
	}

	zap.S().Infof("querying metrics for %d VM(s) in batches of %d ...", len(vms), cfg.BatchSize)
	vmResults, queryWarnings := rightsizing.QueryMetrics(ctx, client, vms, cfg, windowStart, now)
	report.Warnings = append(report.Warnings, queryWarnings...)
	for _, vm := range vms {
		report.VMs = append(report.VMs, vmResults[vm.Ref.Value])
	}

	return rightsizing.PrintReport(report)
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
