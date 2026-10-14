package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/kubev2v/assisted-migration-agent/internal/config"
	"github.com/kubev2v/assisted-migration-agent/pkg/logger"
)

func NewRootCommand() *cobra.Command {
	cfg := config.NewAnalyzerConfiguration()
	cfg.Version = version
	cfg.GitCommit = gitCommit
	root := &cobra.Command{
		Use:          "rvtools-analyzer",
		Short:        "Standalone RVTools Analyzer",
		SilenceUsage: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if cfg.LogFormat != "console" && cfg.LogFormat != "json" {
				return fmt.Errorf("invalid log-format: %s", cfg.LogFormat)
			}
			if _, err := zapcore.ParseLevel(cfg.LogLevel); err != nil {
				return fmt.Errorf("invalid log-level: %s", cfg.LogLevel)
			}
			zap.ReplaceGlobals(logger.Init(cfg.LogFormat, cfg.LogLevel))
			return nil
		},
	}
	root.PersistentFlags().StringVar(&cfg.LogFormat, "log-format", cfg.LogFormat, "format of the logs: console or json")
	root.PersistentFlags().StringVar(&cfg.LogLevel, "log-level", cfg.LogLevel, "log level")
	root.AddCommand(NewRunCommand(cfg), NewVersionCommand(cfg))
	return root
}

func NewVersionCommand(cfg *config.AnalyzerConfiguration) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print analyzer version information",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintf(cmd.OutOrStdout(), "Analyzer Version: %s\nGit Commit:       %s\n", cfg.Version, cfg.GitCommit)
		},
	}
}
