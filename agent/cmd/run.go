package main

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/ecordell/optgen/helpers"
	"github.com/fatih/color"
	"github.com/google/uuid"
	"github.com/jzelinskie/cobrautil/v2"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"go.uber.org/zap"

	"github.com/go-extras/cobraflags"

	agentapp "github.com/kubev2v/assisted-migration-agent/agent/app"
	"github.com/kubev2v/assisted-migration-agent/internal/config"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

const (
	apiV2 string = "/api/v2"
)

func NewRunCommand(cfg *config.Configuration) *cobra.Command {
	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Run agent",
		Args:  cobra.NoArgs,
		Example: `  # Run agent in disconnected mode
  agent run --agent-id 550e8400-e29b-41d4-a716-446655440000 --source-id 6ba7b810-9dad-11d1-80b4-00c04fd430c8

  # Run agent in connected mode with authentication
  agent run --mode connected --agent-id 550e8400-e29b-41d4-a716-446655440000 --source-id 6ba7b810-9dad-11d1-80b4-00c04fd430c8 --authentication-enabled --authentication-jwt-filepath /path/to/jwt

  # Run agent in production mode
  agent run --agent-id 550e8400-e29b-41d4-a716-446655440000 --source-id 6ba7b810-9dad-11d1-80b4-00c04fd430c8 --server-mode prod --server-statics-folder /var/www/statics`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return validateConfiguration(cfg)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			zap.S().Infow("using configuration",
				"agent", helpers.Flatten(cfg.Agent.DebugMap()),
				"server", helpers.Flatten(cfg.Server.DebugMap()),
				"console", helpers.Flatten(cfg.Console.DebugMap()),
				"auth", helpers.Flatten(cfg.Auth.DebugMap()),
			)

			ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGQUIT)
			defer cancel()
			return agentapp.Run(ctx, cfg)
		},
	}

	registerFlags(runCmd, cfg)
	cobraflags.CobraOnInitialize("AGENT", runCmd)

	return runCmd
}

func registerFlags(cmd *cobra.Command, config *config.Configuration) {
	nfs := cobrautil.NewNamedFlagSets(cmd)

	serverFlagSet := nfs.FlagSet(color.New(color.FgBlue, color.Bold).Sprint("Server"))
	registerServerFlags(serverFlagSet, config)

	authenticationFlagSet := nfs.FlagSet(color.New(color.FgBlue, color.Bold).Sprint("Authentication"))
	registerAuthenticationFlags(authenticationFlagSet, config)

	agentFlagSet := nfs.FlagSet(color.New(color.FgBlue, color.Bold).Sprint("Agent"))
	registerAgentFlags(agentFlagSet, config)

	consoleFlagSet := nfs.FlagSet(color.New(color.FgBlue, color.Bold).Sprint("Console"))
	registerConsoleFlags(consoleFlagSet, config)

	nfs.AddFlagSets(cmd)
}

func validateConfiguration(cfg *config.Configuration) error {
	if cfg.Agent.DataFolder == "" {
		return errors.New("data folder must be set")
	}

	if config.ServerModeType(cfg.Server.ServerMode) == config.ServerModeProd && cfg.Server.StaticsFolder == "" {
		return errors.New("statics folder must be set when server mode is production")
	}

	if cfg.Server.HTTPPort < 1 || cfg.Server.HTTPPort > 65535 {
		return fmt.Errorf("invalid http-port %d: must be between 1 and 65535", cfg.Server.HTTPPort)
	}

	if cfg.Auth.Enabled && cfg.Auth.JWTFilePath == "" {
		return errors.New("authentication-jwt-filepath must be set when authentication is enabled")
	}

	switch config.ServerModeType(cfg.Server.ServerMode) {
	case config.ServerModeProd, config.ServerModeDev:
	default:
		return fmt.Errorf("invalid server mode %q: must be %q or %q", cfg.Server.ServerMode, config.ServerModeProd, config.ServerModeDev)
	}

	if err := validateUUID(cfg.Agent.ID, "agent-id"); err != nil {
		return err
	}
	if err := validateUUID(cfg.Agent.SourceID, "source-id"); err != nil {
		return err
	}

	switch models.AgentMode(cfg.Agent.Mode) {
	case models.AgentModeConnected, models.AgentModeDisconnected:
	default:
		return fmt.Errorf("invalid mode %q: must be %q or %q", cfg.Agent.Mode, models.AgentModeConnected, models.AgentModeDisconnected)
	}

	return nil
}

func validateUUID(value, name string) error {
	if value == "" {
		return fmt.Errorf("%s cannot be empty", name)
	}
	if _, err := uuid.Parse(value); err != nil {
		return fmt.Errorf("%s must be a valid UUID: %w", name, err)
	}
	return nil
}

func registerServerFlags(flagSet *pflag.FlagSet, config *config.Configuration) {
	flagSet.IntVar(&config.Server.HTTPPort, "server-http-port", config.Server.HTTPPort, "Port on which the HTTP server is listening")
	flagSet.StringVar(&config.Server.StaticsFolder, "server-statics-folder", config.Server.StaticsFolder, "Path to statics folder")
	flagSet.StringVar(&config.Server.ServerMode, "server-mode", config.Server.ServerMode, "Server mode: either prod or dev. If prod the statics folder must be set")
}

func registerAuthenticationFlags(flagSet *pflag.FlagSet, config *config.Configuration) {
	flagSet.BoolVar(&config.Auth.Enabled, "authentication-enabled", config.Auth.Enabled, "Enable authentication when connecting to console")
	flagSet.StringVar(&config.Auth.JWTFilePath, "authentication-jwt-filepath", config.Auth.JWTFilePath, "Path of the jwt file")
}

func registerAgentFlags(flagSet *pflag.FlagSet, config *config.Configuration) {
	flagSet.StringVar(&config.Agent.Mode, "mode", config.Agent.Mode, "Agent mode: connected or disconnected")
	flagSet.StringVar(&config.Agent.OpaPoliciesFolder, "opa-policies-folder", config.Agent.OpaPoliciesFolder, "Path to the OPA policies folder")
	flagSet.StringVar(&config.Agent.ID, "agent-id", config.Agent.ID, "Unique identifier (UUID) for this agent")
	flagSet.StringVar(&config.Agent.SourceID, "source-id", config.Agent.SourceID, "Source identifier (UUID) for this agent")
	flagSet.StringVar(&config.Agent.Version, "version", config.Agent.Version, "Agent version to report to console")
	flagSet.StringVar(&config.Agent.DataFolder, "data-folder", config.Agent.DataFolder, "Path to the persistent data folder")
}

func registerConsoleFlags(flagSet *pflag.FlagSet, config *config.Configuration) {
	flagSet.StringVar(&config.Console.URL, "console-url", config.Console.URL, "URL of console.redhat.com")
	flagSet.DurationVar(&config.Agent.UpdateInterval, "console-update-interval", config.Agent.UpdateInterval, "Interval for console status updates")
}

// cleanupStaleCollections removes leftover collection markers and their DuckDB
// files from disk. Any row in the collections table at startup is stale,
// successful collections delete their marker during finalize.
// Running collections are also treated as stale because the agent crashed
// before the finalization step.
