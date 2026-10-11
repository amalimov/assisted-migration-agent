package cmd

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ecordell/optgen/helpers"
	"github.com/fatih/color"
	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-extras/cobraflags"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/jzelinskie/cobrautil/v2"
	"github.com/kubev2v/migration-planner/pkg/opa"
	"github.com/spf13/cobra"
	"go.uber.org/zap"

	v2 "github.com/kubev2v/rvtools-analyser/api/v2"
	"github.com/kubev2v/rvtools-analyser/internal/config"
	"github.com/kubev2v/rvtools-analyser/internal/handlers"
	v2Handlers "github.com/kubev2v/rvtools-analyser/internal/handlers/v2"
	"github.com/kubev2v/rvtools-analyser/internal/server"
	service "github.com/kubev2v/rvtools-analyser/internal/services"
	"github.com/kubev2v/rvtools-analyser/internal/store"
	"github.com/kubev2v/rvtools-analyser/internal/store/migrations"
)

const apiV2 = "/api/v2"

func NewRunCommand(cfg *config.Configuration) *cobra.Command {
	runCmd := &cobra.Command{
		Use: "run", Short: "Run the RVTools agent", Args: cobra.NoArgs,
		Example: "agent run --data-folder ./data --opa-policies-folder ./data/policies",
		PreRunE: func(_ *cobra.Command, _ []string) error { return validateConfiguration(cfg) },
		RunE: func(_ *cobra.Command, _ []string) error {
			zap.S().Infow("using configuration", "agent", helpers.Flatten(cfg.Agent.DebugMap()), "server", helpers.Flatten(cfg.Server.DebugMap()))
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGQUIT)
			defer cancel()
			srv, cleanup, err := initV2(cfg)
			if err != nil {
				return err
			}
			defer cleanup()
			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				zap.S().Infof("Starting HTTP server on %s port %d", cfg.Server.ListenAddress, cfg.Server.HTTPPort)
				if err := srv.Start(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
					zap.S().Errorw("failed to start HTTP server", "error", err)
					cancel()
				}
			}()
			go func() {
				<-ctx.Done()
				stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer stopCancel()
				srv.Stop(stopCtx)
			}()
			<-ctx.Done()
			wg.Wait()
			return nil
		},
	}
	registerFlags(runCmd, cfg)
	cobraflags.CobraOnInitialize("AGENT", runCmd)
	return runCmd
}

func initV2(cfg *config.Configuration) (*server.Server, func(), error) {
	opaValidator, err := opa.NewValidatorFromDir(cfg.Agent.OpaPoliciesFolder)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize OPA validator: %w", err)
	}
	pool, err := initPool(cfg)
	if err != nil {
		return nil, nil, err
	}
	mgr := service.NewServiceManager(service.WithConfig(cfg), service.WithPool(pool), service.WithOpaValidator(opaValidator))
	if err := mgr.Initialize(); err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("failed to initialize service: %w", err)
	}
	if v, ok := binding.Validator.Engine().(*validator.Validate); ok {
		handlers.RegisterValidators(v)
	}
	handler := v2Handlers.NewHandler(*cfg, mgr)
	swagger, err := v2.GetSwagger()
	if err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("failed to load v2 swagger spec: %w", err)
	}
	srv, err := server.NewServer(cfg, map[string]server.APIGroup{
		apiV2: {Swagger: swagger, RegisterFn: func(router *gin.RouterGroup) { v2.RegisterHandlers(router, handler) }},
	})
	if err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("failed to create HTTP server: %w", err)
	}
	cleanup := func() { mgr.Stop(context.Background()); pool.Close() }
	return srv, cleanup, nil
}

func initPool(cfg *config.Configuration) (*store.Pool, error) {
	pool := store.NewPool(5 * time.Minute)

	agentPath := filepath.Join(cfg.Agent.DataFolder, "agent.duckdb")
	mainDB, err := pool.NewDatabase(store.MainDatabaseID, agentPath, time.Now(), store.EagerConnectionInitilization, 256, store.ReadWriteDatabase)
	if err != nil {
		return nil, fmt.Errorf("failed to create main database: %w", err)
	}

	if err := mainDB.Migrate(context.Background(), migrations.RunMain); err != nil {
		return nil, fmt.Errorf("failed to migrate main database: %w", err)
	}

	pool.Add(mainDB)
	zap.S().Infow("registered main database", "path", mainDB.Path)

	if err := cleanupStaleCollections(mainDB, cfg.Agent.DataFolder); err != nil {
		zap.S().Errorw("failed to cleanup stale collections", "error", err)
	}

	matches, _ := filepath.Glob(filepath.Join(cfg.Agent.DataFolder, "collection_*.duckdb"))
	sort.Strings(matches)
	for _, match := range matches {
		name := strings.TrimSuffix(filepath.Base(match), ".duckdb")
		tsStr := strings.TrimPrefix(name, "collection_")
		ts, err := strconv.ParseInt(tsStr, 10, 64)
		if err != nil {
			zap.S().Warnw("skipping collection with unparseable timestamp", "file", match)
			continue
		}

		createdAt := time.Unix(ts, 0)
		if len(tsStr) > 10 {
			createdAt = time.Unix(0, ts)
		}
		hash := sha256.Sum256([]byte(match))
		id := hex.EncodeToString(hash[:])[:6]
		db, err := pool.NewDatabase(id, match, createdAt, store.LazyConnectionInitilization, 512, store.ReadWriteDatabase)
		if err != nil {
			zap.S().Warnw("skipping collection database", "file", match, "error", err)
			continue
		}

		if err := db.Migrate(context.Background(), func(ctx context.Context, db *sql.DB) error {
			return migrations.RunCollection(ctx, db, name)
		}); err != nil {
			zap.S().Errorw("failed to migrate collection database", "db_name", name, "error", err)
			_ = db.Close()
			continue
		}

		if err := db.Close(); err != nil {
			zap.S().Errorw("closing database", "db", db.ID, "error", err)
			continue
		}

		pool.Add(db)
		zap.S().Infow("registered collection database", "name", name, "path", match)
	}

	return pool, nil
}

func registerFlags(cmd *cobra.Command, cfg *config.Configuration) {
	nfs := cobrautil.NewNamedFlagSets(cmd)
	serverFlags := nfs.FlagSet(color.New(color.FgBlue, color.Bold).Sprint("Server"))
	serverFlags.IntVar(&cfg.Server.HTTPPort, "server-http-port", cfg.Server.HTTPPort, "HTTP port")
	serverFlags.StringVar(&cfg.Server.ListenAddress, "server-listen-address", cfg.Server.ListenAddress, "IP address to listen on")
	serverFlags.StringVar(&cfg.Server.StaticsFolder, "server-statics-folder", cfg.Server.StaticsFolder, "Static UI directory")
	serverFlags.StringVar(&cfg.Server.ServerMode, "server-mode", cfg.Server.ServerMode, "Server mode: prod or dev")
	agentFlags := nfs.FlagSet(color.New(color.FgBlue, color.Bold).Sprint("Agent"))
	agentFlags.StringVar(&cfg.Agent.DataFolder, "data-folder", cfg.Agent.DataFolder, "Persistent data directory")
	agentFlags.StringVar(&cfg.Agent.OpaPoliciesFolder, "opa-policies-folder", cfg.Agent.OpaPoliciesFolder, "OPA policies directory")
	nfs.AddFlagSets(cmd)
}

func validateConfiguration(cfg *config.Configuration) error {
	if cfg.Agent.DataFolder == "" {
		return errors.New("data folder must be set")
	}
	if cfg.Agent.OpaPoliciesFolder == "" {
		return errors.New("OPA policies folder must be set")
	}
	if cfg.Server.HTTPPort < 1 || cfg.Server.HTTPPort > 65535 {
		return fmt.Errorf("invalid http-port %d: must be between 1 and 65535", cfg.Server.HTTPPort)
	}
	if _, err := netip.ParseAddr(cfg.Server.ListenAddress); err != nil {
		return fmt.Errorf("invalid server-listen-address %q: must be an IP address", cfg.Server.ListenAddress)
	}
	switch config.ServerModeType(cfg.Server.ServerMode) {
	case config.ServerModeProd:
		if cfg.Server.StaticsFolder == "" {
			return errors.New("statics folder must be set when server mode is production")
		}
	case config.ServerModeDev:
	default:
		return fmt.Errorf("invalid server mode %q: must be %q or %q", cfg.Server.ServerMode, config.ServerModeProd, config.ServerModeDev)
	}
	if cfg.Agent.ID == "" {
		cfg.Agent.ID = uuid.NewString()
	}
	return nil
}

// cleanupStaleCollections removes leftover collection markers and their DuckDB
// files from disk. Any row in the collections table at startup is stale,
// successful collections delete their marker during finalize.
// Running collections are also treated as stale because the agent crashed
// before the finalization step.
func cleanupStaleCollections(mainDB *store.Database, dataFolder string) error {
	st, err := mainDB.Store()
	if err != nil {
		return fmt.Errorf("failed to get main store: %w", err)
	}

	collections, err := st.Collection().List(context.Background())
	if err != nil {
		return fmt.Errorf("failed to list stale collections: %w", err)
	}

	for _, col := range collections {
		dbFile := filepath.Join(dataFolder, col.Database+".duckdb")
		if err := os.Remove(dbFile); err != nil && !os.IsNotExist(err) {
			zap.S().Warnw("failed to remove stale collection file", "file", dbFile, "error", err)
			continue
		}

		if err := st.Collection().Delete(context.Background(), col.Database); err != nil {
			zap.S().Warnw("failed to delete stale collection marker", "database", col.Database, "error", err)
			continue
		}

		zap.S().Infow("cleaned up stale collection", "database", col.Database, "state", col.State)
	}

	return nil
}
