package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-extras/cobraflags"
	"github.com/spf13/cobra"

	analyzerapp "github.com/kubev2v/assisted-migration-agent/analyzer/app"
	"github.com/kubev2v/assisted-migration-agent/internal/config"
	"github.com/kubev2v/assisted-migration-agent/internal/util"
)

func NewRunCommand(cfg *config.Configuration) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the standalone RVTools Analyzer",
		Args:  cobra.NoArgs,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return validateConfiguration(cfg)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGQUIT)
			defer cancel()
			return analyzerapp.Run(ctx, cfg)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&cfg.Agent.DataFolder, "data-folder", cfg.Agent.DataFolder, "Folder for analyzer databases and uploads")
	flags.StringVar(&cfg.Agent.OpaPoliciesFolder, "opa-policies-folder", cfg.Agent.OpaPoliciesFolder, "Existing directory containing RVTools validation policies")
	flags.IntVar(&cfg.Server.HTTPPort, "server-http-port", cfg.Server.HTTPPort, "Analyzer HTTP port")
	flags.StringVar(&cfg.Server.ServerMode, "server-mode", cfg.Server.ServerMode, "Server mode: dev or prod")
	flags.StringVar(&cfg.Server.StaticsFolder, "server-statics-folder", cfg.Server.StaticsFolder, "Existing directory for static files (required in production mode)")
	cobraflags.CobraOnInitialize("ANALYZER", cmd)
	return cmd
}

func validateConfiguration(cfg *config.Configuration) error {
	if valid, err := util.IsValidDirectory(cfg.Agent.DataFolder); !valid {
		return fmt.Errorf("data-folder parameter is invalid: %w", err)
	}
	if valid, err := util.IsValidDirectory(cfg.Agent.OpaPoliciesFolder); !valid {
		return fmt.Errorf("opa-policies-folder parameter is invalid: %w", err)
	}
	if config.ServerModeType(cfg.Server.ServerMode) == config.ServerModeProd {
		if valid, err := util.IsValidDirectory(cfg.Server.StaticsFolder); !valid {
			return fmt.Errorf("server-statics-folder parameter is invalid: %w", err)
		}
	}
	if cfg.Server.HTTPPort < 1 || cfg.Server.HTTPPort > 65535 {
		return fmt.Errorf("invalid http-port %d: must be between 1 and 65535", cfg.Server.HTTPPort)
	}
	if cfg.Server.ServerMode != "dev" && cfg.Server.ServerMode != "prod" {
		return fmt.Errorf("invalid server mode %q: must be dev or prod", cfg.Server.ServerMode)
	}
	return nil
}
