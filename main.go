package main

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/kubev2v/assisted-migration-agent/cmd"
	"github.com/kubev2v/assisted-migration-agent/internal/config"
	"github.com/kubev2v/assisted-migration-agent/pkg/logger"
)

// These are set at build time via -ldflags
var (
	version     = "v0.0.0"  // Set via -ldflags "-X main.version=..."
	gitCommit   = "unknown" // Set via -ldflags "-X main.gitCommit=..."
	uiGitCommit = "unknown" // Set via -ldflags "-X main.uiGitCommit=..."
)

func main() {
	cfg := config.NewConfigurationWithOptionsAndDefaults(
		config.WithServer(config.Server{
			HTTPPort:   8000,
			ServerMode: "dev",
		}),
		config.WithAgent(config.Agent{
			Version:             version,
			GitCommit:           gitCommit,
			UIGitCommit:         uiGitCommit,
			Mode:                "disconnected",
			UpdateInterval:      5 * time.Second,
			LegacyStatusEnabled: true,
			RetainCollections:   1,
		}),
		config.WithAuth(config.Authentication{Enabled: false}),
		config.WithLogFormat("console"),
		config.WithLogLevel("debug"),
	)

	rootCmd := &cobra.Command{
		Use:   "agent",
		Short: "Assisted Migration Agent",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			// PersistentPreRun fires after Cobra parses flags, so cfg.LogLevel and
			// cfg.LogFormat already reflect any --log-level / --log-format overrides.
			if err := validateConfig(cfg); err != nil {
				fmt.Printf("%s", err)
				os.Exit(1)
			}
			l := logger.Init(cfg.LogFormat, cfg.LogLevel)
			zap.ReplaceGlobals(l) //nolint:errcheck
		},
	}
	registerLoggingFlags(rootCmd, cfg)

	rootCmd.AddCommand(cmd.NewRunCommand(cfg))
	rootCmd.AddCommand(cmd.NewVersionCommand(cfg))

	if err := rootCmd.Execute(); err != nil {
		fmt.Printf("%s", err)
		os.Exit(1)
	}
}

func validateConfig(cfg *config.Configuration) error {
	switch cfg.LogFormat {
	case "console":
	case "json":
	default:
		return fmt.Errorf("invalid log-format: %s", cfg.LogFormat)
	}

	if _, err := zapcore.ParseLevel(cfg.LogLevel); err != nil {
		return fmt.Errorf("invalid log level %s", cfg.LogLevel)
	}

	return nil
}

func registerLoggingFlags(cmd *cobra.Command, config *config.Configuration) {
	cmd.PersistentFlags().StringVar(&config.LogFormat, "log-format", config.LogFormat, "format of the logs: console or json")
	cmd.PersistentFlags().StringVar(&config.LogLevel, "log-level", config.LogLevel, "log level")
}
