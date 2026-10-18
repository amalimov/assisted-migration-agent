package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ecordell/optgen/helpers"
	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
	"github.com/kubev2v/migration-planner/pkg/opa"
	"go.uber.org/zap"

	v2 "github.com/kubev2v/assisted-migration-agent/agent/api/v2"
	v2Handlers "github.com/kubev2v/assisted-migration-agent/agent/handlers/v2"
	"github.com/kubev2v/assisted-migration-agent/internal/config"
	"github.com/kubev2v/assisted-migration-agent/internal/handlers"
	"github.com/kubev2v/assisted-migration-agent/internal/server"
	service "github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	"github.com/kubev2v/assisted-migration-agent/pkg/console"
	"github.com/kubev2v/assisted-migration-agent/pkg/crypto"
)

const (
	apiV2 string = "/api/v2"
)

func Run(cfg *config.Configuration) error {
	zap.S().Infow("using configuration",
		"agent", helpers.Flatten(cfg.Agent.DebugMap()),
		"server", helpers.Flatten(cfg.Server.DebugMap()),
		"console", helpers.Flatten(cfg.Console.DebugMap()),
		"auth", helpers.Flatten(cfg.Auth.DebugMap()),
	)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGQUIT)
	wg := sync.WaitGroup{}
	wg.Add(1)

	srv, cleanup, err := initV2(cfg)
	if err != nil {
		return err
	}
	defer cleanup()

	go func() {
		defer func() {
			wg.Done()
			cancel()
		}()
		zap.S().Infof("Starting HTTP server on port %d", cfg.Server.HTTPPort)

		if err := srv.Start(ctx); err != nil {
			if !errors.Is(err, http.ErrServerClosed) {
				zap.S().Fatalw("failed to start http server", "error", err)
			}
		}
	}()

	go func() {
		<-ctx.Done()
		stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		srv.Stop(stopCtx)
	}()

	<-ctx.Done()
	wg.Wait()

	zap.S().Info("server shutdown")

	return nil
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

	consoleClient, km, err := initShared(cfg)
	if err != nil {
		return nil, nil, err
	}

	v2SvcMgr := service.NewServiceManager(
		service.WithConfig(cfg),
		service.WithPool(pool),
		service.WithConsoleClient(consoleClient),
		service.WithKeyManager(km),
		service.WithOpaValidator(opaValidator),
	)
	if err := v2SvcMgr.Initialize(); err != nil {
		return nil, nil, fmt.Errorf("failed to initialize v2 service: %w", err)
	}

	if v, ok := binding.Validator.Engine().(*validator.Validate); ok {
		handlers.RegisterValidators(v)
	}

	var v2H v2.ServerInterface
	if cfg.Agent.RVToolsMode {
		zap.S().Info("rvtools mode enabled: credentials, inspector, and vCenter collector endpoints are disabled")
		v2H = v2Handlers.NewRVToolsHandler(*cfg, v2SvcMgr)
	} else {
		v2H = v2Handlers.NewHandler(*cfg, v2SvcMgr)
	}

	swagger, err := v2.GetSwagger()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load v2 swagger spec: %w", err)
	}

	srv, err := server.NewServer(cfg, map[string]server.APIGroup{
		apiV2: {
			Swagger: swagger,
			RegisterFn: func(router *gin.RouterGroup) {
				v2.RegisterHandlers(router, v2H)
			},
		},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create http server: %w", err)
	}

	cleanup := func() {
		v2SvcMgr.Stop(context.Background())
		pool.Close()
		zap.S().Info("v2 service and pool closed")
	}

	return srv, cleanup, nil
}

func initShared(cfg *config.Configuration) (*console.Client, *crypto.KeyManager, error) {
	jwt := ""
	if cfg.Auth.Enabled {
		data, err := os.ReadFile(cfg.Auth.JWTFilePath)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read agent's jwt: %w", err)
		}
		if len(data) == 0 {
			return nil, nil, errors.New("failed to read agent's jwt. the JWT is empty")
		}
		jwt = strings.TrimSpace(string(data))
	}

	consoleClient, err := console.NewConsoleClient(cfg.Console.URL, jwt)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create console client: %w", err)
	}

	keyManager, err := crypto.NewKeyManager(cfg.Agent.DataFolder)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize key manager: %w", err)
	}

	return consoleClient, keyManager, nil
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
	for _, match := range matches {
		name := strings.TrimSuffix(filepath.Base(match), ".duckdb")
		tsStr := strings.TrimPrefix(name, "collection_")
		ts, err := strconv.ParseInt(tsStr, 10, 64)
		if err != nil {
			zap.S().Warnw("skipping collection with unparseable timestamp", "file", match)
			continue
		}

		createdAt := time.Unix(ts, 0)
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
