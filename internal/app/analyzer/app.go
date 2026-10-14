package analyzer

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
	"github.com/kubev2v/migration-planner/pkg/opa"
	"go.uber.org/zap"

	v1 "github.com/kubev2v/assisted-migration-agent/api/analyzer/v1"
	"github.com/kubev2v/assisted-migration-agent/internal/bootstrap"
	"github.com/kubev2v/assisted-migration-agent/internal/config"
	"github.com/kubev2v/assisted-migration-agent/internal/handlers"
	analyzerHandlers "github.com/kubev2v/assisted-migration-agent/internal/handlers/analyzer"
	"github.com/kubev2v/assisted-migration-agent/internal/server"
	"github.com/kubev2v/assisted-migration-agent/internal/services"
)

const apiV1 = "/api/v1"

func Run(ctx context.Context, cfg *config.AnalyzerConfiguration) error {
	srv, cleanup, err := bootstrapAnalyzer(cfg)
	if err != nil {
		return err
	}
	defer cleanup()

	go func() {
		<-ctx.Done()
		stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		srv.Stop(stopCtx)
	}()

	zap.S().Infof("Starting RVTools Analyzer on port %d", cfg.Server.HTTPPort)
	err = srv.Start(ctx)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("failed to start analyzer server: %w", err)
	}
	zap.S().Info("analyzer server shutdown")
	return nil
}

func bootstrapAnalyzer(cfg *config.AnalyzerConfiguration) (*server.Server, func(), error) {
	if err := os.MkdirAll(cfg.DataFolder, 0750); err != nil {
		return nil, nil, fmt.Errorf("create data folder: %w", err)
	}

	opaValidator, err := opa.NewValidatorFromDir(cfg.OpaPoliciesFolder)
	if err != nil {
		return nil, nil, fmt.Errorf("initialize OPA validator: %w", err)
	}
	pool, err := bootstrap.InitPool(cfg.DataFolder)
	if err != nil {
		return nil, nil, err
	}
	cleanupPool := func() { pool.Close() }

	svc := services.NewServiceManager(
		services.WithPool(pool),
		services.WithDataFolder(cfg.DataFolder),
		services.WithOpaValidator(opaValidator),
	)
	if err := svc.InitializeAnalyzer(); err != nil {
		cleanupPool()
		return nil, nil, err
	}
	if v, ok := binding.Validator.Engine().(*validator.Validate); ok {
		handlers.RegisterValidators(v)
	}

	swagger, err := v1.GetSwagger()
	if err != nil {
		cleanupPool()
		return nil, nil, fmt.Errorf("load analyzer API spec: %w", err)
	}
	h := analyzerHandlers.NewHandler(*cfg, svc)
	srv, err := server.NewServerWithConfig(cfg.Server, map[string]server.APIGroup{
		apiV1: {
			Swagger: swagger,
			RegisterFn: func(router *gin.RouterGroup) {
				v1.RegisterHandlers(router, h)
			},
		},
	})
	if err != nil {
		cleanupPool()
		return nil, nil, fmt.Errorf("create analyzer server: %w", err)
	}
	cleanup := func() {
		svc.Stop(context.Background())
		cleanupPool()
	}
	return srv, cleanup, nil
}
